"""Model providers via Pydantic AI.

One adapter covers Claude (Anthropic API), OpenAI and every OpenAI-compatible
server (Gemini, OpenRouter, Ollama, LM Studio, ...). Pydantic AI runs the tool
loop, validates structured output and retries invalid answers, connects MCP
servers, and streams progress. Cubicle's own tools are provider-neutral and are
converted here.

Web access: Claude gets Anthropic's server-side web search and fetch, OpenAI its
native web search; other servers get a DuckDuckGo search plus Cubicle's guarded
web_fetch tool.
"""

from __future__ import annotations

import logging
import time
import uuid
from contextlib import AsyncExitStack
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from cubicle_agents.core.settings import KIND_ANTHROPIC
from cubicle_agents.llm.base import (
    MAX_TOOL_ROUNDS,
    STEP_DONE,
    STEP_ERROR,
    STEP_NOTE,
    STEP_THINKING,
    STEP_TOOL,
    AgentResult,
    EventCallback,
    LLMConfigError,
    LLMRefusalError,
    Step,
    StepCallback,
    Turn,
    describe_tool,
)
from cubicle_agents.office.steps import as_text, tidy_json
from cubicle_agents.staff.roster import McpServer
from cubicle_agents.tools import mcp as mcp_tools
from cubicle_agents.tools.base import Toolbox

log = logging.getLogger(__name__)

PUSH_INTERVAL_S = 0.35
_EFFORTS = ("low", "medium", "high", "xhigh", "max")


@dataclass(frozen=True)
class _Question:
    question: str


@dataclass(frozen=True)
class _Approval:
    action: str
    details: str = ""


def _request_approval(action: str, details: str = "") -> _Approval:
    """Ask the owner to approve ONE specific action before you do it. Required before anything with consequences
    outside your workspace (sending, publishing, deleting, spending, changing accounts). This ends your turn."""
    return _Approval(action.strip(), details.strip())


def _ask_owner(question: str) -> _Question:
    """Ask the owner one clear question when you cannot do a good job without information only they have.
    Don't ask for things you can reasonably assume or look up. This ends your turn."""
    return _Question(question.strip())


class PydanticLLM:
    supports_tools = True

    def __init__(self, *, kind: str, provider: str, api_key: str = "", base_url: str = "", effort: str = "medium",
                 max_output_tokens: int = 16000, web_access: bool = True, model_factory: Any = None) -> None:
        self.name = provider
        self._kind = kind
        self._provider = provider
        self._api_key = api_key
        self._base_url = base_url
        self._effort = effort if effort in _EFFORTS else "medium"
        self._max_tokens = max_output_tokens
        self._web = web_access
        # Tests inject a factory returning Pydantic AI test models.
        self._model_factory = model_factory or self._build_model
        self._client: Any = None

    # ---- models ----

    @property
    def _is_anthropic(self) -> bool:
        return self._kind == KIND_ANTHROPIC

    @property
    def _is_openai(self) -> bool:
        return self._provider == "openai"

    def _sdk_client(self) -> Any:
        if self._client is None:
            # Extra retries ride out rate limits and transient overloads on long tasks.
            if self._is_anthropic:
                import anthropic

                self._client = anthropic.AsyncAnthropic(api_key=self._api_key or None, max_retries=4)
            else:
                import openai

                # Local servers such as Ollama ignore the key, but the SDK requires one.
                self._client = openai.AsyncOpenAI(base_url=self._base_url or None, api_key=self._api_key or "not-needed",
                                                  max_retries=4)
        return self._client

    def _build_model(self, name: str) -> Any:
        if self._is_anthropic:
            from pydantic_ai.models.anthropic import AnthropicModel
            from pydantic_ai.providers.anthropic import AnthropicProvider

            return AnthropicModel(name, provider=AnthropicProvider(anthropic_client=self._sdk_client()))
        from pydantic_ai.providers.openai import OpenAIProvider

        provider = OpenAIProvider(openai_client=self._sdk_client())
        if self._is_openai:
            from pydantic_ai.models.openai import OpenAIResponsesModel

            return OpenAIResponsesModel(name, provider=provider)
        from pydantic_ai.models.openai import OpenAIChatModel

        return OpenAIChatModel(name, provider=provider)

    def _capabilities(self, *, effort: str, web: bool) -> list[Any]:
        from pydantic_ai.capabilities import Caching, Thinking, WebFetch, WebSearch

        caps: list[Any] = []
        if self._is_anthropic:
            caps += [Thinking(), Caching()]
        elif self._is_openai:
            caps.append(Thinking(effort={"xhigh": "high", "max": "high"}.get(effort, effort)))  # type: ignore[arg-type]
        if web:
            if self._is_anthropic:
                caps += [WebSearch(), WebFetch()]
            elif self._is_openai:
                caps.append(WebSearch())
            else:
                caps.append(WebSearch(native=False, local="duckduckgo"))
        return caps

    def _settings(self, effort: str) -> dict[str, Any]:
        settings: dict[str, Any] = {"max_tokens": self._max_tokens}
        if self._is_anthropic:
            settings["anthropic_effort"] = effort
        return settings

    # ---- running ----

    async def run(self, *, system: str, prompt: str, model: str, toolbox: Toolbox | None = None,
                  json_schema: dict[str, Any] | None = None, workdir: Path | None = None,
                  history: list[Turn] | None = None, mcp: tuple[McpServer, ...] = (), effort: str = "",
                  on_event: EventCallback | None = None, web: bool = True,
                  on_step: StepCallback | None = None) -> AgentResult:
        from pydantic_ai import Agent, PromptedOutput, StructuredDict, ToolOutput, UsageLimits
        from pydantic_ai.exceptions import ModelHTTPError, UnexpectedModelBehavior, UsageLimitExceeded
        from pydantic_ai.usage import RunUsage

        if not model:
            raise LLMConfigError("Choose a model for this provider in Settings.")
        effort = effort if effort in _EFFORTS else self._effort
        structured = json_schema is not None
        output: Any = str
        tools: list[Any] = []
        if structured:
            output = StructuredDict(json_schema, name="plan", description="The plan for this assignment.")
            if not (self._is_anthropic or self._is_openai):
                # Many compatible servers lack tool-based or native structured output; a prompted
                # JSON answer is validated and retried just the same.
                output = PromptedOutput(output)
        elif toolbox is not None:
            tools = [_convert(toolbox, t) for t in toolbox.tools if t.name not in ("ask_owner", "request_approval")]
            outputs: list[Any] = [str]
            if toolbox.get("ask_owner") is not None:
                outputs.append(ToolOutput(_ask_owner, name="ask_owner"))
            if toolbox.get("request_approval") is not None:
                outputs.append(ToolOutput(_request_approval, name="request_approval"))
            if len(outputs) > 1:
                output = outputs

        usage = RunUsage()
        result = AgentResult(text="")
        async with AsyncExitStack() as stack:
            toolsets: list[Any] = []
            instructions = system
            if mcp and not structured:
                toolsets, result.warnings = await mcp_tools.open_toolsets(mcp, stack, workdir)
                if result.warnings:
                    instructions += "\n\nUnavailable right now (don't try to use them): " + " ".join(result.warnings)
            agent = Agent(
                self._model_factory(model),
                instructions=instructions,
                output_type=output,
                tools=tools,
                toolsets=toolsets,
                capabilities=[] if structured else self._capabilities(effort=effort, web=web and self._web),
                model_settings=self._settings(effort),  # type: ignore[arg-type]
                retries=2,
            )
            streamer = _Streamer(on_event, on_step) if on_event or on_step else None
            try:
                run = await agent.run(
                    prompt,
                    message_history=_history(history),
                    usage=usage,
                    usage_limits=UsageLimits(request_limit=MAX_TOOL_ROUNDS),
                    event_stream_handler=streamer.handle if streamer else None,
                )
            except UsageLimitExceeded:
                result.text = "I ran out of steps before finishing. Partial work may be in the workspace files."
                return _with_usage(result, usage)
            except UnexpectedModelBehavior:
                if structured:  # an unusable plan: the caller falls back to a simple plan
                    return _with_usage(result, usage)
                raise
            except ModelHTTPError as exc:
                if exc.status_code in (401, 403):
                    raise LLMConfigError("The provider rejected the API key. Check it in Settings.") from exc
                raise

        _with_usage(result, usage)
        out = run.output
        if isinstance(out, _Question):
            result.question = out.question
        elif isinstance(out, _Approval):
            result.question = f"**{out.action}**" + (f"\n\n{out.details[:3000]}" if out.details else "")
            result.question_kind = "approval"
        elif structured:
            import json

            result.text = json.dumps(out)
        else:
            result.text = str(out or "").strip()
            if not result.text and run.response.finish_reason == "content_filter":
                raise LLMRefusalError("The model declined this request.")
            if run.response.finish_reason == "length":
                result.text += "\n\n[Output truncated: reached the output length limit.]"
        return result

    async def list_models(self) -> list[str]:
        client = self._sdk_client()
        if self._is_anthropic:
            import anthropic

            try:
                return [m.id async for m in client.models.list()]
            except anthropic.AuthenticationError as exc:
                raise LLMConfigError("The Anthropic API key was rejected.") from exc
            except anthropic.APIConnectionError as exc:
                raise LLMConfigError("Could not reach the Anthropic API.") from exc
        import openai

        try:
            return sorted([m.id async for m in client.models.list()])
        except openai.AuthenticationError as exc:
            raise LLMConfigError("The API key was rejected.") from exc
        except openai.APIConnectionError as exc:
            raise LLMConfigError("Could not reach the provider. Check the base URL and that the server is running.") from exc


def _convert(toolbox: Toolbox, tool: Any) -> Any:
    """Wrap a Cubicle tool for Pydantic AI. Errors go back to the model as text, so it can adapt."""
    from pydantic_ai import Tool

    async def call(**kwargs: Any) -> str:
        outcome = await toolbox.call(tool.name, kwargs)
        return f"ERROR: {outcome.content}" if outcome.is_error else outcome.content

    return Tool.from_schema(call, name=tool.name, description=tool.description, json_schema=tool.parameters)


def _history(turns: list[Turn] | None) -> list[Any] | None:
    """Conversation turns as model messages, merging consecutive turns by the same side."""
    if not turns:
        return None
    from pydantic_ai import ModelRequest, ModelResponse, TextPart, UserPromptPart

    merged: list[Turn] = []
    for t in turns:
        if not t.content.strip():
            continue
        if merged and merged[-1].role == t.role:
            merged[-1] = Turn(t.role, merged[-1].content + "\n\n" + t.content)
        else:
            merged.append(t)
    while merged and merged[0].role != "owner":  # a conversation starts with the owner
        merged.pop(0)
    return [ModelRequest(parts=[UserPromptPart(t.content)]) if t.role == "owner"
            else ModelResponse(parts=[TextPart(t.content)]) for t in merged] or None


def _with_usage(result: AgentResult, usage: Any) -> AgentResult:
    result.input_tokens = int(usage.input_tokens or 0)
    result.output_tokens = int(usage.output_tokens or 0)
    return result


class _Streamer:
    """Turns Pydantic AI stream events into throttled progress updates and steps (thoughts, remarks, tool calls)."""

    def __init__(self, on_event: EventCallback | None, on_step: StepCallback | None = None) -> None:
        self._on_event = on_event
        self._on_step = on_step
        self._text = ""
        self._last_push = 0.0
        self._counter = 0
        self._thinking: dict[int, Step] = {}  # open thinking parts, by part index
        self._tools: dict[str, Step] = {}  # tool calls waiting for their result, by call id

    async def handle(self, _ctx: Any, stream: Any) -> None:
        from pydantic_ai import (
            FunctionToolCallEvent,
            FunctionToolResultEvent,
            NativeToolCallPart,
            NativeToolReturnPart,
            PartDeltaEvent,
            PartEndEvent,
            PartStartEvent,
            RetryPromptPart,
            TextPart,
            TextPartDelta,
            ThinkingPart,
            ThinkingPartDelta,
        )

        async for event in stream:
            if isinstance(event, PartStartEvent):
                part = event.part
                if isinstance(part, TextPart):
                    self._text += part.content
                    await self._push_text()
                elif isinstance(part, ThinkingPart):
                    step = Step(self._next_id("think"), STEP_THINKING, output=part.content)
                    self._thinking[event.index] = step
                    await self._step(step)
                elif isinstance(part, NativeToolCallPart):
                    await self._flush_note()
                    await self._emit("status", describe_tool(part.tool_name))
                    await self._start_tool(part.tool_call_id, part.tool_name, part.args)
                elif isinstance(part, NativeToolReturnPart):
                    await self._finish_tool(part.tool_call_id, part.content, part.outcome != "success")
            elif isinstance(event, PartDeltaEvent):
                if isinstance(event.delta, TextPartDelta):
                    self._text += event.delta.content_delta
                    await self._push_text()
                elif isinstance(event.delta, ThinkingPartDelta) and event.index in self._thinking:
                    step = self._thinking[event.index]
                    step.output += event.delta.content_delta or ""
                    await self._step(step)
            elif isinstance(event, PartEndEvent) and isinstance(event.part, ThinkingPart):
                step = self._thinking.pop(event.index, None)
                if step is not None:
                    step.output = event.part.content or step.output
                    step.status = STEP_DONE
                    await self._step(step)
            elif isinstance(event, FunctionToolCallEvent):
                await self._flush_note()  # text before a tool call is the agent saying what it is about to do
                await self._emit("status", describe_tool(event.part.tool_name))
                await self._start_tool(event.part.tool_call_id, event.part.tool_name, event.part.args)
            elif isinstance(event, FunctionToolResultEvent):
                part = event.part
                failed = isinstance(part, RetryPromptPart) or getattr(part, "outcome", "success") != "success"
                await self._finish_tool(part.tool_call_id, part.content, failed)

    def _next_id(self, prefix: str) -> str:
        self._counter += 1
        return f"{prefix}-{uuid.uuid4().hex[:10]}-{self._counter}"

    async def _flush_note(self) -> None:
        text = self._text.strip()
        self._text = ""
        if not text:
            return
        await self._emit("text", "")
        await self._step(Step(self._next_id("note"), STEP_NOTE, output=text, status=STEP_DONE))

    async def _start_tool(self, call_id: str, name: str, args: Any) -> None:
        step = Step(call_id or self._next_id("tool"), STEP_TOOL, name=name, input=_args_text(args))
        self._tools[step.id] = step
        await self._step(step)

    async def _finish_tool(self, call_id: str, content: Any, failed: bool) -> None:
        step = self._tools.pop(call_id, None)
        if step is None:  # a result without a seen call (e.g. a server-side tool): still worth showing
            step = Step(call_id or self._next_id("tool"), STEP_TOOL, name="tool")
        step.output = as_text(content)
        step.status = STEP_ERROR if failed or step.output.startswith("ERROR: ") else STEP_DONE
        await self._step(step)

    async def _push_text(self) -> None:
        now = time.monotonic()
        if now - self._last_push >= PUSH_INTERVAL_S and self._text.strip():
            self._last_push = now
            await self._emit("text", self._text)

    async def _emit(self, kind: str, text: str) -> None:
        if self._on_event is None:
            return
        try:
            await self._on_event(kind, text)
        except Exception:  # progress is cosmetic; never let it break the work
            log.debug("progress update failed", exc_info=True)

    async def _step(self, step: Step) -> None:
        if self._on_step is None:
            return
        try:
            await self._on_step(Step(step.id, step.kind, step.name, step.input, step.output, step.status))
        except Exception:
            log.debug("step update failed", exc_info=True)


def _args_text(args: Any) -> str:
    return tidy_json(args) if isinstance(args, str) else as_text(args)

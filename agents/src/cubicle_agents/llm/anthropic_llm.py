"""Claude via the official Anthropic SDK, with a client-side tool loop.

Besides Cubicle's own tools, Claude gets Anthropic's server-side web search and
web fetch tools when web access is enabled; those run on Anthropic's side and
need no code here other than handling `pause_turn`.
"""

from __future__ import annotations

from pathlib import Path
from typing import Any

import anthropic

from cubicle_agents.llm.base import MAX_TOOL_ROUNDS, AgentResult, LLMConfigError, LLMRefusalError
from cubicle_agents.tools.base import Toolbox

# Server-side refusal fallback: a declined request is retried on a suitable
# fallback model instead of returning nothing.
_FALLBACK_BETA = "server-side-fallback-2026-07-01"
_WEB_TOOLS = [
    {"type": "web_search_20260209", "name": "web_search"},
    {"type": "web_fetch_20260209", "name": "web_fetch"},
]


class AnthropicLLM:
    name = "anthropic"
    supports_tools = True

    def __init__(self, *, api_key: str = "", effort: str = "medium", max_output_tokens: int = 16000,
                 web_access: bool = True, client: Any = None) -> None:
        # Extra retries ride out rate limits and transient overloads on long tasks.
        self._client = client or anthropic.AsyncAnthropic(api_key=api_key or None, max_retries=4)
        self._effort = effort
        self._max_tokens = max_output_tokens
        self._web = web_access

    async def run(self, *, system: str, prompt: str, model: str, toolbox: Toolbox | None = None,
                  json_schema: dict[str, Any] | None = None, workdir: Path | None = None) -> AgentResult:
        output_config: dict[str, Any] = {"effort": self._effort}
        tools: list[dict[str, Any]] = []
        if json_schema is not None:
            output_config["format"] = {"type": "json_schema", "schema": json_schema}
        else:
            if toolbox:
                tools += [{"name": t.name, "description": t.description, "input_schema": t.parameters}
                          for t in toolbox.tools]
            if self._web:
                tools += _WEB_TOOLS

        messages: list[dict[str, Any]] = [{"role": "user", "content": prompt}]
        result = AgentResult(text="")
        response = None
        for _ in range(MAX_TOOL_ROUNDS):
            kwargs: dict[str, Any] = {"tools": tools} if tools else {}
            response = await self._client.beta.messages.create(
                model=model,
                max_tokens=self._max_tokens,
                system=system,
                messages=messages,
                thinking={"type": "adaptive"},
                output_config=output_config,
                # Cache the growing conversation prefix: each tool round re-reads it at a fraction of the cost.
                cache_control={"type": "ephemeral"},
                betas=[_FALLBACK_BETA],
                fallbacks="default",
                **kwargs,
            )
            result.input_tokens += response.usage.input_tokens
            result.output_tokens += response.usage.output_tokens
            if response.stop_reason == "refusal":
                category = response.stop_details.category if response.stop_details else None
                raise LLMRefusalError(f"The model declined this task (category: {category}).")

            # Append the full content (thinking, server tool blocks) so the conversation stays valid.
            messages.append({"role": "assistant", "content": response.content})
            if response.stop_reason == "pause_turn":
                continue  # a long server-side tool call; let Claude carry on
            if response.stop_reason != "tool_use" or toolbox is None:
                break

            results = []
            for block in response.content:
                if block.type == "tool_use":
                    outcome = await toolbox.call(block.name, block.input)
                    results.append({"type": "tool_result", "tool_use_id": block.id,
                                    "content": outcome.content, "is_error": outcome.is_error})
            messages.append({"role": "user", "content": results})
            if toolbox.question:
                result.question = toolbox.question
                return result

        if response is not None:
            result.text = "".join(b.text for b in response.content if b.type == "text").strip()
            if response.stop_reason == "max_tokens":
                result.text += "\n\n[Output truncated: reached the max_tokens limit.]"
        return result

    async def list_models(self) -> list[str]:
        try:
            return [m.id async for m in self._client.models.list()]
        except anthropic.AuthenticationError as exc:
            raise LLMConfigError("The Anthropic API key was rejected.") from exc
        except anthropic.APIConnectionError as exc:
            raise LLMConfigError("Could not reach the Anthropic API.") from exc

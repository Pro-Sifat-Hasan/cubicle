"""OpenAI and OpenAI-compatible APIs via the official OpenAI SDK.

Covers OpenAI, Google Gemini (compatibility endpoint), OpenRouter, Ollama and
any custom Chat Completions server. Function calling drives the tool loop.
"""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import openai

from cubicle_agents.llm.base import MAX_TOOL_ROUNDS, AgentResult, LLMConfigError
from cubicle_agents.tools.base import Toolbox


class OpenAICompatibleLLM:
    supports_tools = True

    def __init__(self, *, name: str, base_url: str, api_key: str = "", client: Any = None) -> None:
        self.name = name
        # Local servers such as Ollama ignore the key, but the SDK requires one.
        self._client = client or openai.AsyncOpenAI(base_url=base_url or None, api_key=api_key or "not-needed")

    async def run(self, *, system: str, prompt: str, model: str, toolbox: Toolbox | None = None,
                  json_schema: dict[str, Any] | None = None, workdir: Path | None = None) -> AgentResult:
        if not model:
            raise LLMConfigError("Choose a model for this provider in Settings.")
        messages: list[dict[str, Any]] = [
            {"role": "system", "content": system},
            {"role": "user", "content": prompt},
        ]
        tools = [
            {"type": "function", "function": {"name": t.name, "description": t.description, "parameters": t.parameters}}
            for t in (toolbox.tools if toolbox and json_schema is None else [])
        ]
        result = AgentResult(text="")
        for _ in range(MAX_TOOL_ROUNDS):
            response = await self._create(model, messages, tools, json_schema)
            if response.usage:
                result.input_tokens += response.usage.prompt_tokens or 0
                result.output_tokens += response.usage.completion_tokens or 0
            message = response.choices[0].message
            calls = message.tool_calls or []
            if not calls or toolbox is None:
                result.text = (message.content or "").strip()
                return result

            messages.append({
                "role": "assistant",
                "content": message.content or "",
                "tool_calls": [
                    {"id": c.id, "type": "function",
                     "function": {"name": c.function.name, "arguments": c.function.arguments}}
                    for c in calls
                ],
            })
            for call in calls:
                try:
                    args = json.loads(call.function.arguments or "{}")
                except json.JSONDecodeError:
                    args = None
                outcome = await toolbox.call(call.function.name, args)
                content = f"ERROR: {outcome.content}" if outcome.is_error else outcome.content
                messages.append({"role": "tool", "tool_call_id": call.id, "content": content})
            if toolbox.question:
                result.question = toolbox.question
                return result
        result.text = "I ran out of tool steps before finishing. Partial work may be in the workspace."
        return result

    async def _create(self, model: str, messages: list[dict[str, Any]], tools: list[dict[str, Any]],
                      json_schema: dict[str, Any] | None) -> Any:
        kwargs: dict[str, Any] = {"model": model, "messages": messages}
        if tools:
            kwargs["tools"] = tools
        if json_schema is not None:
            kwargs["response_format"] = {
                "type": "json_schema",
                "json_schema": {"name": "plan", "schema": json_schema, "strict": True},
            }
        try:
            return await self._client.chat.completions.create(**kwargs)
        except openai.BadRequestError:
            if "response_format" not in kwargs:
                raise
            # Some compatible servers lack structured outputs; the prompt still asks for JSON.
            kwargs.pop("response_format")
            return await self._client.chat.completions.create(**kwargs)

    async def list_models(self) -> list[str]:
        try:
            return sorted([m.id async for m in self._client.models.list()])
        except openai.AuthenticationError as exc:
            raise LLMConfigError("The API key was rejected.") from exc
        except openai.APIConnectionError as exc:
            raise LLMConfigError("Could not reach the provider. Check the base URL and that the server is running.") from exc

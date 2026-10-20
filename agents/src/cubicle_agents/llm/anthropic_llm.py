"""Claude via the official Anthropic SDK.

Credentials come from ANTHROPIC_API_KEY (the gateway injects it from its
encrypted secret store) or any other source the SDK supports.
"""

from __future__ import annotations

from typing import Any

import anthropic

from cubicle_agents.llm.base import Completion, LLMRefusalError

# Server-side refusal fallback: if the model declines, the API retries on a
# suitable fallback model automatically instead of returning an empty answer.
_FALLBACK_BETA = "server-side-fallback-2026-07-01"


class AnthropicLLM:
    name = "anthropic"

    def __init__(self, *, effort: str = "medium", max_output_tokens: int = 16000) -> None:
        self._client = anthropic.AsyncAnthropic()
        self._effort = effort
        self._max_tokens = max_output_tokens

    async def complete(
        self,
        *,
        system: str,
        prompt: str,
        model: str,
        json_schema: dict[str, Any] | None = None,
    ) -> Completion:
        output_config: dict[str, Any] = {"effort": self._effort}
        if json_schema is not None:
            output_config["format"] = {"type": "json_schema", "schema": json_schema}

        response = await self._client.beta.messages.create(
            model=model,
            max_tokens=self._max_tokens,
            system=system,
            messages=[{"role": "user", "content": prompt}],
            thinking={"type": "adaptive"},
            output_config=output_config,
            betas=[_FALLBACK_BETA],
            fallbacks="default",
        )

        if response.stop_reason == "refusal":
            category = response.stop_details.category if response.stop_details else None
            raise LLMRefusalError(f"The model declined this task (category: {category}).")

        text = "".join(block.text for block in response.content if block.type == "text")
        if response.stop_reason == "max_tokens":
            text += "\n\n[Output truncated: reached the max_tokens limit.]"
        return Completion(
            text=text,
            input_tokens=response.usage.input_tokens,
            output_tokens=response.usage.output_tokens,
        )

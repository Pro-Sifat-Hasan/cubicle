from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Protocol


@dataclass
class Completion:
    text: str
    input_tokens: int = 0
    output_tokens: int = 0


class LLMRefusalError(Exception):
    """The model declined the request."""


class LLM(Protocol):
    name: str

    async def complete(
        self,
        *,
        system: str,
        prompt: str,
        model: str,
        json_schema: dict[str, Any] | None = None,
    ) -> Completion:
        """Return the model's answer. With json_schema, the text is a JSON document matching it."""
        ...

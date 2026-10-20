from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any, Protocol

from cubicle_agents.tools.base import Toolbox

# Upper bound on model <-> tool round trips within one task.
MAX_TOOL_ROUNDS = 16


@dataclass
class AgentResult:
    text: str
    input_tokens: int = 0
    output_tokens: int = 0
    question: str | None = None  # set when the employee asked the owner via a tool


class LLMRefusalError(Exception):
    """The model declined the request."""


class LLMConfigError(Exception):
    """The provider is not usable as configured (missing key, CLI not installed, ...)."""


class LLM(Protocol):
    name: str
    # True when the adapter runs our Toolbox; otherwise employees fall back to
    # text directives (ASK_OWNER:, REMEMBER:).
    supports_tools: bool

    async def run(
        self,
        *,
        system: str,
        prompt: str,
        model: str,
        toolbox: Toolbox | None = None,
        json_schema: dict[str, Any] | None = None,
        workdir: Path | None = None,
    ) -> AgentResult:
        """Work on the prompt, calling tools as needed, and return the final answer."""
        ...

    async def list_models(self) -> list[str]:
        """Model ids available with the current credentials (also a connection test)."""
        ...

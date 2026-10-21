from __future__ import annotations

from collections.abc import Awaitable, Callable
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Protocol

from cubicle_agents.staff.roster import McpServer
from cubicle_agents.tools.base import Toolbox

# Upper bound on model requests (each tool round trip is one) within one run.
MAX_TOOL_ROUNDS = 24

# Live progress: on_event("status", "Searching the web…") or on_event("text", reply_so_far).
EventCallback = Callable[[str, str], Awaitable[None]]


@dataclass(frozen=True)
class Turn:
    """One message of a conversation: role is "owner" or "agent"."""

    role: str
    content: str


@dataclass
class AgentResult:
    text: str
    input_tokens: int = 0
    output_tokens: int = 0
    question: str | None = None  # set when the agent asked the owner via a tool
    # Notes for the owner about things that did not work (e.g. an MCP server that failed to start).
    warnings: list[str] | None = None


class LLMRefusalError(Exception):
    """The model declined the request."""


class LLMConfigError(Exception):
    """The provider is not usable as configured (missing key, CLI not installed, ...)."""


class LLM(Protocol):
    name: str
    # True when the adapter runs our Toolbox; otherwise agents fall back to
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
        history: list[Turn] | None = None,
        mcp: tuple[McpServer, ...] = (),
        effort: str = "",
        on_event: EventCallback | None = None,
        web: bool = True,
    ) -> AgentResult:
        """Work on the prompt, calling tools as needed, and return the final answer.

        history holds earlier conversation turns (chats); mcp the agent's MCP servers;
        effort overrides the office default; web=False withholds web tools.
        """

    async def list_models(self) -> list[str]:
        """Model ids available with the current credentials (also a connection test)."""

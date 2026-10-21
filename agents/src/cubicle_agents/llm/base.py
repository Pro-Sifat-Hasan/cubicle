from __future__ import annotations

from collections.abc import Awaitable, Callable
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Protocol

from cubicle_agents.staff.roster import McpServer
from cubicle_agents.tools.base import Toolbox

_TOOL_STATUS = {
    "web_search": "Searching the web…", "web_fetch": "Reading a web page…", "write_file": "Writing a file…",
    "read_file": "Reading a file…", "list_files": "Looking at the files…", "remember": "Saving to memory…",
    "load_skill": "Loading a skill…", "ask_teammate": "Asking a teammate…", "start_assignment": "Starting an assignment…",
    "edit_file": "Editing a file…", "find_files": "Looking for files…", "grep_files": "Searching the files…",
    "run_command": "Running a command…", "forget": "Updating memory…", "ask_owner": "Asking you…",
    "request_approval": "Asking for your approval…",
    # Claude Code's own tools
    "Read": "Reading a file…", "Write": "Writing a file…", "Edit": "Editing a file…", "Glob": "Looking for files…",
    "Grep": "Searching the files…", "WebSearch": "Searching the web…", "WebFetch": "Reading a web page…",
}


def describe_tool(name: str) -> str:
    """What to show while a tool runs ("Searching the web…")."""
    return _TOOL_STATUS.get(name) or f"Using {name.replace('_', ' ')}…"


# Upper bound on model requests (each tool round trip is one) within one run.
MAX_TOOL_ROUNDS = 24

# Live progress: on_event("status", "Searching the web…") or on_event("text", reply_so_far).
EventCallback = Callable[[str, str], Awaitable[None]]

STEP_THINKING = "thinking"
STEP_NOTE = "note"  # what the agent says between tool calls
STEP_TOOL = "tool"

STEP_RUNNING = "running"
STEP_DONE = "done"
STEP_ERROR = "error"


@dataclass
class Step:
    """One thing an agent did: thought, said something, or used a tool.

    An adapter reports the same step (same id) again whenever it changes, so a tool call first
    arrives with its input and later with its output. `output` holds the text of a thought or remark.
    """

    id: str
    kind: str
    name: str = ""
    input: str = ""
    output: str = ""
    status: str = STEP_RUNNING


# Reports a step as it starts and each time it changes.
StepCallback = Callable[[Step], Awaitable[None]]


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
    question_kind: str = "question"  # "question", or "approval" for a yes/no request to act
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
        on_step: StepCallback | None = None,
    ) -> AgentResult:
        """Work on the prompt, calling tools as needed, and return the final answer.

        history holds earlier conversation turns (chats); mcp the agent's MCP servers;
        effort overrides the office default; web=False withholds web tools; on_step receives
        every thought, remark and tool call (with input and output) as it happens.
        """

    async def list_models(self) -> list[str]:
        """Model ids available with the current credentials (also a connection test)."""

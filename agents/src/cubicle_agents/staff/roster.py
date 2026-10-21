"""The staff roster.

The owner manages agents in the app; the gateway stores them and hands the
worker each agent's complete configuration (instructions, tools, MCP servers,
skills and team). The roster is reloaded whenever it changes.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

MANAGER_ROLE = "manager"
SPECIALIST_ROLE = "specialist"

# Built-in tool ids (mirrors gateway/internal/catalog).
TOOL_FILES = "files"
TOOL_WEB = "web"
TOOL_MEMORY = "memory"
TOOL_ASSIGNMENTS = "assignments"
TOOL_TERMINAL = "terminal"
STANDARD_TOOLS = (TOOL_FILES, TOOL_WEB, TOOL_MEMORY)


@dataclass(frozen=True)
class McpServer:
    """An MCP server an agent may use. Secrets are referenced by name only."""

    id: str
    name: str
    transport: str  # stdio | http
    command: str = ""
    args: tuple[str, ...] = ()
    env: tuple[tuple[str, str], ...] = ()
    secret_env: tuple[str, ...] = ()
    url: str = ""
    headers: tuple[tuple[str, str], ...] = ()
    auth_secret: str = ""

    @classmethod
    def from_json(cls, d: dict[str, Any]) -> McpServer:
        return cls(
            id=d["id"], name=d.get("name") or d["id"], transport=d.get("transport", "stdio"),
            command=d.get("command") or "", args=tuple(d.get("args") or ()),
            env=tuple(sorted((d.get("env") or {}).items())), secret_env=tuple(d.get("secret_env") or ()),
            url=d.get("url") or "", headers=tuple(sorted((d.get("headers") or {}).items())),
            auth_secret=d.get("auth_secret") or "",
        )


@dataclass(frozen=True)
class Skill:
    """Reusable instructions in the SKILL.md format."""

    name: str
    description: str
    content: str


@dataclass(frozen=True)
class Employee:
    id: str
    name: str
    title: str
    role: str
    description: str = ""  # what they're good at
    instructions: str = ""  # how they work
    avatar: str = ""
    model: str = ""  # empty = the office default
    effort: str = ""  # empty = the office default
    tools: tuple[str, ...] = STANDARD_TOOLS
    team: tuple[str, ...] = ()
    mcp: tuple[McpServer, ...] = ()
    skills: tuple[Skill, ...] = field(default=())

    @property
    def is_manager(self) -> bool:
        return self.role == MANAGER_ROLE

    def has_tool(self, tool: str) -> bool:
        return tool in self.tools

    @classmethod
    def from_json(cls, d: dict[str, Any]) -> Employee:
        return cls(
            id=d["id"], name=d.get("name") or d["id"], title=d.get("title") or "", role=d.get("role") or SPECIALIST_ROLE,
            description=d.get("description") or "", instructions=d.get("instructions") or "",
            avatar=d.get("avatar") or "", model=d.get("model") or "", effort=d.get("effort") or "",
            tools=tuple(d.get("tools") or ()), team=tuple(d.get("team") or ()),
            mcp=tuple(McpServer.from_json(m) for m in d.get("mcp") or ()),
            skills=tuple(Skill(s["name"], s.get("description", ""), s.get("content", ""))
                         for s in d.get("skill_docs") or ()),
        )


class Roster:
    """The current agents, replaced in place when the owner changes them."""

    def __init__(self, employees: list[Employee] | None = None) -> None:
        self._by_id: dict[str, Employee] = {}
        self.replace(employees or [])

    def replace(self, employees: list[Employee]) -> None:
        self._by_id = {e.id: e for e in employees}

    def __iter__(self):
        return iter(list(self._by_id.values()))

    def __len__(self) -> int:
        return len(self._by_id)

    def get(self, agent_id: str) -> Employee | None:
        return self._by_id.get(agent_id)

    @property
    def manager(self) -> Employee | None:
        return next((e for e in self._by_id.values() if e.is_manager), None)

    def team_of(self, employee: Employee) -> list[Employee]:
        """The agents this one can hand work to (enabled ones only, in team order)."""
        return [m for m in (self._by_id.get(i) for i in employee.team) if m is not None and m.id != employee.id]


# A small built-in team used by tests and offline tools. The real roster comes
# from the gateway, which seeds the same team on first run.
DEFAULT_ROSTER: tuple[Employee, ...] = (
    Employee(id="manager", name="Max", title="Chief of Staff", role=MANAGER_ROLE, avatar="🧭",
             description="Plans requests, delegates to the right specialists and delivers final briefings.",
             instructions="You are calm, decisive and organised.",
             tools=(*STANDARD_TOOLS, TOOL_ASSIGNMENTS),
             team=("researcher", "writer", "engineer", "analyst", "reviewer")),
    Employee(id="researcher", name="Rhea", title="Research Analyst", role=SPECIALIST_ROLE, avatar="🔎",
             description="Research, fact-finding, comparisons and summaries of sources.",
             instructions="You are curious and rigorous."),
    Employee(id="writer", name="Quill", title="Writer", role=SPECIALIST_ROLE, avatar="✍️",
             description="Emails, documents, posts and rewriting in the owner's voice.",
             instructions="You write clearly and warmly."),
    Employee(id="engineer", name="Dev", title="Software Engineer", role=SPECIALIST_ROLE, avatar="🛠️",
             description="Code, scripts, automation, debugging and technical explanations.",
             instructions="You are pragmatic and precise."),
    Employee(id="analyst", name="Nova", title="Planning Analyst", role=SPECIALIST_ROLE, avatar="📊",
             description="Schedules, budgets, numbers, checklists and plans.",
             instructions="You think in structure."),
    Employee(id="reviewer", name="Vera", title="Quality Reviewer", role=SPECIALIST_ROLE, avatar="✅",
             description="Reviews drafts, catches mistakes and adds the final polish.",
             instructions="You are constructive and sharp-eyed."),
)

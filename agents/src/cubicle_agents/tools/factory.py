"""Assembles the toolbox an agent gets for one run, from the tools the owner enabled."""

from __future__ import annotations

from collections.abc import Awaitable, Callable
from pathlib import Path
from typing import Any

from cubicle_agents.core.gateway import Gateway
from cubicle_agents.staff.roster import TOOL_ASSIGNMENTS, TOOL_FILES, TOOL_MEMORY, TOOL_WEB, Employee
from cubicle_agents.tools.base import Tool, Toolbox, ToolError, require_str
from cubicle_agents.tools.web import WEB_FETCH
from cubicle_agents.tools.workspace import Workspace

# consult(teammate_id, request) -> the teammate's answer
ConsultFunc = Callable[[str, str], Awaitable[str]]


def build_toolbox(
    *,
    employee: Employee,
    workspace: Path,
    gateway: Gateway,
    may_ask: bool,
    client_web_fetch: bool,
    may_approve: bool = False,
    task_id: str = "",
    web_access: bool = True,
    team: list[Employee] | None = None,
    consult: ConsultFunc | None = None,
    allow_assignments: bool = False,
) -> Toolbox:
    """Tools for one run.

    client_web_fetch adds our own guarded web_fetch; providers with a built-in
    web tool (Claude, OpenAI) pass False and use theirs instead.
    """
    toolbox = Toolbox()
    if employee.has_tool(TOOL_FILES):
        toolbox.tools.extend(Workspace(workspace).tools())
    if employee.has_tool(TOOL_MEMORY):
        toolbox.tools.append(_remember(gateway, employee.id))
    if employee.skills:
        toolbox.tools.append(_load_skill(employee))
    if team and consult is not None:
        toolbox.tools.append(_ask_teammate(team, consult))
    if allow_assignments and employee.has_tool(TOOL_ASSIGNMENTS):
        toolbox.tools.append(_start_assignment(gateway, employee.id, task_id))
    if may_ask:
        async def ask_owner(args: dict[str, Any]) -> str:
            toolbox.question = require_str(args, "question", max_len=2000).strip()
            return "Question sent to the owner. Stop working now; the task resumes when they reply."

        toolbox.tools.append(Tool(
            name="ask_owner",
            description="Ask the owner one clear question when you cannot do a good job without information "
            "only they have. Don't ask for things you can reasonably assume or look up. Ends your turn.",
            parameters={
                "type": "object",
                "properties": {"question": {"type": "string"}},
                "required": ["question"],
                "additionalProperties": False,
            },
            handler=ask_owner,
        ))
    if may_approve:
        async def request_approval(args: dict[str, Any]) -> str:
            action = require_str(args, "action", max_len=600).strip()
            details = args.get("details") if isinstance(args.get("details"), str) else ""
            toolbox.question = f"**{action}**" + (f"\n\n{details.strip()[:3000]}" if details.strip() else "")
            toolbox.question_kind = "approval"
            return "Approval requested. Stop working now; do nothing else until the owner decides."

        toolbox.tools.append(Tool(
            name="request_approval",
            description="Ask the owner to approve ONE specific action before you do it. Required before anything with "
            "consequences outside your workspace: sending or publishing something, deleting or overwriting data, "
            "spending money, changing accounts or settings, or using a tool that changes something. State exactly what "
            "you will do (who, what, where) in `action`, and put the full draft or details in `details`. Ends your turn.",
            parameters={
                "type": "object",
                "properties": {"action": {"type": "string"}, "details": {"type": "string"}},
                "required": ["action"],
                "additionalProperties": False,
            },
            handler=request_approval,
        ))
    if client_web_fetch and web_access and employee.has_tool(TOOL_WEB):
        toolbox.tools.append(WEB_FETCH)
    return toolbox


def _remember(gateway: Gateway, agent_id: str) -> Tool:
    async def remember(args: dict[str, Any]) -> str:
        fact = require_str(args, "fact", max_len=1000)
        await gateway.add_memory(fact.strip(), agent_id)
        return "Saved to the office memory."

    return Tool(
        name="remember",
        description="Save a durable fact about the owner, their preferences, people or projects so the whole "
        "staff remembers it in future work. Only save things that will still be true and useful later.",
        parameters={
            "type": "object",
            "properties": {"fact": {"type": "string", "description": "One short, self-contained sentence."}},
            "required": ["fact"],
            "additionalProperties": False,
        },
        handler=remember,
    )


def _load_skill(employee: Employee) -> Tool:
    skills = {s.name: s for s in employee.skills}

    async def load_skill(args: dict[str, Any]) -> str:
        name = require_str(args, "name", max_len=100).strip()
        skill = skills.get(name)
        if skill is None:
            raise ToolError(f"No skill named {name!r}. Available: {', '.join(sorted(skills))}.")
        return f"# Skill: {skill.name}\n\n{skill.content}"

    return Tool(
        name="load_skill",
        description="Load the full instructions of one of your skills. Do this before work that matches the skill's "
        "description, then follow the instructions.",
        parameters={
            "type": "object",
            "properties": {"name": {"type": "string", "enum": sorted(skills)}},
            "required": ["name"],
            "additionalProperties": False,
        },
        handler=load_skill,
    )


def _ask_teammate(team: list[Employee], consult: ConsultFunc) -> Tool:
    ids = [m.id for m in team]

    async def ask_teammate(args: dict[str, Any]) -> str:
        teammate = require_str(args, "teammate", max_len=64).strip()
        if teammate not in ids:
            raise ToolError(f"Unknown teammate {teammate!r}. Choose one of: {', '.join(ids)}.")
        return await consult(teammate, require_str(args, "request", max_len=20_000))

    return Tool(
        name="ask_teammate",
        description="Ask a teammate to do part of the work or answer a question in their area of expertise. "
        "Describe exactly what you need and the format you want back. Returns their answer.",
        parameters={
            "type": "object",
            "properties": {
                "teammate": {"type": "string", "enum": ids, "description": "The teammate's id."},
                "request": {"type": "string", "description": "A self-contained request with all needed context."},
            },
            "required": ["teammate", "request"],
            "additionalProperties": False,
        },
        handler=ask_teammate,
    )


def _start_assignment(gateway: Gateway, agent_id: str, source_task_id: str = "") -> Tool:
    async def start_assignment(args: dict[str, Any]) -> str:
        title = require_str(args, "title", max_len=300).strip()
        description = args.get("description") if isinstance(args.get("description"), str) else ""
        task = await gateway.create_assignment(title, description, agent_id, source_task_id)
        return (f"Assignment started (id {task.get('id', '?')}). The team is working on it; its plan and "
                "result show in this chat and on the Today page.")

    return Tool(
        name="start_assignment",
        description="Start a background assignment that the main agent plans and the team carries out. Use for "
        "bigger jobs with several parts; include every detail the team needs in the description.",
        parameters={
            "type": "object",
            "properties": {"title": {"type": "string"}, "description": {"type": "string"}},
            "required": ["title", "description"],
            "additionalProperties": False,
        },
        handler=start_assignment,
    )

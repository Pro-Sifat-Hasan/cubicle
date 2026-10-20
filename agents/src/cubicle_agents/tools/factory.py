"""Assembles the toolbox an employee gets for one task."""

from __future__ import annotations

from pathlib import Path
from typing import Any

from cubicle_agents.core.gateway import Gateway
from cubicle_agents.tools.base import Tool, Toolbox, require_str
from cubicle_agents.tools.web import WEB_FETCH
from cubicle_agents.tools.workspace import Workspace


def build_toolbox(
    *,
    workspace: Path,
    gateway: Gateway,
    employee_id: str,
    may_ask: bool,
    client_web_fetch: bool,
) -> Toolbox:
    """Tools for one task.

    client_web_fetch adds our own guarded web_fetch; providers with a built-in
    web tool (Claude) pass False and use theirs instead.
    """
    toolbox = Toolbox()
    toolbox.tools.extend(Workspace(workspace).tools())

    async def remember(args: dict[str, Any]) -> str:
        fact = require_str(args, "fact", max_len=1000)
        await gateway.add_memory(fact.strip(), employee_id)
        return "Saved to the office memory."

    toolbox.tools.append(Tool(
        name="remember",
        description="Save a durable fact about the owner, their preferences, people or projects so the whole "
        "staff remembers it in future tasks. Only save things that will still be true and useful later.",
        parameters={
            "type": "object",
            "properties": {"fact": {"type": "string", "description": "One short, self-contained sentence."}},
            "required": ["fact"],
            "additionalProperties": False,
        },
        handler=remember,
    ))

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

    if client_web_fetch:
        toolbox.tools.append(WEB_FETCH)
    return toolbox

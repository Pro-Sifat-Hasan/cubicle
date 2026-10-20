"""Offline demo provider.

Lets anyone install Cubicle and watch the whole office work end-to-end
(plan -> delegate -> do -> wrap up, and chats) before adding an API key.
Output is clearly labelled as demo content.
"""

from __future__ import annotations

import json
import re
from pathlib import Path
from typing import Any

from cubicle_agents.llm.base import AgentResult, EventCallback, Turn
from cubicle_agents.staff.roster import McpServer
from cubicle_agents.tools.base import Toolbox

_ROSTER_LINE = re.compile(r"^- (?P<id>[a-z0-9_-]+) \((?P<title>[^,)]*)", re.MULTILINE)
_ASSIGNMENT = re.compile(r"^(?:OVERALL )?ASSIGNMENT: (?P<title>.+)$", re.MULTILINE)
_TASK = re.compile(r"^YOUR TASK: (?P<title>.+)$", re.MULTILINE)
_NAME = re.compile(r"^You are (?P<name>[^,.]+)")


class DemoLLM:
    name = "demo"
    supports_tools = False

    async def run(self, *, system: str, prompt: str, model: str, toolbox: Toolbox | None = None,
                  json_schema: dict[str, Any] | None = None, workdir: Path | None = None,
                  history: list[Turn] | None = None, mcp: tuple[McpServer, ...] = (), effort: str = "",
                  on_event: EventCallback | None = None, web: bool = True) -> AgentResult:
        tokens_in = len(prompt) // 4

        if json_schema is not None:
            title = _first(_ASSIGNMENT, prompt) or "your request"
            staff = [(m.group("id"), m.group("title").strip()) for m in _ROSTER_LINE.finditer(prompt)][:3]
            subtasks = [
                {
                    "title": f"{emp_title}: contribute to '{title}'",
                    "description": f"Demo subtask for the {emp_title}.",
                    "assignee": emp_id,
                    "depends_on": [i - 1] if i > 0 else [],
                }
                for i, (emp_id, emp_title) in enumerate(staff)
            ]
            plan = {"summary": f"Demo plan for '{title}'.", "question_for_owner": "", "subtasks": subtasks}
            return AgentResult(text=json.dumps(plan), input_tokens=tokens_in, output_tokens=120)

        title = _first(_TASK, prompt) or _first(_ASSIGNMENT, prompt)
        if title:
            text = (
                f"**[Demo mode]** Work on *{title}* is complete.\n\n"
                "This answer comes from Cubicle's offline demo provider. Choose a real AI provider in "
                "Settings → AI provider so your staff can do real work."
            )
        else:  # a chat message
            who = _first(_NAME, system) or "your agent"
            text = (
                f"**[Demo mode]** Hi, {who} here. You said: “{prompt.strip()[:200]}”.\n\n"
                "I'm running on Cubicle's offline demo provider, so this is a sample reply. Connect an AI in "
                "Settings → AI provider and I'll answer for real."
            )
        if on_event is not None:
            await on_event("text", text)
        return AgentResult(text=text, input_tokens=tokens_in, output_tokens=len(text) // 4)

    async def list_models(self) -> list[str]:
        return ["demo"]


def _first(pattern: re.Pattern[str], text: str) -> str:
    match = pattern.search(text)
    return next((v.strip() for v in match.groupdict().values() if v), "") if match else ""

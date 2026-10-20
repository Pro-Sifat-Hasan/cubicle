"""Offline demo provider.

Lets anyone install Cubicle and watch the whole office work end-to-end
(plan -> delegate -> do -> wrap up) before adding an API key. Output is
clearly labelled as demo content.
"""

from __future__ import annotations

import json
import re
from pathlib import Path
from typing import Any

from cubicle_agents.llm.base import AgentResult
from cubicle_agents.tools.base import Toolbox

_ROSTER_LINE = re.compile(r"^- (?P<id>[a-z0-9_-]+) \((?P<title>[^,)]*)", re.MULTILINE)
_ASSIGNMENT = re.compile(r"^(?:OVERALL )?ASSIGNMENT: (?P<title>.+)$", re.MULTILINE)


class DemoLLM:
    name = "demo"
    supports_tools = False

    async def run(self, *, system: str, prompt: str, model: str, toolbox: Toolbox | None = None,
                  json_schema: dict[str, Any] | None = None, workdir: Path | None = None) -> AgentResult:
        match = _ASSIGNMENT.search(prompt)
        title = match.group("title").strip() if match else "your request"
        tokens_in = len(prompt) // 4

        if json_schema is not None:
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

        text = (
            f"**[Demo mode]** Work on *{title}* is complete.\n\n"
            "This answer comes from Cubicle's offline demo provider. Choose a real AI provider in "
            "Settings → AI provider so your staff can do real work."
        )
        return AgentResult(text=text, input_tokens=tokens_in, output_tokens=len(text) // 4)

    async def list_models(self) -> list[str]:
        return ["demo"]

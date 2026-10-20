"""Offline demo provider.

Lets anyone install Cubicle and watch the whole office work end-to-end
(plan -> delegate -> do -> wrap up) before adding an API key. Output is
clearly labelled as demo content.
"""

from __future__ import annotations

import json
import re
from typing import Any

from cubicle_agents.llm.base import Completion

_ROSTER_LINE = re.compile(r"^- (?P<id>[a-z0-9_-]+) \((?P<title>[^)]*)\)", re.MULTILINE)
_ASSIGNMENT = re.compile(r"^(?:OVERALL )?ASSIGNMENT: (?P<title>.+)$", re.MULTILINE)


class DemoLLM:
    name = "demo"

    async def complete(
        self,
        *,
        system: str,
        prompt: str,
        model: str,
        json_schema: dict[str, Any] | None = None,
    ) -> Completion:
        title_match = _ASSIGNMENT.search(prompt)
        title = title_match.group("title").strip() if title_match else "your request"

        if json_schema is not None:
            staff = [(m.group("id"), m.group("title")) for m in _ROSTER_LINE.finditer(prompt)][:3]
            subtasks = []
            for i, (emp_id, emp_title) in enumerate(staff):
                subtasks.append({
                    "title": f"{emp_title}: contribute to '{title}'",
                    "description": f"Demo subtask for the {emp_title}.",
                    "assignee": emp_id,
                    "depends_on": [i - 1] if i > 0 else [],
                })
            plan = {"summary": f"Demo plan for '{title}'.", "question_for_owner": "", "subtasks": subtasks}
            return Completion(text=json.dumps(plan), input_tokens=len(prompt) // 4, output_tokens=120)

        text = (
            f"**[Demo mode]** Work on *{title}* is complete.\n\n"
            "This answer comes from Cubicle's offline demo provider. Add an `ANTHROPIC_API_KEY` "
            "in Settings → Secrets so your staff can do real work."
        )
        return Completion(text=text, input_tokens=len(prompt) // 4, output_tokens=len(text) // 4)

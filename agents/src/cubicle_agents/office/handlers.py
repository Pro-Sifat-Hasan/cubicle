"""What an employee does with a claimed task.

* Manager + assignment  -> plan, then delegate subtasks (or ask the owner)
* Manager + synthesis   -> merge the team's results into the final deliverable
* Specialist + subtask  -> do the work (or ask the owner)
"""

from __future__ import annotations

import json
import logging
from typing import Any

from cubicle_agents.core.gateway import Claim, Gateway
from cubicle_agents.llm.base import LLM, Completion
from cubicle_agents.staff import prompts
from cubicle_agents.staff.roster import Employee, specialists_of

log = logging.getLogger(__name__)

MAX_SUBTASKS = 6
MAX_QUESTIONS_PER_TASK = 2


class TaskHandler:
    def __init__(self, *, gateway: Gateway, llm: LLM, roster: list[Employee], default_model: str, owner: str) -> None:
        self.gateway = gateway
        self.llm = llm
        self.roster = roster
        self.default_model = default_model
        self.owner = owner

    async def handle(self, employee: Employee, claim: Claim) -> None:
        kind = claim.task.get("kind")
        if employee.is_manager and kind == "assignment":
            await self._plan(employee, claim)
        elif employee.is_manager and kind == "synthesis":
            await self._synthesise(employee, claim)
        else:
            await self._work(employee, claim)

    # ---- helpers ----

    async def _call(self, employee: Employee, task_id: str, prompt: str,
                    json_schema: dict[str, Any] | None = None) -> Completion:
        completion = await self.llm.complete(
            system=prompts.system_prompt(employee, self.owner),
            prompt=prompt,
            model=employee.model or self.default_model,
            json_schema=json_schema,
        )
        await self.gateway.report_usage(task_id, employee.id, completion.input_tokens, completion.output_tokens)
        return completion

    def _may_ask(self, claim: Claim) -> bool:
        return len(claim.human_answers) < MAX_QUESTIONS_PER_TASK

    # ---- manager: plan ----

    async def _plan(self, manager: Employee, claim: Claim) -> None:
        task = claim.task
        specialists = specialists_of(self.roster)
        await self.gateway.log(manager.id, task["id"], f"{manager.name} is planning “{task['title']}”.")
        completion = await self._call(
            manager, task["id"],
            prompts.plan_prompt(task, specialists, claim.human_answers, MAX_SUBTASKS),
            json_schema=prompts.PLAN_SCHEMA,
        )
        plan = parse_plan(completion.text, {e.id for e in specialists})

        if plan.question and not plan.subtasks and self._may_ask(claim):
            await self.gateway.ask(task["id"], manager.id, plan.question)
            await self.gateway.log(manager.id, task["id"], f"{manager.name} needs your input before starting.")
            return
        if not plan.subtasks:  # model gave nothing usable: hand the whole job to the first specialist
            plan.subtasks = [{
                "title": task["title"], "description": task.get("description", ""),
                "assignee": specialists[0].id, "depends_on": [],
            }]

        await self.gateway.delegate(task["id"], plan.subtasks, {
            "title": f"Wrap up: {task['title']}"[:300],
            "description": "Merge the team's results into the final deliverable for the owner.",
            "assignee": manager.id,
        })
        names = {e.id: e.name for e in self.roster}
        who = ", ".join(sorted({names[s["assignee"]] for s in plan.subtasks}))
        await self.gateway.log(manager.id, task["id"],
                               f"{manager.name} split “{task['title']}” into {len(plan.subtasks)} task(s) for {who}.")

    # ---- manager: wrap up ----

    async def _synthesise(self, manager: Employee, claim: Claim) -> None:
        task = claim.task
        if len(claim.dependency_results) == 1:
            # Nothing to merge: pass the single result through without spending tokens.
            result = claim.dependency_results[0].get("result", "")
        else:
            completion = await self._call(manager, task["id"],
                                          prompts.synthesis_prompt(claim.parent, claim.dependency_results))
            result = completion.text
        await self.gateway.complete(task["id"], result)

    # ---- specialist: do the work ----

    async def _work(self, employee: Employee, claim: Claim) -> None:
        task = claim.task
        may_ask = self._may_ask(claim)
        completion = await self._call(
            employee, task["id"],
            prompts.work_prompt(task, claim.parent, claim.dependency_results, claim.human_answers, may_ask),
        )
        text = completion.text.strip()
        if may_ask and text.startswith(prompts.ASK_PREFIX):
            question = text[len(prompts.ASK_PREFIX):].strip().splitlines()[0][:4000]
            await self.gateway.ask(task["id"], employee.id, question or "Could you give me more detail?")
            await self.gateway.log(employee.id, task["id"], f"{employee.name} asked you a question.")
            return
        await self.gateway.complete(task["id"], text)
        await self.gateway.log(employee.id, task["id"], f"{employee.name} finished “{task['title']}”.")


class Plan:
    def __init__(self, subtasks: list[dict[str, Any]], question: str, summary: str) -> None:
        self.subtasks = subtasks
        self.question = question
        self.summary = summary


def parse_plan(text: str, valid_assignees: set[str]) -> Plan:
    """Parse and sanitise the manager's plan. Never trusts the model's ids or indices blindly."""
    try:
        data = json.loads(_strip_fences(text))
    except json.JSONDecodeError:
        log.warning("manager returned invalid plan JSON; falling back")
        return Plan([], "", "")
    if not isinstance(data, dict):
        return Plan([], "", "")

    fallback = sorted(valid_assignees)[0] if valid_assignees else ""
    subtasks: list[dict[str, Any]] = []
    for raw in (data.get("subtasks") or [])[:MAX_SUBTASKS]:
        if not isinstance(raw, dict) or not str(raw.get("title", "")).strip():
            continue
        idx = len(subtasks)
        deps = [d for d in raw.get("depends_on") or [] if isinstance(d, int) and 0 <= d < idx]
        assignee = raw.get("assignee") if raw.get("assignee") in valid_assignees else fallback
        subtasks.append({
            "title": str(raw["title"]).strip()[:300],
            "description": str(raw.get("description", "")).strip()[:20000],
            "assignee": assignee,
            "depends_on": sorted(set(deps)),
        })
    return Plan(subtasks, str(data.get("question_for_owner") or "").strip(), str(data.get("summary") or ""))


def _strip_fences(text: str) -> str:
    t = text.strip()
    if t.startswith("```"):
        t = t.split("\n", 1)[1] if "\n" in t else ""
        t = t.rsplit("```", 1)[0]
    return t.strip()

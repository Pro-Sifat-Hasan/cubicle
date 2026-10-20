"""What an employee does with a claimed task.

* Manager + assignment  -> plan, then delegate subtasks (or ask the owner)
* Manager + synthesis   -> merge the team's results into the final deliverable
* Specialist + subtask  -> do the work with tools (or ask the owner)
"""

from __future__ import annotations

import json
import logging
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from cubicle_agents.core.gateway import Claim, Gateway
from cubicle_agents.llm.base import LLM, AgentResult
from cubicle_agents.office import directives
from cubicle_agents.staff import prompts
from cubicle_agents.staff.roster import Employee, specialists_of
from cubicle_agents.tools import build_toolbox
from cubicle_agents.tools.workspace import Workspace

log = logging.getLogger(__name__)

MAX_SUBTASKS = 6
MAX_QUESTIONS_PER_TASK = 2
MEMORY_LIMIT = 50


@dataclass(frozen=True)
class OfficeContext:
    """Office-wide configuration shared by every desk."""

    gateway: Gateway
    llm: LLM
    roster: list[Employee]
    default_model: str
    owner: str
    workspace_root: Path
    web_access: bool = True
    # Our own web_fetch tool, for providers without a built-in web tool.
    client_web_fetch: bool = False


@dataclass
class Plan:
    subtasks: list[dict[str, Any]] = field(default_factory=list)
    question: str = ""
    summary: str = ""


class TaskHandler:
    def __init__(self, ctx: OfficeContext) -> None:
        self.ctx = ctx

    async def handle(self, employee: Employee, claim: Claim) -> None:
        kind = claim.task.get("kind")
        if employee.is_manager and kind == "assignment":
            await self._plan(employee, claim)
        elif employee.is_manager and kind == "synthesis":
            await self._synthesise(employee, claim)
        else:
            await self._work(employee, claim)

    # ---- shared helpers ----

    def _workdir(self, claim: Claim) -> Path:
        assignment_id = claim.task.get("parent_id") or claim.task["id"]
        return self.ctx.workspace_root / assignment_id

    async def _memories(self) -> list[str]:
        try:
            return await self.ctx.gateway.list_memories(MEMORY_LIMIT)
        except Exception:  # memory is helpful context, never a reason to fail work
            log.warning("could not load office memory", exc_info=True)
            return []

    async def _run(self, employee: Employee, claim: Claim, prompt: str, *,
                   json_schema: dict[str, Any] | None = None, may_ask: bool = False) -> AgentResult:
        ctx = self.ctx
        workdir = self._workdir(claim)
        toolbox = None
        if ctx.llm.supports_tools and json_schema is None:
            toolbox = build_toolbox(workspace=workdir, gateway=ctx.gateway, employee_id=employee.id,
                                    may_ask=may_ask, client_web_fetch=ctx.client_web_fetch)
        system = prompts.system_prompt(employee, ctx.owner, memories=await self._memories(),
                                       tool_mode=ctx.llm.supports_tools, web=ctx.web_access)
        result = await ctx.llm.run(system=system, prompt=prompt, model=employee.model or ctx.default_model,
                                   toolbox=toolbox, json_schema=json_schema, workdir=workdir)
        await ctx.gateway.report_usage(claim.task["id"], employee.id, result.input_tokens, result.output_tokens)

        if not ctx.llm.supports_tools and json_schema is None:
            parsed = directives.extract(result.text)
            for fact in parsed.memories:
                await ctx.gateway.add_memory(fact, employee.id)
            result.text = parsed.text
            if may_ask and parsed.question:
                result.question = parsed.question
        return result

    def _may_ask(self, claim: Claim) -> bool:
        return len(claim.human_answers) < MAX_QUESTIONS_PER_TASK

    # ---- manager: plan ----

    async def _plan(self, manager: Employee, claim: Claim) -> None:
        gw, task = self.ctx.gateway, claim.task
        specialists = specialists_of(self.ctx.roster)
        await gw.log(manager.id, task["id"], f"{manager.name} is planning “{task['title']}”.")
        result = await self._run(manager, claim,
                                 prompts.plan_prompt(task, specialists, claim.human_answers, MAX_SUBTASKS),
                                 json_schema=prompts.PLAN_SCHEMA)
        plan = parse_plan(result.text, {e.id for e in specialists})

        if plan.question and not plan.subtasks and self._may_ask(claim):
            await gw.ask(task["id"], manager.id, plan.question)
            await gw.log(manager.id, task["id"], f"{manager.name} needs your input before starting.")
            return
        if not plan.subtasks:  # nothing usable: hand the whole job to the first specialist
            plan.subtasks = [{"title": task["title"], "description": task.get("description", ""),
                              "assignee": specialists[0].id, "depends_on": []}]

        await gw.delegate(task["id"], plan.subtasks, {
            "title": f"Wrap up: {task['title']}"[:300],
            "description": "Merge the team's results into the final deliverable for the owner.",
            "assignee": manager.id,
        })
        names = {e.id: e.name for e in self.ctx.roster}
        who = ", ".join(sorted({names[s["assignee"]] for s in plan.subtasks}))
        await gw.log(manager.id, task["id"],
                     f"{manager.name} split “{task['title']}” into {len(plan.subtasks)} task(s) for {who}.")

    # ---- manager: wrap up ----

    async def _synthesise(self, manager: Employee, claim: Claim) -> None:
        files = Workspace(self._workdir(claim)).files()
        if len(claim.dependency_results) == 1:
            # Nothing to merge: pass the single result through without spending tokens.
            text = claim.dependency_results[0].get("result", "")
        else:
            prompt = prompts.synthesis_prompt(claim.parent, claim.dependency_results, files)
            text = (await self._run(manager, claim, prompt)).text
        if files:
            text += "\n\n---\n**Files in this assignment's workspace:** " + ", ".join(f"`{f}`" for f in files)
        await self.ctx.gateway.complete(claim.task["id"], text)

    # ---- specialist: do the work ----

    async def _work(self, employee: Employee, claim: Claim) -> None:
        gw, task = self.ctx.gateway, claim.task
        may_ask = self._may_ask(claim)
        prompt = prompts.work_prompt(task, claim.parent, claim.dependency_results, claim.human_answers, may_ask)
        result = await self._run(employee, claim, prompt, may_ask=may_ask)
        if result.question:
            await gw.ask(task["id"], employee.id, result.question)
            await gw.log(employee.id, task["id"], f"{employee.name} asked you a question.")
            return
        await gw.complete(task["id"], result.text or "(no written output; see the workspace files)")
        await gw.log(employee.id, task["id"], f"{employee.name} finished “{task['title']}”.")


def parse_plan(text: str, valid_assignees: set[str]) -> Plan:
    """Parse and sanitise the manager's plan. Never trusts the model's ids or indices blindly."""
    try:
        data = json.loads(_strip_fences(text))
    except json.JSONDecodeError:
        log.warning("manager returned invalid plan JSON; falling back")
        return Plan()
    if not isinstance(data, dict):
        return Plan()

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
    # Tolerate prose around the JSON object (common with text-only providers).
    start, end = t.find("{"), t.rfind("}")
    return t[start:end + 1] if start != -1 and end > start else t

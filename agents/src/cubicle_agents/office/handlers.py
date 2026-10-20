"""What an agent does with a claimed task.

* Main agent + assignment -> plan with its team, then delegate subtasks (or ask the owner)
* Any agent + synthesis   -> merge the team's results into the final deliverable
* Any agent + chat        -> reply in the conversation, consulting teammates when useful
* Any agent + other work  -> do the work with tools (or ask the owner)

An agent whose team is empty does assignments itself. While working, an agent
can consult a teammate (ask_teammate): the teammate runs with its own
instructions, tools, MCP servers and skills and hands back its answer.
"""

from __future__ import annotations

import json
import logging
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from cubicle_agents.core.gateway import Claim, Gateway
from cubicle_agents.llm.base import LLM, AgentResult, EventCallback, Turn
from cubicle_agents.office import directives
from cubicle_agents.office.status import StatusBoard
from cubicle_agents.staff import prompts
from cubicle_agents.staff.roster import TOOL_MEMORY, TOOL_WEB, Employee, Roster
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
    roster: Roster
    default_model: str
    owner: str
    workspace_root: Path
    web_access: bool = True
    # Our own web_fetch tool, for providers without a built-in fetch tool.
    client_web_fetch: bool = False


@dataclass
class Plan:
    subtasks: list[dict[str, Any]] = field(default_factory=list)
    question: str = ""
    summary: str = ""


class TaskHandler:
    def __init__(self, ctx: OfficeContext) -> None:
        self.ctx = ctx
        self.status = StatusBoard(ctx.gateway)

    async def handle(self, employee: Employee, claim: Claim) -> None:
        kind = claim.task.get("kind")
        if kind == "chat":
            await self._chat(employee, claim)
        elif kind == "assignment" and employee.is_manager and self.ctx.roster.team_of(employee):
            await self._plan(employee, claim)
        elif kind == "synthesis":
            await self._synthesise(employee, claim)
        else:
            await self._work(employee, claim)

    # ---- shared helpers ----

    def _workdir(self, claim: Claim) -> Path:
        task = claim.task
        folder = task.get("conversation_id") or task.get("parent_id") or task["id"]
        return self.ctx.workspace_root / folder

    async def _memories(self) -> list[str]:
        try:
            return await self.ctx.gateway.list_memories(MEMORY_LIMIT)
        except Exception:  # memory is helpful context, never a reason to fail work
            log.warning("could not load office memory", exc_info=True)
            return []

    async def _run(self, employee: Employee, claim: Claim, prompt: str, *,
                   json_schema: dict[str, Any] | None = None, may_ask: bool = False, mode: str = prompts.MODE_TASK,
                   history: list[Turn] | None = None, on_event: EventCallback | None = None) -> AgentResult:
        ctx = self.ctx
        workdir = self._workdir(claim)
        tool_mode = ctx.llm.supports_tools
        team = ctx.roster.team_of(employee) if mode != prompts.MODE_CONSULT and json_schema is None else []
        toolbox = None
        if tool_mode and json_schema is None:
            async def consult(teammate_id: str, request: str) -> str:
                return await self._consult(employee, teammate_id, request, claim)

            toolbox = build_toolbox(
                employee=employee, workspace=workdir, gateway=ctx.gateway, may_ask=may_ask,
                client_web_fetch=ctx.client_web_fetch, web_access=ctx.web_access,
                team=team, consult=consult if team else None, allow_assignments=mode == prompts.MODE_CHAT,
            )
        system = prompts.system_prompt(employee, ctx.owner, memories=await self._memories(), tool_mode=tool_mode,
                                       web=ctx.web_access, team=team, mode=mode)
        result = await ctx.llm.run(
            system=system, prompt=prompt, model=employee.model or ctx.default_model, toolbox=toolbox,
            json_schema=json_schema, workdir=workdir, history=history,
            mcp=employee.mcp if json_schema is None else (), effort=employee.effort, on_event=on_event,
            web=ctx.web_access and employee.has_tool(TOOL_WEB),
        )
        await ctx.gateway.report_usage(claim.task["id"], employee.id, result.input_tokens, result.output_tokens)
        for warning in result.warnings or []:
            await ctx.gateway.log(employee.id, claim.task["id"], f"{employee.name}: {warning}")

        if not tool_mode and json_schema is None:
            parsed = directives.extract(result.text)
            if employee.has_tool(TOOL_MEMORY):
                for fact in parsed.memories:
                    await ctx.gateway.add_memory(fact, employee.id)
            result.text = parsed.text
            if may_ask and parsed.question:
                result.question = parsed.question
        return result

    def _may_ask(self, claim: Claim) -> bool:
        return len(claim.human_answers) < MAX_QUESTIONS_PER_TASK

    def _name(self, agent_id: str) -> str:
        e = self.ctx.roster.get(agent_id)
        return e.name if e else agent_id

    # ---- main agent: plan ----

    async def _plan(self, manager: Employee, claim: Claim) -> None:
        gw, task = self.ctx.gateway, claim.task
        team = self.ctx.roster.team_of(manager)
        await gw.log(manager.id, task["id"], f"{manager.name} is planning “{task['title']}”.")
        result = await self._run(manager, claim, prompts.plan_prompt(task, team, claim.human_answers, MAX_SUBTASKS),
                                 json_schema=prompts.PLAN_SCHEMA)
        plan = parse_plan(result.text, [e.id for e in team])

        if plan.question and not plan.subtasks and self._may_ask(claim):
            await gw.ask(task["id"], manager.id, plan.question)
            await gw.log(manager.id, task["id"], f"{manager.name} needs your input before starting.")
            return
        if not plan.subtasks:  # nothing usable: hand the whole job to the best-matching teammate
            plan.subtasks = [{"title": task["title"], "description": task.get("description", ""),
                              "assignee": team[0].id, "depends_on": []}]

        await gw.delegate(task["id"], plan.subtasks, {
            "title": f"Wrap up: {task['title']}"[:300],
            "description": "Merge the team's results into the final deliverable for the owner.",
            "assignee": manager.id,
        })
        who = ", ".join(sorted({self._name(s["assignee"]) for s in plan.subtasks}))
        await gw.log(manager.id, task["id"],
                     f"{manager.name} split “{task['title']}” into {len(plan.subtasks)} task(s) for {who}.")

    # ---- main agent: wrap up ----

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

    # ---- any agent: do the work ----

    async def _work(self, employee: Employee, claim: Claim) -> None:
        gw, task = self.ctx.gateway, claim.task
        may_ask = self._may_ask(claim)
        prompt = prompts.work_prompt(task, claim.parent, claim.dependency_results, claim.human_answers, may_ask)
        result = await self._run(employee, claim, prompt, may_ask=may_ask)
        if result.question:
            await gw.ask(task["id"], employee.id, result.question)
            await gw.log(employee.id, task["id"], f"{employee.name} asked you a question.")
            return
        text = result.text or "(no written output; see the workspace files)"
        if task.get("kind") == "assignment":  # done alone: list the files like a wrap-up would
            files = Workspace(self._workdir(claim)).files()
            if files:
                text += "\n\n---\n**Files in this assignment's workspace:** " + ", ".join(f"`{f}`" for f in files)
        await gw.complete(task["id"], text)
        await gw.log(employee.id, task["id"], f"{employee.name} finished “{task['title']}”.")

    # ---- any agent: chat ----

    async def _chat(self, employee: Employee, claim: Claim) -> None:
        gw, task = self.ctx.gateway, claim.task
        history = [Turn(m.get("role", "owner"), m.get("content", "")) for m in claim.history
                   if m.get("status", "done") == "done"]
        workspace = Workspace(self._workdir(claim))
        before = set(workspace.files())

        async def on_event(kind: str, text: str) -> None:
            if kind == "status":
                await gw.progress(task["id"], status=text)
            else:
                await gw.progress(task["id"], text=text)

        result = await self._run(employee, claim, task.get("description") or task["title"], mode=prompts.MODE_CHAT,
                                 history=history, on_event=on_event)
        text = result.text or result.question or "(I have nothing to add.)"
        new_files = sorted(set(workspace.files()) - before)
        if new_files:
            text += "\n\n---\n**Files saved:** " + ", ".join(f"`{f}`" for f in new_files)
        if result.warnings:
            text += "\n\n> " + " ".join(result.warnings)
        await gw.complete(task["id"], text)

    # ---- teammates ----

    async def _consult(self, requester: Employee, teammate_id: str, request: str, claim: Claim) -> str:
        teammate = self.ctx.roster.get(teammate_id)
        if teammate is None:
            return f"ERROR: {teammate_id} is not available right now."
        gw, task_id = self.ctx.gateway, claim.task["id"]
        await gw.log(teammate.id, task_id, f"{requester.name} asked {teammate.name} for help.")
        try:
            async with self.status.working(teammate.id, task_id):
                result = await self._run(teammate, claim,
                                         prompts.consult_prompt(requester, request, claim.parent or claim.task),
                                         mode=prompts.MODE_CONSULT)
        except Exception as exc:  # the requester can carry on without the help
            log.warning("consultation of %s failed", teammate.id, exc_info=True)
            return f"ERROR: {teammate.name} could not help ({type(exc).__name__}: {exc})."[:1000]
        return result.text or "(no answer)"


def parse_plan(text: str, valid_assignees: list[str] | set[str]) -> Plan:
    """Parse and sanitise the manager's plan. Never trusts the model's ids or indices blindly.
    Unknown assignees go to the first valid one (in team order)."""
    valid_assignees = sorted(valid_assignees) if isinstance(valid_assignees, set) else list(valid_assignees)
    try:
        data = json.loads(_strip_fences(text))
    except json.JSONDecodeError:
        log.warning("manager returned invalid plan JSON; falling back")
        return Plan()
    if not isinstance(data, dict):
        return Plan()

    fallback = valid_assignees[0] if valid_assignees else ""
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

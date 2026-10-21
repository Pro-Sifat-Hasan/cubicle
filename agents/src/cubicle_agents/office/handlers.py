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

import asyncio
import json
import logging
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from cubicle_agents.core.gateway import Claim, Gateway
from cubicle_agents.llm.base import LLM, AgentResult, EventCallback, Turn
from cubicle_agents.office import directives
from cubicle_agents.office.status import StatusBoard
from cubicle_agents.office.steps import StepReporter
from cubicle_agents.staff import prompts
from cubicle_agents.staff.memory import MEMORY_FETCH, is_duplicate, looks_secret, select_memories, trim_history
from cubicle_agents.staff.projects import Project, match_projects, projects_block
from cubicle_agents.staff.roster import TOOL_MEMORY, TOOL_WEB, Employee, Roster
from cubicle_agents.tools import build_toolbox
from cubicle_agents.tools.base import TOOL_PREFIX
from cubicle_agents.tools.workspace import Workspace

log = logging.getLogger(__name__)

MAX_SUBTASKS = 6
MAX_QUESTIONS_PER_TASK = 2
MAX_APPROVALS_PER_TASK = 10
MAX_TOOL_LOGS = 40  # tool calls recorded per run
TERMINAL_STATUS = "Running a command…"  # run_command logs the command itself


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
        if kind == "persona":
            await self._persona(employee, claim)
        elif kind == "chat":
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

    async def _memories(self, query: str) -> tuple[str, list[str], int]:
        """The owner's profile, the facts that fit this request (not all of them: that costs tokens and buries what
        matters), and how many facts are stored."""
        try:
            facts = await self.ctx.gateway.list_memories(MEMORY_FETCH)
            return await self.ctx.gateway.get_profile(), select_memories(facts, query), len(facts)
        except Exception:  # memory is helpful context, never a reason to fail work
            log.warning("could not load office memory", exc_info=True)
            return "", [], 0

    async def _projects(self) -> list[Project]:
        try:
            return [Project(p["id"], p["name"], p.get("path") or "", p.get("notes") or "")
                    for p in await self.ctx.gateway.list_projects()]
        except Exception:  # project notes are helpful context, never a reason to fail work
            log.warning("could not load projects", exc_info=True)
            return []

    async def _run(self, employee: Employee, claim: Claim, prompt: str, *,
                   json_schema: dict[str, Any] | None = None, may_ask: bool = False, mode: str = prompts.MODE_TASK,
                   history: list[Turn] | None = None, on_event: EventCallback | None = None) -> AgentResult:
        ctx = self.ctx
        workdir = self._workdir(claim)
        # A request that names one of the owner's projects is worked on in that project's folder.
        recent = " ".join(t.content for t in (history or [])[-2:])
        projects = await self._projects()
        matched = match_projects(projects, prompt[:3000], recent[:1500]) if json_schema is None else []
        folder = next((p.folder for p in matched if p.folder), None)
        if folder is not None:
            workdir = folder
        tool_mode = ctx.llm.supports_tools
        team = ctx.roster.team_of(employee) if mode != prompts.MODE_CONSULT and json_schema is None else []
        coordinator = employee.is_manager and bool(team)  # a main agent with a team hands building to teammates
        toolbox = None
        if tool_mode and json_schema is None:
            async def consult(teammate_id: str, request: str) -> str:
                return await self._consult(employee, teammate_id, request, claim)

            toolbox = build_toolbox(
                employee=employee, workspace=workdir, gateway=ctx.gateway, may_ask=may_ask,
                may_approve=mode != prompts.MODE_CONSULT and self._may_approve(claim), task_id=claim.task["id"],
                approved_actions=self._approved_actions(claim),
                client_web_fetch=ctx.client_web_fetch, web_access=ctx.web_access,
                team=team, consult=consult if team else None, allow_assignments=mode == prompts.MODE_CHAT,
                read_only_files=coordinator,
            )
        if on_event is None and json_schema is None:
            on_event = self._tool_logger(employee, claim)
        profile, memories, memory_count = await self._memories(prompt[:3000])
        system = prompts.system_prompt(employee, ctx.owner, memories=memories, tool_mode=tool_mode,
                                       web=ctx.web_access, team=team, mode=mode, coordinator=coordinator,
                                       native_files=getattr(ctx.llm, "native_file_tools", False),
                                       projects=projects_block(projects, matched, ctx.owner) if json_schema is None else "",
                                       profile=profile, memory_count=memory_count)
        reporter = StepReporter(ctx.gateway, claim.task["id"], employee.id) if json_schema is None else None
        result = await ctx.llm.run(
            system=system, prompt=prompt, model=employee.model or ctx.default_model, toolbox=toolbox,
            json_schema=json_schema, workdir=workdir, history=history,
            mcp=employee.mcp if json_schema is None else (), effort=employee.effort, on_event=on_event,
            web=ctx.web_access and employee.has_tool(TOOL_WEB),
            on_step=reporter.callback if reporter else None,
        )
        await ctx.gateway.report_usage(claim.task["id"], employee.id, result.input_tokens, result.output_tokens)
        for warning in result.warnings or []:
            await ctx.gateway.log(employee.id, claim.task["id"], f"{employee.name}: {warning}")

        if not tool_mode and json_schema is None:
            parsed = directives.extract(result.text)
            if employee.has_tool(TOOL_MEMORY):
                known = await ctx.gateway.list_memories(MEMORY_FETCH) if parsed.memories else []
                for fact in parsed.memories:
                    if not looks_secret(fact) and not is_duplicate(fact, known):
                        await ctx.gateway.add_memory(fact, employee.id)
                        known.append(fact)
                if parsed.profile and not looks_secret(parsed.profile):
                    await ctx.gateway.set_profile(parsed.profile, employee.id)
            result.text = parsed.text
            if (may_ask or parsed.question_kind == "approval") and parsed.question:
                result.question, result.question_kind = parsed.question, parsed.question_kind
        return result

    def _tool_logger(self, employee: Employee, claim: Claim) -> EventCallback:
        """Records the tools an agent uses as activity, so the owner can see how the work was done."""
        last = ""
        count = 0

        async def on_event(kind: str, text: str) -> None:
            nonlocal last, count
            if kind != "status" or text == last or count >= MAX_TOOL_LOGS or text == TERMINAL_STATUS:
                return
            last, count = text, count + 1
            await self.ctx.gateway.log(employee.id, claim.task["id"], f"{TOOL_PREFIX}{text}")

        return on_event

    def _may_ask(self, claim: Claim) -> bool:
        asked = [a for a in claim.human_answers if a.get("kind") != "approval"]
        return len(asked) < MAX_QUESTIONS_PER_TASK

    @staticmethod
    def _approved_actions(claim: Claim) -> tuple[str, ...]:
        """What the owner approved on this task so far (the agent's own wording of each request)."""
        return tuple(a.get("question", "") for a in claim.human_answers
                     if a.get("kind") == "approval" and str(a.get("answer", "")).startswith("Approved"))

    def _may_approve(self, claim: Claim) -> bool:
        approvals = [a for a in claim.human_answers if a.get("kind") == "approval"]
        return len(approvals) < MAX_APPROVALS_PER_TASK

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
            await self._ask_owner(employee, task["id"], result)
            return
        text = result.text or "(no written output; see the workspace files)"
        if task.get("kind") == "assignment":  # done alone: list the files like a wrap-up would
            files = Workspace(self._workdir(claim)).files()
            if files:
                text += "\n\n---\n**Files in this assignment's workspace:** " + ", ".join(f"`{f}`" for f in files)
        await gw.complete(task["id"], text)
        await gw.log(employee.id, task["id"], f"{employee.name} finished “{task['title']}”.")

    # ---- onboarding: a persona for a custom passion ----

    async def _persona(self, employee: Employee, claim: Claim) -> None:
        passion = (claim.task.get("description") or "").strip()[:200]
        result = await self._run(employee, claim, prompts.persona_prompt(passion, employee.name),
                                 json_schema=prompts.PERSONA_SCHEMA)
        await self.ctx.gateway.complete(claim.task["id"], result.text)

    # ---- any agent: chat ----

    async def _chat(self, employee: Employee, claim: Claim) -> None:
        gw, task = self.ctx.gateway, claim.task
        history = trim_history([Turn(m.get("role", "owner"), m.get("content", "")) for m in claim.history
                                if m.get("status", "done") == "done"])
        workspace = Workspace(self._workdir(claim))
        before = set(workspace.files())

        log_tool = self._tool_logger(employee, claim)

        async def on_event(kind: str, text: str) -> None:
            if kind == "status":
                await gw.progress(task["id"], status=text)
                await log_tool(kind, text)  # kept in the reply's trail: "how it was done"
            else:
                await gw.progress(task["id"], text=text)

        may_ask = self._may_ask(claim)
        message = task.get("description") or task["title"]
        context = prompts.chat_context(claim.human_answers)
        prompt = f"{context}\n\n---\n\n{message}" if context else message
        # The first message of a chat names it, while the reply is being written.
        naming = asyncio.create_task(self._name_chat(employee, claim, message)) if claim.title_pending else None
        try:
            result = await self._run(employee, claim, prompt, mode=prompts.MODE_CHAT, may_ask=may_ask,
                                     history=history, on_event=on_event)
        finally:
            if naming is not None:
                await asyncio.gather(naming, return_exceptions=True)  # never left running, never fails the reply
        if result.question:  # the reply waits for the owner; their answer resumes this same reply
            await self._ask_owner(employee, task["id"], result)
            return
        text = result.text or "(I have nothing to add.)"
        new_files = sorted(set(workspace.files()) - before)
        if new_files:
            text += "\n\n---\n**Files saved:** " + ", ".join(f"`{f}`" for f in new_files)
        if result.warnings:
            text += "\n\n> " + " ".join(result.warnings)
        await gw.complete(task["id"], text)

    async def _name_chat(self, employee: Employee, claim: Claim, message: str) -> None:
        """Have the model title a new chat from its first message; fall back to the start of the message."""
        conversation_id = claim.task.get("conversation_id") or ""
        if not conversation_id:
            return
        title = ""
        try:
            result = await self._run(employee, claim, prompts.title_prompt(message), json_schema=prompts.TITLE_SCHEMA)
            title = prompts.clean_title(json.loads(result.text).get("title", ""))
        except Exception:  # a title is a nicety: a plain one is better than none
            log.warning("could not generate a chat title", exc_info=True)
        title = title or prompts.clean_title("", message)
        if title:
            try:
                await self.ctx.gateway.set_conversation_title(conversation_id, title)
            except Exception:
                log.debug("could not set the chat title", exc_info=True)

    async def _ask_owner(self, employee: Employee, task_id: str, result: AgentResult) -> None:
        """Park the task and put the question, or the approval request, in the owner's inbox."""
        gw = self.ctx.gateway
        assert result.question is not None
        await gw.ask(task_id, employee.id, result.question, result.question_kind)
        what = "asked you for approval" if result.question_kind == "approval" else "asked you a question"
        await gw.log(employee.id, task_id, f"{employee.name} {what}.")

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

"""In-memory test doubles for the gateway and the LLM."""

from __future__ import annotations

import asyncio
from pathlib import Path
from typing import Any

from cubicle_agents.core.gateway import Claim, RosterUpdate
from cubicle_agents.llm.base import AgentResult
from cubicle_agents.office.handlers import OfficeContext, TaskHandler
from cubicle_agents.staff.roster import DEFAULT_ROSTER, Employee, Roster


class FakeGateway:
    def __init__(self, memories: list[str] | None = None) -> None:
        self.calls: list[tuple[str, Any]] = []
        self.delegations: list[dict[str, Any]] = []
        self.completed: dict[str, str] = {}
        self.failed: dict[str, str] = {}
        self.questions: list[tuple[str, str, str]] = []
        self.usage: list[tuple[str, str, int, int]] = []
        self.memories: list[str] = list(memories or [])
        self.projects: list[dict[str, Any]] = []
        self.profile = ""
        self.triage: dict[str, dict[str, Any]] = {}
        self.progress_events: list[tuple[str, str, str]] = []
        self.steps: list[tuple[str, str, dict[str, Any]]] = []
        self.titles: dict[str, str] = {}
        self.assignments: list[tuple[str, str, str]] = []
        self.logs: list[str] = []
        self.question_kinds: list[str] = []
        self.assignment_sources: list[str] = []
        self.started = 0
        self.updates: list[RosterUpdate | None] = []

    async def start_worker(self):
        self.started += 1

    async def roster(self, after=None):
        return self.updates.pop(0) if self.updates else None

    async def set_status(self, agent_id, status, task_id=""):
        self.calls.append(("status", (agent_id, status, task_id)))

    async def claim(self, agent_ids, lane):
        await asyncio.sleep(0.05)  # the real claim long-polls; never spin
        return None

    async def delegate(self, task_id, subtasks, synthesis):
        self.delegations.append({"task_id": task_id, "subtasks": subtasks, "synthesis": synthesis})

    async def complete(self, task_id, result):
        self.completed[task_id] = result

    async def fail(self, task_id, error):
        self.failed[task_id] = error

    async def ask(self, task_id, agent_id, question, kind="question"):
        self.questions.append((task_id, agent_id, question))
        self.question_kinds.append(kind)

    async def progress(self, task_id, status="", text=""):
        self.progress_events.append((task_id, status, text))

    async def work_log(self, period):
        return f"# Work log: {period}\n- Assignments: 1"

    async def set_conversation_title(self, conversation_id, title):
        self.titles.setdefault(conversation_id, title)

    async def save_step(self, task_id, agent_id, step):
        self.steps.append((task_id, agent_id, dict(step)))

    async def create_assignment(self, title, description, agent_id, source_task_id=""):
        self.assignments.append((title, description, agent_id))
        self.assignment_sources.append(source_task_id)
        return {"id": "asg_new"}

    async def report_usage(self, task_id, agent_id, input_tokens, output_tokens):
        self.usage.append((task_id, agent_id, input_tokens, output_tokens))

    async def log(self, agent_id, task_id, message):
        self.calls.append(("log", message))
        self.logs.append(message)

    async def forget_memory(self, fact):
        for m in list(self.memories):
            if fact.lower() in m.lower():
                self.memories.remove(m)
                return True
        return False

    async def list_memories(self, limit=50):
        return self.memories[:limit]

    async def add_memory(self, content, agent_id):
        self.memories.append(content)

    async def triage_new(self, keys):
        return [k for k in keys if k not in self.triage]

    async def triage_record(self, items):
        for item in items:
            self.triage[item["key"]] = item

    async def get_profile(self):
        return self.profile

    async def set_profile(self, content, agent_id):
        self.profile = content

    async def list_projects(self):
        return list(self.projects)

    async def save_project(self, name, path="", notes=""):
        found = next((p for p in self.projects if p["name"].lower() == name.lower()), None)
        if found is None:
            found = {"id": f"prj_{len(self.projects) + 1}", "name": name, "path": "", "notes": ""}
            self.projects.append(found)
        found["path"] = path or found["path"]
        found["notes"] = notes or found["notes"]
        return found


class ScriptedLLM:
    """Returns queued AgentResults (or strings) in order and records every call.

    A response may also be a callable taking the call's kwargs, for tests that
    exercise tools (e.g. a teammate consultation) during the run.
    """

    name = "scripted"

    def __init__(self, *responses: Any, supports_tools: bool = False) -> None:
        self.responses = list(responses)
        self.supports_tools = supports_tools
        self.calls: list[dict[str, Any]] = []

    async def run(self, **kwargs):
        self.calls.append(kwargs)
        response = self.responses.pop(0)
        if callable(response):
            response = await response(kwargs)
        return response if isinstance(response, AgentResult) else AgentResult(response, 10, 5)

    async def list_models(self):
        return ["scripted-1"]


class ExplodingLLM:
    name = "exploding"
    supports_tools = False

    async def run(self, **_):
        raise RuntimeError("model unavailable")

    async def list_models(self):
        return []


def make_handler(gateway, llm, workspace: Path, roster: list[Employee] | None = None, **overrides) -> TaskHandler:
    ctx = OfficeContext(gateway=gateway, llm=llm, roster=Roster(list(roster or DEFAULT_ROSTER)),
                        default_model="test-model", owner="Sam", workspace_root=workspace, **overrides)
    return TaskHandler(ctx)


def claim(task_id="tsk_1", kind="subtask", title="Do the thing", parent_id="asg_1", **task) -> Claim:
    return Claim(task={"id": task_id, "kind": kind, "title": title, "description": "details", "parent_id": parent_id,
                       **task})

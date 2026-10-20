"""In-memory test doubles for the gateway and the LLM."""

from __future__ import annotations

from pathlib import Path
from typing import Any

from cubicle_agents.core.gateway import Claim
from cubicle_agents.llm.base import AgentResult
from cubicle_agents.office.handlers import OfficeContext, TaskHandler
from cubicle_agents.staff.roster import DEFAULT_ROSTER


class FakeGateway:
    def __init__(self, memories: list[str] | None = None) -> None:
        self.calls: list[tuple[str, Any]] = []
        self.delegations: list[dict[str, Any]] = []
        self.completed: dict[str, str] = {}
        self.failed: dict[str, str] = {}
        self.questions: list[tuple[str, str, str]] = []
        self.usage: list[tuple[str, str, int, int]] = []
        self.employees: list[dict[str, Any]] = []
        self.memories: list[str] = list(memories or [])

    async def sync_employees(self, employees):
        self.employees = employees

    async def set_status(self, employee_id, status, task_id=""):
        self.calls.append(("status", (employee_id, status, task_id)))

    async def claim(self, employee_ids):
        return None

    async def delegate(self, task_id, subtasks, synthesis):
        self.delegations.append({"task_id": task_id, "subtasks": subtasks, "synthesis": synthesis})

    async def complete(self, task_id, result):
        self.completed[task_id] = result

    async def fail(self, task_id, error):
        self.failed[task_id] = error

    async def ask(self, task_id, employee_id, question):
        self.questions.append((task_id, employee_id, question))

    async def report_usage(self, task_id, employee_id, input_tokens, output_tokens):
        self.usage.append((task_id, employee_id, input_tokens, output_tokens))

    async def log(self, employee_id, task_id, message):
        self.calls.append(("log", message))

    async def list_memories(self, limit=50):
        return self.memories[:limit]

    async def add_memory(self, content, employee_id):
        self.memories.append(content)


class ScriptedLLM:
    """Returns queued AgentResults (or strings) in order and records every call."""

    name = "scripted"

    def __init__(self, *responses: str | AgentResult, supports_tools: bool = False) -> None:
        self.responses = list(responses)
        self.supports_tools = supports_tools
        self.calls: list[dict[str, Any]] = []

    async def run(self, *, system, prompt, model, toolbox=None, json_schema=None, workdir=None):
        self.calls.append({"system": system, "prompt": prompt, "model": model, "toolbox": toolbox,
                           "json_schema": json_schema, "workdir": workdir})
        response = self.responses.pop(0)
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


def make_handler(gateway, llm, workspace: Path, **overrides) -> TaskHandler:
    ctx = OfficeContext(gateway=gateway, llm=llm, roster=list(DEFAULT_ROSTER), default_model="test-model",
                        owner="Sam", workspace_root=workspace, **overrides)
    return TaskHandler(ctx)


def claim(task_id="tsk_1", kind="subtask", title="Do the thing", parent_id="asg_1") -> Claim:
    return Claim(task={"id": task_id, "kind": kind, "title": title, "description": "details", "parent_id": parent_id})

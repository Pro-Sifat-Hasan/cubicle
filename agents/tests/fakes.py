"""In-memory test doubles for the gateway and the LLM."""

from __future__ import annotations

from typing import Any

from cubicle_agents.core.gateway import Claim
from cubicle_agents.llm.base import Completion


class FakeGateway:
    def __init__(self) -> None:
        self.calls: list[tuple[str, Any]] = []
        self.delegations: list[dict[str, Any]] = []
        self.completed: dict[str, str] = {}
        self.failed: dict[str, str] = {}
        self.questions: list[tuple[str, str, str]] = []
        self.usage: list[tuple[str, str, int, int]] = []
        self.employees: list[dict[str, Any]] = []

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


class ScriptedLLM:
    """Returns queued responses in order and records every prompt."""

    name = "scripted"

    def __init__(self, *responses: str) -> None:
        self.responses = list(responses)
        self.prompts: list[dict[str, Any]] = []

    async def complete(self, *, system, prompt, model, json_schema=None):
        self.prompts.append({"system": system, "prompt": prompt, "model": model, "json_schema": json_schema})
        return Completion(text=self.responses.pop(0), input_tokens=10, output_tokens=5)


class ExplodingLLM:
    name = "exploding"

    async def complete(self, **_):
        raise RuntimeError("model unavailable")


def claim(task_id="tsk_1", kind="subtask", title="Do the thing") -> Claim:
    return Claim(task={"id": task_id, "kind": kind, "title": title, "description": "details"})

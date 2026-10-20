"""Async client for the gateway's worker API (/internal/v1)."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Protocol

import httpx


class BreakerOpenError(Exception):
    """The gateway's daily token budget is exhausted; work is paused."""


class TaskConflictError(Exception):
    """The task changed state underneath us (e.g. the owner cancelled it)."""


@dataclass
class Claim:
    """A task plus everything the employee needs to work on it."""

    task: dict[str, Any]
    parent: dict[str, Any] | None = None
    dependency_results: list[dict[str, Any]] = field(default_factory=list)
    human_answers: list[dict[str, Any]] = field(default_factory=list)

    @classmethod
    def from_json(cls, data: dict[str, Any]) -> Claim:
        return cls(
            task=data["task"],
            parent=data.get("parent"),
            dependency_results=data.get("dependency_results") or [],
            human_answers=data.get("human_answers") or [],
        )


class Gateway(Protocol):
    """What the office needs from the gateway; implemented by GatewayClient and test fakes."""

    async def sync_employees(self, employees: list[dict[str, Any]]) -> None: ...
    async def set_status(self, employee_id: str, status: str, task_id: str = "") -> None: ...
    async def claim(self, employee_ids: list[str]) -> Claim | None: ...
    async def delegate(self, task_id: str, subtasks: list[dict[str, Any]], synthesis: dict[str, Any]) -> None: ...
    async def complete(self, task_id: str, result: str) -> None: ...
    async def fail(self, task_id: str, error: str) -> None: ...
    async def ask(self, task_id: str, employee_id: str, question: str) -> None: ...
    async def report_usage(self, task_id: str, employee_id: str, input_tokens: int, output_tokens: int) -> None: ...
    async def log(self, employee_id: str, task_id: str, message: str) -> None: ...
    async def list_memories(self, limit: int = 50) -> list[str]: ...
    async def add_memory(self, content: str, employee_id: str) -> None: ...


class GatewayClient:
    def __init__(self, base_url: str, token: str) -> None:
        self._http = httpx.AsyncClient(
            base_url=base_url,
            headers={"Authorization": f"Bearer {token}", "User-Agent": "cubicle-agents"},
            # Claims long-poll for up to 25s on the server.
            timeout=httpx.Timeout(10.0, read=40.0),
        )

    async def aclose(self) -> None:
        await self._http.aclose()

    async def _send(self, method: str, path: str, body: Any = None) -> httpx.Response:
        resp = await self._http.request(method, path, json=body)
        if resp.status_code == 409:
            raise TaskConflictError(resp.text)
        if resp.status_code == 503:
            raise BreakerOpenError(resp.text)
        resp.raise_for_status()
        return resp

    async def sync_employees(self, employees: list[dict[str, Any]]) -> None:
        await self._send("PUT", "/internal/v1/employees", employees)

    async def set_status(self, employee_id: str, status: str, task_id: str = "") -> None:
        await self._send("PATCH", f"/internal/v1/employees/{employee_id}",
                         {"status": status, "current_task_id": task_id})

    async def claim(self, employee_ids: list[str]) -> Claim | None:
        resp = await self._send("POST", "/internal/v1/tasks/claim", {"employee_ids": employee_ids})
        if resp.status_code == 204:
            return None
        return Claim.from_json(resp.json())

    async def delegate(self, task_id: str, subtasks: list[dict[str, Any]], synthesis: dict[str, Any]) -> None:
        await self._send("POST", f"/internal/v1/tasks/{task_id}/delegate",
                         {"subtasks": subtasks, "synthesis": synthesis})

    async def complete(self, task_id: str, result: str) -> None:
        await self._send("POST", f"/internal/v1/tasks/{task_id}/complete", {"result": result})

    async def fail(self, task_id: str, error: str) -> None:
        await self._send("POST", f"/internal/v1/tasks/{task_id}/fail", {"error": error})

    async def ask(self, task_id: str, employee_id: str, question: str) -> None:
        await self._send("POST", f"/internal/v1/tasks/{task_id}/ask",
                         {"employee_id": employee_id, "question": question})

    async def report_usage(self, task_id: str, employee_id: str, input_tokens: int, output_tokens: int) -> None:
        await self._send("POST", "/internal/v1/usage", {
            "task_id": task_id, "employee_id": employee_id,
            "input_tokens": input_tokens, "output_tokens": output_tokens,
        })

    async def log(self, employee_id: str, task_id: str, message: str) -> None:
        await self._send("POST", "/internal/v1/activity",
                         {"employee_id": employee_id, "task_id": task_id, "message": message[:2000]})

    async def list_memories(self, limit: int = 50) -> list[str]:
        resp = await self._send("GET", f"/internal/v1/memories?limit={limit}")
        return [m["content"] for m in resp.json()]

    async def add_memory(self, content: str, employee_id: str) -> None:
        await self._send("POST", "/internal/v1/memories", {"content": content[:1000], "employee_id": employee_id})

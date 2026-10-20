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
    """A task plus everything the agent needs to work on it."""

    task: dict[str, Any]
    parent: dict[str, Any] | None = None
    dependency_results: list[dict[str, Any]] = field(default_factory=list)
    human_answers: list[dict[str, Any]] = field(default_factory=list)
    # Earlier messages of the conversation, for chat tasks (oldest first).
    history: list[dict[str, Any]] = field(default_factory=list)

    @classmethod
    def from_json(cls, data: dict[str, Any]) -> Claim:
        return cls(
            task=data["task"],
            parent=data.get("parent"),
            dependency_results=data.get("dependency_results") or [],
            human_answers=data.get("human_answers") or [],
            history=data.get("history") or [],
        )


@dataclass(frozen=True)
class RosterUpdate:
    revision: int
    agents: list[dict[str, Any]]


class Gateway(Protocol):
    """What the office needs from the gateway; implemented by GatewayClient and test fakes."""

    async def start_worker(self) -> None: ...
    async def roster(self, after: int | None = None) -> RosterUpdate | None: ...
    async def set_status(self, agent_id: str, status: str, task_id: str = "") -> None: ...
    async def claim(self, agent_ids: list[str], lane: str) -> Claim | None: ...
    async def delegate(self, task_id: str, subtasks: list[dict[str, Any]], synthesis: dict[str, Any]) -> None: ...
    async def complete(self, task_id: str, result: str) -> None: ...
    async def fail(self, task_id: str, error: str) -> None: ...
    async def ask(self, task_id: str, agent_id: str, question: str) -> None: ...
    async def progress(self, task_id: str, status: str = "", text: str = "") -> None: ...
    async def create_assignment(self, title: str, description: str, agent_id: str) -> dict[str, Any]: ...
    async def report_usage(self, task_id: str, agent_id: str, input_tokens: int, output_tokens: int) -> None: ...
    async def log(self, agent_id: str, task_id: str, message: str) -> None: ...
    async def list_memories(self, limit: int = 50) -> list[str]: ...
    async def add_memory(self, content: str, agent_id: str) -> None: ...


class GatewayClient:
    def __init__(self, base_url: str, token: str) -> None:
        self._http = httpx.AsyncClient(
            base_url=base_url,
            headers={"Authorization": f"Bearer {token}", "User-Agent": "cubicle-agents"},
            # Claims and roster updates long-poll for up to 25s on the server.
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

    async def start_worker(self) -> None:
        await self._send("POST", "/internal/v1/worker/start")

    async def roster(self, after: int | None = None) -> RosterUpdate | None:
        """The agents and their configuration. With `after`, waits for a change (None = no change yet)."""
        path = "/internal/v1/agents" if after is None else f"/internal/v1/agents?after={after}"
        resp = await self._send("GET", path)
        if resp.status_code == 204:
            return None
        data = resp.json()
        return RosterUpdate(int(data["revision"]), data.get("agents") or [])

    async def set_status(self, agent_id: str, status: str, task_id: str = "") -> None:
        await self._send("PATCH", f"/internal/v1/agents/{agent_id}/status", {"status": status, "current_task_id": task_id})

    async def claim(self, agent_ids: list[str], lane: str) -> Claim | None:
        resp = await self._send("POST", "/internal/v1/tasks/claim", {"agent_ids": agent_ids, "lane": lane})
        if resp.status_code == 204:
            return None
        return Claim.from_json(resp.json())

    async def delegate(self, task_id: str, subtasks: list[dict[str, Any]], synthesis: dict[str, Any]) -> None:
        await self._send("POST", f"/internal/v1/tasks/{task_id}/delegate", {"subtasks": subtasks, "synthesis": synthesis})

    async def complete(self, task_id: str, result: str) -> None:
        await self._send("POST", f"/internal/v1/tasks/{task_id}/complete", {"result": result})

    async def fail(self, task_id: str, error: str) -> None:
        await self._send("POST", f"/internal/v1/tasks/{task_id}/fail", {"error": error})

    async def ask(self, task_id: str, agent_id: str, question: str) -> None:
        await self._send("POST", f"/internal/v1/tasks/{task_id}/ask", {"employee_id": agent_id, "question": question})

    async def progress(self, task_id: str, status: str = "", text: str = "") -> None:
        await self._send("POST", f"/internal/v1/tasks/{task_id}/progress", {"status": status[:300], "text": text})

    async def create_assignment(self, title: str, description: str, agent_id: str) -> dict[str, Any]:
        resp = await self._send("POST", "/internal/v1/assignments",
                                {"title": title[:300], "description": description[:20000], "agent_id": agent_id})
        return resp.json()

    async def report_usage(self, task_id: str, agent_id: str, input_tokens: int, output_tokens: int) -> None:
        await self._send("POST", "/internal/v1/usage", {
            "task_id": task_id, "employee_id": agent_id,
            "input_tokens": input_tokens, "output_tokens": output_tokens,
        })

    async def log(self, agent_id: str, task_id: str, message: str) -> None:
        await self._send("POST", "/internal/v1/activity",
                         {"employee_id": agent_id, "task_id": task_id, "message": message[:2000]})

    async def list_memories(self, limit: int = 50) -> list[str]:
        resp = await self._send("GET", f"/internal/v1/memories?limit={limit}")
        return [m["content"] for m in resp.json()]

    async def add_memory(self, content: str, agent_id: str) -> None:
        await self._send("POST", "/internal/v1/memories", {"content": content[:1000], "employee_id": agent_id})

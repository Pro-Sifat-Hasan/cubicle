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
    # A chat's first message: the chat has no title yet and the agent's model names it.
    title_pending: bool = False

    @classmethod
    def from_json(cls, data: dict[str, Any]) -> Claim:
        return cls(
            task=data["task"],
            parent=data.get("parent"),
            dependency_results=data.get("dependency_results") or [],
            human_answers=data.get("human_answers") or [],
            history=data.get("history") or [],
            title_pending=bool(data.get("title_pending")),
        )


@dataclass(frozen=True)
class RosterUpdate:
    revision: int
    agents: list[dict[str, Any]]


class Gateway(Protocol):
    """What the office needs from the gateway; implemented by GatewayClient and test fakes."""

    async def start_worker(self) -> None:
        """Tell the gateway this worker started."""

    async def roster(self, after: int | None = None) -> RosterUpdate | None:
        """The agent roster; with `after`, wait for a newer revision (None on timeout)."""

    async def set_status(self, agent_id: str, status: str, task_id: str = "") -> None:
        """Report what an agent is doing."""

    async def claim(self, agent_ids: list[str], lane: str) -> Claim | None:
        """Long-poll for the next runnable task for these agents."""

    async def delegate(self, task_id: str, subtasks: list[dict[str, Any]], synthesis: dict[str, Any]) -> None:
        """Split a task into subtasks plus a wrap-up."""

    async def complete(self, task_id: str, result: str) -> None:
        """Finish a task with its result."""

    async def fail(self, task_id: str, error: str) -> None:
        """Fail a task with a reason."""

    async def ask(self, task_id: str, agent_id: str, question: str, kind: str = "question") -> None:
        """Park a task and ask the owner a question, or for approval (kind=\"approval\")."""

    async def progress(self, task_id: str, status: str = "", text: str = "") -> None:
        """Stream live progress for a task."""

    async def create_assignment(self, title: str, description: str, agent_id: str,
                                source_task_id: str = "") -> dict[str, Any]:
        """Start a new assignment (from a chat reply, `source_task_id`)."""

    async def report_usage(self, task_id: str, agent_id: str, input_tokens: int, output_tokens: int) -> None:
        """Report the tokens a model call used."""

    async def save_step(self, task_id: str, agent_id: str, step: dict[str, Any]) -> None:
        """Record (or update, by id) something an agent did while working: a thought, a remark or a tool call."""

    async def set_conversation_title(self, conversation_id: str, title: str) -> None:
        """Name a chat that has no title yet (ignored when it already has one)."""

    async def work_log(self, period: str) -> str:
        """What the staff did in a period (today, yesterday, week, last_week, month), as readable text."""

    async def log(self, agent_id: str, task_id: str, message: str) -> None:
        """Add a line to the activity log."""

    async def list_memories(self, limit: int = 50) -> list[str]:
        """Facts the office remembers about the owner."""

    async def add_memory(self, content: str, agent_id: str) -> None:
        """Save a durable fact about the owner."""

    async def forget_memory(self, fact: str) -> bool:
        """Delete the remembered fact that best matches `fact`; False when nothing matches."""

    async def list_projects(self) -> list[dict[str, Any]]:
        """The projects the owner works on: id, name, path, notes."""

    async def save_project(self, name: str, path: str = "", notes: str = "") -> dict[str, Any]:
        """Add a project or update the one with this name; an empty path or notes keeps what is stored."""


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

    async def ask(self, task_id: str, agent_id: str, question: str, kind: str = "question") -> None:
        await self._send("POST", f"/internal/v1/tasks/{task_id}/ask",
                         {"employee_id": agent_id, "question": question, "kind": kind})

    async def progress(self, task_id: str, status: str = "", text: str = "") -> None:
        await self._send("POST", f"/internal/v1/tasks/{task_id}/progress", {"status": status[:300], "text": text})

    async def create_assignment(self, title: str, description: str, agent_id: str,
                                source_task_id: str = "") -> dict[str, Any]:
        resp = await self._send("POST", "/internal/v1/assignments",
                                {"title": title[:300], "description": description[:20000], "agent_id": agent_id,
                                 "source_task_id": source_task_id})
        return resp.json()

    async def report_usage(self, task_id: str, agent_id: str, input_tokens: int, output_tokens: int) -> None:
        await self._send("POST", "/internal/v1/usage", {
            "task_id": task_id, "employee_id": agent_id,
            "input_tokens": input_tokens, "output_tokens": output_tokens,
        })

    async def work_log(self, period: str) -> str:
        resp = await self._send("GET", f"/internal/v1/work-log?period={period}")
        return str(resp.json().get("markdown", ""))

    async def set_conversation_title(self, conversation_id: str, title: str) -> None:
        await self._send("PUT", f"/internal/v1/conversations/{conversation_id}/title", {"title": title[:200]})

    async def save_step(self, task_id: str, agent_id: str, step: dict[str, Any]) -> None:
        await self._send("POST", f"/internal/v1/tasks/{task_id}/steps", {"agent_id": agent_id, **step})

    async def log(self, agent_id: str, task_id: str, message: str) -> None:
        await self._send("POST", "/internal/v1/activity",
                         {"employee_id": agent_id, "task_id": task_id, "message": message[:2000]})

    async def list_memories(self, limit: int = 50) -> list[str]:
        resp = await self._send("GET", f"/internal/v1/memories?limit={limit}")
        return [m["content"] for m in resp.json()]

    async def add_memory(self, content: str, agent_id: str) -> None:
        await self._send("POST", "/internal/v1/memories", {"content": content[:1000], "employee_id": agent_id})

    async def forget_memory(self, fact: str) -> bool:
        wanted = " ".join(fact.lower().split())
        resp = await self._send("GET", "/internal/v1/memories?limit=300")
        items = [m for m in resp.json() if " ".join(m["content"].lower().split())]
        match = next((m for m in items if " ".join(m["content"].lower().split()) == wanted), None) or next(
            (m for m in items if wanted in " ".join(m["content"].lower().split())), None)
        if match is None:
            return False
        await self._send("DELETE", f"/internal/v1/memories/{match['id']}")
        return True

    async def list_projects(self) -> list[dict[str, Any]]:
        return (await self._send("GET", "/internal/v1/projects")).json()

    async def save_project(self, name: str, path: str = "", notes: str = "") -> dict[str, Any]:
        body = {"name": name[:60], "path": path[:500], "notes": notes[:4000]}
        return (await self._send("PUT", "/internal/v1/projects", body)).json()

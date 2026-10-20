"""Live agent status ("working" / "idle") shown in the app.

An agent can be busy in several ways at once (a task, a chat, helping a
teammate), so status is counted: it reads idle only when nothing is running.
"""

from __future__ import annotations

import logging
from collections import Counter
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager

from cubicle_agents.core.gateway import Gateway

log = logging.getLogger(__name__)


class StatusBoard:
    def __init__(self, gateway: Gateway) -> None:
        self._gateway = gateway
        self._busy: Counter[str] = Counter()

    @asynccontextmanager
    async def working(self, agent_id: str, task_id: str) -> AsyncIterator[None]:
        self._busy[agent_id] += 1
        await self._send(agent_id, "working", task_id)
        try:
            yield
        finally:
            self._busy[agent_id] -= 1
            if self._busy[agent_id] <= 0:
                del self._busy[agent_id]
                await self._send(agent_id, "idle", "")

    async def announce(self, agent_id: str) -> None:
        """Show the agent as available (its desk just opened), unless it is already busy."""
        if not self._busy[agent_id]:
            await self._send(agent_id, "idle", "")

    async def _send(self, agent_id: str, status: str, task_id: str) -> None:
        try:
            await self._gateway.set_status(agent_id, status, task_id)
        except Exception:  # status is cosmetic; never let it break work
            log.debug("status update failed", exc_info=True)

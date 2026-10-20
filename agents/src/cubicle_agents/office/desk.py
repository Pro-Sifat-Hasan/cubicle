"""A desk is one agent's work loop on one lane: claim -> work -> report, until stopped.

Every agent has two desks: one for tasks and one for chats, so a long task
never keeps the owner waiting for a reply.
"""

from __future__ import annotations

import asyncio
import logging

import httpx

from cubicle_agents.core.gateway import BreakerOpenError, Claim, TaskConflictError
from cubicle_agents.llm.base import LLMConfigError, LLMRefusalError
from cubicle_agents.office.handlers import TaskHandler
from cubicle_agents.staff.roster import Employee

log = logging.getLogger(__name__)

LANE_WORK = "work"
LANE_CHAT = "chat"
LANES = (LANE_WORK, LANE_CHAT)

BREAKER_PAUSE_S = 60.0
MAX_BACKOFF_S = 30.0


class Desk:
    def __init__(self, agent_id: str, lane: str, handler: TaskHandler) -> None:
        self.agent_id = agent_id
        self.lane = lane
        self.handler = handler
        self.stop = asyncio.Event()  # set when the agent is removed or turned off
        self._last: Employee | None = handler.ctx.roster.get(agent_id)

    @property
    def gateway(self):
        return self.handler.ctx.gateway

    def employee(self) -> Employee | None:
        """The agent's current configuration (it changes when the owner edits it). Work claimed just as
        the agent was removed is finished with its last known configuration."""
        current = self.handler.ctx.roster.get(self.agent_id)
        if current is not None:
            self._last = current
        return current or self._last

    async def run(self, office_stop: asyncio.Event) -> None:
        backoff = 1.0
        while not (office_stop.is_set() or self.stop.is_set()):
            try:
                claim = await self.gateway.claim([self.agent_id], self.lane)
                backoff = 1.0
            except BreakerOpenError:
                log.warning("%s: circuit breaker open, pausing", self.agent_id)
                await sleep_until(office_stop, BREAKER_PAUSE_S)
                continue
            except (httpx.HTTPError, OSError) as exc:
                log.warning("%s: gateway unreachable (%s); retrying in %.0fs", self.agent_id, exc, backoff)
                await sleep_until(office_stop, backoff)
                backoff = min(backoff * 2, MAX_BACKOFF_S)
                continue
            if claim is None:
                continue  # long-poll timed out with no work
            employee = self.employee()
            if employee is None:
                await self._fail(claim.task["id"], "This agent was removed or turned off.")
                return
            await self.work_on(employee, claim)

    async def work_on(self, employee: Employee, claim: Claim) -> None:
        task_id = claim.task["id"]
        log.info("%s picked up %s (%s)", employee.id, task_id, claim.task.get("title"))
        async with self.handler.status.working(employee.id, task_id):
            try:
                await self.handler.handle(employee, claim)
            except TaskConflictError:
                log.info("%s: task %s changed while working (probably cancelled)", employee.id, task_id)
            except (LLMRefusalError, LLMConfigError) as exc:
                await self._fail(task_id, str(exc))
            except Exception as exc:
                log.exception("%s: task %s failed", employee.id, task_id)
                await self._fail(task_id, f"{type(exc).__name__}: {exc}")

    async def _fail(self, task_id: str, error: str) -> None:
        try:
            await self.gateway.fail(task_id, error)
        except TaskConflictError:
            pass


async def sleep_until(stop: asyncio.Event, seconds: float) -> None:
    try:
        await asyncio.wait_for(stop.wait(), timeout=seconds)
    except TimeoutError:
        pass

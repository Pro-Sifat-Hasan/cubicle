"""A desk is one employee's work loop: claim -> work -> report, forever."""

from __future__ import annotations

import asyncio
import logging

import httpx

from cubicle_agents.core.gateway import BreakerOpenError, Claim, Gateway, TaskConflictError
from cubicle_agents.llm.base import LLMConfigError, LLMRefusalError
from cubicle_agents.office.handlers import TaskHandler
from cubicle_agents.staff.roster import Employee

log = logging.getLogger(__name__)

BREAKER_PAUSE_S = 60.0
MAX_BACKOFF_S = 30.0


class Desk:
    def __init__(self, employee: Employee, gateway: Gateway, handler: TaskHandler) -> None:
        self.employee = employee
        self.gateway = gateway
        self.handler = handler

    async def run(self, stop: asyncio.Event) -> None:
        backoff = 1.0
        while not stop.is_set():
            try:
                claim = await self.gateway.claim([self.employee.id])
                backoff = 1.0
            except BreakerOpenError:
                log.warning("%s: circuit breaker open, pausing", self.employee.id)
                await sleep_until(stop, BREAKER_PAUSE_S)
                continue
            except (httpx.HTTPError, OSError) as exc:
                log.warning("%s: gateway unreachable (%s); retrying in %.0fs", self.employee.id, exc, backoff)
                await sleep_until(stop, backoff)
                backoff = min(backoff * 2, MAX_BACKOFF_S)
                continue
            if claim is None:
                continue  # long-poll timed out with no work
            await self.work_on(claim)

    async def work_on(self, claim: Claim) -> None:
        task_id = claim.task["id"]
        emp = self.employee
        log.info("%s picked up %s (%s)", emp.id, task_id, claim.task.get("title"))
        await self._status("working", task_id)
        try:
            await self.handler.handle(emp, claim)
        except TaskConflictError:
            log.info("%s: task %s changed while working (probably cancelled)", emp.id, task_id)
        except (LLMRefusalError, LLMConfigError) as exc:
            await self._fail(task_id, str(exc))
        except Exception as exc:
            log.exception("%s: task %s failed", emp.id, task_id)
            await self._fail(task_id, f"{type(exc).__name__}: {exc}")
        finally:
            await self._status("idle")

    async def _status(self, status: str, task_id: str = "") -> None:
        try:
            await self.gateway.set_status(self.employee.id, status, task_id)
        except Exception:  # status is cosmetic; never let it break work
            log.debug("status update failed", exc_info=True)

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

"""Clocks the staff in, runs every desk concurrently, and follows roster changes.

The gateway owns the roster. The office long-polls it, so when the owner adds,
edits or removes an agent the change applies within moments: new agents get
desks, removed ones stop, and edits take effect from the next task.
"""

from __future__ import annotations

import asyncio
import logging

import httpx

from cubicle_agents.core.gateway import RosterUpdate
from cubicle_agents.office.desk import LANES, Desk, sleep_until
from cubicle_agents.office.handlers import OfficeContext, TaskHandler
from cubicle_agents.staff.roster import Employee

log = logging.getLogger(__name__)


class Office:
    def __init__(self, ctx: OfficeContext) -> None:
        self.ctx = ctx
        self.handler = TaskHandler(ctx)
        self._desks: dict[tuple[str, str], tuple[Desk, asyncio.Task[None]]] = {}
        self._stop = asyncio.Event()
        self._announcements: set[asyncio.Task[None]] = set()  # keeps "agent is available" updates alive

    async def clock_in(self, stop: asyncio.Event) -> int | None:
        """Tell the gateway we started and load the roster, retrying until it is reachable."""
        backoff = 1.0
        while not stop.is_set():
            try:
                await self.ctx.gateway.start_worker()
                update = await self.ctx.gateway.roster()
                assert update is not None
                self.apply(update)
                log.info("clocked in %d agents", len(self.ctx.roster))
                return update.revision
            except (httpx.HTTPError, OSError) as exc:
                log.warning("waiting for gateway (%s)", exc)
                await sleep_until(stop, backoff)
                backoff = min(backoff * 2, 15.0)
        return None

    def apply(self, update: RosterUpdate) -> None:
        """Replace the roster and start or stop desks to match it."""
        employees = []
        for raw in update.agents:
            try:
                employees.append(Employee.from_json(raw))
            except (KeyError, TypeError, ValueError):
                log.warning("ignoring malformed agent %r", raw.get("id") if isinstance(raw, dict) else raw)
        self.ctx.roster.replace(employees)
        wanted = {(e.id, lane) for e in employees for lane in LANES}
        for key in list(self._desks):
            if key not in wanted:
                desk, _ = self._desks.pop(key)
                desk.stop.set()
        opened = set()
        for agent_id, lane in sorted(wanted - self._desks.keys()):
            desk = Desk(agent_id, lane, self.handler)
            self._desks[(agent_id, lane)] = (desk, asyncio.create_task(desk.run(self._stop)))
            opened.add(agent_id)
        for agent_id in sorted(opened):
            self._announcements.add(asyncio.create_task(self.handler.status.announce(agent_id)))
        self._announcements = {t for t in self._announcements if not t.done()}

    async def run(self, stop: asyncio.Event) -> None:
        revision = await self.clock_in(stop)
        if revision is None:
            return
        backoff = 1.0
        try:
            while not stop.is_set():
                try:
                    update = await self.ctx.gateway.roster(after=revision)
                    backoff = 1.0
                except (httpx.HTTPError, OSError) as exc:
                    log.warning("roster watch failed (%s); retrying in %.0fs", exc, backoff)
                    await sleep_until(stop, backoff)
                    backoff = min(backoff * 2, 30.0)
                    continue
                if update is not None:
                    revision = update.revision
                    self.apply(update)
                    log.info("roster updated: %d agents", len(self.ctx.roster))
        finally:
            self._stop.set()
            tasks = [t for _, t in self._desks.values()]
            for t in tasks:
                t.cancel()
            await asyncio.gather(*tasks, return_exceptions=True)

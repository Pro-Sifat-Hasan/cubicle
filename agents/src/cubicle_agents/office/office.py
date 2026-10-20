"""Clocks the staff in and runs every desk concurrently."""

from __future__ import annotations

import asyncio
import logging

import httpx

from cubicle_agents.office.desk import Desk, sleep_until
from cubicle_agents.office.handlers import OfficeContext, TaskHandler

log = logging.getLogger(__name__)


class Office:
    def __init__(self, ctx: OfficeContext) -> None:
        self.ctx = ctx
        handler = TaskHandler(ctx)
        self.desks = [Desk(e, ctx.gateway, handler) for e in ctx.roster]

    async def clock_in(self, stop: asyncio.Event) -> bool:
        """Register the roster with the gateway, retrying until it is reachable."""
        backoff = 1.0
        while not stop.is_set():
            try:
                await self.ctx.gateway.sync_employees([e.to_gateway(self.ctx.default_model) for e in self.ctx.roster])
                log.info("clocked in %d employees", len(self.ctx.roster))
                return True
            except (httpx.HTTPError, OSError) as exc:
                log.warning("waiting for gateway (%s)", exc)
                await sleep_until(stop, backoff)
                backoff = min(backoff * 2, 15.0)
        return False

    async def run(self, stop: asyncio.Event) -> None:
        if not await self.clock_in(stop):
            return
        await asyncio.gather(*(desk.run(stop) for desk in self.desks))

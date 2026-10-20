"""Clocks the staff in and runs every desk concurrently."""

from __future__ import annotations

import asyncio
import logging

import httpx

from cubicle_agents.core.gateway import Gateway
from cubicle_agents.llm.base import LLM
from cubicle_agents.office.desk import Desk, _sleep
from cubicle_agents.office.handlers import TaskHandler
from cubicle_agents.staff.roster import Employee

log = logging.getLogger(__name__)


class Office:
    def __init__(self, *, gateway: Gateway, llm: LLM, roster: list[Employee], default_model: str, owner: str) -> None:
        self.gateway = gateway
        self.roster = roster
        self.default_model = default_model
        handler = TaskHandler(gateway=gateway, llm=llm, roster=roster, default_model=default_model, owner=owner)
        self.desks = [Desk(e, gateway, handler) for e in roster]

    async def clock_in(self, stop: asyncio.Event) -> bool:
        """Register the roster with the gateway, retrying until it is reachable."""
        backoff = 1.0
        while not stop.is_set():
            try:
                await self.gateway.sync_employees([e.to_gateway(self.default_model) for e in self.roster])
                log.info("clocked in %d employees", len(self.roster))
                return True
            except (httpx.HTTPError, OSError) as exc:
                log.warning("waiting for gateway (%s)", exc)
                await _sleep(stop, backoff)
                backoff = min(backoff * 2, 15.0)
        return False

    async def run(self, stop: asyncio.Event) -> None:
        if not await self.clock_in(stop):
            return
        await asyncio.gather(*(desk.run(stop) for desk in self.desks))

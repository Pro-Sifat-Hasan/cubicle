"""Reports what an agent does while working (thoughts, remarks, tool calls) to the gateway as steps.

Adapters report a step every time it changes, which can be many times a second while a model streams.
The reporter sends a step at once when it starts or finishes and at most a few times a second in
between, so the app sees it live without flooding the gateway.
"""

from __future__ import annotations

import json
import logging
import time
from typing import Any

from cubicle_agents.core.gateway import Gateway
from cubicle_agents.llm.base import STEP_RUNNING, Step, StepCallback

log = logging.getLogger(__name__)

PUSH_INTERVAL_S = 0.45
MAX_INPUT_CHARS = 8000
MAX_OUTPUT_CHARS = 12000


def as_text(value: Any) -> str:
    """A tool's arguments or result as readable text: JSON gets indented, everything else is stringified."""
    if value is None:
        return ""
    if isinstance(value, str):
        return value
    try:
        return json.dumps(value, indent=2, ensure_ascii=False, default=str)
    except (TypeError, ValueError):
        return str(value)


def tidy_json(raw: str) -> str:
    """Pretty-print a JSON object string when it parses; otherwise leave it (it may still be streaming in)."""
    try:
        return json.dumps(json.loads(raw), indent=2, ensure_ascii=False)
    except (TypeError, ValueError):
        return raw


class StepReporter:
    """A StepCallback bound to one task and agent."""

    def __init__(self, gateway: Gateway, task_id: str, agent_id: str, *, interval: float = PUSH_INTERVAL_S) -> None:
        self._gateway = gateway
        self._task_id = task_id
        self._agent_id = agent_id
        self._interval = interval
        self._last_sent: dict[str, float] = {}
        self._sent: dict[str, tuple[str, str, str, str]] = {}
        self._failed = False  # an old gateway without the steps API: stop trying

    @property
    def callback(self) -> StepCallback:
        return self.report

    async def report(self, step: Step) -> None:
        if self._failed:
            return
        state = (step.name, step.input, step.output, step.status)
        if self._sent.get(step.id) == state:
            return
        now = time.monotonic()
        if step.status == STEP_RUNNING and step.id in self._last_sent and now - self._last_sent[step.id] < self._interval:
            return
        self._last_sent[step.id] = now
        self._sent[step.id] = state
        try:
            await self._gateway.save_step(self._task_id, self._agent_id, {
                "id": step.id[:80], "kind": step.kind, "name": step.name[:120],
                "input": step.input[:MAX_INPUT_CHARS], "output": step.output[:MAX_OUTPUT_CHARS], "status": step.status,
            })
        except Exception as exc:  # the trail is helpful, never a reason to fail the work
            status = getattr(getattr(exc, "response", None), "status_code", None)
            if status in (404, 405):
                self._failed = True
            log.debug("could not record a step", exc_info=True)

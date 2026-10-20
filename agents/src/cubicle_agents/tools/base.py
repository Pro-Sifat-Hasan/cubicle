"""Provider-neutral tool definitions.

A Tool is a name, a description, a JSON Schema for its arguments and an async
handler. Provider adapters translate tools into their own wire format and feed
the results back to the model; the Toolbox runs the handlers and never raises,
so a failing tool becomes an error result the model can react to.
"""

from __future__ import annotations

import logging
from collections.abc import Awaitable, Callable
from dataclasses import dataclass, field
from typing import Any

log = logging.getLogger(__name__)

MAX_RESULT_CHARS = 40_000


class ToolError(Exception):
    """Raised by handlers for expected failures; the message is shown to the model."""


@dataclass(frozen=True)
class ToolResult:
    content: str
    is_error: bool = False


@dataclass(frozen=True)
class Tool:
    name: str
    description: str
    parameters: dict[str, Any]
    handler: Callable[[dict[str, Any]], Awaitable[str]]


@dataclass
class Toolbox:
    tools: list[Tool] = field(default_factory=list)
    # Set by the ask_owner tool; adapters stop the loop once it is set.
    question: str | None = None

    def __bool__(self) -> bool:
        return bool(self.tools)

    def get(self, name: str) -> Tool | None:
        return next((t for t in self.tools if t.name == name), None)

    async def call(self, name: str, arguments: dict[str, Any] | None) -> ToolResult:
        tool = self.get(name)
        if tool is None:
            return ToolResult(f"Unknown tool {name!r}.", is_error=True)
        if not isinstance(arguments, dict):
            return ToolResult("Tool arguments must be a JSON object.", is_error=True)
        try:
            content = await tool.handler(arguments)
        except ToolError as exc:
            return ToolResult(str(exc), is_error=True)
        except Exception as exc:  # a tool bug must not kill the employee's task
            log.exception("tool %s failed", name)
            return ToolResult(f"{name} failed: {type(exc).__name__}", is_error=True)
        if len(content) > MAX_RESULT_CHARS:
            content = content[:MAX_RESULT_CHARS] + "\n[truncated]"
        return ToolResult(content)


def require_str(args: dict[str, Any], key: str, *, max_len: int = 10_000) -> str:
    value = args.get(key)
    if not isinstance(value, str) or not value.strip():
        raise ToolError(f"'{key}' must be a non-empty string.")
    if len(value) > max_len:
        raise ToolError(f"'{key}' is too long (max {max_len} characters).")
    return value

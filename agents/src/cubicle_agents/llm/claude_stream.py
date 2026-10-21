"""Reads Claude Code's `--output-format stream-json --include-partial-messages` stream.

Each line is one JSON event. The parser turns them into what the app shows live:

    ("status", "Writing a file…")   what the agent is doing right now
    ("text", reply_so_far)          the answer as it is written ("" clears it)
    ("step", Step)                  a thought, a remark or a tool call (again whenever it changes)

Text the agent writes before a tool call is a remark ("Let me look at the page"), not the answer, so
it becomes a note step and clears the live text. The answer is what comes after the last tool call.
"""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from typing import Any

from cubicle_agents.llm.base import (
    STEP_DONE,
    STEP_ERROR,
    STEP_NOTE,
    STEP_THINKING,
    STEP_TOOL,
    Step,
    describe_tool,
)
from cubicle_agents.office.steps import tidy_json

Event = tuple[str, Any]

BRIDGE_PREFIX = "mcp__cubicle__"


def tool_name(raw: str) -> str:
    """Claude Code's tool name as Cubicle names it: our own tools plain, other apps as `server_tool`."""
    if raw.startswith(BRIDGE_PREFIX):
        return raw[len(BRIDGE_PREFIX):]
    if raw.startswith("mcp__"):
        server, _, tool = raw[len("mcp__"):].partition("__")
        return f"{server}_{tool}" if tool else server
    return raw


def result_text(content: Any) -> str:
    """The text of a tool_result block, which is a string or a list of content blocks."""
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts = []
        for block in content:
            if isinstance(block, dict) and block.get("type") == "text":
                parts.append(str(block.get("text", "")))
            elif isinstance(block, dict) and block.get("type") == "image":
                parts.append("[image]")
            elif isinstance(block, str):
                parts.append(block)
        return "\n".join(parts)
    return "" if content is None else str(content)


@dataclass
class _Block:
    kind: str
    step: Step | None = None
    buffer: str = ""


@dataclass
class ClaudeStreamParser:
    """Feed it the stream line by line; read `text`, `usage`, `error` and `denied` once it ends."""

    live_text: str = ""
    final_text: str = ""
    usage: dict[str, Any] = field(default_factory=dict)
    error: str = ""
    finished: bool = False
    denied: list[str] = field(default_factory=list)
    _blocks: dict[int, _Block] = field(default_factory=dict)
    _tools: dict[str, Step] = field(default_factory=dict)
    _streamed: set[str] = field(default_factory=set)  # assistant messages already seen as stream events
    _message: str = ""
    _seq: int = 0
    _thinking: Step | None = None

    @property
    def text(self) -> str:
        return (self.final_text or self.live_text).strip()

    def feed(self, line: str) -> list[Event]:
        line = line.strip()
        if not line:
            return []
        try:
            data = json.loads(line)
        except ValueError:
            return []
        if not isinstance(data, dict) or data.get("parent_tool_use_id"):  # a sub-agent's chatter is not ours to show
            return []
        kind = data.get("type")
        if kind == "stream_event":
            return self._stream_event(data.get("event") or {})
        if kind == "assistant":
            return self._assistant(data.get("message") or {})
        if kind == "user":
            return self._user(data.get("message") or {})
        if kind == "system" and data.get("subtype") == "thinking_tokens":
            return self._thinking_tokens(data)
        if kind == "result":
            return self._result(data)
        return []

    def close(self) -> list[Event]:
        """Finish anything still open when the stream ends."""
        events: list[Event] = []
        for step in [b.step for b in self._blocks.values() if b.step is not None and b.kind == "thinking"]:
            step.status = STEP_DONE
            events.append(("step", _copy(step)))
        self._blocks.clear()
        return events

    # ---- stream events (partial messages) ----

    def _stream_event(self, event: dict[str, Any]) -> list[Event]:
        etype = event.get("type")
        if etype == "message_start":
            self._blocks = {}
            self._message = str((event.get("message") or {}).get("id") or f"m{self._seq}")
            self._streamed.add(self._message)
            return []
        index = int(event.get("index") or 0)
        if etype == "content_block_start":
            return self._block_start(index, event.get("content_block") or {})
        if etype == "content_block_delta":
            return self._block_delta(index, event.get("delta") or {})
        if etype == "content_block_stop":
            return self._block_stop(index)
        return []

    def _block_start(self, index: int, block: dict[str, Any]) -> list[Event]:
        btype = block.get("type")
        if btype == "thinking":
            step = Step(f"{self._message}-{index}", STEP_THINKING, output=str(block.get("thinking") or ""))
            self._blocks[index] = _Block("thinking", step)
            self._thinking = step
            return [("step", _copy(step))]
        if btype == "text":
            self._blocks[index] = _Block("text")
            return []
        if btype in ("tool_use", "server_tool_use"):
            events = self._flush_note(f"{self._message}-note{index}")
            name = tool_name(str(block.get("name") or "tool"))
            step = Step(str(block.get("id") or f"{self._message}-{index}"), STEP_TOOL, name=name)
            self._blocks[index] = _Block("tool", step)
            self._tools[step.id] = step
            events += [("status", describe_tool(name)), ("step", _copy(step))]
            return events
        return []

    def _block_delta(self, index: int, delta: dict[str, Any]) -> list[Event]:
        block = self._blocks.get(index)
        if block is None:
            return []
        dtype = delta.get("type")
        if dtype == "text_delta" and block.kind == "text":
            self.live_text += str(delta.get("text") or "")
            return [("text", self.live_text)]
        if dtype == "thinking_delta" and block.step is not None:
            block.step.output += str(delta.get("thinking") or "")
            return [("step", _copy(block.step))]
        if dtype == "input_json_delta" and block.step is not None:
            block.buffer += str(delta.get("partial_json") or "")
            block.step.input = block.buffer  # the arguments as they are written, e.g. a file's content
            return [("step", _copy(block.step))]
        return []

    def _block_stop(self, index: int) -> list[Event]:
        block = self._blocks.pop(index, None)
        if block is None or block.step is None:
            return []
        if block.kind == "thinking":
            block.step.status = STEP_DONE
            if self._thinking is block.step:
                self._thinking = None
        elif block.kind == "tool":
            block.step.input = tidy_json(block.buffer) if block.buffer else block.step.input
        return [("step", _copy(block.step))]

    def _thinking_tokens(self, data: dict[str, Any]) -> list[Event]:
        step = self._thinking
        tokens = data.get("estimated_tokens")
        if step is None or not isinstance(tokens, int):
            return []
        step.name = f"~{tokens} tokens"
        return [("step", _copy(step))]

    # ---- whole messages ----

    def _assistant(self, message: dict[str, Any]) -> list[Event]:
        """A complete assistant message: gives each tool call its final arguments, and stands in for the
        stream events when the CLI did not send partial messages."""
        events: list[Event] = []
        mid = str(message.get("id") or "")
        streamed = mid in self._streamed
        for i, block in enumerate(message.get("content") or []):
            if not isinstance(block, dict):
                continue
            btype = block.get("type")
            if btype in ("tool_use", "server_tool_use"):
                step = self._tools.get(str(block.get("id")))
                if step is None:
                    events += self._flush_note(f"{mid}-note{i}")
                    step = Step(str(block.get("id") or f"{mid}-{i}"), STEP_TOOL,
                                name=tool_name(str(block.get("name") or "tool")))
                    self._tools[step.id] = step
                    events.append(("status", describe_tool(step.name)))
                step.input = json.dumps(block.get("input") or {}, indent=2, ensure_ascii=False)
                events.append(("step", _copy(step)))
            elif btype == "text" and not streamed:
                self.live_text += str(block.get("text") or "")
                events.append(("text", self.live_text))
            elif btype == "thinking" and not streamed and str(block.get("thinking") or "").strip():
                events.append(("step", Step(f"{mid}-{i}", STEP_THINKING, output=str(block["thinking"]), status=STEP_DONE)))
        return events

    def _user(self, message: dict[str, Any]) -> list[Event]:
        events: list[Event] = []
        content = message.get("content")
        if not isinstance(content, list):
            return events
        for block in content:
            if not isinstance(block, dict) or block.get("type") != "tool_result":
                continue
            ident = str(block.get("tool_use_id") or "")
            step = self._tools.pop(ident, None) or Step(ident or f"result-{self._seq}", STEP_TOOL, name="tool")
            step.output = result_text(block.get("content"))
            step.status = STEP_ERROR if block.get("is_error") else STEP_DONE
            events.append(("step", _copy(step)))
        return events

    def _result(self, data: dict[str, Any]) -> list[Event]:
        self.finished = True
        self.final_text = str(data.get("result") or "").strip()
        self.usage = data.get("usage") or {}
        if data.get("is_error"):
            self.error = self.final_text or str(data.get("subtype") or "unknown error")
        for denial in data.get("permission_denials") or []:
            name = tool_name(str(denial.get("tool_name") or "")) if isinstance(denial, dict) else ""
            if name and name not in self.denied:
                self.denied.append(name)
        return self.close()

    def _flush_note(self, step_id: str) -> list[Event]:
        text = self.live_text.strip()
        self.live_text = ""
        if not text:
            return []
        return [("text", ""), ("step", Step(step_id, STEP_NOTE, output=text, status=STEP_DONE))]


def _copy(step: Step) -> Step:
    return Step(step.id, step.kind, step.name, step.input, step.output, step.status)

"""Tools for going through the owner's messages: what is new, what needs them, and asking before doing anything.

The staff read email, Discord or Slack through the connected apps, work out what needs the owner, and put the whole
list to them as one approval ("I found 3 things for you"). A ledger in the gateway remembers every message that was
looked at, so the same one is never raised twice.
"""

from __future__ import annotations

from typing import Any

from cubicle_agents.core.gateway import Gateway
from cubicle_agents.tools.base import Tool, Toolbox, ToolError

MAX_ITEMS = 10
MAX_KEYS = 100
RECORD_STATUSES = ("approved", "done", "dismissed")

_KEY = {"type": "string", "description": "Stable id of the message, e.g. gmail:<message id> or discord:<channel>/<message>."}


def _text(item: dict[str, Any], key: str, limit: int) -> str:
    value = item.get(key)
    return " ".join(value.split())[:limit] if isinstance(value, str) else ""


def triage_tools(gateway: Gateway, toolbox: Toolbox, may_approve: bool) -> list[Tool]:
    async def triage_check(args: dict[str, Any]) -> str:
        keys = args.get("keys")
        if not isinstance(keys, list) or not keys or not all(isinstance(k, str) and k.strip() for k in keys):
            raise ToolError("'keys' must be a list of message keys such as 'gmail:<id>'.")
        fresh = await gateway.triage_new([k.strip()[:300] for k in keys[:MAX_KEYS]])
        if not fresh:
            return "None of these messages is new; you have looked at all of them before."
        return "New (not looked at before):\n" + "\n".join(f"- {k}" for k in fresh)

    async def triage_record(args: dict[str, Any]) -> str:
        items = args.get("items")
        if not isinstance(items, list) or not items:
            raise ToolError("'items' must be a list of {key, status, summary}.")
        out = []
        for item in items[:MAX_KEYS]:
            if not isinstance(item, dict) or not _text(item, "key", 300) or item.get("status") not in RECORD_STATUSES:
                raise ToolError(f"Each item needs a key and a status of {', '.join(RECORD_STATUSES)}.")
            out.append({"key": _text(item, "key", 300), "source": _text(item, "source", 60),
                        "summary": _text(item, "summary", 400), "status": item["status"]})
        await gateway.triage_record(out)
        return f"Recorded {len(out)} message(s)."

    async def propose_tasks(args: dict[str, Any]) -> str:
        raw = args.get("items")
        if not isinstance(raw, list) or not raw:
            raise ToolError("'items' must list at least one task for the owner to approve.")
        if not may_approve:
            raise ToolError("You cannot ask for approval now; list the items in your answer instead.")
        items = [i for i in raw[:MAX_ITEMS] if isinstance(i, dict) and _text(i, "title", 140)]
        if not items:
            raise ToolError("Each item needs a title.")
        lines = []
        for n, item in enumerate(items, 1):
            origin = " · ".join(p for p in (_text(item, "from", 80), _text(item, "source", 40)) if p)
            lines.append(f"**{n}. {_text(item, 'title', 140)}**" + (f"  \n_{origin}_" if origin else ""))
            for label, key in (("Why it needs you", "why"), ("I suggest", "suggestion"), ("Due", "due")):
                if value := _text(item, key, 400):
                    lines.append(f"{label}: {value}")
            lines.append("")
        noun = "thing" if len(items) == 1 else "things"
        details = "\n".join(lines).rstrip() + (
            "\n\nApprove and I will do all of them. To change the list, add a note, for example “skip 2” or "
            "“reply to 1 more formally”. Nothing is sent or posted without asking again.")
        toolbox.question = f"**I found {len(items)} {noun} that need you**\n\n{details}"
        toolbox.question_kind = "approval"
        records = [{"key": _text(i, "key", 300), "source": _text(i, "source", 60), "summary": _text(i, "title", 400),
                    "status": "proposed"} for i in items if _text(i, "key", 300)]
        skipped = args.get("skipped")
        records += [{"key": _text(s, "key", 300), "source": _text(s, "source", 60), "summary": _text(s, "summary", 400),
                     "status": "dismissed"} for s in (skipped if isinstance(skipped, list) else [])[:MAX_KEYS]
                    if isinstance(s, dict) and _text(s, "key", 300)]
        if records:
            await gateway.triage_record(records)
        return "Approval requested. Stop working now; the owner decides, and you continue with their answer."

    item_schema = {
        "type": "object",
        "properties": {
            "key": _KEY, "title": {"type": "string"}, "from": {"type": "string"},
            "source": {"type": "string", "description": "gmail, discord, slack…"},
            "why": {"type": "string"}, "suggestion": {"type": "string"}, "due": {"type": "string"},
        },
        "required": ["title"],
        "additionalProperties": False,
    }
    skipped_schema = {
        "type": "object",
        "properties": {"key": _KEY, "source": {"type": "string"}, "summary": {"type": "string"}},
        "required": ["key"],
        "additionalProperties": False,
    }
    record_schema = {
        "type": "object",
        "properties": {"key": _KEY, "status": {"type": "string", "enum": list(RECORD_STATUSES)},
                       "source": {"type": "string"}, "summary": {"type": "string"}},
        "required": ["key", "status"],
        "additionalProperties": False,
    }
    return [
        Tool(
            name="triage_check",
            description="Tell which of these messages are new, i.e. not looked at in an earlier check. Call it with the "
            "keys of the messages you just listed, and read only the new ones.",
            parameters={"type": "object", "properties": {"keys": {"type": "array", "items": {"type": "string"}}},
                        "required": ["keys"], "additionalProperties": False},
            handler=triage_check,
        ),
        Tool(
            name="propose_tasks",
            description="Put what needs the owner to them as ONE approval: each item has a title, where it came from, why "
            "it needs them and what you suggest. Call it once per check, after reading, then stop; they approve or change "
            "the list and you carry on with their answer. Messages that need nothing go in `skipped`.",
            parameters={
                "type": "object",
                "properties": {"items": {"type": "array", "items": item_schema},
                               "skipped": {"type": "array", "items": skipped_schema}},
                "required": ["items"],
                "additionalProperties": False,
            },
            handler=propose_tasks,
        ),
        Tool(
            name="triage_record",
            description="Record what became of messages after the owner decided: approved, done or dismissed, with a "
            "short summary of the outcome. Keeps the ledger true and feeds the owner's reports.",
            parameters={"type": "object", "properties": {"items": {"type": "array", "items": record_schema}},
                        "required": ["items"], "additionalProperties": False},
            handler=triage_record,
        ),
    ]

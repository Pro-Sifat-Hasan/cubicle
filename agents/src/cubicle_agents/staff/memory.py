"""What an agent remembers, and how much of it goes into one prompt.

The office can hold hundreds of facts, but sending all of them with every request costs tokens and buries the
facts that matter. select_memories picks the few that fit the request (plus the newest, which are most likely
to be current), within a character budget. Trimming a long chat the same way keeps the recent turns whole.
"""

from __future__ import annotations

import dataclasses
import re
from typing import TYPE_CHECKING

if TYPE_CHECKING:  # llm.base imports the tools, which import this module
    from cubicle_agents.llm.base import Turn

MEMORY_FETCH = 300  # how many facts to consider
MEMORY_RELEVANT = 10
MEMORY_RECENT = 4
MEMORY_BUDGET_CHARS = 2400

HISTORY_TURNS = 16
HISTORY_BUDGET_CHARS = 9000
OLD_TURN_CHARS = 700  # an old, long turn is cut to this

_WORD = re.compile(r"[a-z0-9][a-z0-9'_-]{2,}")
_STOP = frozenset("""the and for are but not you your with that this from have has had was were will would can could should
    what when where which who how why about into than then them they their there here just also any all one out our
    its it's i'm you're don't please need want make get let use using like some more very been being""".split())
# Things that must never be stored as a "fact": keys, tokens, passwords, card numbers.
_SECRET = re.compile(
    r"(sk-[A-Za-z0-9_-]{16,}|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|xox[abprs]-[A-Za-z0-9-]{10,}|"
    r"AKIA[0-9A-Z]{16}|AIza[0-9A-Za-z_-]{30,}|-----BEGIN [A-Z ]*PRIVATE KEY-----|"
    r"(?i:password|passwd|passcode|api[ _-]?key|secret|token)\s*(?:is|=|:)\s*\S{6,}|\b(?:\d[ -]?){13,19}\b)")


def words(text: str) -> set[str]:
    return {w for w in _WORD.findall(text.lower()) if w not in _STOP}


def looks_secret(text: str) -> bool:
    """True for text that looks like a key, token, password or card number."""
    return bool(_SECRET.search(text))


def select_memories(memories: list[str], query: str, *, relevant: int = MEMORY_RELEVANT, recent: int = MEMORY_RECENT,
                    budget_chars: int = MEMORY_BUDGET_CHARS) -> list[str]:
    """The facts worth showing for this request. `memories` is newest first (as the gateway returns them)."""
    if len(memories) <= relevant + recent and sum(len(m) for m in memories) <= budget_chars:
        return memories
    q = words(query)
    scored: list[tuple[int, int, str]] = []  # (overlap, -age, fact)
    for age, fact in enumerate(memories):
        overlap = len(q & words(fact)) if q else 0
        if overlap:
            scored.append((overlap, -age, fact))
    scored.sort(reverse=True)
    chosen: list[str] = [fact for _, _, fact in scored[:relevant]]
    for fact in memories[:recent]:
        if fact not in chosen:
            chosen.append(fact)
    # Keep the newest-first order, and stop at the budget.
    order = {fact: i for i, fact in enumerate(memories)}
    out: list[str] = []
    used = 0
    for fact in sorted(chosen, key=lambda f: order[f]):
        if used + len(fact) > budget_chars:
            continue
        out.append(fact)
        used += len(fact)
    return out


def trim_history(history: list[Turn], *, turns: int = HISTORY_TURNS, budget_chars: int = HISTORY_BUDGET_CHARS,
                 old_turn_chars: int = OLD_TURN_CHARS) -> list[Turn]:
    """Keep the latest turns whole and cut long old ones, so a long chat does not cost more with every message."""
    kept = history[-turns:]
    out: list[Turn] = []
    used = 0
    for i, turn in enumerate(reversed(kept)):
        content = turn.content
        limit = budget_chars - used
        if i >= 2 and len(content) > old_turn_chars:  # the two newest turns are never cut
            content = content[:old_turn_chars].rstrip() + " […]"
        if len(content) > limit:
            if i >= 2 or limit <= 0:
                break
            content = content[:limit]
        used += len(content)
        out.append(dataclasses.replace(turn, content=content))
    out.reverse()
    return out

"""Text directives for providers that cannot call Cubicle's tools.

    REMEMBER: The owner's main client is Acme Ltd.     -> saved to office memory
    ASK_OWNER: Which tone should the email use?        -> question for the owner
"""

from __future__ import annotations

from dataclasses import dataclass, field

from cubicle_agents.staff.prompts import ASK_PREFIX, REMEMBER_PREFIX


@dataclass
class Directives:
    text: str
    question: str | None = None
    memories: list[str] = field(default_factory=list)


def extract(text: str) -> Directives:
    kept: list[str] = []
    memories: list[str] = []
    for line in text.splitlines():
        stripped = line.strip()
        if stripped.upper().startswith(REMEMBER_PREFIX):
            fact = stripped[len(REMEMBER_PREFIX):].strip()
            if fact:
                memories.append(fact[:1000])
            continue
        kept.append(line)
    body = "\n".join(kept).strip()
    question = None
    if body.startswith(ASK_PREFIX):
        rest = body[len(ASK_PREFIX):].strip()
        question = (rest.splitlines()[0] if rest else "")[:2000] or "Could you give me more detail?"
    return Directives(text=body, question=question, memories=memories)

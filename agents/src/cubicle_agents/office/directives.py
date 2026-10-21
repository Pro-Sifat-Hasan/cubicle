"""Text directives for providers that cannot call Cubicle's tools.

    REMEMBER: The owner's main client is Acme Ltd.     -> saved to office memory
    PROFILE: Sam is an AI engineer in London, ...      -> replaces the owner's profile
    ASK_OWNER: Which tone should the email use?        -> question for the owner
"""

from __future__ import annotations

from dataclasses import dataclass, field

from cubicle_agents.staff.prompts import APPROVAL_PREFIX, ASK_PREFIX, PROFILE_PREFIX, REMEMBER_PREFIX


@dataclass
class Directives:
    text: str
    question: str | None = None
    question_kind: str = "question"
    memories: list[str] = field(default_factory=list)
    profile: str = ""


def extract(text: str) -> Directives:
    kept: list[str] = []
    memories: list[str] = []
    profile = ""
    for line in text.splitlines():
        stripped = line.strip()
        if stripped.upper().startswith(PROFILE_PREFIX):
            profile = stripped[len(PROFILE_PREFIX):].strip()[:1500] or profile
            continue
        if stripped.upper().startswith(REMEMBER_PREFIX):
            fact = stripped[len(REMEMBER_PREFIX):].strip()
            if fact:
                memories.append(fact[:300])
            continue
        kept.append(line)
    body = "\n".join(kept).strip()
    question, kind = None, "question"
    if body.startswith(ASK_PREFIX):
        rest = body[len(ASK_PREFIX):].strip()
        question = (rest.splitlines()[0] if rest else "")[:2000] or "Could you give me more detail?"
    elif body.startswith(APPROVAL_PREFIX):
        rest = body[len(APPROVAL_PREFIX):].strip()
        question, kind = rest[:3000] or "May I go ahead?", "approval"
    return Directives(text=body, question=question, question_kind=kind, memories=memories, profile=profile)

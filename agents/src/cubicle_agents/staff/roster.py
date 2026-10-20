"""The staff roster.

The default office is a small personal team led by a Chief of Staff. Owners can
replace it with their own <home>/staff.json (a list of objects with the same
fields as Employee) to hire, rename or re-skill employees.
"""

from __future__ import annotations

import json
import re
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any

MANAGER_ROLE = "manager"
SPECIALIST_ROLE = "specialist"
_ID = re.compile(r"^[a-z0-9_-]{1,64}$")


@dataclass(frozen=True)
class Employee:
    id: str
    name: str
    title: str
    role: str
    skills: str
    persona: str
    avatar: str = ""
    model: str = ""  # empty = use the default model

    @property
    def is_manager(self) -> bool:
        return self.role == MANAGER_ROLE

    def to_gateway(self, default_model: str) -> dict[str, Any]:
        return {
            "id": self.id,
            "name": self.name,
            "title": self.title,
            "role": self.role,
            "avatar": self.avatar,
            "model": self.model or default_model,
            "skills": self.skills,
        }


DEFAULT_ROSTER: tuple[Employee, ...] = (
    Employee(
        id="manager", name="Max", title="Chief of Staff", role=MANAGER_ROLE, avatar="🧭",
        skills="planning, prioritising, delegating, final briefings",
        persona="You are calm, decisive and organised. You turn vague requests into clear plans "
                "and give the owner crisp, actionable briefings.",
    ),
    Employee(
        id="researcher", name="Rhea", title="Research Analyst", role=SPECIALIST_ROLE, avatar="🔎",
        skills="research, fact-finding, comparisons, summaries of sources",
        persona="You are curious and rigorous. You separate facts from assumptions and say "
                "clearly when something needs verification.",
    ),
    Employee(
        id="writer", name="Quill", title="Writer", role=SPECIALIST_ROLE, avatar="✍️",
        skills="emails, documents, social posts, rewriting in the owner's voice",
        persona="You write clearly and warmly, match the requested tone, and keep things concise.",
    ),
    Employee(
        id="engineer", name="Dev", title="Software Engineer", role=SPECIALIST_ROLE, avatar="🛠️",
        skills="code, scripts, automation, debugging, technical explanations",
        persona="You are pragmatic and precise. You write working, well-commented code and "
                "explain how to run it.",
    ),
    Employee(
        id="analyst", name="Nova", title="Planning Analyst", role=SPECIALIST_ROLE, avatar="📊",
        skills="schedules, budgets, numbers, checklists, plans",
        persona="You think in structure: tables, timelines and checklists. You show your working.",
    ),
    Employee(
        id="reviewer", name="Vera", title="Quality Reviewer", role=SPECIALIST_ROLE, avatar="✅",
        skills="reviewing drafts, catching mistakes, risk checks, final polish",
        persona="You are constructive and sharp-eyed. You fix problems directly rather than "
                "just listing them.",
    ),
)


class RosterError(ValueError):
    pass


def validate(roster: list[Employee]) -> list[Employee]:
    if not roster:
        raise RosterError("roster is empty")
    ids = [e.id for e in roster]
    if len(set(ids)) != len(ids):
        raise RosterError("employee ids must be unique")
    for e in roster:
        if not _ID.match(e.id):
            raise RosterError(f"invalid employee id {e.id!r} (use lowercase letters, digits, - and _)")
    managers = [e for e in roster if e.is_manager]
    if len(managers) != 1:
        raise RosterError("roster needs exactly one employee with role 'manager'")
    if len(roster) < 2:
        raise RosterError("roster needs at least one specialist")
    return roster


def load_roster(home: Path) -> list[Employee]:
    path = home / "staff.json"
    if not path.exists():
        return list(DEFAULT_ROSTER)
    raw = json.loads(path.read_text(encoding="utf-8"))
    try:
        roster = [Employee(**item) for item in raw]
    except TypeError as exc:
        raise RosterError(f"{path}: {exc}") from exc
    return validate(roster)


def write_default_roster(path: Path) -> None:
    path.write_text(json.dumps([asdict(e) for e in DEFAULT_ROSTER], indent=2, ensure_ascii=False), encoding="utf-8")


def manager_of(roster: list[Employee]) -> Employee:
    return next(e for e in roster if e.is_manager)


def specialists_of(roster: list[Employee]) -> list[Employee]:
    return [e for e in roster if not e.is_manager]

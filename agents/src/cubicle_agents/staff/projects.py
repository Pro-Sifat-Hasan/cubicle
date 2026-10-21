"""The projects the owner works on, and how much of them goes into one prompt.

A request like "fix the pricing card in abroadmate" should reach the agent with that project's folder and notes,
without the notes of every other project riding along. match_projects finds the projects a request names;
projects_block lists those in full and the rest by name only, within a character budget.
"""

from __future__ import annotations

import re
from dataclasses import dataclass
from pathlib import Path

NOTES_CHARS = 1500  # one matched project's notes in the prompt
MATCHED_MAX = 2
INDEX_MAX = 20  # projects named (without notes) in the prompt

_NON_WORD = re.compile(r"[^a-z0-9]+")


@dataclass(frozen=True)
class Project:
    id: str
    name: str
    path: str = ""
    notes: str = ""

    @property
    def folder(self) -> Path | None:
        """The project's folder when it is set and still exists on disk."""
        if not self.path:
            return None
        folder = Path(self.path)
        return folder if folder.is_absolute() and folder.is_dir() else None


def _squash(text: str) -> str:
    """Lower case, letters and digits only, so 'Abroad Mate', 'abroad-mate' and 'AbroadMate' all match."""
    return _NON_WORD.sub("", text.lower())


def match_projects(projects: list[Project], *texts: str) -> list[Project]:
    """The projects whose name appears in any of the texts, longest names first (so 'abroadmate admin' wins)."""
    tokens = _NON_WORD.sub(" ", " ".join(texts).lower()).split()
    # Runs of up to five words with the gaps closed: "abroad mate" -> "abroadmate", while "cubiclesomething" stays no match.
    runs = {"".join(tokens[i:i + n]) for n in range(1, 6) for i in range(len(tokens))}
    found = []
    for p in projects:
        name = _squash(p.name)
        if len(name) >= 3 and name in runs:
            found.append(p)
    found.sort(key=lambda p: -len(p.name))
    return found[:MATCHED_MAX]


def projects_block(projects: list[Project], matched: list[Project], owner: str) -> str:
    """The prompt section about the owner's projects ('' when there are none)."""
    if not projects:
        return ""
    lines = []
    for p in matched:
        lines.append(f"### {p.name}" + (f"  (folder: {p.path})" if p.path else ""))
        if p.notes:
            lines.append(p.notes[:NOTES_CHARS])
    rest = [p.name for p in projects if p not in matched][:INDEX_MAX]
    out = [f"{owner}'s projects. When a request names one, work in its folder and follow its notes."]
    if lines:
        out.append("\n".join(lines))
    if rest:
        out.append("Other projects (ask for details by naming them): " + ", ".join(rest))
    return "\n\n".join(out)

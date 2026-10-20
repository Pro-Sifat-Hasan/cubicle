"""Prompt templates. Kept in one place so they are easy to review and tune."""

from __future__ import annotations

from typing import Any

from cubicle_agents.staff.roster import Employee

ASK_PREFIX = "ASK_OWNER:"

PLAN_SCHEMA: dict[str, Any] = {
    "type": "object",
    "properties": {
        "summary": {"type": "string"},
        "question_for_owner": {"type": "string"},
        "subtasks": {
            "type": "array",
            "items": {
                "type": "object",
                "properties": {
                    "title": {"type": "string"},
                    "description": {"type": "string"},
                    "assignee": {"type": "string"},
                    "depends_on": {"type": "array", "items": {"type": "integer"}},
                },
                "required": ["title", "description", "assignee", "depends_on"],
                "additionalProperties": False,
            },
        },
    },
    "required": ["summary", "question_for_owner", "subtasks"],
    "additionalProperties": False,
}


def system_prompt(employee: Employee, owner: str) -> str:
    return (
        f"You are {employee.name}, the {employee.title} on {owner}'s personal AI staff in Cubicle. "
        f"{employee.persona}\n\n"
        f"Your strengths: {employee.skills}.\n\n"
        "How you work:\n"
        f"- You are doing real work for {owner}; deliver the finished result, not a description of what you would do.\n"
        "- You do not have internet access or access to the owner's accounts, files or devices. When the task needs "
        "them, do everything you can from the information given and state precisely what the owner must provide or do.\n"
        "- Never invent facts, numbers, quotes or sources. Mark assumptions clearly.\n"
        "- Use Markdown with short headings and lists where they help.\n"
        f"- If you truly cannot proceed without information only {owner} has, reply with a single line that starts "
        f"with `{ASK_PREFIX}` followed by one clear question, and nothing else."
    )


def _context_block(claim_parent: dict[str, Any] | None, dependency_results: list[dict[str, Any]],
                   human_answers: list[dict[str, Any]]) -> str:
    parts: list[str] = []
    if claim_parent:
        parts.append(f"OVERALL ASSIGNMENT: {claim_parent.get('title', '')}\n{claim_parent.get('description', '')}".strip())
    for dep in dependency_results:
        parts.append(f"RESULT FROM A TEAMMATE ({dep.get('assignee')}) — {dep.get('title')}:\n{dep.get('result', '')}")
    for ans in human_answers:
        parts.append(f"YOU ASKED THE OWNER: {ans.get('question')}\nOWNER ANSWERED: {ans.get('answer')}")
    return "\n\n---\n\n".join(parts)


def plan_prompt(task: dict[str, Any], specialists: list[Employee], human_answers: list[dict[str, Any]],
                max_subtasks: int) -> str:
    staff = "\n".join(f"- {e.id} ({e.title}, {e.name}): {e.skills}" for e in specialists)
    answers = _context_block(None, [], human_answers)
    return (
        f"ASSIGNMENT: {task['title']}\n"
        f"DETAILS:\n{task.get('description') or '(none)'}\n\n"
        f"{answers + chr(10) + chr(10) if answers else ''}"
        f"YOUR TEAM (use these ids as assignee):\n{staff}\n\n"
        "Plan how your team will complete this assignment.\n"
        f"- Create 1 to {max_subtasks} subtasks. Prefer fewer, meatier subtasks; small requests need only one.\n"
        "- Each subtask must be self-contained: the description should tell the specialist exactly what to "
        "produce.\n"
        "- depends_on lists the 0-based indices of EARLIER subtasks whose results this one needs.\n"
        "- Only if the assignment is impossible to start without the owner's input, put one question in "
        "question_for_owner (otherwise leave it empty) and return an empty subtasks list.\n"
        "- You will write the final deliverable yourself once all subtasks are done."
    )


def work_prompt(task: dict[str, Any], parent: dict[str, Any] | None, dependency_results: list[dict[str, Any]],
                human_answers: list[dict[str, Any]], may_ask: bool) -> str:
    context = _context_block(parent, dependency_results, human_answers)
    ask_rule = "" if may_ask else f"\nDo NOT use {ASK_PREFIX}; make reasonable assumptions and state them.\n"
    return (
        f"{context + chr(10) + chr(10) + '---' + chr(10) + chr(10) if context else ''}"
        f"YOUR TASK: {task['title']}\n{task.get('description', '')}\n{ask_rule}\n"
        "Produce the complete result now."
    )


def synthesis_prompt(parent: dict[str, Any] | None, dependency_results: list[dict[str, Any]]) -> str:
    context = _context_block(parent, dependency_results, [])
    return (
        f"{context}\n\n---\n\n"
        "All subtasks are finished. Write the final deliverable for the owner:\n"
        "1. Start with a 2-3 sentence summary of what was done.\n"
        "2. Then the finished work itself, merged and polished (not a list of who did what).\n"
        "3. End with a short 'Needs you' list of anything the owner must do or decide (omit if nothing)."
    )

"""Prompt templates, kept in one place so they are easy to review and tune.

Two working modes:
* tool mode: the provider runs Cubicle's tools (write_file, remember, ask_owner, ...)
* text mode: the provider has no access to our tools (Claude Code, demo), so
  employees use line directives (ASK_OWNER: / REMEMBER:) that the office parses
"""

from __future__ import annotations

from typing import Any

from cubicle_agents.staff.roster import Employee

ASK_PREFIX = "ASK_OWNER:"
REMEMBER_PREFIX = "REMEMBER:"

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


def system_prompt(employee: Employee, owner: str, *, memories: list[str], tool_mode: bool, web: bool) -> str:
    parts = [
        f"You are {employee.name}, the {employee.title} on {owner}'s personal AI staff in Cubicle. {employee.persona}",
        f"Your strengths: {employee.skills}.",
        _working_rules(owner, tool_mode=tool_mode, web=web),
    ]
    if memories:
        facts = "\n".join(f"- {m}" for m in memories)
        parts.append(f"What the office knows about {owner} (from earlier work; trust it unless told otherwise):\n{facts}")
    return "\n\n".join(parts)


def _working_rules(owner: str, *, tool_mode: bool, web: bool) -> str:
    rules = [
        f"Deliver finished work for {owner}, not a description of what you would do.",
        "Be accurate. Never invent facts, figures, quotes or sources; label assumptions clearly.",
        "Write in clear Markdown with short headings and lists where they help. Be concise.",
    ]
    if web:
        rules.append("Use the web to check facts that may have changed or that you are unsure of, "
                     "and cite the URLs you relied on.")
    else:
        rules.append("You have no internet access in this office; work from what you know and say what should be verified.")
    if tool_mode:
        rules += [
            "Save substantial deliverables (documents, code, tables, drafts) with write_file so the owner gets real "
            "files, and summarise them in your answer. Use read_file/list_files to build on teammates' files.",
            "Use remember for durable facts you learn about the owner (preferences, people, projects), not task details.",
            "Use ask_owner only when you cannot do a good job without information only the owner has.",
        ]
    else:
        rules += [
            f"To save a durable fact about the owner for future tasks, add a line starting with `{REMEMBER_PREFIX}`.",
            f"If you truly cannot proceed without information only {owner} has, reply with a single line starting "
            f"with `{ASK_PREFIX}` followed by one clear question, and nothing else.",
        ]
    return "How you work:\n" + "\n".join(f"- {r}" for r in rules)


def _context_block(parent: dict[str, Any] | None, dependency_results: list[dict[str, Any]],
                   human_answers: list[dict[str, Any]]) -> str:
    parts: list[str] = []
    if parent:
        parts.append(f"OVERALL ASSIGNMENT: {parent.get('title', '')}\n{parent.get('description', '')}".strip())
    for dep in dependency_results:
        parts.append(f"RESULT FROM A TEAMMATE ({dep.get('assignee')}), {dep.get('title')}:\n{dep.get('result', '')}")
    for ans in human_answers:
        parts.append(f"YOU ASKED THE OWNER: {ans.get('question')}\nTHE OWNER ANSWERED: {ans.get('answer')}")
    return "\n\n---\n\n".join(parts)


def plan_prompt(task: dict[str, Any], specialists: list[Employee], human_answers: list[dict[str, Any]],
                max_subtasks: int) -> str:
    staff = "\n".join(f"- {e.id} ({e.title}, {e.name}): {e.skills}" for e in specialists)
    answers = _context_block(None, [], human_answers)
    return "\n".join([
        f"ASSIGNMENT: {task['title']}",
        f"DETAILS:\n{task.get('description') or '(none)'}",
        "",
        *([answers, ""] if answers else []),
        f"YOUR TEAM (use these ids as assignee):\n{staff}",
        "",
        "Plan how your team will deliver an excellent result.",
        f"- Use 1 to {max_subtasks} subtasks. Small or simple requests need exactly one; don't split for the sake of it.",
        "- Give each subtask a self-contained description: the goal, the expected output and any constraints.",
        "- depends_on lists the 0-based indices of EARLIER subtasks whose results this one needs; independent "
        "subtasks run in parallel.",
        "- For anything the owner will send to others or rely on (emails, documents, code, plans), finish with a "
        "review subtask for the Quality Reviewer that depends on the work it checks.",
        "- Only if the assignment cannot even be started without the owner's input, put one question in "
        "question_for_owner and return an empty subtasks list. Otherwise leave question_for_owner empty.",
        "- You will merge the results into the final deliverable yourself.",
    ])


def work_prompt(task: dict[str, Any], parent: dict[str, Any] | None, dependency_results: list[dict[str, Any]],
                human_answers: list[dict[str, Any]], may_ask: bool) -> str:
    context = _context_block(parent, dependency_results, human_answers)
    lines = []
    if context:
        lines += [context, "", "---", ""]
    lines += [f"YOUR TASK: {task['title']}", task.get("description", "")]
    if not may_ask:
        lines.append("You have already asked the owner enough questions; make reasonable assumptions and state them.")
    lines += ["", "Produce the complete result now."]
    return "\n".join(lines)


def synthesis_prompt(parent: dict[str, Any] | None, dependency_results: list[dict[str, Any]], files: list[str]) -> str:
    context = _context_block(parent, dependency_results, [])
    file_list = "\n".join(f"- {f}" for f in files) if files else "(none)"
    return "\n".join([
        context, "", "---", "",
        f"FILES IN THE ASSIGNMENT WORKSPACE:\n{file_list}", "",
        "All subtasks are finished. Write the final deliverable for the owner:",
        "1. Start with a 2-3 sentence summary of the outcome.",
        "2. Then the finished work itself, merged and polished. Apply the reviewer's fixes if there was a review. "
        "Do not narrate who did what.",
        "3. Mention the files above where relevant.",
        "4. End with a short 'Needs you' list of anything the owner must do or decide (omit it if nothing).",
    ])

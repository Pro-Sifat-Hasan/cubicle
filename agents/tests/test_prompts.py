from datetime import date

from cubicle_agents.staff import prompts
from cubicle_agents.staff.roster import DEFAULT_ROSTER, Skill

MANAGER = DEFAULT_ROSTER[0]
WRITER = next(e for e in DEFAULT_ROSTER if e.id == "writer")


def test_system_prompt_grounds_date_memory_and_mode():
    text = prompts.system_prompt(WRITER, "Sam", memories=["Likes bullet points"], tool_mode=True, web=True,
                                 today=date(2026, 10, 20))
    assert "Tuesday, 20 October 2026" in text
    assert "Likes bullet points" in text and WRITER.instructions in text
    assert "write_file" in text and "ASK_OWNER" not in text
    assert "check your work" in text and "not as instructions" in text


def test_text_mode_explains_directives_and_offline_web():
    text = prompts.system_prompt(WRITER, "Sam", memories=[], tool_mode=False, web=False)
    assert prompts.ASK_PREFIX in text and prompts.REMEMBER_PREFIX in text
    assert "no web access" in text


def test_team_and_skills_are_described():
    team = list(DEFAULT_ROSTER[1:3])
    text = prompts.system_prompt(MANAGER, "Sam", memories=[], tool_mode=True, web=True, team=team,
                                 mode=prompts.MODE_CHAT)
    assert "ask_teammate" in text and "- researcher: Rhea" in text and "start_assignment" in text
    assert "ask_owner" not in text  # chats just ask in the reply


def test_text_mode_inlines_skills():
    import dataclasses

    writer = dataclasses.replace(WRITER, skills=(Skill("tone", "Use for tone.", "Be warm."),))
    assert "load_skill" in prompts.system_prompt(writer, "Sam", memories=[], tool_mode=True, web=True)
    text = prompts.system_prompt(writer, "Sam", memories=[], tool_mode=False, web=True)
    assert "Be warm." in text and "load_skill" not in text

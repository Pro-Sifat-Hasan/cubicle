from datetime import date

from cubicle_agents.staff import prompts
from cubicle_agents.staff.roster import DEFAULT_ROSTER

WRITER = next(e for e in DEFAULT_ROSTER if e.id == "writer")


def test_system_prompt_grounds_date_memory_and_mode():
    text = prompts.system_prompt(WRITER, "Sam", memories=["Likes bullet points"], tool_mode=True, web=True,
                                 today=date(2026, 10, 20))
    assert "Tuesday, 20 October 2026" in text
    assert "Likes bullet points" in text
    assert "write_file" in text and "ASK_OWNER" not in text
    assert "check your work" in text


def test_text_mode_explains_directives_and_offline_web():
    text = prompts.system_prompt(WRITER, "Sam", memories=[], tool_mode=False, web=False)
    assert prompts.ASK_PREFIX in text and prompts.REMEMBER_PREFIX in text
    assert "no internet access" in text

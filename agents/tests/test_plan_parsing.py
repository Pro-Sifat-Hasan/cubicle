import json

from cubicle_agents.office.handlers import MAX_SUBTASKS, parse_plan

VALID = {"researcher", "writer"}


def plan(**kw):
    base = {"summary": "s", "question_for_owner": "", "subtasks": []}
    base.update(kw)
    return json.dumps(base)


def test_parses_valid_plan():
    p = parse_plan(plan(subtasks=[
        {"title": "Research", "description": "d", "assignee": "researcher", "depends_on": []},
        {"title": "Write", "description": "d", "assignee": "writer", "depends_on": [0]},
    ]), VALID)
    assert [s["assignee"] for s in p.subtasks] == ["researcher", "writer"]
    assert p.subtasks[1]["depends_on"] == [0]


def test_unknown_assignee_falls_back_to_a_real_employee():
    p = parse_plan(plan(subtasks=[{"title": "x", "description": "", "assignee": "ceo", "depends_on": []}]), VALID)
    assert p.subtasks[0]["assignee"] in VALID


def test_forward_and_self_dependencies_are_dropped():
    p = parse_plan(plan(subtasks=[
        {"title": "a", "description": "", "assignee": "writer", "depends_on": [0, 1, -1]},
        {"title": "b", "description": "", "assignee": "writer", "depends_on": [0, 1, 5]},
    ]), VALID)
    assert p.subtasks[0]["depends_on"] == []
    assert p.subtasks[1]["depends_on"] == [0]


def test_caps_number_of_subtasks():
    many = [{"title": f"t{i}", "description": "", "assignee": "writer", "depends_on": []} for i in range(20)]
    assert len(parse_plan(plan(subtasks=many), VALID).subtasks) == MAX_SUBTASKS


def test_invalid_json_yields_empty_plan():
    p = parse_plan("not json at all", VALID)
    assert p.subtasks == [] and p.question == ""


def test_code_fenced_json_is_accepted():
    body = plan(subtasks=[{"title": "x", "description": "", "assignee": "writer", "depends_on": []}])
    assert len(parse_plan(f"```json\n{body}\n```", VALID).subtasks) == 1


def test_question_is_extracted():
    assert parse_plan(plan(question_for_owner="Which client?"), VALID).question == "Which client?"

import asyncio
from pathlib import Path

from cubicle_agents.staff.projects import NOTES_CHARS, Project, match_projects, projects_block
from cubicle_agents.staff.roster import DEFAULT_ROSTER
from cubicle_agents.tools import build_toolbox
from cubicle_agents.tools.factory import project_approval

from fakes import FakeGateway, ScriptedLLM, claim, make_handler


def run(coro):
    return asyncio.run(coro)


PROJECTS = [Project("p1", "AbroadMate", "", "SaaS for students."), Project("p2", "Cubicle"),
            Project("p3", "AbroadMate Admin", "", "Back office.")]


def test_a_project_is_found_by_name_however_it_is_spelled():
    for text in ("fix the pricing card in abroadmate", "Abroad Mate: change the navbar", "update abroad-mate ui"):
        assert [p.name for p in match_projects(PROJECTS[:2], text)] == ["AbroadMate"], text
    assert match_projects(PROJECTS, "nothing relevant here") == []
    assert match_projects(PROJECTS, "the cubiclesomething") == []  # a single word must stand alone


def test_the_longest_matching_name_comes_first():
    assert [p.name for p in match_projects(PROJECTS, "AbroadMate Admin needs a new table")][0] == "AbroadMate Admin"


def test_prompt_lists_matched_notes_and_only_names_the_rest():
    block = projects_block(PROJECTS, [PROJECTS[0]], "Sam")
    assert "SaaS for students." in block and "Back office." not in block
    assert "Cubicle" in block and "AbroadMate Admin" in block
    assert projects_block([], [], "Sam") == ""
    long = Project("p", "Big", "", "x" * 5000)
    assert len(projects_block([long], [long], "Sam")) < NOTES_CHARS + 400


def _box(gw, approved=(), may_approve=True):
    return build_toolbox(employee=DEFAULT_ROSTER[1], workspace=Path("."), gateway=gw, may_ask=False,
                         client_web_fetch=False, may_approve=may_approve, approved_actions=approved)


def test_a_new_folder_needs_approval_but_notes_do_not(tmp_path):
    gw = FakeGateway()
    box = _box(gw)
    asked = run(box.call("save_project", {"name": "AbroadMate", "path": str(tmp_path), "notes": "Next.js"}))
    assert "Approval requested" in asked.content and gw.projects == []
    assert box.question and str(tmp_path) in box.question and box.question_kind == "approval"

    approved = _box(gw, approved=(f"**{project_approval('AbroadMate', str(tmp_path))}**\n\nThe staff will...",))
    assert not run(approved.call("save_project", {"name": "AbroadMate", "path": str(tmp_path), "notes": "Next.js"})).is_error
    assert gw.projects[0]["path"] == str(tmp_path)

    # Known folder, new notes: nothing to approve.
    again = {"name": "abroadmate", "path": str(tmp_path), "notes": "Next.js, Postgres"}
    assert not run(_box(gw).call("save_project", again)).is_error
    assert gw.projects[0]["notes"] == "Next.js, Postgres" and len(gw.projects) == 1


def test_project_notes_refuse_secrets_and_empty_calls():
    box = _box(FakeGateway())
    assert run(box.call("save_project", {"name": "X", "notes": "password is hunter2hunter2"})).is_error
    assert run(box.call("save_project", {"name": "X"})).is_error


def test_work_on_a_named_project_happens_in_its_folder(tmp_path):
    folder = tmp_path / "abroadmate"
    folder.mkdir()
    gw = FakeGateway()
    gw.projects = [{"id": "p1", "name": "AbroadMate", "path": str(folder), "notes": "UI lives in src/components."}]
    llm = ScriptedLLM("Done")
    writer = next(e for e in DEFAULT_ROSTER if e.id == "writer")
    run(make_handler(gw, llm, tmp_path / "ws").handle(writer, claim(title="Fix the navbar in abroadmate")))
    call = llm.calls[0]
    assert call["workdir"] == folder
    assert "UI lives in src/components." in call["system"]

    other = ScriptedLLM("Done")
    run(make_handler(gw, other, tmp_path / "ws").handle(writer, claim(title="Write a poem")))
    assert other.calls[0]["workdir"] != folder
    assert "UI lives in src/components." not in other.calls[0]["system"]

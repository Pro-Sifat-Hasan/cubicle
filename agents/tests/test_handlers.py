import asyncio
import json

from cubicle_agents.core.gateway import Claim
from cubicle_agents.llm.base import AgentResult
from cubicle_agents.llm.demo_llm import DemoLLM
from cubicle_agents.office.desk import Desk
from cubicle_agents.staff.roster import DEFAULT_ROSTER, manager_of

from fakes import ExplodingLLM, FakeGateway, ScriptedLLM, claim, make_handler

ROSTER = list(DEFAULT_ROSTER)
MANAGER = manager_of(ROSTER)
WRITER = next(e for e in ROSTER if e.id == "writer")


def run(coro):
    return asyncio.run(coro)


def plan_json(*subtasks, question=""):
    return json.dumps({"summary": "s", "question_for_owner": question, "subtasks": list(subtasks)})


def test_manager_plans_and_delegates(tmp_path):
    gw = FakeGateway(memories=["Prefers short emails"])
    llm = ScriptedLLM(plan_json({"title": "Draft email", "description": "d", "assignee": "writer", "depends_on": []}))
    run(make_handler(gw, llm, tmp_path).handle(MANAGER, claim(kind="assignment", title="Reply to Bob", parent_id="")))

    d = gw.delegations[0]
    assert d["subtasks"][0]["assignee"] == "writer"
    assert d["synthesis"]["assignee"] == "manager"
    call = llm.calls[0]
    assert call["json_schema"] is not None and call["toolbox"] is None
    assert "Sam" in call["system"] and "Prefers short emails" in call["system"]
    assert gw.usage == [("tsk_1", "manager", 10, 5)]


def test_manager_asks_owner_when_plan_has_question(tmp_path):
    gw = FakeGateway()
    run(make_handler(gw, ScriptedLLM(plan_json(question="Which Bob?")), tmp_path)
        .handle(MANAGER, claim(kind="assignment")))
    assert gw.questions == [("tsk_1", "manager", "Which Bob?")]
    assert gw.delegations == []


def test_manager_falls_back_to_single_subtask_on_garbage_plan(tmp_path):
    gw = FakeGateway()
    run(make_handler(gw, ScriptedLLM("garbage"), tmp_path).handle(MANAGER, claim(kind="assignment")))
    assert len(gw.delegations[0]["subtasks"]) == 1


def test_specialist_completes_work_in_assignment_workspace(tmp_path):
    gw = FakeGateway()
    llm = ScriptedLLM("Here is the email.", supports_tools=True)
    run(make_handler(gw, llm, tmp_path).handle(WRITER, claim(parent_id="asg_7")))
    assert gw.completed == {"tsk_1": "Here is the email."}
    call = llm.calls[0]
    assert call["workdir"] == tmp_path / "asg_7"
    assert {t.name for t in call["toolbox"].tools} >= {"write_file", "read_file", "remember", "ask_owner"}


def test_specialist_question_via_tool(tmp_path):
    gw = FakeGateway()
    llm = ScriptedLLM(AgentResult("", question="What tone?"), supports_tools=True)
    run(make_handler(gw, llm, tmp_path).handle(WRITER, claim()))
    assert gw.questions == [("tsk_1", "writer", "What tone?")]
    assert gw.completed == {}


def test_text_mode_directives_are_applied(tmp_path):
    gw = FakeGateway()
    llm = ScriptedLLM("REMEMBER: Sam signs emails as 'S.'\nDraft body")
    run(make_handler(gw, llm, tmp_path).handle(WRITER, claim()))
    assert gw.memories == ["Sam signs emails as 'S.'"]
    assert gw.completed["tsk_1"] == "Draft body"
    assert "ASK_OWNER" in llm.calls[0]["system"]  # text mode explains the directives


def test_text_mode_question(tmp_path):
    gw = FakeGateway()
    run(make_handler(gw, ScriptedLLM("ASK_OWNER: What tone should I use?"), tmp_path).handle(WRITER, claim()))
    assert gw.questions == [("tsk_1", "writer", "What tone should I use?")]


def test_specialist_cannot_ask_forever(tmp_path):
    gw = FakeGateway()
    c = claim()
    c.human_answers = [{"question": "q", "answer": "a"}, {"question": "q2", "answer": "a2"}]
    llm = ScriptedLLM("ASK_OWNER: again?", supports_tools=True)
    run(make_handler(gw, llm, tmp_path).handle(WRITER, c))
    assert gw.questions == []
    assert "ask_owner" not in {t.name for t in llm.calls[0]["toolbox"].tools}


def test_synthesis_single_result_skips_model_and_lists_files(tmp_path):
    gw = FakeGateway()
    (tmp_path / "asg_1").mkdir()
    (tmp_path / "asg_1" / "report.md").write_text("x", encoding="utf-8")
    c = Claim(task={"id": "syn", "kind": "synthesis", "title": "Wrap", "parent_id": "asg_1"},
              dependency_results=[{"title": "x", "assignee": "writer", "result": "only result"}])
    llm = ScriptedLLM()
    run(make_handler(gw, llm, tmp_path).handle(MANAGER, c))
    assert gw.completed["syn"].startswith("only result")
    assert "`report.md`" in gw.completed["syn"]
    assert llm.calls == []


def test_synthesis_merges_multiple_results(tmp_path):
    gw = FakeGateway()
    c = Claim(task={"id": "syn", "kind": "synthesis", "title": "Wrap", "parent_id": "asg_1"},
              parent={"title": "Weekly report", "description": ""},
              dependency_results=[{"title": "a", "assignee": "researcher", "result": "facts"},
                                  {"title": "b", "assignee": "writer", "result": "draft"}])
    llm = ScriptedLLM("Final report")
    run(make_handler(gw, llm, tmp_path).handle(MANAGER, c))
    assert gw.completed == {"syn": "Final report"}
    assert "facts" in llm.calls[0]["prompt"] and "draft" in llm.calls[0]["prompt"]


def test_desk_reports_failure_and_returns_to_idle(tmp_path):
    gw = FakeGateway()
    desk = Desk(WRITER, gw, make_handler(gw, ExplodingLLM(), tmp_path))
    run(desk.work_on(claim()))
    assert "model unavailable" in gw.failed["tsk_1"]
    assert [c[1][1] for c in gw.calls if c[0] == "status"] == ["working", "idle"]


def test_demo_llm_runs_the_whole_flow_offline(tmp_path):
    gw = FakeGateway()
    h = make_handler(gw, DemoLLM(), tmp_path)
    run(h.handle(MANAGER, claim(kind="assignment", title="Plan my week", parent_id="")))
    subtasks = gw.delegations[0]["subtasks"]
    assert 1 <= len(subtasks) <= 3
    assert all(s["assignee"] != "manager" for s in subtasks)
    run(h.handle(WRITER, claim(task_id="t2", title=subtasks[0]["title"])))
    assert "Demo mode" in gw.completed["t2"]

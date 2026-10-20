import asyncio
import json

from cubicle_agents.core.gateway import Claim
from cubicle_agents.llm.demo_llm import DemoLLM
from cubicle_agents.office.desk import Desk
from cubicle_agents.office.handlers import TaskHandler
from cubicle_agents.staff.roster import DEFAULT_ROSTER, manager_of

from fakes import ExplodingLLM, FakeGateway, ScriptedLLM, claim

ROSTER = list(DEFAULT_ROSTER)
MANAGER = manager_of(ROSTER)
WRITER = next(e for e in ROSTER if e.id == "writer")


def handler(gw, llm):
    return TaskHandler(gateway=gw, llm=llm, roster=ROSTER, default_model="claude-opus-5-5", owner="Sam")


def run(coro):
    return asyncio.run(coro)


def test_manager_plans_and_delegates():
    gw = FakeGateway()
    llm = ScriptedLLM(json.dumps({"summary": "s", "question_for_owner": "", "subtasks": [
        {"title": "Draft email", "description": "d", "assignee": "writer", "depends_on": []},
    ]}))
    run(handler(gw, llm).handle(MANAGER, claim(kind="assignment", title="Reply to Bob")))

    assert len(gw.delegations) == 1
    d = gw.delegations[0]
    assert d["subtasks"][0]["assignee"] == "writer"
    assert d["synthesis"]["assignee"] == "manager"
    assert llm.prompts[0]["json_schema"] is not None
    assert "Sam" in llm.prompts[0]["system"]
    assert gw.usage == [("tsk_1", "manager", 10, 5)]


def test_manager_asks_owner_when_plan_has_question():
    gw = FakeGateway()
    llm = ScriptedLLM(json.dumps({"summary": "", "question_for_owner": "Which Bob?", "subtasks": []}))
    run(handler(gw, llm).handle(MANAGER, claim(kind="assignment")))
    assert gw.questions == [("tsk_1", "manager", "Which Bob?")]
    assert gw.delegations == []


def test_manager_falls_back_to_single_subtask_on_garbage_plan():
    gw = FakeGateway()
    run(handler(gw, ScriptedLLM("garbage")).handle(MANAGER, claim(kind="assignment", title="Anything")))
    assert len(gw.delegations[0]["subtasks"]) == 1


def test_specialist_completes_work():
    gw = FakeGateway()
    run(handler(gw, ScriptedLLM("Here is the email.")).handle(WRITER, claim()))
    assert gw.completed == {"tsk_1": "Here is the email."}


def test_specialist_can_ask_owner():
    gw = FakeGateway()
    run(handler(gw, ScriptedLLM("ASK_OWNER: What tone should I use?")).handle(WRITER, claim()))
    assert gw.questions == [("tsk_1", "writer", "What tone should I use?")]
    assert gw.completed == {}


def test_specialist_cannot_ask_forever():
    gw = FakeGateway()
    c = claim()
    c.human_answers = [{"question": "q", "answer": "a"}, {"question": "q2", "answer": "a2"}]
    llm = ScriptedLLM("ASK_OWNER: again?")
    run(handler(gw, llm).handle(WRITER, c))
    assert gw.questions == []
    assert "Do NOT use ASK_OWNER" in llm.prompts[0]["prompt"]


def test_synthesis_single_result_skips_model_call():
    gw = FakeGateway()
    c = Claim(task={"id": "syn", "kind": "synthesis", "title": "Wrap"},
              dependency_results=[{"title": "x", "assignee": "writer", "result": "only result"}])
    llm = ScriptedLLM()
    run(handler(gw, llm).handle(MANAGER, c))
    assert gw.completed == {"syn": "only result"}
    assert llm.prompts == []


def test_synthesis_merges_multiple_results():
    gw = FakeGateway()
    c = Claim(task={"id": "syn", "kind": "synthesis", "title": "Wrap"},
              parent={"title": "Weekly report", "description": ""},
              dependency_results=[{"title": "a", "assignee": "researcher", "result": "facts"},
                                  {"title": "b", "assignee": "writer", "result": "draft"}])
    llm = ScriptedLLM("Final report")
    run(handler(gw, llm).handle(MANAGER, c))
    assert gw.completed == {"syn": "Final report"}
    assert "facts" in llm.prompts[0]["prompt"] and "draft" in llm.prompts[0]["prompt"]


def test_desk_reports_failure_and_returns_to_idle():
    gw = FakeGateway()
    desk = Desk(WRITER, gw, handler(gw, ExplodingLLM()))
    run(desk.work_on(claim()))
    assert "model unavailable" in gw.failed["tsk_1"]
    statuses = [c[1][1] for c in gw.calls if c[0] == "status"]
    assert statuses == ["working", "idle"]


def test_demo_llm_runs_the_whole_flow_offline():
    gw = FakeGateway()
    h = handler(gw, DemoLLM())
    run(h.handle(MANAGER, claim(kind="assignment", title="Plan my week")))
    subtasks = gw.delegations[0]["subtasks"]
    assert 1 <= len(subtasks) <= 3
    assert all(s["assignee"] != "manager" for s in subtasks)
    run(h.handle(WRITER, claim(task_id="t2", title=subtasks[0]["title"])))
    assert "Demo mode" in gw.completed["t2"]

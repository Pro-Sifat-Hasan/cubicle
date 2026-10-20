import asyncio
import dataclasses
import json

from cubicle_agents.core.gateway import Claim, RosterUpdate
from cubicle_agents.llm.base import AgentResult
from cubicle_agents.llm.demo_llm import DemoLLM
from cubicle_agents.office.desk import Desk
from cubicle_agents.office.office import Office
from cubicle_agents.staff.roster import DEFAULT_ROSTER, Employee, Roster, Skill

from fakes import ExplodingLLM, FakeGateway, ScriptedLLM, claim, make_handler

ROSTER = list(DEFAULT_ROSTER)
MANAGER = ROSTER[0]
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
    assert call["json_schema"] is not None and call["toolbox"] is None and call["mcp"] == ()
    assert "Sam" in call["system"] and "Prefers short emails" in call["system"]
    assert gw.usage == [("tsk_1", "manager", 10, 5)]


def test_plan_only_uses_the_main_agents_team(tmp_path):
    gw = FakeGateway()
    main = dataclasses.replace(MANAGER, team=("writer",))
    roster = [main, *ROSTER[1:]]
    llm = ScriptedLLM(plan_json({"title": "Code it", "description": "d", "assignee": "engineer", "depends_on": []}))
    run(make_handler(gw, llm, tmp_path, roster=roster).handle(main, claim(kind="assignment", parent_id="")))
    assert "- writer (" in llm.calls[0]["prompt"] and "engineer" not in llm.calls[0]["prompt"]
    # The model picked someone outside the team: the plan is corrected to a team member.
    assert gw.delegations[0]["subtasks"][0]["assignee"] == "writer"


def test_main_agent_without_team_does_the_work_itself(tmp_path):
    gw = FakeGateway()
    main = dataclasses.replace(MANAGER, team=())
    llm = ScriptedLLM("All done myself.", supports_tools=True)
    run(make_handler(gw, llm, tmp_path, roster=[main]).handle(main, claim(kind="assignment", parent_id="")))
    assert gw.delegations == []
    assert gw.completed == {"tsk_1": "All done myself."}


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


def test_specialist_works_with_its_own_config(tmp_path):
    gw = FakeGateway()
    writer = dataclasses.replace(WRITER, model="claude-haiku-5-5", effort="low", instructions="Always rhyme.",
                                 skills=(Skill("tone", "Use for tone.", "Be warm."),))
    llm = ScriptedLLM("Here is the email.", supports_tools=True)
    run(make_handler(gw, llm, tmp_path, roster=[MANAGER, writer]).handle(writer, claim(parent_id="asg_7")))
    assert gw.completed == {"tsk_1": "Here is the email."}
    call = llm.calls[0]
    assert call["workdir"] == tmp_path / "asg_7"
    assert call["model"] == "claude-haiku-5-5" and call["effort"] == "low"
    assert "Always rhyme." in call["system"] and "tone: Use for tone." in call["system"]
    assert {t.name for t in call["toolbox"].tools} >= {"write_file", "read_file", "remember", "ask_owner", "load_skill"}


def test_agent_tools_follow_the_owners_choice(tmp_path):
    gw = FakeGateway()
    writer = dataclasses.replace(WRITER, tools=())  # no files, web or memory
    llm = ScriptedLLM("ok", supports_tools=True)
    run(make_handler(gw, llm, tmp_path, roster=[MANAGER, writer], client_web_fetch=True).handle(writer, claim()))
    call = llm.calls[0]
    assert {t.name for t in call["toolbox"].tools} == {"ask_owner"}
    assert call["web"] is False


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


def test_mcp_servers_reach_the_model_and_failures_are_logged(tmp_path):
    from cubicle_agents.staff.roster import McpServer

    gw = FakeGateway()
    writer = dataclasses.replace(WRITER, mcp=(McpServer("time", "Time", "stdio", command="uvx"),))
    llm = ScriptedLLM(AgentResult("done", warnings=["Time could not be reached (boom)."]), supports_tools=True)
    run(make_handler(gw, llm, tmp_path, roster=[MANAGER, writer]).handle(writer, claim()))
    assert llm.calls[0]["mcp"] == writer.mcp
    assert any("Time could not be reached" in m for m in gw.logs)


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


# ---- chat ----

def chat_claim(**kw):
    c = claim(kind="chat", title="Hi", parent_id="", conversation_id="cnv_1", **kw)
    c.task["description"] = "Can you help with my trip?"
    c.history = [{"role": "owner", "content": "Hello", "status": "done"},
                 {"role": "agent", "content": "boom", "status": "failed"},
                 {"role": "agent", "content": "Hi Sam!", "status": "done"}]
    return c


def test_chat_replies_with_history_and_streams_progress(tmp_path):
    gw = FakeGateway()

    async def reply(call):
        await call["on_event"]("status", "Searching the web…")
        await call["on_event"]("text", "Sure")
        return AgentResult("Sure, where to?", 3, 2)

    llm = ScriptedLLM(reply, supports_tools=True)
    run(make_handler(gw, llm, tmp_path).handle(MANAGER, chat_claim()))
    call = llm.calls[0]
    assert call["prompt"] == "Can you help with my trip?"
    assert [(t.role, t.content) for t in call["history"]] == [("owner", "Hello"), ("agent", "Hi Sam!")]
    assert call["workdir"] == tmp_path / "cnv_1"
    assert "chatting with Sam" in call["system"]
    tools = {t.name for t in call["toolbox"].tools}
    assert "ask_owner" not in tools and {"ask_teammate", "start_assignment"} <= tools
    assert gw.completed == {"tsk_1": "Sure, where to?"}
    assert gw.progress_events == [("tsk_1", "Searching the web…", ""), ("tsk_1", "", "Sure")]


def test_chat_lists_new_files(tmp_path):
    gw = FakeGateway()

    async def reply(call):
        await call["toolbox"].call("write_file", {"path": "itinerary.md", "content": "Day 1"})
        return AgentResult("Saved your itinerary.")

    run(make_handler(gw, ScriptedLLM(reply, supports_tools=True), tmp_path).handle(WRITER, chat_claim()))
    assert "`itinerary.md`" in gw.completed["tsk_1"]


def test_main_agent_consults_a_teammate_during_chat(tmp_path):
    gw = FakeGateway()

    async def manager_turn(call):
        answer = await call["toolbox"].call("ask_teammate",
                                            {"teammate": "researcher", "request": "Best time to visit Rome?"})
        return AgentResult(f"Rhea says: {answer.content}")

    llm = ScriptedLLM(manager_turn, "April to June.", supports_tools=True)
    run(make_handler(gw, llm, tmp_path).handle(MANAGER, chat_claim()))
    teammate_call = llm.calls[1]
    assert "REQUEST FROM MAX" in teammate_call["prompt"] and "Best time to visit Rome?" in teammate_call["prompt"]
    assert "You are Rhea" in teammate_call["system"]
    # A consulted teammate can't consult further (no loops between agents).
    assert "ask_teammate" not in {t.name for t in teammate_call["toolbox"].tools}
    assert gw.completed["tsk_1"] == "Rhea says: April to June."
    assert ("tsk_1", "researcher", 10, 5) in gw.usage
    assert any("asked Rhea for help" in m for m in gw.logs)
    assert ("status", ("researcher", "working", "tsk_1")) in gw.calls


def test_chat_can_start_an_assignment(tmp_path):
    gw = FakeGateway()

    async def reply(call):
        await call["toolbox"].call("start_assignment", {"title": "Plan Rome trip", "description": "4 days in May"})
        return AgentResult("Started!")

    run(make_handler(gw, ScriptedLLM(reply, supports_tools=True), tmp_path).handle(MANAGER, chat_claim()))
    assert gw.assignments == [("Plan Rome trip", "4 days in May", "manager")]


# ---- desks and the office ----

def test_desk_reports_failure_and_returns_to_idle(tmp_path):
    gw = FakeGateway()
    handler = make_handler(gw, ExplodingLLM(), tmp_path)
    desk = Desk("writer", "work", handler)
    run(desk.work_on(WRITER, claim()))
    assert "model unavailable" in gw.failed["tsk_1"]
    assert [c[1][1] for c in gw.calls if c[0] == "status"] == ["working", "idle"]


def test_office_follows_roster_changes(tmp_path):
    gw = FakeGateway()
    handler = make_handler(gw, DemoLLM(), tmp_path, roster=[])
    office = Office(handler.ctx)

    async def scenario():
        agents = [{"id": "manager", "name": "Max", "role": "manager", "team": ["writer"]},
                  {"id": "writer", "name": "Quill", "role": "specialist", "tools": ["files"]}]
        gw.updates = [RosterUpdate(1, agents)]
        assert await office.clock_in(asyncio.Event()) == 1
        assert gw.started == 1
        assert set(office._desks) == {("manager", "work"), ("manager", "chat"), ("writer", "work"), ("writer", "chat")}
        assert handler.ctx.roster.team_of(handler.ctx.roster.manager)[0].tools == ("files",)
        await asyncio.sleep(0)  # agents are shown as available as soon as their desks open
        assert ("status", ("writer", "idle", "")) in gw.calls

        office.apply(RosterUpdate(2, agents[:1]))  # the writer was removed
        assert set(office._desks) == {("manager", "work"), ("manager", "chat")}
        office._stop.set()
        for _, task in office._desks.values():
            task.cancel()

    run(scenario())


def test_roster_from_gateway_json():
    e = Employee.from_json({
        "id": "tess", "name": "Tess", "title": "Travel Planner", "role": "specialist", "instructions": "Plan well.",
        "tools": ["web"], "team": [], "mcp": [{"id": "time", "name": "Time", "transport": "stdio", "command": "uvx",
                                               "args": ["mcp-server-time"], "env": {"TZ": "UTC"}}],
        "skill_docs": [{"name": "packing", "description": "Use for packing lists.", "content": "Pack light."}],
    })
    assert e.mcp[0].args == ("mcp-server-time",) and dict(e.mcp[0].env) == {"TZ": "UTC"}
    assert e.skills[0].content == "Pack light." and e.has_tool("web") and not e.has_tool("files")
    roster = Roster([e, dataclasses.replace(MANAGER, team=("tess", "ghost"))])
    assert [m.id for m in roster.team_of(roster.manager)] == ["tess"]


def test_demo_llm_runs_the_whole_flow_offline(tmp_path):
    gw = FakeGateway()
    h = make_handler(gw, DemoLLM(), tmp_path)
    run(h.handle(MANAGER, claim(kind="assignment", title="Plan my week", parent_id="")))
    subtasks = gw.delegations[0]["subtasks"]
    assert 1 <= len(subtasks) <= 3
    assert all(s["assignee"] != "manager" for s in subtasks)
    run(h.handle(WRITER, claim(task_id="t2", title=subtasks[0]["title"])))
    assert "Demo mode" in gw.completed["t2"]
    run(h.handle(WRITER, chat_claim(task_id="t3")))
    assert "Quill here" in gw.completed["t3"]

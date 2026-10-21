import asyncio
import dataclasses
from pathlib import Path

from cubicle_agents.staff.roster import DEFAULT_ROSTER, McpServer
from cubicle_agents.tools import build_toolbox

from fakes import FakeGateway


def run(coro):
    return asyncio.run(coro)


def _assistant():
    base = next(e for e in DEFAULT_ROSTER if e.id == "writer")
    mail = McpServer(id="gmail", name="Gmail", transport="stdio", command="npx", args=("-y", "x"))
    return dataclasses.replace(base, mcp=(mail,))


def _box(gw, employee=None, may_approve=True):
    return build_toolbox(employee=employee or _assistant(), workspace=Path("."), gateway=gw, may_ask=False,
                         client_web_fetch=False, may_approve=may_approve)


def test_only_agents_with_connected_apps_get_the_message_tools():
    plain = build_toolbox(employee=next(e for e in DEFAULT_ROSTER if e.id == "writer"), workspace=Path("."),
                          gateway=FakeGateway(), may_ask=False, client_web_fetch=False)
    assert "propose_tasks" not in {t.name for t in plain.tools}
    assert {"triage_check", "propose_tasks", "triage_record"} <= {t.name for t in _box(FakeGateway()).tools}


def test_check_reports_only_messages_not_seen_before():
    gw = FakeGateway()
    box = _box(gw)
    run(box.call("triage_record", {"items": [{"key": "gmail:1", "status": "done", "summary": "Invoice"}]}))
    out = run(box.call("triage_check", {"keys": ["gmail:1", "gmail:2"]})).content
    assert "gmail:2" in out and "gmail:1" not in out
    assert "None of these" in run(box.call("triage_check", {"keys": ["gmail:1"]})).content
    assert run(box.call("triage_check", {"keys": []})).is_error


def test_propose_tasks_asks_once_with_the_whole_list_and_remembers_it():
    gw = FakeGateway()
    box = _box(gw)
    result = run(box.call("propose_tasks", {
        "items": [
            {"key": "gmail:1", "title": "Reply to Acme about the invoice", "from": "bob@acme.com", "source": "gmail",
             "why": "Asks for a corrected invoice by Friday", "suggestion": "Send the corrected PDF", "due": "Friday"},
            {"key": "discord:9/10", "title": "Answer Sam's question in #dev", "source": "discord"},
        ],
        "skipped": [{"key": "gmail:2", "summary": "Newsletter"}],
    }))
    assert "Approval requested" in result.content and box.question_kind == "approval"
    assert "I found 2 things that need you" in box.question and "**2. Answer Sam's question in #dev**" in box.question
    assert "Asks for a corrected invoice by Friday" in box.question and "Send the corrected PDF" in box.question
    assert gw.triage["gmail:1"]["status"] == "proposed" and gw.triage["gmail:2"]["status"] == "dismissed"


def test_propose_tasks_needs_items_and_an_open_approval_slot():
    assert run(_box(FakeGateway()).call("propose_tasks", {"items": []})).is_error
    assert run(_box(FakeGateway()).call("propose_tasks", {"items": [{"title": ""}]})).is_error
    assert run(_box(FakeGateway(), may_approve=False).call("propose_tasks", {"items": [{"title": "x"}]})).is_error


def test_record_rejects_a_bad_status():
    assert run(_box(FakeGateway()).call("triage_record", {"items": [{"key": "a", "status": "maybe"}]})).is_error

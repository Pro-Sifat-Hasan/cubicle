"""Claude Code's stream-json output, as steps and live text; and the adapter that runs the CLI."""

from __future__ import annotations

import asyncio
import json
import sys
import textwrap
from pathlib import Path

import pytest

from cubicle_agents.llm import claude_code_llm
from cubicle_agents.llm.base import Step
from cubicle_agents.llm.claude_code_llm import ClaudeCodeLLM
from cubicle_agents.llm.claude_stream import ClaudeStreamParser, result_text, tool_name
from cubicle_agents.tools.base import Tool, Toolbox


def ev(event: dict, **extra) -> str:
    return json.dumps({"type": "stream_event", "event": event, "parent_tool_use_id": None, **extra})


def start(i: int, block: dict) -> str:
    return ev({"type": "content_block_start", "index": i, "content_block": block})


def delta(i: int, d: dict) -> str:
    return ev({"type": "content_block_delta", "index": i, "delta": d})


def stop(i: int) -> str:
    return ev({"type": "content_block_stop", "index": i})


def message_start(mid: str) -> str:
    return ev({"type": "message_start", "message": {"id": mid, "content": []}})


def tool_result(tool_id: str, content, error: bool = False) -> str:
    block = {"type": "tool_result", "tool_use_id": tool_id, "content": content, **({"is_error": True} if error else {})}
    return json.dumps({"type": "user", "message": {"role": "user", "content": [block]}, "parent_tool_use_id": None})


def result(text: str, **extra) -> str:
    return json.dumps({"type": "result", "subtype": "success", "is_error": False, "result": text,
                       "usage": {"input_tokens": 10, "cache_read_input_tokens": 90, "output_tokens": 7}, **extra})


# A turn like a real one: think, say what comes next, write a file, ask a teammate, then answer.
TURN = [
    json.dumps({"type": "system", "subtype": "init"}),
    message_start("msg_1"),
    start(0, {"type": "thinking", "thinking": ""}),
    json.dumps({"type": "system", "subtype": "thinking_tokens", "estimated_tokens": 232}),
    stop(0),
    start(1, {"type": "text", "text": ""}),
    delta(1, {"type": "text_delta", "text": "I'll write the page"}),
    delta(1, {"type": "text_delta", "text": " now."}),
    stop(1),
    start(2, {"type": "tool_use", "id": "toolu_1", "name": "Write", "input": {}}),
    delta(2, {"type": "input_json_delta", "partial_json": '{"file_path": "index.ht'}),
    delta(2, {"type": "input_json_delta", "partial_json": 'ml", "content": "<h1>Hi</h1>"}'}),
    stop(2),
    json.dumps({"type": "assistant", "message": {"id": "msg_1", "content": [
        {"type": "tool_use", "id": "toolu_1", "name": "Write",
         "input": {"file_path": "index.html", "content": "<h1>Hi</h1>"}}]}}),
    tool_result("toolu_1", "File created successfully at: index.html"),
    message_start("msg_2"),
    start(0, {"type": "tool_use", "id": "toolu_2", "name": "mcp__cubicle__ask_teammate", "input": {}}),
    delta(0, {"type": "input_json_delta", "partial_json": '{"teammate": "engineer", "request": "check it"}'}),
    stop(0),
    tool_result("toolu_2", [{"type": "text", "text": "Looks fine"}], error=True),
    message_start("msg_3"),
    start(0, {"type": "text", "text": ""}),
    delta(0, {"type": "text_delta", "text": "All done."}),
    stop(0),
    result("All done.", permission_denials=[{"tool_name": "Bash", "tool_use_id": "x"}]),
]


def feed_all(lines: list[str]):
    parser = ClaudeStreamParser()
    events = []
    for line in lines:
        events += parser.feed(line)
    return parser, events


def last_by_id(events):
    steps: dict[str, Step] = {}
    for kind, value in events:
        if kind == "step":
            steps[value.id] = value
    return steps


def test_stream_becomes_thinking_remark_and_tool_steps():
    parser, events = feed_all(TURN)
    steps = last_by_id(events)

    thought = steps["msg_1-0"]
    assert (thought.kind, thought.status, thought.name) == ("thinking", "done", "~232 tokens")

    note = steps["msg_1-note2"]  # text before a tool call is a remark, not the answer
    assert (note.kind, note.output, note.status) == ("note", "I'll write the page now.", "done")

    write = steps["toolu_1"]
    assert (write.kind, write.name, write.status) == ("tool", "Write", "done")
    assert json.loads(write.input) == {"file_path": "index.html", "content": "<h1>Hi</h1>"}
    assert write.output == "File created successfully at: index.html"

    ask = steps["toolu_2"]  # Cubicle's own tools are named plainly; a failed one is marked
    assert (ask.name, ask.status, ask.output) == ("ask_teammate", "error", "Looks fine")

    assert parser.text == "All done." and parser.finished and not parser.error
    assert parser.denied == ["Bash"]
    assert parser.usage["output_tokens"] == 7


def test_tool_input_streams_in_as_it_is_written():
    _, events = feed_all(TURN[:12])
    inputs = [v.input for k, v in events if k == "step" and v.id == "toolu_1"]
    assert inputs[1] == '{"file_path": "index.ht' and inputs[2].endswith('"<h1>Hi</h1>"}')


def test_live_text_is_cleared_when_it_turns_into_a_remark():
    _, events = feed_all(TURN)
    texts = [v for k, v in events if k == "text"]
    assert texts[:2] == ["I'll write the page", "I'll write the page now."]
    assert "" in texts  # cleared before the tool call
    assert texts[-1] == "All done."


def test_status_names_what_the_tool_does():
    _, events = feed_all(TURN)
    assert [v for k, v in events if k == "status"][:2] == ["Writing a file…", "Asking a teammate…"]


def test_complete_messages_stand_in_when_there_are_no_partial_events():
    lines = [
        json.dumps({"type": "assistant", "message": {"id": "a1", "content": [
            {"type": "text", "text": "Checking."},
            {"type": "tool_use", "id": "t1", "name": "Read", "input": {"file_path": "x.txt"}}]}}),
        tool_result("t1", "1\thello"),
        json.dumps({"type": "assistant", "message": {"id": "a2", "content": [{"type": "text", "text": "It says hello."}]}}),
        result("It says hello."),
    ]
    parser, events = feed_all(lines)
    steps = last_by_id(events)
    assert steps["a1-note1"].output == "Checking." and steps["t1"].output == "1\thello"
    assert parser.text == "It says hello."


def test_sub_agent_chatter_and_junk_lines_are_ignored():
    parser = ClaudeStreamParser()
    assert parser.feed("not json") == [] and parser.feed("") == [] and parser.feed("[1]") == []
    sub = json.dumps({"type": "assistant", "parent_tool_use_id": "toolu_9", "message": {"content": []}})
    assert parser.feed(sub) == []


def test_errors_and_helpers():
    parser, _ = feed_all([json.dumps({"type": "result", "is_error": True, "result": "Credit balance is too low"})])
    assert parser.error == "Credit balance is too low"
    assert tool_name("mcp__cubicle__remember") == "remember"
    assert tool_name("mcp__github__create_issue") == "github_create_issue"
    assert tool_name("Read") == "Read"
    assert result_text([{"type": "text", "text": "a"}, {"type": "image"}, "b"]) == "a\n[image]\nb"


# ---- the adapter, against a stand-in for the CLI ----

FAKE_CLI = textwrap.dedent("""
    import json, sys, urllib.request
    config = json.load(open(sys.argv[1]))["mcpServers"]["cubicle"]
    sys.stdin.read()
    def call(name, args):
        body = {"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": {"name": name, "arguments": args}}
        req = urllib.request.Request(config["url"], method="POST", data=json.dumps(body).encode(),
                                     headers={**config["headers"], "Content-Type": "application/json"})
        return json.load(urllib.request.urlopen(req))["result"]["content"][0]["text"]
    answer = call("ask_teammate", {"teammate": "engineer", "request": "build it"})
    call("ask_owner", {"question": "Which colour?"})
    lines = [
        {"type": "stream_event", "event": {"type": "message_start", "message": {"id": "m1"}}},
        {"type": "stream_event", "event": {"type": "content_block_start", "index": 0,
                                           "content_block": {"type": "text", "text": ""}}},
        {"type": "stream_event", "event": {"type": "content_block_delta", "index": 0,
                                           "delta": {"type": "text_delta", "text": answer}}},
        {"type": "result", "is_error": False, "result": answer, "usage": {"input_tokens": 5, "output_tokens": 2}},
    ]
    for line in lines:
        print(json.dumps(line), flush=True)
""")


def test_adapter_streams_and_serves_cubicle_tools_over_the_bridge(tmp_path, monkeypatch):
    script = tmp_path / "fake_claude.py"
    script.write_text(FAKE_CLI, encoding="utf-8")
    captured: dict = {}

    def fake_command(exe, *, model, system_file, tools, mcp_file, mcp_names, effort):
        captured.update(tools=tools, mcp_names=mcp_names, system=Path(system_file).read_text(encoding="utf-8"))
        return [sys.executable, str(script), str(mcp_file)]

    monkeypatch.setattr(claude_code_llm, "build_command", fake_command)
    monkeypatch.setattr(claude_code_llm, "find_cli", lambda: "claude")

    toolbox = Toolbox()

    async def ask_teammate(args):
        return f"{args['teammate']} built it"

    async def ask_owner(args):
        toolbox.question = args["question"]
        return "sent"

    toolbox.tools += [
        Tool("ask_teammate", "", {"type": "object"}, ask_teammate),
        Tool("ask_owner", "", {"type": "object"}, ask_owner),
        Tool("read_file", "", {"type": "object"}, ask_teammate),
        Tool("write_file", "", {"type": "object"}, ask_teammate),
    ]
    events: list[tuple[str, str]] = []

    async def on_event(kind, text):
        events.append((kind, text))

    result = asyncio.run(ClaudeCodeLLM().run(system="SYSTEM PROMPT", prompt="hi", model="sonnet", toolbox=toolbox,
                                             workdir=tmp_path / "work", on_event=on_event, web=False))

    assert result.text == "engineer built it"  # the bridge ran the tool for the CLI
    assert (result.input_tokens, result.output_tokens) == (5, 2)
    assert (result.question, result.question_kind) == ("Which colour?", "question")
    assert events[0] == ("status", "Working in Claude Code…") and events[-1] == ("text", "engineer built it")
    assert captured["tools"] == ["Read", "Glob", "Grep", "Write", "Edit"]  # web=False: no web tools
    assert captured["mcp_names"] == ["cubicle"] and captured["system"] == "SYSTEM PROMPT"


def test_adapter_reports_a_failed_run(tmp_path, monkeypatch):
    script = tmp_path / "broken.py"
    script.write_text("import sys\nsys.stderr.write('not logged in')\nsys.exit(1)\n", encoding="utf-8")
    monkeypatch.setattr(claude_code_llm, "build_command", lambda *a, **k: [sys.executable, str(script)])
    monkeypatch.setattr(claude_code_llm, "find_cli", lambda: "claude")
    with pytest.raises(RuntimeError, match="not logged in"):
        asyncio.run(ClaudeCodeLLM().run(system="s", prompt="p", model="", workdir=tmp_path / "w"))

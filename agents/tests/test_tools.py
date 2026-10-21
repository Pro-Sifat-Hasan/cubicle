import asyncio

import pytest

from cubicle_agents.office.directives import extract
from cubicle_agents.staff.roster import DEFAULT_ROSTER, McpServer, Skill
from cubicle_agents.tools import build_toolbox
from cubicle_agents.tools.base import ToolError
from cubicle_agents.tools.mcp import MCPUnavailableError, server_env
from cubicle_agents.tools.web import check_url, html_to_text
from cubicle_agents.tools.workspace import Workspace

from fakes import FakeGateway

WRITER = next(e for e in DEFAULT_ROSTER if e.id == "writer")


def run(coro):
    return asyncio.run(coro)


# ---- workspace sandbox ----

def test_workspace_write_read_list(tmp_path):
    ws = Workspace(tmp_path / "asg")
    run(ws.write_file({"path": "drafts/email.md", "content": "Hello"}))
    assert run(ws.read_file({"path": "drafts/email.md"})) == "Hello"
    assert run(ws.list_files({})) == "drafts/email.md"


@pytest.mark.parametrize("path", ["../escape.txt", "a/../../b.txt", "/etc/passwd", "C:\\Windows\\win.ini", "", "  "])
def test_workspace_rejects_escapes(tmp_path, path):
    with pytest.raises(ToolError):
        Workspace(tmp_path / "asg").resolve(path)


def test_toolbox_turns_errors_into_results(tmp_path):
    box = build_toolbox(employee=WRITER, workspace=tmp_path, gateway=FakeGateway(), may_ask=True, client_web_fetch=False)
    bad = run(box.call("read_file", {"path": "../../secret"}))
    assert bad.is_error
    assert run(box.call("no_such_tool", {})).is_error
    assert run(box.call("write_file", None)).is_error


def test_remember_and_ask_tools(tmp_path):
    gw = FakeGateway()
    box = build_toolbox(employee=WRITER, workspace=tmp_path, gateway=gw, may_ask=True, client_web_fetch=True)
    assert {t.name for t in box.tools} == {"write_file", "read_file", "list_files", "edit_file", "insert_lines",
                                           "find_files", "grep_files", "remember", "forget", "ask_owner", "web_fetch"}
    run(box.call("remember", {"fact": "Owner is vegetarian"}))
    run(box.call("ask_owner", {"question": "Which city?"}))
    assert gw.memories == ["Owner is vegetarian"]
    assert box.question == "Which city?"


def test_skill_tool_loads_full_instructions(tmp_path):
    import dataclasses

    writer = dataclasses.replace(WRITER, skills=(Skill("email-writing", "Use for emails.", "Lead with the point."),))
    box = build_toolbox(employee=writer, workspace=tmp_path, gateway=FakeGateway(), may_ask=False, client_web_fetch=False)
    assert "Lead with the point." in run(box.call("load_skill", {"name": "email-writing"})).content
    assert run(box.call("load_skill", {"name": "nope"})).is_error


def test_mcp_server_env_only_passes_configured_secrets():
    server = McpServer("brave", "Brave", "stdio", command="npx", env=(("MODE", "x"),), secret_env=("BRAVE_API_KEY",))
    environ = {"PATH": "C:\\bin", "BRAVE_API_KEY": "b-key", "ANTHROPIC_API_KEY": "sk-secret"}
    env = server_env(server, environ)
    assert env == {"PATH": "C:\\bin", "MODE": "x", "BRAVE_API_KEY": "b-key"}
    with pytest.raises(MCPUnavailableError, match="BRAVE_API_KEY"):
        server_env(server, {"PATH": "C:\\bin"})


# ---- web fetch SSRF guard ----

@pytest.mark.parametrize("url", [
    "http://127.0.0.1:7878/api/v1/status",
    "http://localhost/",
    "http://10.0.0.5/",
    "http://192.168.1.1/admin",
    "http://169.254.169.254/latest/meta-data/",
    "http://[::1]/",
    "http://[::ffff:127.0.0.1]/",
    "file:///etc/passwd",
    "ftp://example.com/",
    "http://user:pass@example.com/",
])
def test_web_fetch_blocks_non_public_targets(url):
    with pytest.raises(ToolError):
        run(check_url(url))


def test_html_to_text_strips_scripts():
    html = "<html><head><title>x</title><script>evil()</script></head><body><h1>Hi</h1><p>There</p></body></html>"
    assert html_to_text(html) == "Hi\nThere"


# ---- text directives ----

def test_extract_directives():
    d = extract("REMEMBER: Likes tea\nThe answer\nremember: also coffee")
    assert d.memories == ["Likes tea", "also coffee"]
    assert d.text == "The answer" and d.question is None
    assert extract("ASK_OWNER: Which file?\nignored").question == "Which file?"
    assert extract("ASK_OWNER:").question == "Could you give me more detail?"


def _tools(tmp_path, **kw):
    return build_toolbox(employee=WRITER, workspace=tmp_path, gateway=FakeGateway(), may_ask=False,
                         client_web_fetch=False, **kw)


def test_edit_file_replaces_exact_unique_text(tmp_path):
    box = _tools(tmp_path)
    run(box.call("write_file", {"path": "a.py", "content": "x = 1\ny = 2\nx = 1\n"}))
    done = run(box.call("edit_file", {"path": "a.py", "old_text": "y = 2", "new_text": "y = 3"}))
    assert done.content.startswith("Edited a.py")
    assert (tmp_path / "a.py").read_text() == "x = 1\ny = 3\nx = 1\n"
    ambiguous = run(box.call("edit_file", {"path": "a.py", "old_text": "x = 1", "new_text": "x = 9"}))
    assert ambiguous.is_error and "2 times" in ambiguous.content
    everywhere = run(box.call("edit_file", {"path": "a.py", "old_text": "x = 1", "new_text": "x = 9", "replace_all": True}))
    assert "replaced 2" in everywhere.content
    assert run(box.call("edit_file", {"path": "a.py", "old_text": "nope", "new_text": "z"})).is_error
    assert run(box.call("edit_file", {"path": "../a.py", "old_text": "x", "new_text": "z"})).is_error


def test_find_and_grep_files(tmp_path):
    box = _tools(tmp_path)
    run(box.call("write_file", {"path": "src/app.py", "content": "def main():\n    return 'Hello'\n"}))
    run(box.call("write_file", {"path": "src/util.ts", "content": "export const hello = 1\n"}))
    run(box.call("write_file", {"path": "notes.md", "content": "hello world\n"}))
    assert run(box.call("find_files", {"pattern": "*.py"})).content == "src/app.py"
    assert run(box.call("find_files", {"pattern": "src/**/*.ts"})).content == "src/util.ts"
    assert run(box.call("find_files", {"pattern": "*.rs"})).content == "No files match."
    assert run(box.call("find_files", {"pattern": "/etc/*"})).is_error
    hits = run(box.call("grep_files", {"pattern": "hello", "ignore_case": True})).content.splitlines()
    assert hits == ["notes.md:1: hello world", "src/app.py:2: return 'Hello'", "src/util.ts:1: export const hello = 1"]
    assert run(box.call("grep_files", {"pattern": "hello", "file_pattern": "*.md"})).content == "notes.md:1: hello world"
    assert run(box.call("grep_files", {"pattern": "("})).is_error


def test_terminal_is_only_offered_to_agents_that_have_it(tmp_path):
    import dataclasses

    assert "run_command" not in {t.name for t in _tools(tmp_path).tools}
    dev = dataclasses.replace(WRITER, tools=(*WRITER.tools, "terminal"))
    box = build_toolbox(employee=dev, workspace=tmp_path, gateway=FakeGateway(), may_ask=False, client_web_fetch=False)
    assert "run_command" in {t.name for t in box.tools}


def _edit_box(tmp_path, name, content):
    box = _tools(tmp_path)
    (tmp_path / name).write_bytes(content.encode("utf-8"))
    return box


def test_edit_file_shows_the_diff(tmp_path):
    box = _edit_box(tmp_path, "a.py", "def f():\n    return 1\n")
    done = run(box.call("edit_file", {"path": "a.py", "old_text": "return 1", "new_text": "return 2"})).content
    assert "-    return 1" in done and "+    return 2" in done


def test_edit_file_applies_several_edits_together_or_not_at_all(tmp_path):
    box = _edit_box(tmp_path, "site.css", "a { color: red; }\nb { color: blue; }\n")
    edits = [{"old_text": "red", "new_text": "green"}, {"old_text": "blue", "new_text": "navy"}]
    done = run(box.call("edit_file", {"path": "site.css", "edits": edits}))
    assert "Edit 1: replaced 1" in done.content and "Edit 2: replaced 1" in done.content
    assert (tmp_path / "site.css").read_text() == "a { color: green; }\nb { color: navy; }\n"
    bad = [{"old_text": "green", "new_text": "lime"}, {"old_text": "missing", "new_text": "x"}]
    failed = run(box.call("edit_file", {"path": "site.css", "edits": bad}))
    assert failed.is_error and failed.content.startswith("Edit 2:")
    assert "green" in (tmp_path / "site.css").read_text()  # the first edit was not kept either


def test_edit_file_keeps_windows_line_endings(tmp_path):
    box = _edit_box(tmp_path, "w.txt", "one\r\ntwo\r\nthree\r\n")
    run(box.call("edit_file", {"path": "w.txt", "old_text": "one\ntwo", "new_text": "uno\ndos"}))
    assert (tmp_path / "w.txt").read_bytes() == b"uno\r\ndos\r\nthree\r\n"


def test_edit_file_forgives_wrong_indentation_but_keeps_the_files(tmp_path):
    box = _edit_box(tmp_path, "m.py", "class A:\n    def run(self):\n        x = 1\n        return x\n")
    done = run(box.call("edit_file", {"path": "m.py", "old_text": "x = 1\nreturn x", "new_text": "x = 2\nreturn x * 2"}))
    assert "ignoring indentation" in done.content
    assert (tmp_path / "m.py").read_text() == "class A:\n    def run(self):\n        x = 2\n        return x * 2\n"


def test_edit_file_explains_a_miss_and_an_ambiguity(tmp_path):
    box = _edit_box(tmp_path, "n.py", "alpha = 1\nbeta = 2\nalpha = 1\n")
    miss = run(box.call("edit_file", {"path": "n.py", "old_text": "beta = 22", "new_text": "beta = 3"}))
    assert miss.is_error and "most similar line is 2" in miss.content
    twice = run(box.call("edit_file", {"path": "n.py", "old_text": "alpha = 1", "new_text": "alpha = 5"}))
    assert twice.is_error and "lines 1, 3" in twice.content
    assert run(box.call("edit_file", {"path": "n.py", "old_text": "beta = 2", "new_text": "beta = 2"})).is_error
    assert run(box.call("edit_file", {"path": "n.py", "edits": []})).is_error


def test_edit_file_warns_when_it_breaks_python_or_json(tmp_path):
    box = _edit_box(tmp_path, "b.py", "def f():\n    return 1\n")
    broke = run(box.call("edit_file", {"path": "b.py", "old_text": "def f():", "new_text": "def f(:"}))
    assert not broke.is_error and "WARNING" in broke.content and "no longer parses" in broke.content
    box = _edit_box(tmp_path, "c.json", '{"a": 1}')
    trailing = run(box.call("edit_file", {"path": "c.json", "old_text": "1", "new_text": "1,"}))
    assert "no longer valid JSON" in trailing.content
    ok = run(box.call("edit_file", {"path": "c.json", "old_text": "1,", "new_text": "2"}))
    assert "WARNING" not in ok.content


def test_insert_lines_adds_text_at_a_line(tmp_path):
    box = _edit_box(tmp_path, "i.txt", "a\nb\nc\n")
    run(box.call("insert_lines", {"path": "i.txt", "after_line": 1, "text": "x\ny"}))
    run(box.call("insert_lines", {"path": "i.txt", "after_line": 0, "text": "top"}))
    run(box.call("insert_lines", {"path": "i.txt", "after_line": 6, "text": "end"}))
    assert (tmp_path / "i.txt").read_text() == "top\na\nx\ny\nb\nc\nend\n"
    assert run(box.call("insert_lines", {"path": "i.txt", "after_line": 99, "text": "z"})).is_error
    assert run(box.call("insert_lines", {"path": "i.txt", "after_line": -1, "text": "z"})).is_error

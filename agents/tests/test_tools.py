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
    assert {t.name for t in box.tools} == {"write_file", "read_file", "list_files", "remember", "ask_owner", "web_fetch"}
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

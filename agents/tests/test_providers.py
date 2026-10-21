"""Provider adapters. The Pydantic AI engine is exercised offline with Pydantic AI's
FunctionModel, and MCP with a real stdio server started by the test."""

import asyncio
import json
import sys
from pathlib import Path

import pytest
from pydantic_ai import ModelResponse, TextPart, ToolCallPart
from pydantic_ai.models.function import AgentInfo, DeltaToolCall, FunctionModel

from cubicle_agents.core.settings import LLMSettings
from cubicle_agents.llm import make_llm
from cubicle_agents.llm.base import Turn
from cubicle_agents.llm.claude_code_llm import ClaudeCodeLLM, build_command, mcp_config, parse_output, with_history
from cubicle_agents.llm.demo_llm import DemoLLM
from cubicle_agents.llm.pydantic_llm import PydanticLLM
from cubicle_agents.staff import prompts
from cubicle_agents.staff.roster import DEFAULT_ROSTER, McpServer
from cubicle_agents.tools import build_toolbox

from fakes import FakeGateway

WRITER = next(e for e in DEFAULT_ROSTER if e.id == "writer")
ECHO = McpServer("echo", "Echo", "stdio", command=sys.executable,
                 args=("-I", str(Path(__file__).with_name("mcp_echo_server.py"))),
                 env=(("ECHO_TOKEN", "plain-value"),))


def run(coro):
    return asyncio.run(coro)


def engine(fn=None, stream=None, **kw) -> PydanticLLM:
    model = FunctionModel(fn, stream_function=stream) if stream else FunctionModel(fn)
    return PydanticLLM(kind="openai_compatible", provider="ollama", web_access=False,
                       model_factory=lambda _name: model, **kw)


def last_part(messages):
    return messages[-1].parts[-1]


def test_tool_loop_runs_cubicle_tools_and_returns_final_text(tmp_path):
    seen = {}

    def model(messages, info: AgentInfo):
        seen["tools"] = sorted(t.name for t in info.function_tools)
        if last_part(messages).part_kind == "user-prompt":
            return ModelResponse(parts=[ToolCallPart("write_file", {"path": "notes.md", "content": "Hi"}),
                                        ToolCallPart("read_file", {"path": "../escape"})])
        results = {p.tool_name: p.content for p in messages[-1].parts}
        assert results["read_file"].startswith("ERROR:")  # tool errors go back to the model as text
        return ModelResponse(parts=[TextPart("Saved notes.md")])

    box = build_toolbox(employee=WRITER, workspace=tmp_path, gateway=FakeGateway(), may_ask=False, client_web_fetch=False)
    result = run(engine(model).run(system="s", prompt="go", model="m", toolbox=box, workdir=tmp_path))
    assert result.text == "Saved notes.md"
    assert (tmp_path / "notes.md").read_text(encoding="utf-8") == "Hi"
    assert seen["tools"] == ["edit_file", "find_files", "forget", "grep_files", "insert_lines", "list_files", "read_file",
                             "remember", "save_project", "write_file"]
    assert result.input_tokens > 0 and result.output_tokens > 0


def test_structured_plan_is_validated_json_without_tools():
    def model(messages, info: AgentInfo):
        assert info.function_tools == []
        return ModelResponse(parts=[TextPart(json.dumps({"summary": "s", "question_for_owner": "", "subtasks": []}))])

    result = run(engine(model).run(system="s", prompt="plan", model="m", json_schema=prompts.PLAN_SCHEMA))
    assert json.loads(result.text)["summary"] == "s"


def test_unusable_plan_returns_empty_text_for_fallback():
    def model(messages, info):
        return ModelResponse(parts=[TextPart("not json at all")])

    result = run(engine(model).run(system="s", prompt="plan", model="m", json_schema=prompts.PLAN_SCHEMA))
    assert result.text == ""


def test_ask_owner_ends_the_run(tmp_path):
    def model(messages, info: AgentInfo):
        assert "ask_owner" in [t.name for t in info.output_tools]
        return ModelResponse(parts=[ToolCallPart("ask_owner", {"question": "Which city?"})])

    box = build_toolbox(employee=WRITER, workspace=tmp_path, gateway=FakeGateway(), may_ask=True, client_web_fetch=False)
    result = run(engine(model).run(system="s", prompt="go", model="m", toolbox=box))
    assert result.question == "Which city?" and result.text == ""


def test_chat_history_reaches_the_model():
    def model(messages, info):
        texts = [str(getattr(p, "content", "")) for m in messages for p in m.parts]
        assert texts[:3] == ["Hello\n\nAre you there?", "Hi Sam!", "And now?"]
        return ModelResponse(parts=[TextPart("Yes")])

    history = [Turn("agent", "orphan"), Turn("owner", "Hello"), Turn("owner", "Are you there?"), Turn("agent", "Hi Sam!")]
    assert run(engine(model).run(system="s", prompt="And now?", model="m", history=history)).text == "Yes"


def test_streaming_reports_tools_and_partial_text(tmp_path):
    async def stream(messages, info):
        if last_part(messages).part_kind == "user-prompt":
            yield {0: DeltaToolCall(name="list_files", json_args="{}")}
            return
        for word in ["All ", "files ", "listed."]:
            yield word

    events = []

    async def on_event(kind, text):
        events.append((kind, text))

    box = build_toolbox(employee=WRITER, workspace=tmp_path, gateway=FakeGateway(), may_ask=False, client_web_fetch=False)
    result = run(engine(stream=stream).run(system="s", prompt="go", model="m", toolbox=box, on_event=on_event))
    assert result.text == "All files listed."
    assert ("status", "Looking at the files…") in events
    assert any(kind == "text" and text.startswith("All") for kind, text in events)


def test_mcp_server_tools_are_available_and_work(tmp_path):
    def model(messages, info: AgentInfo):
        names = [t.name for t in info.function_tools]
        assert "echo_shout" in names and "echo_secret_hint" in names
        if last_part(messages).part_kind == "user-prompt":
            return ModelResponse(parts=[ToolCallPart("echo_shout", {"text": "hello"}), ToolCallPart("echo_secret_hint", {})])
        return ModelResponse(parts=[TextPart(" | ".join(str(p.content) for p in messages[-1].parts))])

    result = run(engine(model).run(system="s", prompt="go", model="m", mcp=(ECHO,), workdir=tmp_path))
    assert "HELLO" in result.text
    assert "token=set" in result.text and "other=missing" in result.text  # only configured variables are passed
    assert not result.warnings


def test_broken_mcp_server_is_skipped_with_a_warning(tmp_path):
    broken = McpServer("broken", "Broken", "stdio", command=sys.executable, args=("-c", "import sys; sys.exit(3)"))
    missing = McpServer("gh", "GitHub", "http", url="https://example.invalid/mcp", auth_secret="NOT_SET_ANYWHERE_123")

    def model(messages, info: AgentInfo):
        assert "Unavailable right now" in (info.instructions or "")
        return ModelResponse(parts=[TextPart("Worked anyway")])

    result = run(engine(model).run(system="s", prompt="go", model="m", mcp=(broken, missing), workdir=tmp_path))
    assert result.text == "Worked anyway"
    assert len(result.warnings) == 2 and "NOT_SET_ANYWHERE_123" in result.warnings[1]


def test_mcp_tools_command_lists_tools(monkeypatch, capsys):
    from cubicle_agents.cli import _mcp_tools

    monkeypatch.setenv("CUBICLE_MCP_SERVER", json.dumps({
        "id": "echo", "name": "Echo", "transport": "stdio", "command": ECHO.command, "args": list(ECHO.args)}))
    assert run(_mcp_tools(as_json=True)) == 0
    out = json.loads(capsys.readouterr().out.strip().splitlines()[-1])
    assert {t["name"] for t in out["tools"]} == {"echo_shout", "echo_secret_hint"}


def test_capabilities_per_provider():
    anthropic = PydanticLLM(kind="anthropic", provider="anthropic")
    assert {type(c).__name__ for c in anthropic._capabilities(effort="high", web=True)} == \
        {"Thinking", "Caching", "WebSearch", "WebFetch"}
    ollama = PydanticLLM(kind="openai_compatible", provider="ollama")
    caps = ollama._capabilities(effort="high", web=True)
    assert [type(c).__name__ for c in caps] == ["WebSearch"]
    assert ollama._capabilities(effort="high", web=False) == []
    assert anthropic._settings("max")["anthropic_effort"] == "max"


# ---- Claude Code CLI ----

def test_claude_code_command_is_locked_down(tmp_path):
    cmd = build_command("claude", model="sonnet", system_file=tmp_path / "s.md", tools=["Read", "Write"])
    assert cmd[:6] == ["claude", "-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages"]
    assert "Bash" not in cmd and "WebFetch" not in cmd and "--mcp-config" not in cmd
    assert cmd[cmd.index("--permission-mode") + 1] == "dontAsk"
    assert cmd[cmd.index("--tools") + 1:cmd.index("--tools") + 3] == ["Read", "Write"]
    assert cmd[cmd.index("--allowedTools") + 1:cmd.index("--allowedTools") + 3] == ["Read", "Write"]
    # The owner's own Claude Code setup (skills, MCP servers, settings, hooks) is never loaded.
    assert "--strict-mcp-config" in cmd and "--disable-slash-commands" in cmd
    assert cmd[cmd.index("--setting-sources") + 1] == ""
    assert cmd[-2:] == ["--model", "sonnet"]


def test_claude_code_without_tools_gets_none_and_effort_is_passed(tmp_path):
    cmd = build_command("claude", model="", system_file=tmp_path / "s.md", tools=[], effort="high")
    assert cmd[cmd.index("--tools") + 1] == "" and "--allowedTools" not in cmd
    assert cmd[-2:] == ["--effort", "high"]
    assert "--effort" not in build_command("claude", model="", system_file=tmp_path / "s.md", tools=[], effort="warp")


def test_claude_code_file_tools_follow_the_agents_toolbox():
    from cubicle_agents.llm.claude_code_llm import file_tools
    from cubicle_agents.tools.base import Tool, Toolbox

    def box(*names):
        return Toolbox([Tool(n, "", {}, None) for n in names])  # type: ignore[arg-type]

    assert file_tools(box("read_file", "write_file", "edit_file")) == ["Read", "Glob", "Grep", "Write", "Edit"]
    assert file_tools(box("read_file", "list_files")) == ["Read", "Glob", "Grep"]  # a main agent with a team: look only
    assert file_tools(box("remember")) == []


def test_claude_code_gets_mcp_servers_without_secrets_on_disk(tmp_path):
    gh = McpServer("github", "GitHub", "http", url="https://api.example/mcp", auth_secret="GITHUB_TOKEN")
    brave = McpServer("brave-search", "Brave", "stdio", command="npx", args=("-y", "brave"), secret_env=("BRAVE_API_KEY",))
    config = mcp_config((gh, brave))["mcpServers"]
    assert config["github"]["headers"]["Authorization"] == "Bearer ${GITHUB_TOKEN}"
    assert config["brave_search"]["env"] == {"BRAVE_API_KEY": "${BRAVE_API_KEY}"}
    cmd = build_command("claude", model="", system_file=tmp_path / "s.md", tools=["Read"], mcp_file=tmp_path / "m.json",
                        mcp_names=["github", "brave_search"])
    assert "mcp__github" in cmd and "--strict-mcp-config" in cmd


def test_claude_code_history_is_part_of_the_prompt():
    prompt = with_history("And now?", [Turn("owner", "Hi"), Turn("agent", "Hello!")])
    assert prompt.index("OWNER: Hi") < prompt.index("YOU: Hello!") < prompt.index("And now?")


def test_claude_code_parses_json_output():
    out = json.dumps({"type": "result", "is_error": False, "result": "All done",
                      "usage": {"input_tokens": 10, "cache_read_input_tokens": 90, "output_tokens": 7}})
    result = parse_output(out.encode(), b"", 0)
    assert (result.text, result.input_tokens, result.output_tokens) == ("All done", 100, 7)


# ---- factory ----

def test_make_llm_picks_adapter():
    assert isinstance(make_llm(LLMSettings(kind="demo", provider="demo", model="demo")), DemoLLM)
    assert isinstance(make_llm(LLMSettings(kind="claude_code", provider="claude_code", model="sonnet")), ClaudeCodeLLM)
    llm = make_llm(LLMSettings(kind="openai_compatible", provider="ollama", model="llama3.3",
                               base_url="http://127.0.0.1:11434/v1"))
    assert isinstance(llm, PydanticLLM) and llm.name == "ollama"
    assert isinstance(make_llm(LLMSettings(kind="anthropic", provider="anthropic", model="claude-opus-5-5")), PydanticLLM)


def test_missing_model_is_a_config_error():
    from cubicle_agents.llm.base import LLMConfigError

    with pytest.raises(LLMConfigError):
        run(engine(lambda m, i: None).run(system="s", prompt="p", model=""))

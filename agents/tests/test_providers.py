"""Provider adapters tested against fake SDK clients (no network)."""

import asyncio
import json
from types import SimpleNamespace as NS

from cubicle_agents.core.settings import LLMSettings
from cubicle_agents.llm import make_llm
from cubicle_agents.llm.anthropic_llm import AnthropicLLM
from cubicle_agents.llm.claude_code_llm import ClaudeCodeLLM, build_command, parse_output
from cubicle_agents.llm.demo_llm import DemoLLM
from cubicle_agents.llm.openai_llm import OpenAICompatibleLLM
from cubicle_agents.tools import build_toolbox

from fakes import FakeGateway


def run(coro):
    return asyncio.run(coro)


class Recorder:
    """Fake SDK method: returns queued responses and records kwargs."""

    def __init__(self, *responses):
        self.responses = list(responses)
        self.calls = []

    async def __call__(self, **kwargs):
        self.calls.append(json.loads(json.dumps(kwargs, default=lambda o: o.__dict__)))
        return self.responses.pop(0)


# ---- Anthropic ----

def a_block(**kw):
    return NS(**kw)


def a_response(stop_reason, *content, inp=100, out=20):
    return NS(stop_reason=stop_reason, stop_details=None, content=list(content),
              usage=NS(input_tokens=inp, output_tokens=out))


def test_anthropic_tool_loop_runs_tools_and_returns_final_text(tmp_path):
    create = Recorder(
        a_response("tool_use", a_block(type="tool_use", id="t1", name="write_file",
                                       input={"path": "plan.md", "content": "# Plan"})),
        a_response("end_turn", a_block(type="text", text="Saved the plan.")),
    )
    client = NS(beta=NS(messages=NS(create=create)))
    llm = AnthropicLLM(client=client, web_access=True)
    box = build_toolbox(workspace=tmp_path, gateway=FakeGateway(), employee_id="writer",
                        may_ask=False, client_web_fetch=False)

    result = run(llm.run(system="sys", prompt="go", model="claude-opus-5-5", toolbox=box))

    assert result.text == "Saved the plan."
    assert (result.input_tokens, result.output_tokens) == (200, 40)
    assert (tmp_path / "plan.md").read_text(encoding="utf-8") == "# Plan"
    first, second = create.calls
    tool_names = {t.get("name") for t in first["tools"]}
    assert {"write_file", "web_search", "web_fetch"} <= tool_names
    assert first["thinking"] == {"type": "adaptive"} and first["fallbacks"] == "default"
    assert second["messages"][-1]["content"][0]["tool_use_id"] == "t1"


def test_anthropic_plan_uses_structured_output_without_tools():
    create = Recorder(a_response("end_turn", a_block(type="text", text='{"subtasks": []}')))
    llm = AnthropicLLM(client=NS(beta=NS(messages=NS(create=create))))
    run(llm.run(system="s", prompt="p", model="m", json_schema={"type": "object"}))
    call = create.calls[0]
    assert "tools" not in call
    assert call["output_config"]["format"]["type"] == "json_schema"


def test_anthropic_stops_when_owner_is_asked(tmp_path):
    create = Recorder(a_response("tool_use", a_block(type="tool_use", id="t1", name="ask_owner",
                                                     input={"question": "Budget?"})))
    llm = AnthropicLLM(client=NS(beta=NS(messages=NS(create=create))), web_access=False)
    box = build_toolbox(workspace=tmp_path, gateway=FakeGateway(), employee_id="analyst",
                        may_ask=True, client_web_fetch=False)
    result = run(llm.run(system="s", prompt="p", model="m", toolbox=box))
    assert result.question == "Budget?"
    assert len(create.calls) == 1


# ---- OpenAI-compatible ----

def o_response(content=None, tool_calls=None):
    return NS(choices=[NS(message=NS(content=content, tool_calls=tool_calls))],
              usage=NS(prompt_tokens=50, completion_tokens=10))


def test_openai_tool_loop(tmp_path):
    call = NS(id="c1", function=NS(name="write_file", arguments=json.dumps({"path": "a.txt", "content": "hi"})))
    create = Recorder(o_response(tool_calls=[call]), o_response(content="Done."))
    client = NS(chat=NS(completions=NS(create=create)))
    llm = OpenAICompatibleLLM(name="openai", base_url="", client=client)
    box = build_toolbox(workspace=tmp_path, gateway=FakeGateway(), employee_id="engineer",
                        may_ask=False, client_web_fetch=True)

    result = run(llm.run(system="s", prompt="p", model="gpt-5", toolbox=box))

    assert result.text == "Done." and (tmp_path / "a.txt").exists()
    assert result.input_tokens == 100
    second = create.calls[1]["messages"]
    assert second[-2]["tool_calls"][0]["id"] == "c1"
    assert second[-1] == {"role": "tool", "tool_call_id": "c1", "content": "Saved a.txt (2 bytes)."}
    assert "web_fetch" in {t["function"]["name"] for t in create.calls[0]["tools"]}


def test_openai_bad_tool_arguments_become_error_results(tmp_path):
    call = NS(id="c1", function=NS(name="write_file", arguments="{not json"))
    create = Recorder(o_response(tool_calls=[call]), o_response(content="ok"))
    llm = OpenAICompatibleLLM(name="x", base_url="", client=NS(chat=NS(completions=NS(create=create))))
    box = build_toolbox(workspace=tmp_path, gateway=FakeGateway(), employee_id="e", may_ask=False,
                        client_web_fetch=False)
    run(llm.run(system="s", prompt="p", model="m", toolbox=box))
    assert create.calls[1]["messages"][-1]["content"].startswith("ERROR:")


# ---- Claude Code CLI ----

def test_claude_code_command_is_locked_down(tmp_path):
    cmd = build_command("claude", model="sonnet", system_file=tmp_path / "s.md", web=False)
    assert cmd[:4] == ["claude", "-p", "--output-format", "json"]
    assert "Bash" not in cmd and "WebFetch" not in cmd
    assert cmd[cmd.index("--permission-mode") + 1] == "dontAsk"
    assert cmd[-2:] == ["--model", "sonnet"]


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
    assert isinstance(llm, OpenAICompatibleLLM) and llm.name == "ollama"

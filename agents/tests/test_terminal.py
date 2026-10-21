import asyncio
import sys

import pytest

from cubicle_agents.tools import terminal
from cubicle_agents.tools.base import ToolError


def run(coro):
    return asyncio.run(coro)


@pytest.mark.parametrize("command", ["ls | cat", "pytest && rm -rf x", "echo hi > out.txt", "python -c `id`",
                                      "git log; rm x", "x $(id)"])
def test_shell_syntax_is_refused(command):
    with pytest.raises(ToolError, match="one plain command"):
        terminal.parse_command(command)


def test_quotes_are_understood():
    assert terminal.parse_command('git commit -m "fix the bug"')[-1] == "fix the bug"


@pytest.mark.parametrize("command", ["rm -rf .", "powershell -c x", "curl http://x", "bash -c x", "git push origin main",
                                      "git clone http://x", "npm publish", "python -m pip install x",
                                      "C:/Windows/py.exe x"])
def test_dangerous_commands_are_not_available(command):
    with pytest.raises(ToolError):
        terminal.check_allowed(terminal.parse_command(command))


def test_only_read_only_git_runs_without_approval():
    assert not terminal.needs_approval(["git", "status"])
    assert not terminal.needs_approval(["git", "diff", "--stat"])
    assert terminal.needs_approval(["git", "branch", "new-feature"])  # creates a branch
    assert terminal.needs_approval(["git", "commit", "-m", "x"])
    assert terminal.needs_approval(["pytest", "-q"])
    assert terminal.needs_approval(["python", "script.py"])


def test_approval_must_name_the_command():
    approved = ("**Run: pytest -q**\n\nRuns the unit tests in the workspace.",)
    assert terminal.is_approved("pytest -q", approved)
    assert terminal.is_approved("pytest   -q", approved)  # spacing does not matter
    assert not terminal.is_approved("python evil.py", approved)
    assert not terminal.is_approved("rm -rf .", approved)


def test_command_environment_holds_no_secrets(monkeypatch):
    monkeypatch.setenv("ANTHROPIC_API_KEY", "sk-test")
    monkeypatch.setenv("CUBICLE_WORKER_TOKEN", "t")
    env = terminal.minimal_env()
    assert "ANTHROPIC_API_KEY" not in env and "CUBICLE_WORKER_TOKEN" not in env and "PATH" in env


def _tool(tmp_path, approved=()):
    logs = []

    async def log(message):
        logs.append(message)

    return terminal.terminal_tool(tmp_path, log, approved), logs


def test_unapproved_command_is_refused_with_instructions(tmp_path):
    tool, logs = _tool(tmp_path)
    with pytest.raises(ToolError, match="request_approval"):
        run(tool.handler({"command": "python --version"}))
    assert logs == []


def test_approved_command_runs_in_the_workspace_and_is_logged(tmp_path):
    (tmp_path / "hello.py").write_text("print('hi from workspace')\n")
    tool, logs = _tool(tmp_path, approved=("Run: python hello.py",))
    out = run(tool.handler({"command": "python hello.py"}))
    assert out.startswith("exit code 0") and "hi from workspace" in out
    assert logs == ["Ran `python hello.py` (exit code 0)"]


def test_failures_and_timeouts_are_reported(tmp_path):
    (tmp_path / "boom.py").write_text("import sys; sys.exit(3)\n")
    (tmp_path / "slow.py").write_text("import time; time.sleep(30)\n")
    tool, _ = _tool(tmp_path, approved=("Run: python boom.py", "Run: python slow.py"))
    assert run(tool.handler({"command": "python boom.py"})).startswith("exit code 3")
    with pytest.raises(ToolError, match="longer than 1 s"):
        run(tool.handler({"command": "python slow.py", "timeout_seconds": 1}))


def test_git_status_needs_no_approval(tmp_path):
    tool, _ = _tool(tmp_path)
    # It ran: outside a repository git exits with a non-zero code, which is fine here.
    assert run(tool.handler({"command": "git status"})).startswith("exit code")


def test_a_python_interpreter_exists():
    assert sys.executable

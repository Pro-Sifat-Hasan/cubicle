"""A guarded terminal for agents that build and test things.

A command runs without a shell (so pipes, redirects and chaining do not exist), in the assignment's
workspace folder, with a minimal environment that holds none of Cubicle's secrets, a time limit and
a cap on its output. Programs come from an allowlist; shells, downloaders and deleters are not on it.

Running code is never free: apart from read-only git queries, every command needs the owner's
approval first. The agent asks with request_approval ("Run: pytest -q"), and the command runs only if
an approved request names exactly that command. Approving lets the command run with the owner's own
permissions, so the owner should look at what it will run.
"""

from __future__ import annotations

import asyncio
import os
import re
import shlex
import shutil
from collections.abc import Awaitable, Callable
from pathlib import Path
from typing import Any

from cubicle_agents.tools.base import Tool, ToolError, require_str

DEFAULT_TIMEOUT_S = 60
MAX_TIMEOUT_S = 300
MAX_OUTPUT_CHARS = 12_000
APPROVAL_PREFIX = "Run: "  # how an agent words the approval request for a command

# Programs an agent may start (after approval). Subcommands listed here are refused.
BLOCKED_SUBCOMMANDS: dict[str, set[str]] = {
    "git": {"push", "pull", "fetch", "clone", "remote", "config", "credential", "submodule", "daemon"},
    "npm": {"publish", "login", "adduser", "logout", "token", "owner", "access", "exec", "x"},
    "uv": {"publish", "tool", "self"},
    "cargo": {"publish", "login", "install", "owner"},
    "dotnet": {"nuget", "tool", "workload"},
}
ALLOWED_PROGRAMS = frozenset({
    "python", "python3", "py", "pytest", "ruff", "mypy", "black", "flake8", "uv",
    "node", "npm", "tsc", "eslint", "prettier",
    "go", "gofmt", "dotnet", "cargo", "rustc", "git", "make",
})
# git queries that only read: these run without approval.
READ_ONLY_GIT = frozenset({"status", "diff", "log", "show", "branch", "rev-parse", "ls-files", "blame", "describe"})
# The only environment variables a command inherits.
ENV_KEEP = ("PATH", "PATHEXT", "SYSTEMROOT", "TEMP", "TMP", "LANG", "LC_ALL", "COMSPEC", "WINDIR")
_SHELL_CHARS = re.compile(r"[|&;<>`\n\r]|\$\(")

Logger = Callable[[str], Awaitable[None]]


def parse_command(command: str) -> list[str]:
    """Split a command into program and arguments, refusing shell syntax."""
    command = command.strip()
    if not command:
        raise ToolError("'command' must not be empty.")
    if _SHELL_CHARS.search(command):
        raise ToolError("Run one plain command at a time: pipes, redirects, ';', '&&' and backticks are not supported. "
                        "Use find_files and grep_files to search, and run separate commands.")
    try:
        parts = shlex.split(command, posix=os.name != "nt")
    except ValueError as exc:
        raise ToolError(f"Could not read the command: {exc}.") from exc
    parts = [p[1:-1] if len(p) > 1 and p[0] == p[-1] and p[0] in "\"'" else p for p in parts]
    if not parts:
        raise ToolError("'command' must not be empty.")
    return parts


def program_name(program: str) -> str:
    base = re.split(r"[\\/]", program)[-1].lower()
    return re.sub(r"\.(exe|cmd|bat|com)$", "", base)


def check_allowed(parts: list[str]) -> str:
    """Return the program's name, or raise when it (or its subcommand) is not allowed."""
    name = program_name(parts[0])
    if name != parts[0].lower() and re.search(r"[\\/]", parts[0]):
        raise ToolError("Use the program's name, not a path.")
    if name not in ALLOWED_PROGRAMS:
        raise ToolError(f"'{name}' is not available. Allowed programs: {', '.join(sorted(ALLOWED_PROGRAMS))}.")
    sub = next((a.lower() for a in parts[1:] if not a.startswith("-")), "")
    if sub in BLOCKED_SUBCOMMANDS.get(name, set()):
        raise ToolError(f"'{name} {sub}' is not allowed: it reaches outside the workspace.")
    if name == "git" and any(a in ("-c", "--exec-path") or a.startswith("--upload-pack") for a in parts[1:]):
        raise ToolError("That git option is not allowed.")
    if name in ("python", "python3", "py") and "-m" in parts:
        module = parts[parts.index("-m") + 1].lower() if parts.index("-m") + 1 < len(parts) else ""
        if module in ("pip", "ensurepip", "venv", "http.server", "smtpd"):
            raise ToolError(f"'python -m {module}' is not allowed.")
    return name


def needs_approval(parts: list[str]) -> bool:
    """Everything but read-only git queries needs the owner's approval first."""
    if program_name(parts[0]) == "git":
        sub = next((a.lower() for a in parts[1:] if not a.startswith("-")), "")
        if sub in READ_ONLY_GIT and not (sub == "branch" and any(not a.startswith("-") for a in parts[2:])):
            return False
    return True


def approval_text(command: str) -> str:
    return APPROVAL_PREFIX + " ".join(command.split())


def is_approved(command: str, approved_actions: tuple[str, ...]) -> bool:
    wanted = approval_text(command).lower()
    return any(wanted in " ".join(a.replace("*", "").split()).lower() for a in approved_actions)


def minimal_env() -> dict[str, str]:
    env = {k: v for k in ENV_KEEP if (v := os.environ.get(k))}
    env.setdefault("PYTHONUTF8", "1")
    env["CI"] = "1"  # tools should not wait for a person
    return env


async def run(parts: list[str], cwd: Path, limit_s: int) -> tuple[int, str]:
    program = shutil.which(parts[0], path=os.environ.get("PATH"))
    if program is None:
        raise ToolError(f"'{parts[0]}' is not installed on this computer.")
    await asyncio.to_thread(cwd.mkdir, parents=True, exist_ok=True)
    proc = await asyncio.create_subprocess_exec(
        program, *parts[1:], cwd=cwd, env=minimal_env(), stdin=asyncio.subprocess.DEVNULL,
        stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.STDOUT,
    )
    try:
        async with asyncio.timeout(limit_s):
            out, _ = await proc.communicate()
    except TimeoutError:
        proc.kill()
        await proc.wait()
        raise ToolError(f"The command took longer than {limit_s} s and was stopped.") from None
    text = out.decode("utf-8", errors="replace")
    if len(text) > MAX_OUTPUT_CHARS:
        text = text[:MAX_OUTPUT_CHARS] + "\n[output truncated]"
    return proc.returncode or 0, text


def terminal_tool(workspace: Path, log: Logger, approved_actions: tuple[str, ...]) -> Tool:
    async def run_command(args: dict[str, Any]) -> str:
        command = require_str(args, "command", max_len=1000)
        timeout = args.get("timeout_seconds")
        timeout = min(max(int(timeout), 1), MAX_TIMEOUT_S) if isinstance(timeout, int) else DEFAULT_TIMEOUT_S
        parts = parse_command(command)
        check_allowed(parts)
        if needs_approval(parts) and not is_approved(command, approved_actions):
            raise ToolError(
                f"This command needs the owner's approval first. Call request_approval with action "
                f"'{approval_text(command)}' and, in details, what it does and which files it touches. "
                "Once they approve, call run_command again with exactly the same command.")
        code, output = await run(parts, workspace, timeout)
        await log(f"Ran `{' '.join(command.split())[:200]}` (exit code {code})")
        return f"exit code {code}\n{output}".rstrip()

    return Tool(
        name="run_command",
        description="Run one command (tests, linters, builds, git) in this assignment's workspace folder. No shell: no "
        "pipes, redirects or chaining. Read-only git queries run at once; every other command needs the owner's "
        "approval first via request_approval, worded exactly 'Run: <command>'. Returns the exit code and output.",
        parameters={
            "type": "object",
            "properties": {
                "command": {"type": "string", "description": "For example: pytest -q  or  go test ./..."},
                "timeout_seconds": {"type": "integer", "description": "Optional, 1-300. Default 60."},
            },
            "required": ["command"],
            "additionalProperties": False,
        },
        handler=run_command,
    )

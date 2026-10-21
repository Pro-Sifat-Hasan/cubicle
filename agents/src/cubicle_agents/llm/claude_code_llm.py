"""Claude Code CLI provider: runs work on the owner's own Claude subscription.

Cubicle invokes the `claude` command already installed and signed in on this PC, in headless print
mode, inside the assignment's workspace folder. Cubicle never sees or stores the owner's Claude
credentials.

* Claude Code's own Read/Write/Edit/Glob/Grep (and web) tools do the hands-on work, limited to what the
  agent may do: an agent without file tools gets none, and a main agent that has a team gets read-only
  access, so it hands building to a teammate. Shell access is never granted.
* Cubicle's own tools (ask a teammate, start an assignment, remember, load a skill, ask the owner,
  request approval, run a command under Cubicle's approval rules) arrive over a local MCP bridge for
  the length of the run.
* The agent's MCP servers are passed with --mcp-config; secrets appear there only as ${NAME}
  references that Claude Code expands from its environment, so they are never written to disk.
* The CLI is isolated: none of the owner's own Claude Code settings, skills, MCP servers or CLAUDE.md
  files are loaded, which also keeps every request small.
* The run streams: every thought, remark and tool call (with its input and output) is reported as it
  happens, and the answer arrives as it is written.
"""

from __future__ import annotations

import asyncio
import json
import logging
import os
import shutil
import tempfile
import time
from pathlib import Path
from typing import Any

from cubicle_agents.llm.base import AgentResult, EventCallback, LLMConfigError, StepCallback, Turn
from cubicle_agents.llm.claude_stream import ClaudeStreamParser
from cubicle_agents.staff.roster import McpServer
from cubicle_agents.tools.base import Toolbox
from cubicle_agents.tools.bridge import ToolBridge
from cubicle_agents.tools.mcp import tool_prefix

log = logging.getLogger(__name__)

TIMEOUT_S = 20 * 60
MODEL_ALIASES = ["opus", "sonnet", "haiku"]
READ_TOOLS = ["Read", "Glob", "Grep"]
WRITE_TOOLS = ["Write", "Edit"]
WEB_TOOLS = ["WebSearch", "WebFetch"]
PUSH_INTERVAL_S = 0.35
STREAM_LIMIT = 32 * 1024 * 1024  # one line can hold a whole file's content
BRIDGE_NAME = "cubicle"
_EFFORTS = ("low", "medium", "high", "xhigh", "max")
# Make sure the CLI uses the owner's subscription login, not an API key that
# may be present in the worker's environment for another provider.
_STRIP_ENV = ("ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN")


def find_cli() -> str:
    exe = shutil.which("claude")
    if not exe:
        raise LLMConfigError("Claude Code is not installed or not on PATH. Install it, run `claude` once to sign in, "
                             "then try again.")
    return exe


def file_tools(toolbox: Toolbox | None) -> list[str]:
    """Claude Code's file tools for what the agent's own toolbox allows: reading, and writing."""
    if toolbox is None:
        return READ_TOOLS + WRITE_TOOLS  # a plain run (no toolbox) keeps the classic behaviour
    names = {t.name for t in toolbox.tools}
    return (READ_TOOLS if "read_file" in names else []) + (WRITE_TOOLS if "write_file" in names else [])


def build_command(exe: str, *, model: str, system_file: Path, tools: list[str], mcp_file: Path | None = None,
                  mcp_names: list[str] | None = None, effort: str = "") -> list[str]:
    allowed = tools + [f"mcp__{n}" for n in mcp_names or []]
    cmd = [exe, "-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
           "--append-system-prompt-file", str(system_file), "--tools", *(tools or [""])]
    if allowed:
        cmd += ["--allowedTools", *allowed]
    cmd += ["--permission-mode", "dontAsk", "--disable-slash-commands", "--setting-sources", "",
            "--no-session-persistence", "--exclude-dynamic-system-prompt-sections", "--strict-mcp-config"]
    if mcp_file is not None:
        cmd += ["--mcp-config", str(mcp_file)]
    if model:
        cmd += ["--model", model]
    if effort in _EFFORTS:
        cmd += ["--effort", effort]
    return cmd


def mcp_config(servers: tuple[McpServer, ...]) -> dict[str, Any]:
    """Claude Code's MCP config for the agent's servers, with secrets as ${NAME} references."""
    out: dict[str, Any] = {}
    for s in servers:
        name = tool_prefix(s)
        if s.transport == "http":
            headers = dict(s.headers)
            if s.auth_secret:
                headers["Authorization"] = "Bearer ${" + s.auth_secret + "}"
            out[name] = {"type": "http", "url": s.url, "headers": headers}
        else:
            env = dict(s.env) | {k: "${" + k + "}" for k in s.secret_env}
            out[name] = {"type": "stdio", "command": s.command, "args": list(s.args), "env": env}
    return {"mcpServers": out}


def with_history(prompt: str, history: list[Turn] | None) -> str:
    """Claude Code gets the conversation as part of the prompt."""
    if not history:
        return prompt
    lines = [f"{'OWNER' if t.role == 'owner' else 'YOU'}: {t.content}" for t in history if t.content.strip()]
    return "CONVERSATION SO FAR:\n" + "\n\n".join(lines) + "\n\n---\n\nTHE OWNER'S NEW MESSAGE:\n" + prompt


class ClaudeCodeLLM:
    name = "claude_code"
    supports_tools = True  # Cubicle's tools reach Claude Code over a local MCP bridge
    native_file_tools = True  # the prompt names Claude Code's own Read/Write/Edit/Glob/Grep

    def __init__(self, *, web_access: bool = True) -> None:
        self._web = web_access

    async def run(self, *, system: str, prompt: str, model: str, toolbox: Toolbox | None = None,
                  json_schema: dict[str, Any] | None = None, workdir: Path | None = None,
                  history: list[Turn] | None = None, mcp: tuple[McpServer, ...] = (), effort: str = "",
                  on_event: EventCallback | None = None, web: bool = True,
                  on_step: StepCallback | None = None) -> AgentResult:
        exe = find_cli()
        prompt = with_history(prompt, history)
        if json_schema is not None:
            prompt += ("\n\nRespond with only a JSON object (no prose, no code fences) matching this JSON Schema:\n"
                       + json.dumps(json_schema))
        structured = json_schema is not None
        cwd = workdir or Path(tempfile.gettempdir())
        cwd.mkdir(parents=True, exist_ok=True)
        cwd = cwd.resolve()  # Claude Code matches its folder permissions on the real path, not a short 8.3 name
        env = {k: v for k, v in os.environ.items() if k not in _STRIP_ENV}
        env["CLAUDE_CODE_DISABLE_CLAUDE_MDS"] = "1"  # instructions come from Cubicle, not from files in the workspace
        env["MCP_TOOL_TIMEOUT"] = str(TIMEOUT_S * 1000)  # a teammate consulted over the bridge may take a while
        tools = [] if structured else file_tools(toolbox)
        if self._web and web and not structured:
            tools = tools + WEB_TOOLS

        bridge = ToolBridge(toolbox) if toolbox is not None and not structured else None
        servers = {} if structured else mcp_config(mcp)["mcpServers"]
        parser = ClaudeStreamParser()
        if on_event is not None:
            await on_event("status", "Working in Claude Code…")

        try:
            if bridge is not None and bridge.has_tools:
                await bridge.start()
                servers = {BRIDGE_NAME: bridge.config(), **servers}
            with tempfile.TemporaryDirectory(prefix="cubicle-") as tmp:
                system_file = Path(tmp) / "system.md"
                system_file.write_text(system, encoding="utf-8")
                mcp_file = Path(tmp) / "mcp.json"
                mcp_file.write_text(json.dumps({"mcpServers": servers}), encoding="utf-8")
                cmd = build_command(exe, model=model, system_file=system_file, tools=tools, mcp_file=mcp_file,
                                    mcp_names=list(servers), effort=effort)
                stderr = await self._run_process(cmd, cwd, env, prompt, parser, on_event, on_step)
        finally:
            if bridge is not None:
                await bridge.stop()

        return self._result(parser, stderr, toolbox)

    async def _run_process(self, cmd: list[str], cwd: Path, env: dict[str, str], prompt: str,
                           parser: ClaudeStreamParser, on_event: EventCallback | None,
                           on_step: StepCallback | None) -> str:
        proc = await asyncio.create_subprocess_exec(
            *cmd, cwd=str(cwd), env=env, limit=STREAM_LIMIT,
            stdin=asyncio.subprocess.PIPE, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE,
        )
        assert proc.stdin and proc.stdout and proc.stderr
        stderr_task = asyncio.create_task(proc.stderr.read())
        proc.stdin.write(prompt.encode("utf-8"))
        await proc.stdin.drain()
        proc.stdin.close()
        last_text = 0.0
        pending_text = ""

        async def pump() -> None:
            nonlocal last_text, pending_text
            async for raw in proc.stdout:  # type: ignore[union-attr]
                for kind, value in parser.feed(raw.decode("utf-8", errors="replace")):
                    if kind == "step" and on_step is not None:
                        await _safe(on_step, value)
                    elif kind == "status" and on_event is not None:
                        await _safe(on_event, "status", value)
                    elif kind == "text" and on_event is not None:
                        now = time.monotonic()
                        if value == "" or now - last_text >= PUSH_INTERVAL_S:
                            last_text, pending_text = now, ""
                            await _safe(on_event, "text", value)
                        else:
                            pending_text = value
            if pending_text and on_event is not None:  # the tail of the answer the throttle was holding back
                await _safe(on_event, "text", pending_text)

        try:
            await asyncio.wait_for(pump(), TIMEOUT_S)
            await proc.wait()
        except TimeoutError:
            proc.kill()
            await proc.wait()
            raise RuntimeError("Claude Code did not finish within 20 minutes.") from None
        except asyncio.CancelledError:
            proc.kill()
            raise
        finally:
            if proc.returncode is None:
                proc.kill()
        for kind, value in parser.close():
            if kind == "step" and on_step is not None:
                await _safe(on_step, value)
        return (await stderr_task).decode("utf-8", errors="replace")

    @staticmethod
    def _result(parser: ClaudeStreamParser, stderr: str, toolbox: Toolbox | None) -> AgentResult:
        if not parser.finished:
            detail = stderr.strip()[-500:] or "no result was returned"
            raise RuntimeError(f"Claude Code failed: {detail}")
        if parser.error:
            raise RuntimeError(f"Claude Code reported an error: {parser.error[:500]}")
        usage = parser.usage
        input_tokens = sum(int(usage.get(k) or 0) for k in
                           ("input_tokens", "cache_creation_input_tokens", "cache_read_input_tokens"))
        result = AgentResult(text=parser.text, input_tokens=input_tokens, output_tokens=int(usage.get("output_tokens") or 0))
        if parser.denied:
            result.warnings = [f"Claude Code was not allowed to use {', '.join(parser.denied)} here."]
        if toolbox is not None and toolbox.question:  # the agent asked you something or wants approval
            result.question, result.question_kind = toolbox.question, toolbox.question_kind
        return result

    async def list_models(self) -> list[str]:
        exe = find_cli()
        proc = await asyncio.create_subprocess_exec(exe, "--version", stdout=asyncio.subprocess.PIPE,
                                                    stderr=asyncio.subprocess.PIPE)
        await asyncio.wait_for(proc.communicate(), 30)
        if proc.returncode != 0:
            raise LLMConfigError("Claude Code is installed but did not start. Run `claude` in a terminal to check.")
        return list(MODEL_ALIASES)


async def _safe(callback: Any, *args: Any) -> None:
    try:
        await callback(*args)
    except Exception:  # progress is cosmetic; never let it break the work
        log.debug("progress update failed", exc_info=True)


def parse_output(stdout: bytes, stderr: bytes, returncode: int | None) -> AgentResult:
    """Read a complete stream (every line) and return its result."""
    parser = ClaudeStreamParser()
    for line in stdout.decode("utf-8", errors="replace").splitlines():
        parser.feed(line)
    parser.close()
    if not parser.finished:
        detail = stderr.decode("utf-8", errors="replace").strip()[-500:] or f"exit code {returncode}"
        raise RuntimeError(f"Claude Code failed: {detail}")
    return ClaudeCodeLLM._result(parser, "", None)

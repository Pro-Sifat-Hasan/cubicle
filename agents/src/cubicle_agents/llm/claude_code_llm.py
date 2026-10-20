"""Claude Code CLI provider: runs work on the owner's own Claude subscription.

Cubicle invokes the `claude` command already installed and signed in on this
PC, in headless print mode, inside the assignment's workspace folder. Cubicle
never sees or stores the owner's Claude credentials. Claude Code brings its own
file and web tools; shell access is not granted.
"""

from __future__ import annotations

import asyncio
import json
import os
import shutil
import tempfile
from pathlib import Path
from typing import Any

from cubicle_agents.llm.base import AgentResult, LLMConfigError
from cubicle_agents.tools.base import Toolbox

TIMEOUT_S = 20 * 60
MODEL_ALIASES = ["opus", "sonnet", "haiku"]
_FILE_TOOLS = ["Read", "Write", "Edit", "Glob", "Grep"]
_WEB_TOOLS = ["WebSearch", "WebFetch"]
# Make sure the CLI uses the owner's subscription login, not an API key that
# may be present in the worker's environment for another provider.
_STRIP_ENV = ("ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN")


def find_cli() -> str:
    exe = shutil.which("claude")
    if not exe:
        raise LLMConfigError("Claude Code is not installed or not on PATH. Install it, run `claude` once to sign in, "
                             "then try again.")
    return exe


def build_command(exe: str, *, model: str, system_file: Path, web: bool) -> list[str]:
    tools = _FILE_TOOLS + (_WEB_TOOLS if web else [])
    cmd = [exe, "-p", "--output-format", "json", "--append-system-prompt-file", str(system_file),
           "--tools", *tools, "--allowedTools", *tools, "--permission-mode", "dontAsk"]
    if model:
        cmd += ["--model", model]
    return cmd


class ClaudeCodeLLM:
    name = "claude_code"
    supports_tools = False  # Claude Code uses its own tools; we pass directives as text

    def __init__(self, *, web_access: bool = True) -> None:
        self._web = web_access

    async def run(self, *, system: str, prompt: str, model: str, toolbox: Toolbox | None = None,
                  json_schema: dict[str, Any] | None = None, workdir: Path | None = None) -> AgentResult:
        exe = find_cli()
        if json_schema is not None:
            prompt += ("\n\nRespond with only a JSON object (no prose, no code fences) matching this JSON Schema:\n"
                       + json.dumps(json_schema))
        cwd = workdir or Path(tempfile.gettempdir())
        cwd.mkdir(parents=True, exist_ok=True)
        env = {k: v for k, v in os.environ.items() if k not in _STRIP_ENV}

        with tempfile.TemporaryDirectory(prefix="cubicle-") as tmp:
            system_file = Path(tmp) / "system.md"
            system_file.write_text(system, encoding="utf-8")
            proc = await asyncio.create_subprocess_exec(
                *build_command(exe, model=model, system_file=system_file, web=self._web),
                cwd=str(cwd), env=env,
                stdin=asyncio.subprocess.PIPE, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE,
            )
            try:
                stdout, stderr = await asyncio.wait_for(proc.communicate(prompt.encode("utf-8")), TIMEOUT_S)
            except TimeoutError:
                proc.kill()
                await proc.wait()
                raise RuntimeError("Claude Code did not finish within 20 minutes.") from None
        return parse_output(stdout, stderr, proc.returncode)

    async def list_models(self) -> list[str]:
        exe = find_cli()
        proc = await asyncio.create_subprocess_exec(exe, "--version", stdout=asyncio.subprocess.PIPE,
                                                    stderr=asyncio.subprocess.PIPE)
        await asyncio.wait_for(proc.communicate(), 30)
        if proc.returncode != 0:
            raise LLMConfigError("Claude Code is installed but did not start. Run `claude` in a terminal to check.")
        return list(MODEL_ALIASES)


def parse_output(stdout: bytes, stderr: bytes, returncode: int | None) -> AgentResult:
    try:
        data = json.loads(stdout.decode("utf-8", errors="replace").strip().splitlines()[-1])
    except (json.JSONDecodeError, IndexError):
        detail = stderr.decode("utf-8", errors="replace").strip()[-500:] or f"exit code {returncode}"
        raise RuntimeError(f"Claude Code failed: {detail}") from None
    if data.get("is_error"):
        raise RuntimeError(f"Claude Code reported an error: {str(data.get('result', ''))[:500]}")
    usage = data.get("usage") or {}
    input_tokens = sum(int(usage.get(k) or 0) for k in
                       ("input_tokens", "cache_creation_input_tokens", "cache_read_input_tokens"))
    return AgentResult(text=str(data.get("result", "")).strip(), input_tokens=input_tokens,
                       output_tokens=int(usage.get("output_tokens") or 0))

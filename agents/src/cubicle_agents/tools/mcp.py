"""MCP (Model Context Protocol) servers: connect agents to the owner's apps.

Each server runs (stdio) or is reached (http) only for the run that needs it.
A server that fails to start is skipped with a warning instead of failing the
work. A stdio server gets a minimal environment plus the variables and secrets
configured for it, never every secret the worker holds.
"""

from __future__ import annotations

import logging
import os
import re
import shutil
import sys
from contextlib import AsyncExitStack
from pathlib import Path
from typing import Any

from cubicle_agents.core.settings import default_home
from cubicle_agents.staff.roster import McpServer

log = logging.getLogger(__name__)

CONNECT_TIMEOUT_S = 45.0
CALL_TIMEOUT_S = 120.0

# Variables a stdio server may need to start (finding node/python, temp and profile folders).
_PASSTHROUGH = (
    "PATH", "PATHEXT", "SYSTEMROOT", "SYSTEMDRIVE", "WINDIR", "COMSPEC", "TEMP", "TMP", "TMPDIR", "HOME",
    "USERPROFILE", "HOMEDRIVE", "HOMEPATH", "USERNAME", "APPDATA", "LOCALAPPDATA", "PROGRAMDATA", "PROGRAMFILES",
    "PROGRAMFILES(X86)", "COMMONPROGRAMFILES", "NUMBER_OF_PROCESSORS", "PROCESSOR_ARCHITECTURE", "OS", "LANG",
    "LC_ALL", "SSL_CERT_FILE", "NODE_EXTRA_CA_CERTS", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
)
_PREFIX_SAFE = re.compile(r"[^a-zA-Z0-9_]")


class MCPUnavailableError(Exception):
    """The server could not be started or reached."""


def tool_prefix(server: McpServer) -> str:
    """Prefix for the server's tool names, keeping names unique across servers."""
    return (_PREFIX_SAFE.sub("_", server.id) or "mcp")[:24]


def server_env(server: McpServer, environ: dict[str, str] | None = None) -> dict[str, str]:
    environ = dict(os.environ if environ is None else environ)
    upper = {k.upper(): v for k, v in environ.items()}
    env = {k: upper[k] for k in _PASSTHROUGH if k in upper}
    env.update(dict(server.env))
    missing = [name for name in server.secret_env if not environ.get(name)]
    if missing:
        raise MCPUnavailableError(f"{server.name}: add the secret {', '.join(missing)} in Settings first.")
    env.update({name: environ[name] for name in server.secret_env})
    return env


def resolve_command(command: str) -> str:
    """Find the executable (npx is npx.cmd on Windows) so it starts without a shell."""
    found = shutil.which(command)
    if found:
        return found
    if command in ("python", "python3"):
        return sys.executable
    raise MCPUnavailableError(f"'{command}' was not found on this PC. Install it (or fix the command) and try again.")


def http_headers(server: McpServer, environ: dict[str, str] | None = None) -> dict[str, str]:
    environ = os.environ if environ is None else environ
    headers = dict(server.headers)
    if server.auth_secret:
        token = environ.get(server.auth_secret, "")
        if not token:
            raise MCPUnavailableError(f"{server.name}: add the secret {server.auth_secret} in Settings first.")
        headers["Authorization"] = f"Bearer {token}"
    return headers


def log_path(server: McpServer) -> Path:
    """Where a stdio server's own messages (stderr) go: <home>/logs/mcp-<id>.log."""
    folder = default_home() / "logs"
    folder.mkdir(parents=True, exist_ok=True)
    return folder / f"mcp-{tool_prefix(server)}.log"


def build_toolset(server: McpServer, workdir: Path | None = None) -> Any:
    """A Pydantic AI toolset for the server; its tools are prefixed with the server id."""
    from pydantic_ai.mcp import MCPToolset, StdioTransport, StreamableHttpTransport

    if server.transport == "stdio":
        if workdir is not None:
            workdir.mkdir(parents=True, exist_ok=True)
        transport = StdioTransport(resolve_command(server.command), list(server.args), env=server_env(server),
                                   cwd=str(workdir) if workdir else None, log_file=log_path(server))
    elif server.transport == "http":
        transport = StreamableHttpTransport(server.url, headers=http_headers(server))
    else:
        raise MCPUnavailableError(f"{server.name}: unknown transport {server.transport!r}.")
    # 'failed' shows tool errors to the model instead of ending the run.
    toolset = MCPToolset(transport, id=server.id, tool_error_behavior="failed",
                         init_timeout=CONNECT_TIMEOUT_S, read_timeout=CALL_TIMEOUT_S)
    return toolset.prefixed(tool_prefix(server))


async def open_toolsets(servers: tuple[McpServer, ...], stack: AsyncExitStack,
                        workdir: Path | None = None) -> tuple[list[Any], list[str]]:
    """Start or connect every server; returns the working toolsets and a warning per failure."""
    toolsets: list[Any] = []
    warnings: list[str] = []
    for server in servers:
        try:
            toolset = build_toolset(server, workdir)
            await stack.enter_async_context(toolset)
            toolsets.append(toolset)
        except MCPUnavailableError as exc:
            warnings.append(str(exc))
        except Exception as exc:  # a broken server must never break the work
            log.warning("MCP server %s failed to start", server.id, exc_info=True)
            warnings.append(f"{server.name} could not be reached ({_describe(exc)}).")
    return toolsets, warnings


async def list_tools(server: McpServer) -> list[dict[str, str]]:
    """Connect to one server and list its tools (the app's "Test" button)."""
    async with AsyncExitStack() as stack:
        toolsets, warnings = await open_toolsets((server,), stack)
        if warnings:
            raise MCPUnavailableError(warnings[0])
        tools = await toolsets[0].wrapped.list_tools()
        prefix = tool_prefix(server)
        return [{"name": f"{prefix}_{t.name}", "description": (t.description or "").strip()[:500]} for t in tools]


def _describe(exc: BaseException) -> str:
    # Connection errors often arrive wrapped in exception groups; show the innermost cause.
    while isinstance(exc, BaseExceptionGroup) and exc.exceptions:
        exc = exc.exceptions[0]
    text = str(exc).strip() or type(exc).__name__
    return text[:300]

"""A local MCP server that hands Cubicle's own tools to Claude Code.

Claude Code brings its own file and web tools, but not Cubicle's: asking a teammate, starting an
assignment, remembering a fact, loading a skill, asking the owner a question or for approval, and
running a command under Cubicle's approval rules. For the length of one run the worker serves them
over MCP (streamable HTTP, JSON replies) on a loopback port, guarded by a random bearer token that
exists only for that run.

Only the methods Claude Code uses are implemented: initialize, ping, tools/list and tools/call.
"""

from __future__ import annotations

import asyncio
import logging
import secrets
import socket
from typing import Any

from cubicle_agents.tools.base import Toolbox

log = logging.getLogger(__name__)

SERVER_NAME = "cubicle"
SUPPORTED_VERSIONS = ("2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05")
START_TIMEOUT_S = 10.0

# Tools Claude Code already has natively (it must use those), so they are not offered over the bridge.
NATIVE_TOOLS = frozenset({"read_file", "write_file", "edit_file", "insert_lines", "list_files", "find_files", "grep_files",
                          "web_fetch"})


class ToolBridge:
    """Serves the tools of a Toolbox (minus the ones Claude Code has natively) until stopped."""

    def __init__(self, toolbox: Toolbox) -> None:
        self._toolbox = toolbox
        self._tools = [t for t in toolbox.tools if t.name not in NATIVE_TOOLS]
        self.token = secrets.token_urlsafe(32)
        self.port = 0
        self._server: Any = None
        self._task: asyncio.Task[None] | None = None

    @property
    def has_tools(self) -> bool:
        return bool(self._tools)

    @property
    def url(self) -> str:
        return f"http://127.0.0.1:{self.port}/mcp"

    def config(self) -> dict[str, Any]:
        """The entry for Claude Code's --mcp-config."""
        return {"type": "http", "url": self.url, "headers": {"Authorization": f"Bearer {self.token}"}}

    async def start(self) -> None:
        import uvicorn

        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        sock.bind(("127.0.0.1", 0))
        self.port = sock.getsockname()[1]
        config = uvicorn.Config(self._app(), log_level="warning", lifespan="off", access_log=False)
        self._server = uvicorn.Server(config)
        self._server.install_signal_handlers = lambda: None  # the worker owns signals
        self._task = asyncio.create_task(self._server.serve(sockets=[sock]))
        loop = asyncio.get_running_loop()
        deadline = loop.time() + START_TIMEOUT_S
        while not self._server.started:
            if self._task.done() or loop.time() > deadline:
                await self.stop()
                raise RuntimeError("could not start the local tool server")
            await asyncio.sleep(0.01)

    async def stop(self) -> None:
        if self._server is not None:
            self._server.should_exit = True
        if self._task is not None:
            try:
                await asyncio.wait_for(self._task, 5)
            except (TimeoutError, asyncio.CancelledError):
                self._task.cancel()
            except Exception:
                log.debug("tool server stopped with an error", exc_info=True)
            self._task = None

    # ---- the HTTP app ----

    def _app(self) -> Any:
        from starlette.applications import Starlette
        from starlette.requests import Request
        from starlette.responses import JSONResponse, Response
        from starlette.routing import Route

        async def endpoint(request: Request) -> Response:
            if not secrets.compare_digest(request.headers.get("authorization", ""), f"Bearer {self.token}"):
                return Response(status_code=401)
            if request.method == "DELETE":
                return Response(status_code=200)
            if request.method != "POST":
                return Response(status_code=405, headers={"Allow": "POST, DELETE"})
            try:
                body = await request.json()
            except ValueError:
                return JSONResponse(_error(None, -32700, "Parse error"), status_code=400)
            if isinstance(body, list):
                replies = [r for r in [await self._handle(m) for m in body] if r is not None]
                return JSONResponse(replies) if replies else Response(status_code=202)
            reply = await self._handle(body)
            return JSONResponse(reply) if reply is not None else Response(status_code=202)

        return Starlette(routes=[Route("/mcp", endpoint, methods=["POST", "GET", "DELETE"])])

    async def _handle(self, message: Any) -> dict[str, Any] | None:
        if not isinstance(message, dict):
            return _error(None, -32600, "Invalid request")
        method, ident = message.get("method"), message.get("id")
        params = message.get("params") if isinstance(message.get("params"), dict) else {}
        if ident is None:  # a notification (e.g. notifications/initialized): nothing to answer
            return None
        if method == "initialize":
            wanted = params.get("protocolVersion")
            return _result(ident, {
                "protocolVersion": wanted if wanted in SUPPORTED_VERSIONS else SUPPORTED_VERSIONS[0],
                "capabilities": {"tools": {"listChanged": False}},
                "serverInfo": {"name": SERVER_NAME, "version": "1"},
            })
        if method == "ping":
            return _result(ident, {})
        if method == "tools/list":
            return _result(ident, {"tools": [
                {"name": t.name, "description": t.description, "inputSchema": t.parameters} for t in self._tools]})
        if method == "tools/call":
            name = params.get("name")
            if not any(t.name == name for t in self._tools):
                return _error(ident, -32602, f"Unknown tool {name!r}")
            outcome = await self._toolbox.call(str(name), params.get("arguments") or {})
            return _result(ident, {"content": [{"type": "text", "text": outcome.content}], "isError": outcome.is_error})
        return _error(ident, -32601, f"Method not found: {method}")


def _result(ident: Any, result: dict[str, Any]) -> dict[str, Any]:
    return {"jsonrpc": "2.0", "id": ident, "result": result}


def _error(ident: Any, code: int, message: str) -> dict[str, Any]:
    return {"jsonrpc": "2.0", "id": ident, "error": {"code": code, "message": message}}

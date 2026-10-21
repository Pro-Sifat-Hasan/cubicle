"""The local MCP bridge that gives Claude Code Cubicle's tools."""

from __future__ import annotations

import asyncio
from collections.abc import Awaitable, Callable

import httpx
import pytest

from cubicle_agents.tools.base import Tool, Toolbox
from cubicle_agents.tools.bridge import NATIVE_TOOLS, ToolBridge


def _toolbox() -> Toolbox:
    async def echo(args):
        return f"echo: {args['text']}"

    async def boom(_args):
        raise RuntimeError("bug")

    schema = {"type": "object", "properties": {"text": {"type": "string"}}, "required": ["text"]}
    return Toolbox([
        Tool("echo", "Echo the text back.", schema, echo),
        Tool("boom", "Always fails.", {"type": "object", "properties": {}}, boom),
        Tool("write_file", "Claude Code has its own.", schema, echo),
    ])


def with_bridge(scenario: Callable[[ToolBridge], Awaitable[None]]) -> None:
    async def main() -> None:
        bridge = ToolBridge(_toolbox())
        await bridge.start()
        try:
            await scenario(bridge)
        finally:
            await bridge.stop()

    asyncio.run(main())


def _rpc(method: str, params: dict | None = None, ident: int | None = 1) -> dict:
    msg: dict = {"jsonrpc": "2.0", "method": method}
    if ident is not None:
        msg["id"] = ident
    if params is not None:
        msg["params"] = params
    return msg


async def _post(bridge: ToolBridge, body, token: str | None = None) -> httpx.Response:
    headers = {"Authorization": f"Bearer {token or bridge.token}", "Accept": "application/json, text/event-stream"}
    async with httpx.AsyncClient() as client:
        return await client.post(bridge.url, json=body, headers=headers)


def test_bridge_offers_only_the_tools_claude_code_lacks():
    async def scenario(bridge):
        reply = (await _post(bridge, _rpc("tools/list"))).json()
        names = [t["name"] for t in reply["result"]["tools"]]
        assert names == ["echo", "boom"]
        assert "write_file" in NATIVE_TOOLS
        assert reply["result"]["tools"][0]["inputSchema"]["required"] == ["text"]

    with_bridge(scenario)


def test_bridge_runs_tools_and_reports_failures_as_results():
    async def scenario(bridge):
        ok = (await _post(bridge, _rpc("tools/call", {"name": "echo", "arguments": {"text": "hi"}}))).json()
        assert ok["result"] == {"content": [{"type": "text", "text": "echo: hi"}], "isError": False}
        bad = (await _post(bridge, _rpc("tools/call", {"name": "boom", "arguments": {}}))).json()
        assert bad["result"]["isError"] is True and "boom failed" in bad["result"]["content"][0]["text"]
        unknown = (await _post(bridge, _rpc("tools/call", {"name": "write_file", "arguments": {"text": "x"}}))).json()
        assert unknown["error"]["code"] == -32602

    with_bridge(scenario)


def test_bridge_handshake_and_notifications():
    async def scenario(bridge):
        init = (await _post(bridge, _rpc("initialize", {"protocolVersion": "2025-06-18"}))).json()
        assert init["result"]["protocolVersion"] == "2025-06-18"
        assert init["result"]["serverInfo"]["name"] == "cubicle"
        assert (await _post(bridge, _rpc("notifications/initialized", ident=None))).status_code == 202
        assert (await _post(bridge, _rpc("ping"))).json()["result"] == {}
        assert (await _post(bridge, _rpc("nope"))).json()["error"]["code"] == -32601

    with_bridge(scenario)


def test_bridge_refuses_callers_without_the_token():
    async def scenario(bridge):
        assert (await _post(bridge, _rpc("tools/list"), token="wrong")).status_code == 401
        async with httpx.AsyncClient() as client:
            assert (await client.post(bridge.url, json=_rpc("tools/list"))).status_code == 401
            assert (await client.get(bridge.url, headers={"Authorization": f"Bearer {bridge.token}"})).status_code == 405

    with_bridge(scenario)


def test_a_real_mcp_client_can_use_the_bridge():
    from fastmcp import Client
    from fastmcp.client.transports import StreamableHttpTransport

    async def scenario(bridge):
        async with Client(StreamableHttpTransport(bridge.url, headers=bridge.config()["headers"])) as client:
            tools = await client.list_tools()
            assert [t.name for t in tools] == ["echo", "boom"]
            result = await client.call_tool("echo", {"text": "from a client"})
            assert "echo: from a client" in str(result)

    with_bridge(scenario)


def test_bridge_stops_cleanly():
    async def main():
        bridge = ToolBridge(_toolbox())
        await bridge.start()
        url = bridge.url
        await bridge.stop()
        with pytest.raises(httpx.HTTPError):
            async with httpx.AsyncClient() as client:
                await client.post(url, json=_rpc("ping"), timeout=2)

    asyncio.run(main())

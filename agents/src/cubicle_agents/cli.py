"""Command-line entry point.

    cubicle-agents [run]             clock in the staff and start working (default)
    cubicle-agents models --json     list the configured provider's models (used by the gateway)
    cubicle-agents mcp-tools --json  connect to the MCP server in CUBICLE_MCP_SERVER and list its tools
"""

from __future__ import annotations

import os

# Pydantic AI prints a banner to stdout on first use; the gateway reads our stdout as JSON.
os.environ.setdefault("PYDANTIC_AI_NO_BANNER", "1")

import argparse  # noqa: E402
import asyncio  # noqa: E402
import contextlib  # noqa: E402
import json  # noqa: E402
import logging  # noqa: E402
import signal  # noqa: E402
import sys  # noqa: E402

from cubicle_agents import __version__  # noqa: E402
from cubicle_agents.core.gateway import GatewayClient  # noqa: E402
from cubicle_agents.core.settings import KIND_ANTHROPIC, KIND_OPENAI_COMPATIBLE, LLMSettings, Settings  # noqa: E402
from cubicle_agents.llm import LLMConfigError, make_llm  # noqa: E402
from cubicle_agents.office.handlers import OfficeContext  # noqa: E402
from cubicle_agents.office.office import Office  # noqa: E402
from cubicle_agents.staff.roster import McpServer, Roster  # noqa: E402


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="cubicle-agents", description="Cubicle AI staff worker")
    parser.add_argument("--version", action="version", version=f"cubicle-agents {__version__}")
    parser.add_argument("-v", "--verbose", action="store_true", help="debug logging")
    sub = parser.add_subparsers(dest="command")
    sub.add_parser("run", help="clock in the staff and start working (default)")
    models = sub.add_parser("models", help="list models available from the configured provider")
    models.add_argument("--json", action="store_true", help="print a single JSON object")
    mcp = sub.add_parser("mcp-tools", help="list the tools of the MCP server described in CUBICLE_MCP_SERVER")
    mcp.add_argument("--json", action="store_true", help="print a single JSON object")
    args = parser.parse_args(argv)

    one_off = args.command in ("models", "mcp-tools")
    logging.basicConfig(
        level=logging.DEBUG if args.verbose else logging.INFO,
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
        stream=sys.stderr if one_off else sys.stdout,
    )
    # Never log request bodies/headers from HTTP libraries (they can carry tokens).
    for noisy in ("httpx", "httpcore", "anthropic", "openai", "mcp", "fastmcp", "pydantic_ai"):
        logging.getLogger(noisy).setLevel(logging.WARNING)

    if args.command == "models":
        return asyncio.run(_models(as_json=args.json))
    if args.command == "mcp-tools":
        return asyncio.run(_mcp_tools(as_json=args.json))
    return asyncio.run(_run())


async def _models(*, as_json: bool) -> int:
    models: list[str] = []
    error = ""
    try:
        models = await make_llm(LLMSettings.from_env()).list_models()
    except LLMConfigError as exc:
        error = str(exc)
    except Exception as exc:  # report, don't crash: the gateway shows this to the owner
        error = f"{type(exc).__name__}: {exc}"[:500]
    if as_json:
        print(json.dumps({"error": error} if error else {"models": models}))
    else:
        print(error or "\n".join(models))
    return 1 if error else 0


async def _mcp_tools(*, as_json: bool) -> int:
    from cubicle_agents.tools.mcp import MCPUnavailableError, list_tools

    tools: list[dict[str, str]] = []
    error = ""
    try:
        server = McpServer.from_json(json.loads(os.environ.get("CUBICLE_MCP_SERVER") or "{}"))
        tools = await asyncio.wait_for(list_tools(server), timeout=80)
    except (KeyError, json.JSONDecodeError):
        error = "No MCP server was given."
    except MCPUnavailableError as exc:
        error = str(exc)
    except TimeoutError:
        error = "The server did not answer in time. The first start can be slow while packages download; try again."
    except Exception as exc:
        error = f"{type(exc).__name__}: {exc}"[:500]
    if as_json:
        print(json.dumps({"tools": tools, "error": error} if error else {"tools": tools}))
    else:
        print(error or "\n".join(f"{t['name']}: {t['description']}" for t in tools))
    return 1 if error else 0


async def _run() -> int:
    settings = Settings.from_env()
    gateway = GatewayClient(settings.gateway_url, settings.worker_token)
    llm = settings.llm
    ctx = OfficeContext(
        gateway=gateway,
        llm=make_llm(llm),
        roster=Roster(),
        default_model=llm.model,
        owner=settings.owner_name,
        workspace_root=settings.workspace,
        web_access=llm.web_access,
        # Claude has its own fetch tool; every other API provider gets Cubicle's guarded one.
        client_web_fetch=llm.web_access and llm.kind == KIND_OPENAI_COMPATIBLE,
    )
    logging.getLogger("cubicle").info(
        "cubicle-agents %s | provider=%s model=%s web=%s engine=%s",
        __version__, llm.provider, llm.model or "(default)", ctx.web_access,
        "pydantic-ai" if llm.kind in (KIND_ANTHROPIC, KIND_OPENAI_COMPATIBLE) else llm.kind,
    )

    stop = asyncio.Event()
    loop = asyncio.get_running_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        with contextlib.suppress(NotImplementedError, RuntimeError):  # not supported on Windows
            loop.add_signal_handler(sig, stop.set)
    try:
        await Office(ctx).run(stop)
    finally:
        await gateway.aclose()
    return 0

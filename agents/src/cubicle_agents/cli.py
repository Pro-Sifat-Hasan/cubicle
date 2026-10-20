"""Command-line entry point.

    cubicle-agents [run]          clock in the staff and start working (default)
    cubicle-agents models --json  list the configured provider's models (used by the gateway)
    cubicle-agents roster         print the staff roster
    cubicle-agents init-staff     write the default roster to <home>/staff.json for editing
"""

from __future__ import annotations

import argparse
import asyncio
import contextlib
import json
import logging
import signal
import sys

from cubicle_agents import __version__
from cubicle_agents.core.gateway import GatewayClient
from cubicle_agents.core.settings import KIND_OPENAI_COMPATIBLE, LLMSettings, Settings, default_home
from cubicle_agents.llm import LLMConfigError, make_llm
from cubicle_agents.office.handlers import OfficeContext
from cubicle_agents.office.office import Office
from cubicle_agents.staff.roster import load_roster, write_default_roster


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="cubicle-agents", description="Cubicle AI staff worker")
    parser.add_argument("--version", action="version", version=f"cubicle-agents {__version__}")
    parser.add_argument("-v", "--verbose", action="store_true", help="debug logging")
    sub = parser.add_subparsers(dest="command")
    sub.add_parser("run", help="clock in the staff and start working (default)")
    models = sub.add_parser("models", help="list models available from the configured provider")
    models.add_argument("--json", action="store_true", help="print a single JSON object")
    sub.add_parser("roster", help="print the current staff roster")
    sub.add_parser("init-staff", help="write the default roster to <home>/staff.json for editing")
    args = parser.parse_args(argv)

    logging.basicConfig(
        level=logging.DEBUG if args.verbose else logging.INFO,
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
        stream=sys.stderr if args.command == "models" else sys.stdout,
    )
    # Never log request bodies/headers from HTTP libraries (they can carry tokens).
    for noisy in ("httpx", "httpcore", "anthropic", "openai"):
        logging.getLogger(noisy).setLevel(logging.WARNING)

    if args.command == "models":
        return asyncio.run(_models(as_json=args.json))
    if args.command == "roster":
        for e in load_roster(default_home()):
            print(f"{e.avatar} {e.name:<8} {e.title:<20} [{e.id}] {e.skills}")
        return 0
    if args.command == "init-staff":
        path = default_home() / "staff.json"
        if path.exists():
            print(f"{path} already exists; edit it directly.")
            return 1
        path.parent.mkdir(parents=True, exist_ok=True)
        write_default_roster(path)
        print(f"Wrote {path}. Edit it, then restart Cubicle.")
        return 0
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


async def _run() -> int:
    settings = Settings.from_env()
    gateway = GatewayClient(settings.gateway_url, settings.worker_token)
    ctx = OfficeContext(
        gateway=gateway,
        llm=make_llm(settings.llm),
        roster=load_roster(settings.home),
        default_model=settings.llm.model,
        owner=settings.owner_name,
        workspace_root=settings.workspace,
        web_access=settings.llm.web_access,
        client_web_fetch=settings.llm.web_access and settings.llm.kind == KIND_OPENAI_COMPATIBLE,
    )
    logging.getLogger("cubicle").info(
        "cubicle-agents %s | provider=%s model=%s staff=%d web=%s",
        __version__, settings.llm.provider, settings.llm.model or "(default)", len(ctx.roster), ctx.web_access,
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

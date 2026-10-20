"""Command-line entry point: `cubicle-agents [run | roster | init-staff]`."""

from __future__ import annotations

import argparse
import asyncio
import contextlib
import logging
import signal
import sys

from cubicle_agents import __version__
from cubicle_agents.core.gateway import GatewayClient
from cubicle_agents.core.settings import Settings, default_home
from cubicle_agents.llm import make_llm
from cubicle_agents.office.office import Office
from cubicle_agents.staff.roster import load_roster, write_default_roster


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="cubicle-agents", description="Cubicle AI staff worker")
    parser.add_argument("--version", action="version", version=f"cubicle-agents {__version__}")
    parser.add_argument("-v", "--verbose", action="store_true", help="debug logging")
    sub = parser.add_subparsers(dest="command")
    sub.add_parser("run", help="clock in the staff and start working (default)")
    sub.add_parser("roster", help="print the current staff roster")
    sub.add_parser("init-staff", help="write the default roster to <home>/staff.json for editing")
    args = parser.parse_args(argv)

    logging.basicConfig(
        level=logging.DEBUG if args.verbose else logging.INFO,
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
        stream=sys.stdout,
    )
    # Never log request bodies/headers from HTTP libraries (they can carry tokens).
    for noisy in ("httpx", "httpcore", "anthropic"):
        logging.getLogger(noisy).setLevel(logging.WARNING)

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


async def _run() -> int:
    settings = Settings.from_env()
    roster = load_roster(settings.home)
    llm = make_llm(settings)
    logging.getLogger("cubicle").info(
        "cubicle-agents %s | provider=%s model=%s staff=%d gateway=%s",
        __version__, llm.name, settings.model, len(roster), settings.gateway_url,
    )

    stop = asyncio.Event()
    loop = asyncio.get_running_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        with contextlib.suppress(NotImplementedError, RuntimeError):  # not supported on Windows
            loop.add_signal_handler(sig, stop.set)

    gateway = GatewayClient(settings.gateway_url, settings.worker_token)
    try:
        office = Office(gateway=gateway, llm=llm, roster=roster,
                        default_model=settings.model, owner=settings.owner_name)
        await office.run(stop)
    finally:
        await gateway.aclose()
    return 0

"""A tiny MCP server used by the tests (run as a subprocess over stdio)."""

import os

from fastmcp import FastMCP

server = FastMCP("echo")


@server.tool
def shout(text: str) -> str:
    """Return the text in capitals."""
    return text.upper()


@server.tool
def secret_hint() -> str:
    """Show which configured variables the server received."""
    def state(name: str) -> str:
        return "set" if os.environ.get(name) else "missing"

    return f"token={state('ECHO_TOKEN')} other={state('OTHER_SECRET')}"


if __name__ == "__main__":
    server.run(show_banner=False)

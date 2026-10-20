"""A guarded web_fetch tool for providers without a built-in web tool.

Only public http(s) addresses are allowed: every hostname (including redirect
targets) is resolved and rejected if it points at loopback, private, link-local
or other non-public ranges. This keeps agents away from the gateway on
127.0.0.1 and from the owner's local network.
"""

from __future__ import annotations

import asyncio
import ipaddress
import socket
from html.parser import HTMLParser
from typing import Any
from urllib.parse import urljoin, urlsplit

import httpx

from cubicle_agents.tools.base import Tool, ToolError, require_str

MAX_BYTES = 2_000_000
MAX_CHARS = 30_000
MAX_REDIRECTS = 4
TIMEOUT = httpx.Timeout(15.0)
TEXT_TYPES = ("text/", "application/json", "application/xml", "application/xhtml+xml")


def _is_public(ip: str) -> bool:
    addr = ipaddress.ip_address(ip)
    if isinstance(addr, ipaddress.IPv6Address) and addr.ipv4_mapped:
        addr = addr.ipv4_mapped
    return addr.is_global and not addr.is_multicast


async def check_url(url: str) -> None:
    """Raise ToolError unless url is http(s) and every address it resolves to is public."""
    parts = urlsplit(url)
    if parts.scheme not in ("http", "https") or not parts.hostname:
        raise ToolError("Only http(s) URLs can be fetched.")
    if parts.username or parts.password:
        raise ToolError("URLs with credentials are not allowed.")
    host = parts.hostname
    try:
        ipaddress.ip_address(host)
        addresses = [host]
    except ValueError:
        try:
            infos = await asyncio.get_running_loop().getaddrinfo(host, parts.port or 443, type=socket.SOCK_STREAM)
        except socket.gaierror as exc:
            raise ToolError(f"Could not resolve {host}.") from exc
        addresses = [info[4][0] for info in infos]
    if not addresses or not all(_is_public(a) for a in addresses):
        raise ToolError("That address is not on the public internet.")


class _TextExtractor(HTMLParser):
    SKIP = {"script", "style", "noscript", "svg", "head"}
    BLOCK = {"p", "div", "br", "li", "tr", "h1", "h2", "h3", "h4", "h5", "h6", "section", "article"}

    def __init__(self) -> None:
        super().__init__()
        self.parts: list[str] = []
        self._skip = 0

    def handle_starttag(self, tag: str, attrs: Any) -> None:
        if tag in self.SKIP:
            self._skip += 1
        elif tag in self.BLOCK:
            self.parts.append("\n")

    def handle_endtag(self, tag: str) -> None:
        if tag in self.SKIP and self._skip:
            self._skip -= 1

    def handle_data(self, data: str) -> None:
        if not self._skip and data.strip():
            self.parts.append(data.strip() + " ")


def html_to_text(html: str) -> str:
    parser = _TextExtractor()
    parser.feed(html)
    lines = (line.strip() for line in "".join(parser.parts).splitlines())
    return "\n".join(line for line in lines if line)


async def fetch(url: str, client: httpx.AsyncClient | None = None) -> str:
    own_client = client is None
    client = client or httpx.AsyncClient(timeout=TIMEOUT, follow_redirects=False,
                                         headers={"User-Agent": "Cubicle/0.1 (+https://github.com/Pro-Sifat-Hasan/cubicle)"})
    try:
        for _ in range(MAX_REDIRECTS + 1):
            await check_url(url)
            async with client.stream("GET", url) as resp:
                if resp.is_redirect and "location" in resp.headers:
                    url = urljoin(url, resp.headers["location"])
                    continue
                if resp.status_code >= 400:
                    raise ToolError(f"The page returned HTTP {resp.status_code}.")
                ctype = resp.headers.get("content-type", "").lower()
                if ctype and not ctype.startswith(TEXT_TYPES):
                    raise ToolError(f"Not a text page ({ctype.split(';')[0]}).")
                body = bytearray()
                async for chunk in resp.aiter_bytes():
                    body.extend(chunk)
                    if len(body) > MAX_BYTES:
                        break
                text = body.decode(resp.encoding or "utf-8", errors="replace")
                if "html" in ctype or text.lstrip().startswith("<"):
                    text = html_to_text(text)
                return text[:MAX_CHARS] + ("\n[truncated]" if len(text) > MAX_CHARS else "")
        raise ToolError("Too many redirects.")
    except httpx.HTTPError as exc:
        raise ToolError(f"Could not fetch the page: {type(exc).__name__}.") from exc
    finally:
        if own_client:
            await client.aclose()


async def _web_fetch(args: dict[str, Any]) -> str:
    return await fetch(require_str(args, "url", max_len=2000))


WEB_FETCH = Tool(
    name="web_fetch",
    description="Download a public web page and return its readable text. Use it to check facts, read "
    "documentation or articles. Always cite the URLs you relied on.",
    parameters={
        "type": "object",
        "properties": {"url": {"type": "string", "description": "Full http(s) URL."}},
        "required": ["url"],
        "additionalProperties": False,
    },
    handler=_web_fetch,
)

"""Worker settings, read from CUBICLE_* environment variables.

When the gateway supervises the worker it injects CUBICLE_GATEWAY_URL,
CUBICLE_WORKER_TOKEN and CUBICLE_HOME. For local development the token is read
from <home>/worker.token, which the gateway creates on first start.
"""

from __future__ import annotations

import os
import sys
from dataclasses import dataclass
from pathlib import Path

DEFAULT_GATEWAY_URL = "http://127.0.0.1:7878"
DEFAULT_MODEL = "claude-opus-5-5"


def default_home() -> Path:
    if env := os.environ.get("CUBICLE_HOME"):
        return Path(env)
    if sys.platform == "win32" and (local := os.environ.get("LOCALAPPDATA")):
        return Path(local) / "Cubicle"
    return Path.home() / ".cubicle"


@dataclass(frozen=True)
class Settings:
    gateway_url: str
    worker_token: str
    home: Path
    provider: str
    model: str
    effort: str
    max_output_tokens: int
    owner_name: str

    @classmethod
    def from_env(cls) -> Settings:
        home = default_home()
        token = os.environ.get("CUBICLE_WORKER_TOKEN", "").strip()
        if not token:
            token_file = home / "worker.token"
            if token_file.exists():
                token = token_file.read_text(encoding="utf-8").strip()
        if not token:
            raise RuntimeError(
                f"No worker token found. Start the gateway once (it creates {home / 'worker.token'}) "
                "or set CUBICLE_WORKER_TOKEN."
            )

        provider = os.environ.get("CUBICLE_PROVIDER", "auto").lower()
        if provider == "auto":
            provider = "anthropic" if os.environ.get("ANTHROPIC_API_KEY") else "demo"

        return cls(
            gateway_url=os.environ.get("CUBICLE_GATEWAY_URL", DEFAULT_GATEWAY_URL).rstrip("/"),
            worker_token=token,
            home=home,
            provider=provider,
            model=os.environ.get("CUBICLE_MODEL", DEFAULT_MODEL),
            effort=os.environ.get("CUBICLE_EFFORT", "medium"),
            max_output_tokens=int(os.environ.get("CUBICLE_MAX_OUTPUT_TOKENS", "16000")),
            owner_name=os.environ.get("CUBICLE_OWNER_NAME", "the owner"),
        )

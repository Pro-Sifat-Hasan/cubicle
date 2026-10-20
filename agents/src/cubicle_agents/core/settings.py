"""Worker settings, read from environment variables.

The gateway resolves the owner's choices (Settings page in the app) and passes
them to the worker as CUBICLE_* variables. For local development the worker
token is read from <home>/worker.token, which the gateway creates on first start.
"""

from __future__ import annotations

import os
import sys
from dataclasses import dataclass
from pathlib import Path

DEFAULT_GATEWAY_URL = "http://127.0.0.1:7878"

# Provider adapter kinds (mirrors gateway/internal/providers).
KIND_ANTHROPIC = "anthropic"
KIND_OPENAI_COMPATIBLE = "openai_compatible"
KIND_CLAUDE_CODE = "claude_code"
KIND_DEMO = "demo"


def default_home() -> Path:
    if env := os.environ.get("CUBICLE_HOME"):
        return Path(env)
    if sys.platform == "win32" and (local := os.environ.get("LOCALAPPDATA")):
        return Path(local) / "Cubicle"
    return Path.home() / ".cubicle"


def _flag(name: str, default: bool) -> bool:
    value = os.environ.get(name)
    if value is None or value == "":
        return default
    return value.strip().lower() in {"1", "true", "yes", "on"}


@dataclass(frozen=True)
class LLMSettings:
    """Which model provider to use and how to reach it."""

    kind: str
    provider: str
    model: str
    effort: str = "medium"
    base_url: str = ""
    api_key: str = ""
    web_access: bool = True
    max_output_tokens: int = 16000

    @classmethod
    def from_env(cls) -> LLMSettings:
        kind = os.environ.get("CUBICLE_LLM_KIND", "").strip()
        provider = os.environ.get("CUBICLE_LLM_PROVIDER", "").strip()
        if not kind:
            # Running without the gateway (development): Claude if a key is set, else demo.
            kind = KIND_ANTHROPIC if os.environ.get("ANTHROPIC_API_KEY") else KIND_DEMO
            provider = provider or kind
        key_env = os.environ.get("CUBICLE_LLM_KEY_ENV", "").strip()
        default_model = "claude-opus-5-5" if kind == KIND_ANTHROPIC else ""
        return cls(
            kind=kind,
            provider=provider or kind,
            model=os.environ.get("CUBICLE_MODEL", "").strip() or default_model,
            effort=os.environ.get("CUBICLE_EFFORT", "medium").strip() or "medium",
            base_url=os.environ.get("CUBICLE_LLM_BASE_URL", "").strip(),
            api_key=os.environ.get(key_env, "") if key_env else "",
            web_access=_flag("CUBICLE_WEB_ACCESS", True),
            max_output_tokens=int(os.environ.get("CUBICLE_MAX_OUTPUT_TOKENS", "16000")),
        )


@dataclass(frozen=True)
class Settings:
    """Everything the worker needs to run the office."""

    gateway_url: str
    worker_token: str
    home: Path
    workspace: Path
    owner_name: str
    llm: LLMSettings

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
        workspace = Path(os.environ.get("CUBICLE_WORKSPACE") or home / "workspace")
        return cls(
            gateway_url=os.environ.get("CUBICLE_GATEWAY_URL", DEFAULT_GATEWAY_URL).rstrip("/"),
            worker_token=token,
            home=home,
            workspace=workspace,
            owner_name=os.environ.get("CUBICLE_OWNER_NAME", "").strip() or "the owner",
            llm=LLMSettings.from_env(),
        )

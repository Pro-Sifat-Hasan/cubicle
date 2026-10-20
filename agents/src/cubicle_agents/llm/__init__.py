"""Language-model providers. `make_llm` picks one from settings."""

from __future__ import annotations

from cubicle_agents.core.settings import Settings
from cubicle_agents.llm.base import LLM, Completion, LLMRefusalError

__all__ = ["LLM", "Completion", "LLMRefusalError", "make_llm"]


def make_llm(settings: Settings) -> LLM:
    if settings.provider == "anthropic":
        from cubicle_agents.llm.anthropic_llm import AnthropicLLM

        return AnthropicLLM(effort=settings.effort, max_output_tokens=settings.max_output_tokens)
    if settings.provider == "demo":
        from cubicle_agents.llm.demo_llm import DemoLLM

        return DemoLLM()
    raise ValueError(f"Unknown CUBICLE_PROVIDER {settings.provider!r} (use anthropic, demo or auto)")

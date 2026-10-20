"""Model providers. `make_llm` builds the adapter selected in the owner's settings."""

from __future__ import annotations

from cubicle_agents.core.settings import (
    KIND_ANTHROPIC,
    KIND_CLAUDE_CODE,
    KIND_DEMO,
    KIND_OPENAI_COMPATIBLE,
    LLMSettings,
)
from cubicle_agents.llm.base import LLM, AgentResult, LLMConfigError, LLMRefusalError, Turn

__all__ = ["LLM", "AgentResult", "LLMConfigError", "LLMRefusalError", "Turn", "make_llm"]


def make_llm(settings: LLMSettings) -> LLM:
    if settings.kind in (KIND_ANTHROPIC, KIND_OPENAI_COMPATIBLE):
        from cubicle_agents.llm.pydantic_llm import PydanticLLM

        return PydanticLLM(kind=settings.kind, provider=settings.provider, api_key=settings.api_key,
                           base_url=settings.base_url, effort=settings.effort,
                           max_output_tokens=settings.max_output_tokens, web_access=settings.web_access)
    if settings.kind == KIND_CLAUDE_CODE:
        from cubicle_agents.llm.claude_code_llm import ClaudeCodeLLM

        return ClaudeCodeLLM(web_access=settings.web_access)
    if settings.kind == KIND_DEMO:
        from cubicle_agents.llm.demo_llm import DemoLLM

        return DemoLLM()
    raise LLMConfigError(f"Unknown provider kind {settings.kind!r}.")

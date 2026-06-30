"""Starter AgentField agent.

Registers as `starter` by default and exposes:

- echo: no-key smoke test
- summarize: optional LLM call when a provider key is configured
"""

from __future__ import annotations

import logging
import os
from typing import Any

from agentfield import Agent, AIConfig
from pydantic import BaseModel


def _init_sentry() -> None:
    dsn = os.getenv("SENTRY_DSN", "").strip()
    if not dsn:
        return
    import sentry_sdk
    from sentry_sdk.integrations.logging import LoggingIntegration

    sentry_sdk.init(
        dsn=dsn,
        environment=os.getenv("SENTRY_ENVIRONMENT") or os.getenv("AF_STACK_ENV") or "development",
        release=os.getenv("SENTRY_RELEASE") or os.getenv("AGENT_VERSION") or "dev",
        traces_sample_rate=0.0,
        integrations=[
            LoggingIntegration(level=logging.INFO, event_level=logging.ERROR),
        ],
    )


_init_sentry()


def select_model() -> str | None:
    override = os.getenv("STARTER_AGENT_MODEL")
    if override:
        return override
    if os.getenv("OPENROUTER_API_KEY"):
        return "openrouter/qwen/qwen-2.5-72b-instruct"
    if os.getenv("ANTHROPIC_API_KEY"):
        return "anthropic/claude-haiku-4-5-20251001"
    if os.getenv("OPENAI_API_KEY"):
        return "openai/gpt-4o-mini"
    return None


MODEL = select_model()

app = Agent(
    node_id=os.getenv("NODE_ID", "starter"),
    version=os.getenv("AGENT_VERSION", "0.1.0"),
    ai_config=AIConfig(model=MODEL) if MODEL else None,
)


@app.reasoner(tags=["starter", "echo"])
async def echo(payload: dict[str, Any]) -> dict[str, Any]:
    """Return input verbatim. Use this for first smoke tests."""
    return {"echoed": payload}


class Summary(BaseModel):
    tldr: str
    next_steps: list[str]


if MODEL is not None:

    @app.reasoner(tags=["starter", "text"])
    async def summarize(payload: dict[str, Any]) -> dict[str, Any]:
        """Summarize text and suggest next steps."""
        text = payload.get("text") or payload.get("content") or ""
        if not text:
            return {"error": "missing text"}
        result = await app.ai(
            system=(
                "Summarize the user's text in one sentence and return three practical next steps."
            ),
            user=text,
            schema=Summary,
        )
        return result.model_dump()


if __name__ == "__main__":
    app.run()

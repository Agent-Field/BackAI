# SPDX-License-Identifier: Apache-2.0

"""Notable agent entrypoint — registers all three reasoners on one node.

The reasoners live in sibling modules so the file each developer touches
to add a feature stays small. Importing them here is what triggers the
``@app.reasoner`` decorator and makes them visible to AgentField.

Run:
    AGENTFIELD_SERVER=http://localhost:8081 NODE_ID=notable-ai \
        OPENROUTER_API_KEY=... python main.py
"""

from __future__ import annotations

import logging
import os

from agentfield import Agent, AIConfig


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


def _select_default_model() -> str | None:
    """Cost-disciplined default. See sample agent for the same logic."""
    override = os.getenv("NOTABLE_MODEL")
    if override:
        return override
    if os.getenv("OPENROUTER_API_KEY"):
        return "openrouter/qwen/qwen-2.5-72b-instruct"
    if os.getenv("ANTHROPIC_API_KEY"):
        return "anthropic/claude-haiku-4-5-20251001"
    if os.getenv("OPENAI_API_KEY"):
        return "openai/gpt-4o-mini"
    return None


_MODEL = _select_default_model()

# Single Agent shared across all three reasoners. node_id="notable-ai"
# means calls land at notable-ai.summarize / notable-ai.suggest_tags /
# notable-ai.todo_completer.
app = Agent(
    node_id=os.getenv("NODE_ID", "notable-ai"),
    version=os.getenv("AGENT_VERSION", "0.1.0"),
    ai_config=AIConfig(model=_MODEL) if _MODEL else None,
)

# Importing each module registers its decorated reasoner. We do this
# after `app` is constructed so the modules can do `from main import app`.
# Using importlib lets us tolerate the absence of any single reasoner
# (e.g. someone deletes one) without breaking the others.
from summarize import main as _summarize  # noqa: E402,F401
from suggest_tags import main as _suggest_tags  # noqa: E402,F401
from todo_completer import main as _todo_completer  # noqa: E402,F401


if __name__ == "__main__":
    app.run()

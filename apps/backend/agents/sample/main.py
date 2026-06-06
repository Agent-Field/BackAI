"""Sample AgentField agent — the smallest possible agent.

Registers as `sample` with the AgentField control plane and exposes one
reasoner that echoes its input. Used by the 60-second quickstart and by CI
to verify end-to-end connectivity (gateway → AF → agent → response).

Optionally exposes a `summarize` reasoner if an LLM key is set, so the
quickstart demo actually does something interesting.

Run:
    AGENTFIELD_SERVER=http://localhost:8081 \
    NODE_ID=sample \
    python main.py
"""

from __future__ import annotations

import os
from typing import Any

from agentfield import Agent, AIConfig
from pydantic import BaseModel


# Choose a model only if we have a key for it. Otherwise the agent still
# registers and the echo reasoner still works, but summarize is omitted.
_MODEL = None
if os.getenv("OPENROUTER_API_KEY"):
    _MODEL = "openrouter/anthropic/claude-3.5-sonnet"
elif os.getenv("ANTHROPIC_API_KEY"):
    _MODEL = "anthropic/claude-sonnet-4-20250514"
elif os.getenv("OPENAI_API_KEY"):
    _MODEL = "openai/gpt-4o-mini"

app = Agent(
    node_id=os.getenv("NODE_ID", "sample"),
    version=os.getenv("AGENT_VERSION", "0.0.1"),
    ai_config=AIConfig(model=_MODEL) if _MODEL else None,
)


@app.reasoner(tags=["test", "echo"])
async def echo(payload: dict[str, Any]) -> dict[str, Any]:
    """Return the input verbatim. Smallest possible reasoner."""
    return {"echoed": payload}


class Summary(BaseModel):
    """LLM-produced summary."""

    tldr: str
    key_points: list[str]


if _MODEL is not None:
    @app.reasoner(tags=["text", "demo"])
    async def summarize(payload: dict[str, Any]) -> dict[str, Any]:
        """Summarize the given text. Demonstrates `app.ai()` with structured output."""
        text = payload.get("text") or payload.get("content") or ""
        if not text:
            return {"error": "missing 'text' in input"}
        result = await app.ai(
            system="Summarize the user's text in 1-2 sentences plus key points.",
            user=text,
            schema=Summary,
        )
        return result.model_dump()


if __name__ == "__main__":
    app.run()

"""Sample AgentField agent — the smallest possible agent.

Registers as `sample` with the AgentField control plane and exposes two
reasoners:

- `echo` — returns input verbatim. Always available, no LLM key needed.
  Used by the 60-second quickstart and by CI to verify end-to-end
  connectivity (gateway → AF → agent → response).

- `summarize` — uses an LLM to summarize text. Registered only if one of
  ``OPENROUTER_API_KEY``, ``ANTHROPIC_API_KEY``, or ``OPENAI_API_KEY`` is
  set. Defaults to a cheap-but-capable model so the demo doesn't burn
  through budget.

Model selection prefers OpenRouter with Qwen 2.5 72B (about $0.35 / $0.40
per 1M tokens in/out) because it's strong on structured output, supports
tool calling, and keeps the per-call cost in the cents.

Run:
    AGENTFIELD_SERVER=http://localhost:8081 \\
    NODE_ID=sample \\
    OPENROUTER_API_KEY=... \\
    python main.py
"""

from __future__ import annotations

import os
from typing import Any

from agentfield import Agent, AIConfig
from pydantic import BaseModel


def _select_default_model() -> str | None:
    """Pick a cheap-but-capable model from whatever credentials are set.

    Priority order:
      1. ``SAMPLE_AGENT_MODEL`` env var — operator override
      2. OpenRouter (Qwen 2.5 72B Instruct, very cheap, strong reasoning)
      3. Anthropic (Claude Haiku — cheapest Claude tier)
      4. OpenAI (gpt-4o-mini — cheapest OpenAI tier)

    Returns ``None`` if no provider is configured.
    """
    override = os.getenv("SAMPLE_AGENT_MODEL")
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
        """Summarize the given text. Demonstrates `app.ai()` with structured output.

        Cheap enough to invoke as a smoke test: a 100-word input via Qwen
        2.5 72B costs roughly $0.0001.
        """
        text = payload.get("text") or payload.get("content") or ""
        if not text:
            return {"error": "missing 'text' in input"}
        result = await app.ai(
            system=(
                "Summarize the user's text in 1-2 sentences for the TLDR, "
                "then list 3-5 key points."
            ),
            user=text,
            schema=Summary,
        )
        return result.model_dump()


if __name__ == "__main__":
    app.run()

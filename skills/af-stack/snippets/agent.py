# Template: AgentField agent for AF Stack.
#
# Drop a copy into apps/backend/agents/<YOUR-AGENT-NAME>/main.py and edit
# the marked sections. The agent registers with the AgentField control
# plane on startup; the runtime gateway forwards calls to
# /api/v1/agents/<your-agent>.<reasoner> to here.
#
# What you GET from AgentField for free (don't reinvent):
#   - LLM calls via app.ai(...) routed through the suite gateway → LiteLLM
#     → providers. Cost lands on the calling tenant automatically.
#   - Memory across 4 scopes (Global / Session / Actor / Workflow) via
#     app.memory.set / get / list_keys / similarity_search. See
#     rules/agents.md for the scope decision tree.
#   - Harness calls via app.harness("claude-code" | "codex" | "gemini" |
#     "opencode") that delegate to the installed CLI inside this
#     container.
#   - MCP servers declared in __capabilities__ become callable as tools.
#
# What you WRITE (the bare minimum):
#   - A reasoner per piece of agent logic. Use `.ai()` for fast
#     structured classification (single LLM call, schema out). Use
#     `.harness(...)` for multi-step work that needs tool use,
#     navigation, or escalation. The CLAUDE.md decision tree is your
#     guide: when in doubt, `.harness()`.

from __future__ import annotations

import os
import shutil
from typing import Any

from agentfield import Agent
from pydantic import BaseModel


# ─── EDIT: change the node_id to your agent name ─────────────────────────
app = Agent(node_id="REPLACE_ME")


# ─── EDIT: define your output schemas ────────────────────────────────────
# Pydantic schemas keep agent outputs typed and contractual. The runtime
# returns them as JSON; the calling SDK validates back on its side.


class GreetingResult(BaseModel):
    """Example: a tiny structured output."""

    greeting: str
    language: str


# ─── EDIT: define your reasoners ─────────────────────────────────────────
# Each @app.reasoner is exposed at
# POST /api/v1/agents/<node_id>.<reasoner-name>
# with body { "input": <payload> } and response { "output": <result> }.


@app.reasoner(tags=["example"])
async def greet(payload: dict) -> dict:
    """Fast classification example — single LLM call, structured out.

    Use .ai() when the input fits comfortably in one context window and
    the output is a flat schema. For document analysis or anything
    requiring navigation, use .harness() instead.
    """
    name = payload.get("name", "world")
    result = await app.ai(
        system="Greet the user warmly in their preferred language.",
        user=f"Greet someone named {name}.",
        schema=GreetingResult,
    )
    return result.model_dump()


# ─── Optional: a harness-based reasoner for complex work ─────────────────
#
# Uncomment if you need multi-step / tool-using work. See rules/agents.md
# for the .ai() vs .harness() decision tree.
#
# @app.reasoner(tags=["example"])
# async def review_codebase(payload: dict) -> dict:
#     """Multi-step example — uses claude-code harness inside a sandbox."""
#     review = await app.harness(provider="claude-code").run(
#         prompt="Review this repo and report issues.",
#         sandbox=dict(
#             image="node:20-alpine",
#             setup=[f"git clone {payload['repo_url']} /work"],
#             timeout_s=300,
#         ),
#         schema=YourReviewSchema,
#     )
#     return review.model_dump()


# ─── Capabilities probe — keep as-is in most cases ───────────────────────
#
# The runtime queries __capabilities__ to learn what tools live in this
# container. Don't remove this — the Build → Agents dashboard tab and
# the /api/v1/harnesses endpoint depend on it.
#
# Schema must match services/runtime/internal/harnesses/interface.go.
# Status values: "ready" (binary + env present) / "needs_auth" (binary
# present, env missing) / "missing" (no binary on PATH).

_HARNESS_SPECS: list[tuple[str, list[str], list[str]]] = [
    ("claude-code", ["claude", "claude-code"], ["ANTHROPIC_API_KEY"]),
    ("codex", ["codex"], ["OPENAI_API_KEY"]),
    ("gemini", ["gemini"], ["GEMINI_API_KEY"]),
    ("opencode", ["opencode"], []),
]
_MCP_RUNNER_BINARIES: list[str] = ["uvx", "npx"]


def _resolve_binary(candidates: list[str]) -> str | None:
    for name in candidates:
        if path := shutil.which(name):
            return path
    return None


def _harness_status(binaries: list[str], env_vars: list[str]) -> str:
    if not _resolve_binary(binaries):
        return "missing"
    if env_vars and not all(os.getenv(v) for v in env_vars):
        return "needs_auth"
    return "ready"


@app.reasoner()
async def __capabilities__(_: dict[str, Any]) -> dict[str, Any]:
    harnesses = [
        {"provider": p, "status": _harness_status(bins, envs), "binaries": bins}
        for p, bins, envs in _HARNESS_SPECS
    ]
    mcp_runners = [
        {"name": b, "status": "ready" if shutil.which(b) else "missing"}
        for b in _MCP_RUNNER_BINARIES
    ]
    return {"harnesses": harnesses, "mcp_runners": mcp_runners}


# ─── Entry point — keep as-is ────────────────────────────────────────────
if __name__ == "__main__":
    app.run()

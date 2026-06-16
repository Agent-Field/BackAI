"""Sample AgentField agent — the smallest possible agent.

Registers as `sample` with the AgentField control plane and exposes:

- ``echo`` — returns input verbatim. Always available, no LLM key needed.
  Used by the 60-second quickstart and by CI to verify end-to-end
  connectivity (gateway -> AF -> agent -> response).

- ``summarize`` — uses an LLM to summarize text. Registered only if one
  of ``OPENROUTER_API_KEY``, ``ANTHROPIC_API_KEY``, or ``OPENAI_API_KEY``
  is set. Defaults to a cheap-but-capable model so the demo doesn't burn
  through budget.

- ``__capabilities__`` — declarative reasoner that reports which CLI
  harnesses (Claude Code, Codex, Gemini, OpenCode) and MCP runners
  (uvx, npx) are physically present in *this* agent container, plus
  whether the corresponding env keys are set. The runtime aggregates
  this across all registered agents to power /api/v1/harnesses and the
  Build -> Agents view in the dashboard.

  Why the agent owns this: the runtime container is distroless and
  cannot host these tools. They belong in the agent container where
  ``app.harness(provider="claude-code")`` and ``app.mcp.call(...)``
  actually execute.

Model selection prefers OpenRouter with Qwen 2.5 72B (about $0.35 /
$0.40 per 1M tokens in/out) because it's strong on structured output,
supports tool calling, and keeps the per-call cost in the cents.

Run:
    AGENTFIELD_SERVER=http://localhost:8081 \\
    NODE_ID=sample \\
    OPENROUTER_API_KEY=... \\
    python main.py
"""

from __future__ import annotations

import logging
import os
import shutil
import subprocess
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


# ─── Harness + MCP capability declaration ────────────────────────────────
#
# The runtime queries this agent's ``__capabilities__`` reasoner at boot
# (and on demand) to learn what tools live in this container. The schema
# matches what the runtime's harnesses package aggregates across agents
# — keep these names + status enum in sync with
# services/runtime/internal/harnesses/interface.go.

# Each entry: (provider_id, candidate_binaries, required_env_vars).
# A provider is "ready" iff at least one candidate binary resolves AND
# all required env vars are non-empty. "needs_auth" iff binary present
# but env missing. "missing" iff no binary resolved.
_HARNESS_SPECS: list[tuple[str, list[str], list[str]]] = [
    ("claude-code", ["claude", "claude-code"], ["ANTHROPIC_API_KEY"]),
    ("codex", ["codex"], ["OPENAI_API_KEY"]),
    ("gemini", ["gemini"], ["GEMINI_API_KEY"]),
    # OpenCode bundles credentials in a settings file, so no env var is
    # required at probe time. The harness still appears as "ready" once
    # the binary is on PATH.
    ("opencode", ["opencode"], []),
]

# MCP runners are *spawners* for stdio MCP servers (uvx mcp-server-github,
# npx @modelcontextprotocol/server-postgres, etc.). The runtime forwards
# spawn requests to an agent that has the matching runner.
_MCP_RUNNER_BINARIES: list[str] = ["uvx", "npx"]


def _resolve_binary(candidates: list[str]) -> str | None:
    """Return the first candidate that resolves on PATH, else None."""
    for name in candidates:
        path = shutil.which(name)
        if path:
            return path
    return None


def _probe_version(binary: str) -> str | None:
    """Run ``<binary> --version`` with a 5s timeout. Returns the first
    non-empty line of stdout (or stderr), or None on failure. Failures
    are intentionally swallowed — version is best-effort metadata, not a
    gate on ``ready`` status.
    """
    try:
        result = subprocess.run(
            [binary, "--version"],
            capture_output=True,
            text=True,
            timeout=5,
            check=False,
        )
    except (FileNotFoundError, subprocess.TimeoutExpired, OSError):
        return None
    text = (result.stdout or result.stderr or "").strip()
    if not text:
        return None
    return text.splitlines()[0].strip()


def _build_capabilities() -> dict[str, Any]:
    """Snapshot the harness + MCP-runner availability of this container.

    Snapshotted once at process startup (binaries don't appear / disappear
    on a running container) but env-var presence is re-checked on every
    call so an operator who fixed a missing ANTHROPIC_API_KEY can
    re-probe without a restart.
    """
    harnesses: list[dict[str, Any]] = []
    for provider, candidates, required_env in _HARNESS_SPECS:
        binary_path = _resolve_binary(candidates)
        if binary_path is None:
            harnesses.append(
                {
                    "provider": provider,
                    "is_installed": False,
                    "binary_path": None,
                    "version": None,
                    "status": "missing",
                    "last_error": None,
                    "required_env": required_env,
                }
            )
            continue
        version = _probe_version(binary_path)
        missing_env = [
            name for name in required_env if not (os.environ.get(name) or "").strip()
        ]
        if missing_env:
            harnesses.append(
                {
                    "provider": provider,
                    "is_installed": True,
                    "binary_path": binary_path,
                    "version": version,
                    "status": "needs_auth",
                    "last_error": "required env not set: " + ", ".join(missing_env),
                    "required_env": required_env,
                }
            )
            continue
        harnesses.append(
            {
                "provider": provider,
                "is_installed": True,
                "binary_path": binary_path,
                "version": version,
                "status": "ready",
                "last_error": None,
                "required_env": required_env,
            }
        )

    mcp_runners: list[dict[str, Any]] = []
    for name in _MCP_RUNNER_BINARIES:
        path = shutil.which(name)
        if path is None:
            continue
        mcp_runners.append(
            {
                "name": name,
                "binary_path": path,
                "version": _probe_version(path),
            }
        )

    return {
        "node_id": os.getenv("NODE_ID", "sample"),
        "harnesses": harnesses,
        "mcp_runners": mcp_runners,
    }


# ─── Default LLM model selection ─────────────────────────────────────────


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


@app.reasoner(tags=["capabilities", "system"])
async def __capabilities__(payload: dict[str, Any]) -> dict[str, Any]:
    """Report harness + MCP-runner availability in this agent container.

    Called by the runtime's harness aggregator. The payload is ignored;
    callers may pass ``{}``. Returns the shape expected by the runtime's
    agentfield.Client.Capabilities() method.
    """
    return _build_capabilities()


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

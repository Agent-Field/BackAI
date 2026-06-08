"""Shipwright stub agent — simulates the SWE-AF flow.

This is the iteration stub. The real Shipwright will replace this file
with the actual SWE-AF library invocation. Keep the agent node_id and
reasoner name stable so the customer-app + workload module don't change.

The same reasoner name (`run`) will:
- Today (stub): simulate steps with sleeps + return a fake result
- Tomorrow (real): drive claude-code / codex / gemini inside a sandbox

To swap: replace this file with a real implementation that invokes
swe-af. Keep `Agent(node_id="shipwright-v2")` and `@app.reasoner` name `run`.
"""

from __future__ import annotations

import asyncio
import os
import shutil
from typing import Any

from agentfield import Agent
from pydantic import BaseModel

app = Agent(node_id="shipwright-v2")


class Step(BaseModel):
    idx: int
    title: str
    status: str
    detail: str | None = None


class RunResult(BaseModel):
    status: str
    summary: str
    diff_preview: str | None = None
    steps: list[Step]


@app.reasoner(tags=["shipwright"])
async def execute_task(payload: dict) -> dict:
    """Simulate a code-agent run for the iteration UI.

    Returns a deterministic-ish plan + result so the UI can render real
    state. Total runtime ~10s so screenshots can capture mid-flight.
    """
    issue_url = payload.get("issue_url", "(no url)")
    title = payload.get("title", "untitled task")

    print(f"[shipwright] starting task title={title!r} issue={issue_url!r}", flush=True)

    steps_template = [
        ("Parse GitHub issue", 1.5,
         f"Fetched {issue_url} - classified as bug-fix"),
        ("Clone target repository", 1.5,
         "git clone -> /work - checked out main"),
        ("Read relevant source files", 2.0,
         "5 files - 412 lines reviewed"),
        ("Generate change plan", 2.0,
         "Plan: rename method - add nil-check - cover with unit test"),
        ("Apply changes", 1.5,
         "3 files modified - diff cached"),
        ("Run test suite", 1.0,
         "12 passed - 0 failed - 8s"),
        ("Open pull request", 0.5,
         "PR #142 opened against main"),
    ]

    steps: list[Step] = []
    for idx, (step_title, duration, detail) in enumerate(steps_template, start=1):
        await asyncio.sleep(duration)
        steps.append(Step(idx=idx, title=step_title, status="completed",
                          detail=detail))
        print(f"[shipwright] step {idx}/{len(steps_template)}: {step_title}",
              flush=True)

    summary = (
        f"Reviewed {issue_url}. Plan executed: 3 files edited, 12 tests pass, "
        f"PR opened."
    )
    diff = (
        "diff --git a/src/handler.go b/src/handler.go\n"
        "--- a/src/handler.go\n"
        "+++ b/src/handler.go\n"
        "@@ -42,7 +42,11 @@ func handle(ctx context.Context, req Request) error {\n"
        "     if req.User == nil {\n"
        "-        return errors.New(\"missing user\")\n"
        "+        return ErrMissingUser\n"
        "     }\n"
        "     if req.Body == \"\" {\n"
        "+        return ErrEmptyBody\n"
        "+    }\n"
        "     return processRequest(ctx, req)\n"
        " }\n"
    )

    print(f"[shipwright] task complete title={title!r}", flush=True)
    return RunResult(
        status="completed",
        summary=summary,
        diff_preview=diff,
        steps=steps,
    ).model_dump()


_HARNESS_SPECS: list[tuple[str, list[str], list[str]]] = [
    ("claude-code", ["claude", "claude-code"], ["ANTHROPIC_API_KEY"]),
    ("codex", ["codex"], ["OPENAI_API_KEY"]),
    ("gemini", ["gemini"], ["GEMINI_API_KEY"]),
    ("opencode", ["opencode"], []),
]
_MCP_RUNNERS = ["uvx", "npx"]


def _resolve(candidates: list[str]) -> str | None:
    for name in candidates:
        if path := shutil.which(name):
            return path
    return None


def _status(bins: list[str], envs: list[str]) -> str:
    if not _resolve(bins):
        return "missing"
    if envs and not all(os.getenv(v) for v in envs):
        return "needs_auth"
    return "ready"


@app.reasoner()
async def __capabilities__(_: dict[str, Any]) -> dict[str, Any]:
    harnesses = [
        {"provider": p, "status": _status(b, e), "binaries": b}
        for p, b, e in _HARNESS_SPECS
    ]
    mcp_runners = [
        {"name": b, "status": "ready" if shutil.which(b) else "missing"}
        for b in _MCP_RUNNERS
    ]
    return {"harnesses": harnesses, "mcp_runners": mcp_runners}


if __name__ == "__main__":
    app.run()

# SPDX-License-Identifier: Apache-2.0

"""Deep Research agent — long-running fan-out with parallel sub-investigations.

This is what AF Stack looks like when you build a long-running agent. The
operator hands the agent a research question; the agent decomposes it into
sub-questions, fans those out to parallel sub-investigations (each its own
``.harness()`` call), accumulates the findings in AF memory as they land,
then synthesises a structured Report from everything it gathered.

Pipeline (WHY each stage exists, not just what):

  1. DECOMPOSE  (``.ai()`` — single fast structured call)
       Splitting the question into 5 sub-questions up-front is cheap, and
       it gives us 5 independent work items we can run in parallel. The
       alternative — one giant prompt — produces shallow generalist output
       and forfeits all the parallelism downstream.

  2. FAN-OUT    (``.harness()`` per sub-question, ``asyncio.gather``)
       Each sub-question is its own multi-turn investigation. A harness
       can read references, follow up on partial answers, escalate when
       it finds something worth digging into. That is the whole point of
       the atomic unit of intelligence pattern: the orchestrator hands
       over a goal and verifies the result, not every step.

       When the runtime reports the claude-code harness is ``missing``,
       we drop to a mock harness (single ``.ai()`` per sub-question)
       so the example still demonstrates the data flow on a fresh box.

  3. ACCUMULATE (``app.memory.set`` at ``scope=run, key=finding.<i>``)
       Findings land in AF memory keyed by sub-question index. Two
       reasons: (a) the dashboard's Memory browser surfaces them as the
       run progresses, so an operator can watch the agent work; (b) any
       downstream agent (or the synthesiser itself) reads from memory
       rather than juggling in-process state.

  4. SYNTHESISE (``.ai()`` with the Report schema)
       Final structured pass. Now the input is small and well-shaped
       (N findings + the original question), so ``.ai()`` is the right
       primitive — no need to navigate, no follow-up reads.

Composite reasoning > monolithic prompting: every stage is constrained,
verifiable, and replaceable, and the *system* reasons better than any
single LLM call could.
"""

from __future__ import annotations

import asyncio
import logging
import os
from typing import Any

from agentfield import Agent, AIConfig
from pydantic import BaseModel, Field

from mocks.harness import mock_investigate

logger = logging.getLogger("researcher")
logging.basicConfig(
    level=os.getenv("LOG_LEVEL", "INFO").upper(),
    format="%(asctime)s %(levelname)s %(name)s %(message)s",
)


# ─── Model selection ──────────────────────────────────────────────────────
#
# Qwen 2.5 72B on OpenRouter: ~$0.35/$0.40 per 1M tokens. The whole
# pipeline (1 decompose + 5 sub-investigations + 1 synthesis) lands well
# under a cent for a typical question. Operators can override via
# RESEARCHER_MODEL.

def _select_model() -> str | None:
    override = os.getenv("RESEARCHER_MODEL")
    if override:
        return override
    if os.getenv("OPENROUTER_API_KEY"):
        return "openrouter/qwen/qwen-2.5-72b-instruct"
    if os.getenv("ANTHROPIC_API_KEY"):
        return "anthropic/claude-haiku-4-5-20251001"
    if os.getenv("OPENAI_API_KEY"):
        return "openai/gpt-4o-mini"
    return None


_MODEL = _select_model()

app = Agent(
    node_id=os.getenv("NODE_ID", "researcher"),
    version=os.getenv("AGENT_VERSION", "0.0.1"),
    ai_config=AIConfig(model=_MODEL) if _MODEL else None,
)


# ─── Schemas ──────────────────────────────────────────────────────────────


class SubQuestionList(BaseModel):
    """Output of the decompose step.

    Five sub-questions is the sweet spot: enough breadth to cover most
    research topics, few enough that the fan-out stays cheap and the
    synthesiser's input stays small.
    """

    sub_questions: list[str] = Field(
        ...,
        description="5 focused sub-questions that together cover the user's question.",
        min_length=3,
        max_length=8,
    )


class Finding(BaseModel):
    """One sub-investigation's output.

    Used by both the real harness fallback and the mock harness. Kept
    deliberately small so the synthesis step's prompt stays bounded
    even when 5+ findings are concatenated.
    """

    summary: str = Field(..., description="Concise narrative of what was found.")
    references: list[str] = Field(
        default_factory=list,
        description="Sources, citations, or pointers — URLs, paper titles, etc.",
    )


class FindingWithQuestion(BaseModel):
    """A Finding tagged with the sub-question that produced it.

    This is what gets stored in AF memory: the synthesiser needs both
    the question and the answer to reason cleanly.
    """

    sub_question: str
    summary: str
    references: list[str] = Field(default_factory=list)


class Report(BaseModel):
    """Final synthesis output.

    ``confidence`` is the model's self-reported certainty on a 0-1 scale;
    treat it as a heuristic for surfacing low-confidence reports in
    review queues, not as a calibrated probability.
    """

    thesis: str = Field(..., description="One-paragraph answer to the user's question.")
    key_findings: list[FindingWithQuestion] = Field(
        ..., description="The sub-findings that support the thesis.", min_length=1
    )
    confidence: float = Field(..., ge=0.0, le=1.0)
    sources_consulted: int = Field(..., ge=0)


# ─── Harness availability probe ──────────────────────────────────────────
#
# Probed once at module load. If claude-code is not installed/usable,
# every sub-investigation drops to the mock — so the example always
# runs end-to-end, even with no harness binary on the host.

_HARNESS_PROVIDER = os.getenv("RESEARCHER_HARNESS", "claude-code")
_HARNESS_AVAILABLE: bool | None = None


async def _harness_available() -> bool:
    """Cached probe of the configured harness provider.

    We call ``app.harness()`` once with a trivial prompt; if it raises
    or returns ``failure_type != none``, we mark the provider unavailable
    and route every future call through the mock.

    Rationale: doing this once at startup avoids ~3s of per-sub-question
    probe overhead, and the result is stable for the lifetime of the
    process (operators install harnesses, they don't appear mid-run).
    """
    global _HARNESS_AVAILABLE
    if _HARNESS_AVAILABLE is not None:
        return _HARNESS_AVAILABLE

    try:
        # A single-turn trivial call. Real failure modes (binary missing,
        # missing API key, bad install) all surface here.
        result = await app.harness(
            prompt="Reply with the single word: ok",
            provider=_HARNESS_PROVIDER,
            max_turns=1,
            max_budget_usd=0.05,
        )
        _HARNESS_AVAILABLE = bool(result and not result.is_error)
    except Exception as exc:  # pragma: no cover — depends on host setup
        logger.info(
            "harness probe failed: %s — falling back to mock for all sub-investigations",
            exc,
        )
        _HARNESS_AVAILABLE = False

    if _HARNESS_AVAILABLE:
        logger.info("harness provider %s is available", _HARNESS_PROVIDER)
    else:
        logger.info(
            "harness provider %s is missing — using mock fallback (single .ai() per sub-question)",
            _HARNESS_PROVIDER,
        )
    return _HARNESS_AVAILABLE


# ─── Pipeline steps ──────────────────────────────────────────────────────


async def _decompose(question: str, run_id: str) -> list[str]:
    """Stage 1: split the question into sub-questions.

    Single fast ``.ai()`` call — input fits in one prompt, output is a
    flat list. This is the textbook ``.ai()`` use case.
    """
    logger.info("step=decompose run_id=%s", run_id)
    result = await app.ai(
        system=(
            "You are a research planner. Given a broad research question, "
            "break it into 5 focused sub-questions that together would let "
            "a researcher answer the original question. Each sub-question "
            "should be independently investigable."
        ),
        user=question,
        schema=SubQuestionList,
    )
    qs = result.sub_questions[:5]
    logger.info(
        "step=decompose run_id=%s produced %d sub-questions", run_id, len(qs)
    )
    return qs


async def _investigate(
    idx: int, sub_question: str, run_id: str
) -> FindingWithQuestion:
    """Stage 2: one sub-investigation.

    Tries the real harness first; falls back to the mock if the harness
    probe reported ``missing``. Real harness investigation can read URLs,
    follow citation chains, run tools — the mock just does one ``.ai()``
    summary so the data flow still works end-to-end.
    """
    logger.info(
        "step=investigate run_id=%s idx=%d question=%r", run_id, idx, sub_question
    )

    if await _harness_available():
        prompt = (
            f"Research the following question and return a concise factual "
            f"summary with any references (URLs, paper titles, project names) "
            f"you can cite.\n\nQuestion: {sub_question}"
        )
        try:
            result = await app.harness(
                prompt=prompt,
                provider=_HARNESS_PROVIDER,
                schema=Finding,
                max_turns=int(os.getenv("RESEARCHER_HARNESS_MAX_TURNS", "5")),
                max_budget_usd=float(os.getenv("RESEARCHER_HARNESS_BUDGET_USD", "0.50")),
            )
            if result and not result.is_error and result.parsed:
                finding: Finding = result.parsed
                logger.info(
                    "step=investigate run_id=%s idx=%d source=harness ok", run_id, idx
                )
                return FindingWithQuestion(
                    sub_question=sub_question,
                    summary=finding.summary,
                    references=finding.references,
                )
            logger.info(
                "step=investigate run_id=%s idx=%d harness returned no parsed result; "
                "falling back to mock", run_id, idx,
            )
        except Exception as exc:
            # Real-world: a single harness blowing up shouldn't kill the
            # whole research pass. Log and degrade to the mock.
            logger.warning(
                "step=investigate run_id=%s idx=%d harness raised %s; falling back",
                run_id, idx, exc,
            )

    # Mock fallback. Single .ai() with the Finding schema.
    finding = await mock_investigate(app, sub_question)
    logger.info(
        "step=investigate run_id=%s idx=%d source=mock ok", run_id, idx
    )
    return FindingWithQuestion(
        sub_question=sub_question,
        summary=finding.summary,
        references=finding.references,
    )


async def _accumulate(
    run_id: str, findings: list[FindingWithQuestion]
) -> None:
    """Stage 3: write findings into AF memory at run scope.

    Each finding lands at ``key=finding.<i>`` under the current run's
    workflow scope. The dashboard's Memory browser keys off this layout —
    operators can watch findings appear as the run progresses.

    We write inside a ``gather`` so the disk-level fan-in matches the
    investigation fan-out; otherwise the last finding waits behind the
    first.
    """
    logger.info(
        "step=accumulate run_id=%s writing %d findings to memory", run_id, len(findings)
    )

    async def _put(i: int, f: FindingWithQuestion) -> None:
        # ``app.memory.set`` auto-scopes to the current run (workflow
        # scope). Operators reading the Memory browser see one key per
        # finding under this run_id.
        await app.memory.set(f"finding.{i}", f.model_dump())

    await asyncio.gather(*(_put(i, f) for i, f in enumerate(findings)))


async def _synthesise(
    question: str, findings: list[FindingWithQuestion], run_id: str
) -> Report:
    """Stage 4: turn the findings into a Report.

    Read the findings back out of memory (rather than re-using the local
    list) to demonstrate the round-trip and to keep this step replaceable
    by a separate agent in a real production system.
    """
    logger.info("step=synthesise run_id=%s", run_id)

    keys = [f"finding.{i}" for i in range(len(findings))]
    rehydrated: list[FindingWithQuestion] = []
    for k in keys:
        raw = await app.memory.get(k)
        if raw:
            rehydrated.append(FindingWithQuestion.model_validate(raw))

    # Build a compact bullet list — the synthesiser doesn't need JSON
    # framing, it needs natural-language context (archei rules: data
    # flowing into an LLM agent should be string, not structured).
    bullets = "\n".join(
        f"- Sub-question: {f.sub_question}\n  Summary: {f.summary}\n  "
        f"References: {', '.join(f.references) if f.references else 'none'}"
        for f in rehydrated
    )

    report = await app.ai(
        system=(
            "You are a senior research synthesiser. You will receive a "
            "research question and a list of findings from independent "
            "sub-investigations. Produce a single structured Report:\n"
            "- thesis: one paragraph answering the original question\n"
            "- key_findings: pass through the supporting sub-findings\n"
            "- confidence: your self-reported certainty in [0, 1]\n"
            "- sources_consulted: count of distinct references across findings\n"
            "Be honest about uncertainty: lower confidence when sources "
            "disagree, are weak, or are missing."
        ),
        user=f"Research question: {question}\n\nFindings:\n{bullets}",
        schema=Report,
    )
    logger.info(
        "step=synthesise run_id=%s confidence=%.2f findings=%d",
        run_id, report.confidence, len(report.key_findings),
    )
    return report


# ─── Reasoner ────────────────────────────────────────────────────────────


@app.reasoner(tags=["research", "long-running"])
async def research(question: str, depth: int = 2) -> dict[str, Any]:
    """Run the full deep-research pipeline.

    Args:
        question: The research question to investigate.
        depth: Reserved for future recursive deepening (currently the
            pipeline always runs the four canonical stages once).

    Returns:
        Report.model_dump() — the structured synthesis. Cost is tracked
        automatically through the gateway; query ``GET /api/v1/cost``
        with the run_id to see the spend breakdown.
    """
    # Pull the run_id from the active execution context so logs and
    # memory writes are correlated. AgentField populates this for every
    # reasoner invocation.
    ec = app._current_execution_context  # type: ignore[attr-defined]
    run_id = getattr(ec, "run_id", "unknown") if ec is not None else "unknown"

    logger.info(
        "step=start run_id=%s question=%r depth=%d", run_id, question, depth
    )

    if _MODEL is None:
        return {
            "error": (
                "No LLM provider configured. Set OPENROUTER_API_KEY, "
                "ANTHROPIC_API_KEY, or OPENAI_API_KEY."
            )
        }

    # 1. Decompose
    sub_questions = await _decompose(question, run_id)

    # 2. Fan-out (concurrent sub-investigations)
    findings: list[FindingWithQuestion] = await asyncio.gather(
        *(_investigate(i, q, run_id) for i, q in enumerate(sub_questions))
    )

    # 3. Accumulate into run-scoped memory
    await _accumulate(run_id, findings)

    # 4. Synthesise the final Report
    report = await _synthesise(question, findings, run_id)

    logger.info(
        "step=done run_id=%s thesis_chars=%d", run_id, len(report.thesis)
    )
    return report.model_dump()


if __name__ == "__main__":
    app.run()
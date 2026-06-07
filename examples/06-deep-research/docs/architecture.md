# Architecture — Deep Research

This document explains the multi-reasoner architecture behind the
deep-research example, why each piece exists, and where it lives in the
code. The master patterns reference is `code/CLAUDE.md` in the platform
root — read it for the broader composite-intelligence philosophy.

## Premise

A single LLM call answering a broad research question produces shallow,
generalist output. Five focused sub-investigations stitched together
produce something a human researcher would recognise as work.

The trick is that the five sub-investigations are *independent* — they
can fan out in parallel — and the synthesis only ever sees their flat
outputs, not the full intermediate state. That's the whole pattern in
one sentence.

## Pipeline

```
                  ┌──────────────────────┐
question  ──────▶ │ 1. DECOMPOSE         │   .ai()  + SubQuestionList
                  │   (one fast call)    │
                  └──────────┬───────────┘
                             │ 5 sub-questions
                             ▼
        ┌────────────────────┴────────────────────┐
        │              2. FAN-OUT                 │
        │                                         │   asyncio.gather
        │   ┌────────┐ ┌────────┐ ┌────────┐ ...  │
        │   │ inv #0 │ │ inv #1 │ │ inv #2 │      │   .harness() per sub-q
        │   └────────┘ └────────┘ └────────┘      │   (mock if missing)
        └────────────────────┬────────────────────┘
                             │ 5 findings
                             ▼
                  ┌──────────────────────┐
                  │ 3. ACCUMULATE        │   app.memory.set
                  │   scope=run          │   key=finding.<i>
                  │   key=finding.<i>    │
                  └──────────┬───────────┘
                             │ read back
                             ▼
                  ┌──────────────────────┐
                  │ 4. SYNTHESISE        │   .ai() + Report
                  │   (one final call)   │
                  └──────────┬───────────┘
                             │
                             ▼
                          Report
```

## Pattern mapping

This example uses three of the patterns documented in `code/CLAUDE.md`.

### 1. Parallel Hunters + Signal Cascade

> Multiple specialized agents analyse different dimensions of the same
> input in parallel. Findings are collected, deduplicated, and cascaded
> downstream.

Where: stage 2 (FAN-OUT) in `agents/researcher/main.py`. The
sub-investigations run via `asyncio.gather`; each one is independently
investigable. The cascade is the `accumulate -> synthesise` boundary.

Why here: a research question has multiple independent dimensions
(architecture, evaluation, security, deployment, tooling). Running them
sequentially throws away the natural parallelism.

### 2. The `.ai()` Fallback Pattern (mock harness as graceful degradation)

> Every `.ai()` call in your pipeline should have an answer to: "What
> happens when the input doesn't fit?"

Where: `_investigate()` in `agents/researcher/main.py`. The agent prefers
`.harness()` (multi-turn, tool-using) for each sub-question. When the
harness probe reports `missing`, we degrade to a single `.ai()` call
defined in `agents/researcher/mocks/harness.py`.

This is the inverse of the classic pattern (the original is "`.ai()`
falls back to `.harness()` when overwhelmed"); here we fall *back* to
`.ai()` when the richer primitive isn't installed. Same principle:
never hard-fail when there's a degraded path that preserves data flow.

### 3. Fan-Out -> Filter -> Synthesise (subset of recursive deep research)

> Breadth-first exploration followed by quality-gated recursion into gaps.

Where: the four pipeline stages in `research()`. We currently do a
single-level fan-out (no recursion into gaps yet — that's what the
unused `depth` parameter is reserved for). The structure is set up so
operators can extend `_investigate()` to recurse when a finding's
confidence is low or its references are weak.

## Primitive selection — why each stage uses what it uses

| Stage | Primitive | Reasoning |
|---|---|---|
| Decompose | `.ai()` | Input is the question (small), output is a flat list (3-8 strings). Textbook structured classification. |
| Investigate | `.harness()` → mock `.ai()` | Each sub-question may need to read sources, follow citations, escalate. `.harness()` is the right primitive; the mock exists only so the example runs without the binary installed. |
| Accumulate | `app.memory.set` | Findings need to survive past the in-process state so the dashboard can show them mid-run, and so any downstream agent can read them. |
| Synthesise | `.ai()` | Input is bounded (5 short findings + the question), output is a flat structured Report. Same reasoning as decompose. |

The pattern from `code/CLAUDE.md`:

> **The rule:** When in doubt, use `.harness()`. Reserve `.ai()` for
> gates, classifiers, and routing decisions.

Decompose and synthesise look like routing/classification (small input,
flat output). Investigation looks like reading and reasoning over an
open-ended input. The decision tree drops out cleanly.

## Data-flow shape (Archei rules)

`code/multi-reasoner-archei-rules.md` says: structured JSON for
programmatic routing, strings for LLM-to-LLM, hybrid only when needed.
Here:

- **Decompose → Investigate**: structured (list of strings). Code uses
  the list length and indexes findings by it. JSON.
- **Investigate → Accumulate**: structured (`FindingWithQuestion`).
  Memory keys it by index; the dashboard renders it as a table. JSON.
- **Accumulate → Synthesise**: **string**. We pull findings out of
  memory, flatten them into a natural-language bullet list, and pass
  *that* to the synthesiser's prompt. The synthesiser is an LLM; LLMs
  reason over natural language, not over JSON schemas.
- **Synthesise → caller**: structured `Report`. The caller (operator,
  CI, smoke test) makes decisions on `confidence`, displays
  `key_findings`, etc. JSON.

The string-vs-JSON boundary lands exactly where the rule says it
should: at the LLM-to-LLM hop in stage 4.

## Cost shape

Default model is Qwen 2.5 72B via OpenRouter (~$0.35 in / $0.40 out per
1M tokens). One full research pass is:

- 1 × decompose call (~1k tokens total)
- 5 × investigate calls (~1-5k tokens each, depending on whether the
  real harness or the mock is used)
- 5 × `memory.set` (no LLM cost)
- 1 × synthesise call (~3-5k tokens total)

Total: a few cents in the harness path, well under a cent in the mock
path.

Cost is recorded per-call by the LLM gateway (Phase 7). Query
`/api/v1/cost/events` to see the breakdown; the dashboard's Cost panel
filters by `run_id`.

## What to look at in the dashboard

1. **Runs tab** — see the `researcher.research` run progress through the
   four stages. Each `.ai()` and `.harness()` call becomes a sub-span.
2. **Memory browser** — filter by the run's ID; you'll see five
   `finding.<i>` keys appear as the fan-out completes.
3. **Cost panel** — six (or eleven, depending on harness path) LLM calls,
   total well under a cent for the default model.

## How to extend

- **Deeper investigations**: replace `mocks/harness.py` with a richer
  prompt or wire a real harness. Install `claude-code`, set
  `RESEARCHER_HARNESS=claude-code`, and the probe at startup will route
  every sub-investigation through the real binary.
- **Recursive gap-finding**: in `_investigate`, after the first pass,
  inspect the finding's `confidence` (or absence of references) and
  recurse with a sharper sub-question. Cap recursion by the `depth`
  argument.
- **Cross-sub-question deduplication**: between accumulate and
  synthesise, add a filter step that drops findings whose summary is
  too similar to an earlier one (cosine similarity over the embedding,
  via `app.memory.set_vector` / `similarity_search`).
- **Adversarial review**: after synthesise, run an adversary agent
  (`.harness()`) that tries to falsify the thesis. Lower `confidence`
  if it finds counterevidence. This is the HUNT → PROVE pattern from
  `code/CLAUDE.md`.

Every one of these extensions is a single new stage in the pipeline,
not a rewrite. That's the property composite reasoning buys you.

# 06 — Deep Research

Deep Research is what AF Stack looks like when you build long-running
agents with parallel sub-investigations. The agent fans out, each
sub-question runs in its own harness, findings accumulate in AF memory,
and the final synthesis joins everything.

You hand the agent a research question. It decomposes the question into
five sub-questions, runs five investigations in parallel, drops each
finding into AF memory as it lands, and synthesises a structured Report
at the end. The dashboard's Runs tab shows the fan-out happen live; the
Memory browser shows the findings accumulate; the Cost panel shows the
total spend break down per LLM call.

Composite reasoning beats one big prompt. The intelligence is in the
composition.

## Quickstart

```bash
cd examples/06-deep-research
cp .env.example .env
# edit .env — set OPENROUTER_API_KEY (one key is enough)

docker compose -f docker-compose.yml up --build

# in another shell:
./scripts/run-research.sh
# or with your own question:
./scripts/run-research.sh "What are open problems in retrieval-augmented generation?"
```

Output is the structured Report (thesis, key findings, confidence,
sources consulted), and the script prints deep links into the
dashboard's Memory browser and Cost panel.

To validate end-to-end without watching the logs:

```bash
./scripts/smoke-test.sh
```

The smoke test posts a small research question, waits up to two
minutes, and asserts the Report shape. It exits 0 with a SKIP message
when no LLM key is set.

## Architecture

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

Why this shape (decompose → fan-out → accumulate → synthesise) and not
one big prompt:

- **Parallelism**: five independent investigations run concurrently. A
  monolithic prompt is sequential by definition.
- **Verifiability**: each stage has a tight schema. You can drop in a
  better synthesiser without touching the fan-out, or vice versa.
- **Cost shape**: tiny gate calls (`.ai()`) wrap an expensive middle
  layer (`.harness()`). The total bill scales with the number of
  sub-questions, not with prompt length.
- **Observability**: each stage writes its output somewhere the
  dashboard can read. The operator can watch the agent work.

Full pattern walkthrough — including which patterns from
`code/CLAUDE.md` show up where — lives in [`docs/architecture.md`](docs/architecture.md).

## Cost shape

Default model is Qwen 2.5 72B via OpenRouter (~$0.35 in / $0.40 out per
1M tokens). One research pass:

| Stage | Calls | Tokens (typical) | Cost (Qwen) |
|---|---|---|---|
| Decompose | 1 | ~1k | ~$0.0004 |
| Investigate (mock path) | 5 | ~1k each | ~$0.002 |
| Investigate (harness path) | 5 | 1-5k each | $0.005 - $0.05 |
| Synthesise | 1 | ~3k | ~$0.0012 |

Mock-path total: well under a cent. Harness-path total: a few cents.
The agent enforces `max_budget_usd` per sub-investigation; cap it
tighter in `.env` if you need to.

## What to look at in the dashboard

After `./scripts/run-research.sh`:

1. **Runs tab** (`http://localhost:3000/dashboard/runs`)
   — see the `researcher.research` run. Each pipeline stage shows up
   as a sub-span you can drill into.

2. **Memory browser** (`http://localhost:3000/dashboard/memory`)
   — filter by the run ID printed by the script. You'll see five
   `finding.<i>` keys, one per sub-investigation, with the structured
   finding payload.

3. **Cost panel** (`http://localhost:3000/dashboard/cost`)
   — six (mock path) or eleven (harness path) LLM calls. The
   per-model breakdown shows where the spend goes.

## Files

```
06-deep-research/
├── README.md                          (this file)
├── docker-compose.yml                 (includes root compose, adds researcher service)
├── config.yaml                        (modules: llm-gateway, memory, harnesses on)
├── .env.example                       (OPENROUTER_API_KEY, harness knobs)
├── Dockerfile                         (researcher image)
├── agents/
│   └── researcher/
│       ├── main.py                    (the agent — read this first)
│       ├── requirements.txt
│       └── mocks/
│           └── harness.py             (.ai() fallback when no real harness)
├── scripts/
│   ├── run-research.sh                (kick off a run, stream logs, print dashboard links)
│   └── smoke-test.sh                  (assert Report shape, ~2min)
└── docs/
    └── architecture.md                (pattern walkthrough, primitive selection)
```

## How to extend

The four-stage pipeline is set up to be extended in place — each
extension is a new stage, not a rewrite. See the "How to extend"
section of `docs/architecture.md` for recipes:

- Wire a real `claude-code` harness instead of the mock
- Recurse into low-confidence findings (the unused `depth` parameter)
- Deduplicate sub-questions with vector memory
- Add an adversarial reviewer (HUNT → PROVE)

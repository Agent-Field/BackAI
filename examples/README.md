# Examples

Six end-to-end examples that demonstrate the range of AF Stack. Each one
is self-contained: `docker compose up` from inside the directory boots
the stack with everything wired so you can see real data flow without
glueing things together yourself.

Three are shipped in v1. The remaining three are scaffolded with
working compose + config so you can run them; the workload modules they
depend on (multimodal-storage, change-stream-listener) ship as docs
under `docs/workload-modules.md` and are open issues on GitHub.

## Ready to run

### 03 — LLM gateway only

[`examples/03-llm-gateway-only/`](./03-llm-gateway-only/)

The smallest possible deploy: postgres + agentfield + runtime +
dashboard. Multi-tenancy off, no sandbox, no MinIO. One env var
(`OPENROUTER_API_KEY`) and `docker compose up` gets you an
OpenAI-compatible endpoint at `http://localhost:8080/api/v1/llm/...` with
per-call cost ledger and in-memory cache.

**Demonstrates:** the gateway, cost tracking, the dashboard's Cost tab.
**Time to first call:** 60 seconds.

### 01 — Notable (Notion-with-AI)

[`examples/01-notable/`](./01-notable/)

A multi-tenant note-taking SaaS. Three AF agents (summarize,
suggest_tags, todo_completer) wired through the LLM gateway. Memory
store remembers each user's tag preferences. Billing meter
(`notable_notes_created`) ticks on every write. PostgreSQL RLS enforces
tenant isolation at the database boundary. Custom dashboard plugin
(`apps/dashboard/plugins/notable/`) renders per-tenant stats.

**Demonstrates:** multi-tenancy + LLM gateway + memory + billing
metering + custom workload + dashboard plugins, all together.
**Time to first usable demo:** ~5 minutes (includes seeding 2 tenants
and 10 sample notes).

### 06 — Deep research

[`examples/06-deep-research/`](./06-deep-research/)

A long-running agent that decomposes a research question, fans out into
five parallel sub-investigations via `.harness()`, accumulates findings
in AF memory, then synthesises a structured `Report`. Falls back to a
mock `.ai()` harness when no Claude Code / Codex / Gemini binary is
installed so the example always runs.

**Demonstrates:** the composite-reasoning pattern from
`code/CLAUDE.md` (decompose → fan-out → accumulate → synthesise),
harnesses, memory accumulation, multi-stage cost shape, the dashboard's
Runs + Memory + Sandbox tabs all lighting up on the same operation.
**Time to first result:** ~2 minutes with default settings; cost capped
per-sub-question via `RESEARCHER_HARNESS_BUDGET_USD`.

## In progress

The compose files boot; the workload modules ship in a follow-up.

### 02 — Shipwright (SWE-AF SaaS)

`examples/02-shipwright/` — directory exists but the workload module
isn't shipped yet. Tracking in
[issue #82](https://github.com/Agent-Field/backai/issues) (when filed).

Will be: SWE-AF imported as a workload module + custom estimator and
classifier agents + e2b as the default sandbox + GitHub OAuth + a
`git-workload` module exposing branch/PR primitives to agents.

### 04 — Podcast creator

`examples/04-podcast-creator/` — needs the `multimodal-storage`
workload module. Will be: ffmpeg + Whisper + Vision pipelines in the
sandbox adapter, AF agents for chaptering and clipping, the storage
adapter holding large mp3/mp4 artefacts.

### 05 — Reactive enrichment

`examples/05-reactive-enrichment/` — needs the
`change-stream-listener` workload module. Will be: a PostgreSQL +
Mongo change stream subscriber that wakes an AF agent on every insert,
runs an enrichment, and writes the result back through a transaction.

## Picking an example

| You want to… | Start with |
|---|---|
| Try AF Stack as just an OpenAI-compatible gateway | **03 — LLM gateway only** |
| See what a production-shape multi-tenant SaaS looks like | **01 — Notable** |
| Understand the composite-reasoning pattern with real cost data | **06 — Deep research** |
| Build a coding-agent SaaS | wait for 02 / port from `code/labs/codeaf/` |
| Build a multimodal pipeline | wait for 04 / read `docs/workload-modules.md` |
| Build a change-stream-driven agent | wait for 05 / read `docs/workload-modules.md` |

## Adding your own example

The pattern, in short:

```
examples/<NN>-<slug>/
  README.md          — value prop + quickstart + what it demos
  docker-compose.yml — extends root services as needed
  config.yaml        — modules enabled, adapter choices
  agents/            — AF agents specific to this example
  migrations/        — example-specific SQL
  handlers/          — custom HTTP handlers (or a workload module)
  scripts/
    smoke-test.sh    — end-to-end assertion script
  .env.example       — minimal env to run
```

If the example needs domain-specific code that doesn't belong in
`agents/`, factor it into a workload module under `workload-modules/`
and import it via `config.yaml` (see `docs/workload-modules.md`).

## Smoke-testing

Each ready-to-run example has `scripts/smoke-test.sh`:

```bash
cd examples/03-llm-gateway-only && ./scripts/smoke-test.sh  # if present
cd examples/01-notable          && ./scripts/smoke-test.sh
cd examples/06-deep-research    && ./scripts/smoke-test.sh
```

Each one skips cleanly when its prerequisites are missing (no LLM key
set, dashboard unreachable, etc.) so you can wire them into CI without
flake.

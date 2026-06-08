# Examples

AF Stack ships one canonical starter template and four example tracks.
Ready examples are
self-contained: `docker compose up` from inside the directory boots the
stack with everything wired so you can see real data flow without
gluing things together yourself.

## Recommended starting point

### Starter

[`examples/starter/`](./starter/)

The bare-bones fork basis: one AgentField agent, one customer-app first
action page, one dashboard plugin, and one workload module with a route
plus migration. Copy these four surfaces into your fork after running
`af-stack init`.

**Use this when:** you are starting a new AF Stack product.

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

### 02 — Shipwright (SWE-AF SaaS)

[`examples/02-shipwright/`](./02-shipwright/)

Autonomous coding-agent factory scaffold. AF Stack stores task + patch
metadata, starts the AgentField `shipwright.build` reasoner, and links
the runtime task to the AgentField execution id. AgentField owns the
execution graph, live logs, harness calls, spans, traces, and memory.

The example is intentionally runnable without bundling Codex / Claude /
Gemini CLIs: if a harness binary is present, the agent routes through
`app.harness(provider=..., cwd=...)`; otherwise it falls back to an
`.ai()` patch sketch so the topology still works in fresh Docker builds.
When `GH_TOKEN` is configured, the harness path can push a branch and
open a draft GitHub PR; without it, Shipwright writes a durable patch
file into the `shipwright-patches` volume.

**Demonstrates:** AF Stack task metadata + AgentField async execution +
guarded coding harness flow + patch/optional PR callback.
**Still maturing:** production hardening and a deeper `git-workload`
module for branch / diff / PR primitives.

## Picking an example

| You want to…                                                   | Start with                                                    |
| -------------------------------------------------------------- | ------------------------------------------------------------- |
| Build your own product from the canonical fork basis           | **Starter**                                                   |
| Try AF Stack as just an OpenAI-compatible gateway              | **03 — LLM gateway only**                                     |
| See what a production-shape multi-tenant SaaS looks like       | **01 — Notable**                                              |
| Understand the composite-reasoning pattern with real cost data | **06 — Deep research**                                        |
| Build a coding-agent SaaS                                      | **02 — Shipwright**                                           |

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

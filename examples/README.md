# Examples

BackAI has one default first-run product, SupportDesk AI, in the root
stack. The examples are optional references for narrower or heavier
paths.

Each example includes a `capabilities.yaml` file that declares the
prerequisites it needs: provider key, auth, multi-tenancy, billing,
storage, sandbox, and GitHub access. Read that file first when deciding
whether an example belongs in your fork.

## Recommended starting point

### Starter

[`examples/starter/`](./starter/)

The bare-bones fork basis: one AgentField agent, one customer-app first
action page, one dashboard plugin, and one workload module with a route
plus migration. Copy these four surfaces into your fork after branding
the repo.

**Use this when:** SupportDesk AI is too domain-specific and you want a
neutral starting point.

## Ready to run

### 03 — LLM gateway only

[`examples/03-llm-gateway-only/`](./03-llm-gateway-only/)

The smallest possible deploy: postgres + agentfield + runtime +
dashboard. Multi-tenancy off, no sandbox, no MinIO. One provider key and
`docker compose up` gets you an OpenAI-compatible endpoint at
`http://localhost:8080/api/v1/llm/...` with per-call cost ledger and
in-memory cache.

**Demonstrates:** the gateway, cost tracking, the dashboard's Cost tab.
**Time to first call:** 60 seconds.
**Capability manifest:** [`capabilities.yaml`](./03-llm-gateway-only/capabilities.yaml)

### 01 — Notable (Notion-with-AI)

[`examples/01-notable/`](./01-notable/)

A multi-tenant note-taking SaaS. Three AgentField agents (summarize,
suggest_tags, todo_completer) wired through the LLM gateway. Memory
store remembers each user's tag preferences. Billing meter
(`notable_notes_created`) ticks on every write. PostgreSQL RLS enforces
tenant isolation at the database boundary. Custom dashboard plugin
(`apps/dashboard/plugins/notable/`) renders per-tenant stats.

**Demonstrates:** multi-tenancy + LLM gateway + memory + billing
metering + custom workload + dashboard plugins, all together.
**Time to first usable demo:** ~5 minutes (includes seeding 2 tenants
and 10 sample notes).
**Capability manifest:** [`capabilities.yaml`](./01-notable/capabilities.yaml)

### 06 — Deep research

[`examples/06-deep-research/`](./06-deep-research/)

A long-running agent that decomposes a research question, fans out into
five parallel sub-investigations via `.harness()`, accumulates findings
in AF memory, then synthesises a structured `Report`. Falls back to a
mock `.ai()` harness when no Claude Code / Codex / Gemini binary is
installed so the example always runs.

**Demonstrates:** decompose, fan-out, accumulate, synthesise, harness
fallbacks, memory accumulation, multi-stage cost shape, and the
dashboard's Runs + Memory + Cost surfaces lighting up on the same
operation.
**Time to first result:** ~2 minutes with default settings; cost capped
per-sub-question via `RESEARCHER_HARNESS_BUDGET_USD`.
**Capability manifest:** [`capabilities.yaml`](./06-deep-research/capabilities.yaml)

### 02 — Shipwright

[`examples/02-shipwright/`](./02-shipwright/)

Advanced coding-agent factory scaffold. BackAI stores task + patch
metadata, starts the AgentField reasoner, and links the runtime task to
the AgentField execution id. AgentField owns the execution graph, live
logs, harness calls, spans, traces, and memory.

The example is intentionally runnable without bundling Codex / Claude /
Gemini CLIs: if a harness binary is present, the agent routes through
`app.harness(provider=..., cwd=...)`; otherwise it falls back to an
`.ai()` patch sketch so the topology still works in fresh Docker builds.
When `GH_TOKEN` is configured, the harness path can push a branch and
open a draft GitHub PR; without it, Shipwright writes a durable patch
file into the `shipwright-patches` volume.

**Demonstrates:** BackAI task metadata + AgentField async execution +
guarded coding harness flow + patch/optional PR callback.
**Capability manifest:** [`capabilities.yaml`](./02-shipwright/capabilities.yaml)
**Important:** this is not the default product path. Use it when you
want the coding-agent shape specifically.

## Picking an example

| You want to…                                                   | Start with                                                    |
| -------------------------------------------------------------- | ------------------------------------------------------------- |
| Build your own product from the canonical fork basis           | **Starter**                                                   |
| Attach BackAI behind an existing app                           | **03 — LLM gateway only**                                     |
| See what a production-shape multi-tenant SaaS looks like       | **01 — Notable**                                              |
| Understand the composite-reasoning pattern with real cost data | **06 — Deep research**                                        |
| Build a coding-agent SaaS                                      | **02 — Shipwright**                                           |

## Adding your own example

The pattern, in short:

```
examples/<NN>-<slug>/
  README.md          — value prop + quickstart + what it demos
  capabilities.yaml  — honest prerequisites + what the example demonstrates
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

Capability manifests use this shape:

```yaml
id: my-example
name: My Example
profile: production-shaped-saas-example
requires:
  provider_key: recommended
  auth: true
  multi_tenancy: true
  billing: optional
  storage: false
  sandbox: false
  github: false
demonstrates:
  - llm_gateway
  - cost_events
deploy:
  compose: true
  railway: not_packaged
```

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

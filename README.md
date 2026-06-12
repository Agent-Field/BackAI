<div align="center">

# BackAI

### The open-source AI app template with the backend already wired.

_Start with SupportDesk AI, then replace the app with your own product._

[![Status: planning](https://img.shields.io/badge/status-pre--alpha-orange)](#)
[![License: Apache 2.0](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)
[![AI substrate: AgentField](https://img.shields.io/badge/AI%20substrate-AgentField-0A66C2)](https://github.com/Agent-Field/agentfield)

</div>

> **Working name**. The brand is configured in [`brand.yaml`](brand.yaml).
> The legacy uppercase `BRAND.yaml` path has been retired.

## What this is

BackAI is a fork-friendly AI app template. You clone one repo and get a
customer app, admin dashboard, API runtime, auth, tenants, API keys, LLM
gateway, cost tracking, billing stubs, storage, jobs, agents, and deploy
targets.

The default app is **SupportDesk AI**: a support workflow that lets a
customer sign up, draft a support reply, and then inspect the exact usage
and cost in admin. Replace that customer app with your own product when
you fork.

The category is the AI backend: the substrate behind AI SaaS apps where
model calls, cost, tenant isolation, jobs, storage, billing, and agent
execution all live together.

## Canonical DX

The default path is **fork and edit**:

```bash
git clone https://github.com/Agent-Field/backai supportdesk-ai
cd supportdesk-ai
# Optional once you replace the default app:
# af-stack init --name "DocuChat" --color "#0A66C2" --logo ./logo.png
docker compose up
```

Your product code lives inside the fork. You brand it, add agents,
customize the customer app, add workload modules, add dashboard plugins,
and deploy the whole thing as one unit.

API-only consumption is supported for mobile apps and existing products,
but it is the secondary path. The primary experience is closer to
Cal.com or Plane than to a hosted BaaS: the repo is the product.

## What You Edit

| Surface | Path | What belongs there |
| --- | --- | --- |
| Customer product | `apps/customer-app/` | Your end-user UI, flows, pages, and brand-specific app logic. |
| Agents | `apps/backend/agents/<name>/` | Python AgentField agents, reasoners, MCP config, and harness setup. |
| Workload modules | `workload-modules/<id>/` | Domain backend routes, migrations, jobs, and crons. |
| Dashboard plugins | `apps/dashboard/plugins/<id>/` | Operator-console tabs for your domain metrics and controls. |

Start with the bundled SupportDesk AI customer app when you want a
polished product-shaped baseline. Use [`examples/starter/`](examples/starter/)
when you want the smallest neutral fork: one agent, one customer-app
flow, one workload module, and one dashboard plugin.

## Pre-Wired vs Configurable

| Area | Pre-wired default | Configurable by you |
| --- | --- | --- |
| Data | Postgres 16 + pgvector | External Postgres, RLS policy shape, workload tables. |
| Storage | MinIO in dev, S3 contract in prod | S3, R2, GCS, Azure Blob via adapter/env. |
| Identity | better-auth, first-operator bootstrap | OAuth providers, trusted origins, operator creation CLI. |
| LLM routing | OpenAI-compatible gateway + LiteLLM sidecar | Provider keys, model map, budgets, virtual-key strategy. |
| Sandboxes | Docker in dev, e2b/gVisor/Firecracker options | Adapter choice, limits, provider credentials. |
| Delivery | Svix for outbound webhooks, log notifications | Resend/Postmark/etc. notifications, billing adapter. |
| Deploy | Docker Compose, Helm, Fly, Railway, Render | Your domains, secrets, scaling, managed services. |

For the layered architecture and OSS placement, read
[`STACK.md`](STACK.md). For the product strategy, consumption model, and
execution checklist, read [`POSITIONING.md`](POSITIONING.md).

## Why This Exists

Building an AI-native product today means assembling 10+ services: auth,
db, storage, queue, gateway, agent runtime, sandboxes, webhooks, billing,
observability. Each integration costs weeks. Each vendor adds lock-in.

Supabase-shaped backends don't include AI primitives. AI platforms don't
include backend primitives. Builders rebuild the same plumbing for every
project.

BackAI ships both halves. The app template gives you the product
surface. The backend gives you the operational substrate for AI calls,
agents, costs, tenants, jobs, storage, and billing.

## The invariant

**Every app-level model call goes through the BackAI gateway.** No
bypass. The OpenAI-compatible endpoint at `/api/v1/llm/*` preserves
tenant identity, cost, policy, and audit metadata before routing to the
configured provider layer.

## SDK Boundary

> **`app.*` defines agents. `suite.*` calls them and runs everything else.**

| SDK                      | Use inside                                                |
| ------------------------ | --------------------------------------------------------- |
| **AgentField** (`app.*`) | Agent processes                                           |
| **Suite** (`suite.*`)    | App handlers, jobs, dashboard — anywhere outside an agent |

Plus a REST + OpenAPI surface so any language works.

## The operator console

<div align="center">

<img src="dashboard-screenshots/home.png" alt="AF Stack Home — KPI strip, recent runs, cost" width="900" />

<sub>Home: requests/min · error rate · cost today · queue depth · live runs</sub>

<img src="dashboard-screenshots/runs.png" alt="AF Stack Runs — execution list with link-out trace" width="900" />

<sub>Operate → Runs: filter by agent / tenant / status, link out to full trace</sub>

<img src="dashboard-screenshots/cost.png" alt="AF Stack Cost dashboard" width="900" />

<sub>Operate → Cost: spend by model · agent · tenant · day, with budgets and forecast</sub>

<img src="dashboard-screenshots/customers-tenants.png" alt="AF Stack Customers — tenant list with detail drawer" width="900" />
<sub>Customers → Tenants: per-customer drilldown with usage, members, audit</sub>

<img src="dashboard-screenshots/customers-api-keys.png" alt="AF Stack Customers — API key issuance" width="900" />
<sub>Customers → API Keys: issue / rotate / revoke with one-time-reveal</sub>

</div>

## Quickstart (under 60 seconds)

```bash
git clone https://github.com/Agent-Field/backai supportdesk-ai
cd supportdesk-ai
cp .env.example .env
# Optional: set OPENROUTER_API_KEY for live model calls.
docker compose up
```

Open the customer app first:

- Customer app: `http://localhost:34000`
- Admin dashboard: `http://localhost:33000`
- API runtime: `http://localhost:8080/api/v1/`
- Health + metrics: `http://localhost:8080/health` · `/ready` · `/metrics`
- AgentField control plane: `http://localhost:8081/`
- MinIO console: `http://localhost:9001/`

Sign up in SupportDesk AI. BackAI provisions a tenant, membership,
billing record, and API key. Then use Support Desk to draft a reply and
open the admin dashboard to inspect usage and cost.

You can also call the LLM gateway directly with the official OpenAI SDK
by changing only the base URL:

```js
import OpenAI from "openai"

const client = new OpenAI({
  baseURL: "http://localhost:8080/api/v1/llm",
  apiKey: process.env.BACKAI_API_KEY,
})

const completion = await client.chat.completions.create({
  model: "qwen/qwen-2.5-72b-instruct",
  messages: [{ role: "user", content: "Draft a support reply." }],
})
```

For deeper platform integration, use the Suite SDK for agents, memory,
jobs, costs, tenants, and admin APIs.

## Advanced: sample agent call

The repo still includes a bundled sample AgentField agent behind the
gateway:

```bash
curl -X POST http://localhost:8080/api/v1/agents/sample.echo \
  -H "Content-Type: application/json" \
  -d '{"input":{"payload":{"message":"hello world"}}}'
```

## Phase 7 — LLM Gateway

Every LLM call in the suite goes through the gateway at `/api/v1/llm/*`.
The wire shape is OpenAI-compatible, so any OpenAI-shaped client works
by changing one line: the base URL.

<div align="center">
<img src="dashboard-screenshots/cost-live.png" alt="AF Stack Cost dashboard with live LLM traffic" width="900" />
<sub>Operate → Cost: live cost events, model mix, per-tenant spend, budget meters</sub>
</div>

**One-line OpenAI SDK config** (works with the official `openai` package
on every language):

```js
import OpenAI from "openai"

const client = new OpenAI({
  baseURL: "http://localhost:8080/api/v1/llm",
  apiKey: process.env.AF_STACK_TENANT_KEY,
})

const completion = await client.chat.completions.create({
  model: "qwen/qwen-2.5-72b-instruct",
  messages: [{ role: "user", content: "Hello!" }],
})
```

**Or use the Suite SDK** — same gateway, ergonomic helpers, typed
responses, plus access to the cost log and budgets:

```python
from af_stack import suite

# Chat — non-streaming
response = await suite.llm.chat(
    model="qwen/qwen-2.5-72b-instruct",
    messages=[{"role": "user", "content": "Hello!"}],
)
print(response["choices"][0]["message"]["content"])

# Chat — streaming
async for chunk in await suite.llm.chat(
    model="qwen/qwen-2.5-72b-instruct",
    messages=[{"role": "user", "content": "Tell me a story."}],
    stream=True,
):
    delta = chunk["choices"][0]["delta"].get("content", "")
    print(delta, end="", flush=True)

# Cost log + budgets
events = await suite.cost.events(tenant="acme", limit=20)
await suite.admin.budgets.set({
    "tenant_id": "acme",
    "monthly_usd": 500,
    "alert_threshold_pct": 80,
})
```

Every call is recorded with tenant, agent, model, tokens, cost, and
cache-hit flag. Budgets are per-tenant; when a tenant exceeds its
monthly cap, subsequent calls fail with `HTTP 402 BUDGET_EXCEEDED`.
Gateway guardrails are on by default: regex PII redaction runs before
and after provider calls, and optional moderation regexes can block
requests or responses. See [`docs/guardrails.md`](docs/guardrails.md)
for Presidio sidecar configuration.

End-to-end tests:

```bash
# Real openai npm package against a live runtime
node scripts/test-openai-sdk.mjs

# Budget enforcement (creates tiny budget, verifies 402 on overrun)
./scripts/test-budget-enforcement.sh
```

## Phase 8 — Database studio + memory

Every AF Stack deployment ships a full Postgres browser in the operator
dashboard. Inspect tables, view row-level security policies, run
read-only SQL, and manage the per-scope memory store — all from the same
console. The `Build → Database` tab covers the four operator workflows
that previously required `psql`: browsing data, reading schema +
indexes, auditing RLS, and ad-hoc queries.

Per-scope KV with vector search out of the box — store agent context
across runs, search semantically.

<div align="center">
<img src="dashboard-screenshots/database.png" alt="AF Stack Database studio — table browser + SQL runner + RLS policies + memory" width="900" />
<sub>Build → Database: tables sidebar, row browser, structure / policies / SQL / memory tabs</sub>
</div>

End-to-end tests:

```bash
# DB studio API: tables, table detail, SQL runner read-only guard
./scripts/test-db-studio.sh

# Memory API: put / get / search / rerank / delete
./scripts/test-memory.sh
```

## Phase 9 — Sandboxes

Every AF Stack deployment ships a managed code-execution sandbox so
agents and jobs can run arbitrary commands, build artifacts, or test
generated code without the operator wiring docker into their app code.
Four pluggable adapters (`docker` for local dev, `firecracker` for
single-host isolation, `e2b` and `modal` for managed remote sandboxes)
share one API; each tenant gets its own pool with isolated filesystems,
egress controls, and timeout/CPU/memory caps. Every run is cost-tracked
per-tenant alongside LLM spend so a tenant's monthly budget covers both
inference and compute.

<div align="center">
<img src="dashboard-screenshots/sandbox-activity.png" alt="AF Stack Sandbox Activity — recent runs, pool stats, cost today" width="900" />
<sub>Operate → Sandbox Activity: recent runs · adapter pool (warm / active / queued) · CPU-seconds and cost today</sub>
</div>

**Python (Suite SDK):**

```python
from af_stack import suite

result = await suite.sandbox.run(
    image="python:3.12-slim",
    command=["python", "-c", "print(2 + 2)"],
    timeout_s=30,
)
print(result.exit_code, result.duration_s, result.cost_usd)
```

**TypeScript (Suite SDK):**

```ts
import { suite } from "@af-stack/sdk"

const result = await suite.sandbox.run({
  image: "node:20-alpine",
  command: ["node", "-e", "console.log(2 + 2)"],
  timeout_s: 30,
})
console.log(result.exit_code, result.duration_s, result.cost_usd)
```

End-to-end test:

```bash
# Sandbox API: happy path, pool stats, run list, failing exit code, timeout
./scripts/test-sandbox.sh
```

### Make it your own

Replace the sample agent with your own at `apps/backend/agents/<name>/` —
each subfolder is its own container that registers with AgentField on
startup. Edit `apps/backend/config.yaml` to enable / disable suite
modules. Customize as you like; everything in this repo is yours after
the fork.

## Status

v1 feature-complete. See [`STRATEGY.md`](STRATEGY.md) for the v1.1 plan:
LiteLLM virtual keys and the Stripe/Lago billing adapter have landed.
Shipwright now has the first task metadata API / SDK / AgentField example
slice in-tree, including durable patch capture and optional draft GitHub
PR creation when `GH_TOKEN` is configured. Remaining Tier 1 work is the
production hardening path. AgentField run data is surfaced via inline
summary/actions plus link-out to AgentField, and the general approvals
primitive has landed.

For the full layered stack diagram, see [`STACK.md`](STACK.md).

## Documentation

Architecture and product docs live in this repo:

- [`STACK.md`](STACK.md) — Layered architecture (Supabase-shaped, 8 bands)
- [`STRATEGY.md`](STRATEGY.md) — What's shipping next
- [`PRODUCT.md`](PRODUCT.md) — What it is, what it isn't, the DX
- [`ARCHITECTURE.md`](ARCHITECTURE.md) — Extension points + adapter contracts
- [`OSS-AUDIT.md`](OSS-AUDIT.md) — Every OSS we vendor + rationale
- [`NAVBAR.md`](NAVBAR.md) — Operator-console inventory
- [`docs/realtime.md`](docs/realtime.md) — Postgres NOTIFY → WebSocket bridge
- [`docs/search.md`](docs/search.md) — Postgres FTS + pgvector app-data search
- [`docs/activity.md`](docs/activity.md) — tenant-scoped customer activity log
- [`docs/feature-flags.md`](docs/feature-flags.md) — runtime feature flags
- [`docs/storage-transforms.md`](docs/storage-transforms.md) — resize and thumbnail images on storage GETs
- [`docs/embeddings.md`](docs/embeddings.md) — OpenAI-compatible embeddings through LiteLLM
- [`docs/multimodal.md`](docs/multimodal.md) — image generation, speech, and transcription
- [`docs/run-subscriptions.md`](docs/run-subscriptions.md) — live AgentField run events over WebSocket
- [`docs/tool-adapters.md`](docs/tool-adapters.md) — built-in browser-use, SearXNG, fs, exec, HTTP, and SQL adapters
- [`docs/oauth.md`](docs/oauth.md) — OAuth grants for backend agents acting as a user
- [`docs/`](docs/) — Per-area guides
- [`docs/archive/`](docs/archive/) — Historical Phase 0-16 planning

## Built on AgentField

AgentField is the agent runtime at the core of AF Stack. AgentField
provides agents, the LLM gateway, memory, traces, MCP integration via
harnesses, verifiable credentials, and cryptographic identity. AF Stack
adds the production wrapping around it.

[AgentField repo →](https://github.com/Agent-Field/agentfield)

## License

Apache 2.0. See [`LICENSE`](LICENSE).

## Contributing

Read [`CONTRIBUTING.md`](CONTRIBUTING.md). Issues and PRs welcome.

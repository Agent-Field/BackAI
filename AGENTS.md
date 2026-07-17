# AGENTS.md — orientation for AI agents working in BackAI

You are working in **BackAI**, an open-source, fork-friendly **AI-backend
template**. One clone gives a product a customer app, an operator
dashboard, a Go API runtime, auth, multi-tenancy, API keys, an
OpenAI-compatible LLM gateway with cost tracking + budgets, storage, jobs,
crons, webhooks, a secrets vault, billing, and AgentField-backed agents —
all behind one base URL. Brand: **BackAI**. CLI: **`af-stack`**. Default
app: **SupportDesk AI**. The repo **is** the product (Cal.com / Plane
style), so most work is editing *this checkout*, not calling a hosted API.

> This file is the front door. It routes you into the deeper guides; it
> does not restate them. When you need depth, follow the pointers in
> [Deep guidance](#deep-guidance) — especially
> [`skills/af-stack/SKILL.md`](skills/af-stack/SKILL.md).

## First 60 seconds

```bash
git clone https://github.com/Agent-Field/backai supportdesk-ai
cd supportdesk-ai
cp .env.example .env
# Optional: set OPENROUTER_API_KEY in .env for live model calls (demo mode
# works with no key).
af-stack dev            # preflight (auto-allocates conflict-free ports) + docker compose up
```

`af-stack dev` boots the whole stack and prints a "what runs where" map.
The default local URLs:

| Surface | URL | Notes |
| --- | --- | --- |
| Customer app | `http://localhost:34000` | The end-user product (SupportDesk AI) |
| Operator dashboard | `http://localhost:33000` | Sign in `operator@af-stack.local` / `changeme123` |
| API runtime | `http://localhost:8080/api/v1/` | One base URL for everything |
| Health / ready / metrics | `http://localhost:8080/{health,ready,metrics}` | Liveness + Prometheus |
| AgentField control plane | `http://localhost:8081/` | Agent registry + traces |
| MinIO console | `http://localhost:9001/` | Dev object storage |

Prove the wiring without any key (the default `supportdesk` agent ships a
no-key `echo` reasoner for exactly this — the heavier `sample` agent lives
behind the `advanced` compose profile):

```bash
curl -X POST http://localhost:8080/api/v1/agents/supportdesk.echo \
  -H "Content-Type: application/json" \
  -d '{"input":{"payload":{"message":"hello world"}}}'
```

## Personal vs SaaS mode

Running BackAI just for yourself? Turn **auth and billing off** so the app
boots straight into the product — no login, no paywall:

```bash
af-stack mode personal   # or set AF_STACK_MODE=personal in .env
docker compose up -d      # restart to apply
af-stack mode saas        # switch back (auth + billing on)
af-stack mode             # print the current mode + where it's set
```

Default is `saas`. Details: [`docs/CONFIGURATION.md`](docs/CONFIGURATION.md).

## What you edit — the four surfaces

Almost everything you build lands in one of these. **Every other directory
is platform code you do not edit.**

| Surface | Path | Language | What belongs there |
| --- | --- | --- | --- |
| Customer app | `apps/customer-app/` | TS / React | End-user UI, flows, pages, brand-specific app logic |
| Agents | `apps/backend/agents/<name>/` | Python | AgentField agents, reasoners, harness setup, MCP registration |
| Workload modules | `workload-modules/<id>/` | Python / Go | Domain backend routes, migrations, jobs, crons |
| Dashboard plugins | `apps/dashboard/plugins/<id>/` | TS / React | Operator-console tabs for your domain metrics + controls |

Scaffold each with the CLI: `af-stack agent new <name>`,
`af-stack module new <id>`, `af-stack plugin new <id>`. Ownership of the
rest of the tree (`services/` = platform runtime, `packages/` = shared
SDKs, `deploy/` = deploy targets) is in
[`docs/repo-map.md`](docs/repo-map.md).

## Invariants — do not break these

- **Every app-level model call goes through the gateway** at
  `/api/v1/llm/*` (OpenAI-compatible). No direct provider calls — the
  gateway is what preserves tenant identity, cost, budget, and audit.
- **Multi-tenancy is automatic.** Tenant identity comes from the API key /
  session, enforced by Postgres RLS. Never read `tenant_id` from a query
  param or trust a client-supplied tenant.
- **Don't reinvent platform primitives.** Auth, tenancy, billing, storage,
  jobs, secrets, and webhooks already exist — call them, don't rebuild
  them. The full primitives table is in the Skill.
- **Adapters swap via env var**, not code forks (storage, sandbox,
  notifications, billing, secrets).
- **Keep real credentials in `.env`** (gitignored). Never hardcode secrets;
  use the secrets vault (`secret:<name>` references) instead.

The 10 critical rules live in
[`skills/af-stack/SKILL.md`](skills/af-stack/SKILL.md).

## The CLI is the front door

`af-stack` is a thin, scriptable wrapper over the runtime REST API — good
for agents. Configure once with env (`AF_STACK_URL`, `AF_STACK_API_KEY`)
and drive everything.

- **Scaffold / lifecycle** (no key): `init`, `dev`, `mode`, `upgrade`
  (`--check` for a dry run), `agent|module|plugin new`, `adapter list`,
  `deploy <helm|fly|railway|render>`.
- **Billing** (agent-first): `af-stack billing plan set --id pro --name Pro
  --price 29 --budget 25 --entitlement seats=5 --default` auto-provisions
  the Stripe Product + Price — no dashboard, no copy-pasted price IDs. See
  [`docs/billing.md`](docs/billing.md).
- **Operator surface** (needs an operator key — mint one with `af-stack
  operator key`): `keys`, `agents`, `reasoners`, `runs`, `logs`, `errors`,
  `audit`, `sessions`, `tenants`, `activity`. Reference:
  [`docs/cli-admin.md`](docs/cli-admin.md).
- **MCP**: `af-stack mcp list|add|remove|call` manages MCP servers
  registered with the runtime; `mcp call` takes/emits JSON.

Errors are structured: every failure carries a stable `code`, a `message`,
and a `request_id` (e.g. `[BUDGET_EXCEEDED] ...`). The machine-readable API
truth is `GET /api/v1/openapi.json`.

## Build & test gates (run before you push)

```bash
make dev          # boots docker compose + runtime
make test         # go + ts + py
```

- **Go**: `gofmt`, `goimports`, `golangci-lint run` (`.golangci.yml`).
- **TypeScript**: `prettier`, `eslint`.
- **Python**: `ruff format`, `ruff check` (`pyproject.toml`).
- **DCO sign-off is required** on every commit: `git commit -s`.
- New feature → tests in the same PR; don't break the 60-second quickstart.

## Deep guidance

Read these when the front door isn't enough:

- [`skills/af-stack/SKILL.md`](skills/af-stack/SKILL.md) — the primitives
  table, boundaries, and canonical build workflow. **Start here for depth.**
- [`skills/af-stack/rules/*.md`](skills/af-stack/rules/) — deep dives:
  `agents.md`, `boundaries.md`, `adapters.md`, `customer-app.md`,
  `dashboard-plugins.md`, `workload-modules.md`, `multi-tenancy.md`,
  `primitives.md`, `sdk.md`, `deploy.md`.
- [`docs/repo-map.md`](docs/repo-map.md) — who owns each directory.
- [`llms.txt`](llms.txt) — a curated index of the docs for LLMs.
- [`docs/stack.md`](docs/stack.md) — the 8-band layered architecture.

Nested `AGENTS.md` files refine these rules for a subtree — e.g.
[`apps/dashboard/AGENTS.md`](apps/dashboard/AGENTS.md) adds a Next.js
guardrail. When one exists in a directory you're editing, it wins for that
directory.

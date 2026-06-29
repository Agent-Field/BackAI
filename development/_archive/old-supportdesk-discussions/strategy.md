# AF Stack Strategy

AF Stack is the complete self-hostable backend for the AI era. The shape is
Supabase / Firebase, but AI is a native compute primitive instead of an
integration bolted on later. A developer should be able to fork this repo,
set one provider key, run `docker compose up`, and get most of the backend
needed for an AI product: auth, tenancy, API keys, billing, jobs, crons,
storage, sandboxes, webhooks, dashboards, deploy targets, and AgentField
agents.

Strategically, AF Stack exists to drive AgentField adoption. The front-door
message is not "look at our agent runtime." The front-door message is
"ship the backend and AI infrastructure without assembling ten vendors."
AgentField becomes the intelligence layer inside that complete backend.

For the layered stack diagram and OSS placement, see [`docs/stack.md`](../docs/stack.md).

## Ownership Boundary

| Layer | Owns | Does not own |
|---|---|---|
| **AgentField** | AI-stateful primitives: memory scopes, session context, actor state, run/workflow state, agent registration, reasoner lifecycle, harness invocation, MCP host, skills, tool calls, spans, traces | Tenants, billing, app auth, dashboards, public REST gateway |
| **AF Stack runtime** | SaaS/backend platform around AgentField: tenants, users, memberships, API keys, secrets, public LLM gateway, cost ledger, sandboxes, webhooks, jobs, crons, notifications, storage, billing, audit, dashboards | Memory, sessions, agent state, traces |
| **LiteLLM** | LLM provider routing, virtual keys, per-key budgets, rate limits, spend analytics | Non-LLM backend primitives |
| **Svix** | Outbound webhook delivery, retries, signing, replay protection | Inbound `forward_to` semantics |

The rule is simple: anything AI-stateful belongs to AgentField. Anything
LLM-routing belongs to LiteLLM. Anything else belongs to AF Stack or to an
optional workload module.

## Current State

These surfaces are implemented in the repo today:

- Go runtime with public REST gateway, OpenAPI, auth middleware, tenant
  resolver, audit log, secrets, storage, jobs, crons, webhooks, billing,
  notifications, cost, LLM gateway, sandbox, MCP, skills, harness probes,
  DB studio, and metrics.
- AgentField control plane runs as a container and owns agent execution.
- LiteLLM runs as the LLM routing sidecar.
- Svix runs as the outbound webhook sidecar.
- Postgres + pgvector, MinIO/S3, Redis-for-Svix, Caddy, Helm, Fly, Railway,
  Render, and Docker Compose are wired.
- Operator dashboard, customer app, docs site, Python SDK, TypeScript SDK,
  and partial Go SDK exist.
- Live examples: Notable, LLM Gateway Only, Deep Research, and the first
  Shipwright slice (task metadata API + AgentField-backed coding-agent
  scaffold with durable patch capture and optional draft GitHub PRs).

## What We Do Not Add To Core

These names should not appear as new AF Stack core primitives:

- `suite_sessions`, `suite_threads`, or `suite_chat_messages`
- A second vector store or trace/span store
- Prompt management or eval framework
- LangFuse / Helicone / Portkey as core dependencies
- Conversation, document, or RAG primitives in the runtime core

If a product needs those shapes, compose them through AgentField or ship
them as an optional workload module.

## Cleanup Decisions

The Phase 0-16 planning docs are historical and live under
[`docs/archive/`](../docs/archive/). Current root docs should stay focused:

- [`README.md`](../README.md) — entry point and quickstart
- [`docs/stack.md`](../docs/stack.md) — layered architecture and OSS stack
- [`docs/product.md`](../docs/product.md) — product pitch and DX
- [`docs/architecture.md`](../docs/architecture.md) — extension points and adapter contracts
- [`docs/oss-audit.md`](../docs/oss-audit.md) — OSS choices and remaining swaps
- [`development/operator-console-inventory.md`](operator-console-inventory.md) — operator-console inventory
- `development/strategy.md` — this file

The cleanup pass deliberately does not rewrite `services/runtime/internal/memory`
or `services/runtime/internal/llmcache`. Those are real refactors and need
separate audits. (`services/runtime/internal/ratelimit` was removed by item
#32 — rate limiting is now enforced upstream by LiteLLM via per-virtual-key
`rpm_limit` / `tpm_limit` from item #22; the LLM handler proxies LiteLLM's
429 + Retry-After + X-RateLimit-\* headers back to the client unchanged.)

## Execution Plan — Four Phases

The full task graph (with checkboxes, sub-tasks, file paths, and
acceptance criteria) lives in [`development/positioning.md`](positioning.md) Part 4.
**That is the canonical execution doc.** This section gives the
phase-level summary; the executing agent works from development/positioning.md.

Total scope: ~25 weeks from Phase 16 to canonical Supabase-shaped AI
backend.

### Phase 0 — Cleanup (done ✅)

Archive Phase 0-16 planning artifacts; merge `PLAN-NEXT.md` +
`PLAN-CLEAN.md` into this doc; remove empty stub directories; collapse
redundant dashboard tabs; create `docs/stack.md` and `development/positioning.md`.

### Phase 1 — DX polish (~8 weeks)

Make the canonical "fork → brand → deploy your AI SaaS" path
bulletproof. Without this, the Supabase-shaped + Cal.com-forkable
positioning in development/positioning.md Part 1 is aspirational.

Highlights:
- Sidebar IA refactor → 4-group split (Build / Operate / Customers /
  Infrastructure)
- `brand.yaml` as single brand-configuration surface
- `af-stack init` + `af-stack dev` + CLI v2 (the developer's primary
  surface in their fork)
- `examples/starter/` — bare-bones canonical fork basis
- Deploy-target CI verification (Helm / Fly / Railway / Render /
  compose)
- First-run onboarding, auth bootstrap, README rewrite

See development/positioning.md Part 4 §1a-1m for the 13 work-streams with
checkboxes.

### Phase 2 — Completeness features (~8-10 weeks)

The 11 features that take the platform from "Phase 16 launch-ready" to
"any AI app builds on this without forking."

**General-backend parity** (~5 weeks): Realtime · Search API · User
activity log · Feature flags wired · File transforms.

**AI-specific completeness** (~5 weeks): Embeddings API (✅ shipped —
available today as `suite.llm.embed()`) · Multimodal API · Realtime run
subscriptions · Agent tool adapters (✅ shipped — strict-interface
adapter set in `services/runtime/internal/tools/`; MCP-callable via
`app.mcp.call("native:<tool>", ...)`; per-tenant enable in dashboard) ·
PII redaction + moderation · OAuth-on-behalf-of-user.

See development/positioning.md Part 4 §2 for the 11 items with checkboxes.

### Phase 3 — Product Tier 1 (~5 weeks)

The differentiator features that turn the platform into a sellable
product story.

1. **LiteLLM Virtual Keys** — DONE ✅ (item #22). Budget + rate-limit
   enforcement now lives in LiteLLM. `IssueAPIKey` mints a matching
   LiteLLM virtual key per `suite_api_keys` row; the LiteLLM secret
   sits encrypted in the vault under `litellm/key/{api_key_id}`. The
   LLM gateway uses the per-tenant key at request time so LiteLLM
   attributes spend and enforces caps upstream. Dashboard reads from
   `/spend/keys`; `suite_cost_events` is now a write-through audit
   table. The in-memory rate limiter has been retired (item #32):
   `services/runtime/internal/ratelimit` is deleted, and the LLM
   handler surfaces LiteLLM's 429 with `Retry-After` plus the
   `X-RateLimit-Limit / Remaining / Reset` trio proxied through to
   the client.

2. **Billing Adapter** — DONE. Stripe and Lago now sit behind one
   billing adapter interface. `AF_STACK_BILLING_ADAPTER=stripe|lago|none`.
   Dashboard shows the active adapter, and portal / customer flows work
   against either provider.

3. **Shipwright** — DONE. Autonomous AI agent factory. Customer submits a
   task or GitHub issue; runtime enqueues; AgentField agent runs in a
   sandbox with Claude Code / Codex / Gemini; customer watches live
   logs + AgentField step data; final output is PR / patch / deployable
   change. The first slice ships the task metadata API, SDK bindings,
   and runnable AgentField example. The example now clones the repo, runs
   `app.harness(..., cwd=repo)` when a harness CLI is installed, captures
   `git diff --binary` into a durable patch volume, and opens a draft
   GitHub PR when `GH_TOKEN` is configured. The local Compose example
   has been live-smoked through AF Stack → AgentField → Codex harness →
   durable patch callback. Deeper git workload primitives remain the
   hardening path. Minimal AF Stack tables:
   ```sql
   suite_shipwright_tasks    (id, tenant_id, user_id, title,
                              description, repo_url, status, run_id,
                              created_at)
   suite_shipwright_patches  (task_id, ref, summary, diff_url,
                              created_at)
   ```
   Everything else (agent memory, step log, tool calls, spans) lives
   in AgentField.

4. **AgentField Data in Dashboard** — ✅ shipped via link-out + inline
   summary. Per docs/architecture.md's "don't rebuild what's already
   excellent" rule, AgentField's own UI at `:8081` owns the DAG /
   step inspector deep view; the af-stack dashboard inlines a summary
   card (status, agent, timing, cost, approval) and ships control
   actions (cancel / pause / resume / request approval) that proxy to
   AgentField. Memory tab stays on `suite_memory` (see #31 audit —
   that's the canonical store). See `docs/agentfield-integration.md`.

5. **Approvals Primitive** — DONE. General primitive for any flow, not
   agent-specific. AF Stack owns tenant-scoped business approval rows;
   AgentField run approvals remain in AgentField. Schema:
   ```sql
   suite_approvals (id, tenant_id, requested_by, kind, payload,
                    status, decided_by, decided_at, created_at)
   ```
   Uses: destructive job review, high-budget LLM calls, content
   moderation, billing overrides.

See development/positioning.md Part 4 §3 for the 5 items with checkboxes.

### Phase 4 — Enterprise (~4 weeks)

- **SSO/SAML** — Authentik (self-host) or WorkOS (managed)
- **RBAC** — Casbin or Oso layered over PG RLS
- **BYOK secrets** — cloud KMS adapters (AWS / GCP / Azure)
- **GDPR** — data export + erase endpoints

See development/positioning.md Part 4 §4 for the 4 items with checkboxes.

## Working Rules

1. **Phases run sequentially**: Phase 1 (DX) finishes before Phase 2
   (features) starts. Phase 3 (Tier 1 product) can overlap with Phase 2
   tail if capacity allows.
2. **Within a phase, items are mostly independent** — multiple agents
   can parallelize.
3. **Tick boxes in development/positioning.md Part 4** as items land. Don't track
   progress here in development/strategy.md; this doc is for *intent*, POSITIONING
   is for *execution*.
4. **Respect the AgentField boundary** (see "Ownership Boundary"
   above). Anything AI-stateful (memory, sessions, runs, spans, traces)
   belongs to AgentField. Never duplicate.
5. **Each item ships independently.** Each either uses AgentField for
   AI state or stays out of AI state. No item is a "big bang" release.

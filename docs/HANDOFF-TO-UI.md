# Backend Handoff — Ready for UI

> Single entry point for the next agent. Don't read other docs in
> isolation — they're all referenced from here.

## Read these in order

1. `docs/ARCHITECTURE.md` — system overview, 8 bands, adapter system, lifecycle diagrams.
2. `development/ui-plan-v1.md` — the operator-console product spec (authoritative for every page).
3. `development/admin-design-patterns-v1.md` — the implementation contract (shell, grid, page archetypes).
4. `development/admin-api-gap-registry-v1.md` — per-page UI contract status.
5. `development/backend-admin-contract-audit-v1.md` — **the to-do list**: backend gaps, compose changes, frontend wiring, ordered roadmap.
6. `docs/adapters/PROTOCOL.md` + `docs/adapters/protocols/<slot>-v1.md` — adapter contracts (8 slots shipped; 4 more queued in roadmap).

## What's done (shipped on this branch)

| Area | State |
|---|---|
| Adapter system | **8 Tier-1 slots** (sandbox, storage, notifications, secrets, billing, multimodal, llm-chat, auth) + shared remote HTTP client + capability registry + conformance harness + reference Python adapter. |
| Tests | 61 packages pass. 0 failures. 2 E2E tests (sandbox via Python adapter; LLM via real OpenRouter Kimi via OpenAI-compat proxy). |
| Dashboard shell | 47 routes registered with central navigation + catch-all renderer + seeded-fallback data loader pulling 44 live runtime endpoints. |
| Docs | ARCHITECTURE, PROTOCOL, AUTHORING, CONFORMANCE, per-slot specs, dashboard spec, design patterns, gap registry, contract audit. |

## What's NOT done (the roadmap)

All outstanding work is consolidated in `development/backend-admin-contract-audit-v1.md` §6 (Roadmap). Summary:

| Order | Block | Effort |
|---|---|---|
| 1 | Quick wins — no new OSS (adapter-registry mount, /admin/services synth, /admin/db/health, provider-health poller, cron trigger, cache flush, key rotate, brand R/W, SQL Health tab) | ~2 days |
| 2 | **`logs` adapter slot** — Loki + Vector via observability profile | ~2 days |
| 3 | **`traces` adapter slot** — Tempo + otel-collector via observability profile | ~2 days |
| 4 | **`metrics` adapter slot** — Prometheus + cAdvisor via observability profile (Cost charts + Container subsection) | ~2 days |
| 5 | **`errors` adapter slot** — GlitchTip via observability profile | ~2 days |
| 6 | Aggregation endpoints (reasoners analytics, tools usage, search index stats, notifications channels CRUD, OAuth refresh history) | ~2 days |
| 7 | Polish (adapter pill on adapter-backed pages, Home Connected Services strip, Grafana link-outs) | ~1 day |

**Total: ~13 days.** Each block is independently shippable.

## Locked decisions baked into the roadmap

- **No new admin nav items.** Every gap closes against an existing page via tab / section / data-source swap.
- **One central "Connected services" hub** at Operate → Health. Reads `/api/v1/admin/services` (synth from adapter registry + observability env). All "Open native UI" link-outs live here.
- **Per-page deep links only for specific entities** (e.g., Operate → Runs row → "Open in AgentField" for that run id).
- **Every new observability layer is an adapter slot.** Same scaffolding as existing 8 — protocol doc, Go interface, remote shim, registry row, conformance check. Third parties can swap backends.
- **Observability profile** — `docker compose --profile observability up` brings up Vector + Loki + otel-collector + Tempo + Prometheus + cAdvisor + GlitchTip + Grafana. Base stack stays lean.
- **LLM-specific observability (Langfuse / Helicone / OpenLIT) is NOT in v1.** Covered by generic Logs + Traces + Errors.
- **River UI / pgHero / Sentry self-hosted: NOT in v1.** Existing queue page + DB Health tab + GlitchTip cover the same ground.

## Quick verification

```bash
go build ./...
go vet ./services/runtime/...
go test ./services/runtime/...

# E2E (requires OPENROUTER_API_KEY)
OPENROUTER_API_KEY=$OPENROUTER_API_KEY \
  go test -tags=e2e ./services/runtime/internal/sandbox/adapters/remote/ \
                    ./services/runtime/internal/llmgateway/...

# Conformance binary
go build -o /tmp/backai-adapter-conformance \
  ./services/runtime/cmd/backai-adapter-conformance/
cd examples/adapters/sandbox-echo-py
python3.12 -m venv .venv
.venv/bin/pip install -r requirements.txt
.venv/bin/uvicorn main:app --port 18090 &
sleep 2
/tmp/backai-adapter-conformance --slot sandbox --url http://localhost:18090
# Expect: 8/8 checks passed
```

## Consumption pattern (read this before adding any new endpoint)

Two distinct API classes:

| Class | Path prefix | Caller | Purpose |
|---|---|---|---|
| SDK / customer-facing | `/api/v1/agents`, `/api/v1/llm/*`, `/api/v1/embeddings`, `/api/v1/audio/*`, `/api/v1/images/*`, `/api/v1/sandbox/run`, `/api/v1/memory`, `/api/v1/storage`, `/api/v1/realtime` | Customer apps via tenant key | Initiate work. |
| Admin / operator-facing | `/api/v1/admin/*`, `/api/v1/home/overview`, `/api/v1/cost`, `/api/v1/runs`, `/api/v1/queues/summary`, … | Admin dashboard only | Observe, configure, administer. Read-heavy; mutations audited. |

**Admin never initiates customer-shaped traffic.** Admin "test" buttons dispatch through SDK paths with operator credentials; aggregates come from durable tables (`suite_cost_events`, `suite_audit_log`, `suite_sandbox_runs`, etc.).

Streaming on admin endpoints: SSE on `.../tail` for one-shot pushes; WebSocket on `/api/v1/realtime` for KPI subscriptions. Both honour cancellation.

## Files this handoff supersedes

- Any prior `audit-*.md` (now folded into `backend-admin-contract-audit-v1.md`).
- Any "TODO" / "next steps" comments scattered through other docs — the contract audit is the only source of truth for outstanding work.

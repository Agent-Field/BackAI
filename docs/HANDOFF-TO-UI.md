# Backend Handoff — Ready for UI

> Single entry point for the next agent. Don't read other docs in
> isolation — they're all referenced from here.

## Read these in order

1. `docs/ARCHITECTURE.md` — system overview, 8 bands, adapter system, lifecycle diagrams.
2. `development/ui-plan-v1.md` — the operator-console product spec (authoritative for every page).
3. `development/admin-design-patterns-v1.md` — the implementation contract (shell, grid, page archetypes).
4. `development/admin-api-gap-registry-v1.md` — per-page UI contract status.
5. `development/backend-admin-contract-audit-v1.md` — backend-side gap catalogue.
6. `development/execution-blocks-v1.md` — **the to-do list**: 7 ordered execution blocks, each independently shippable, with the full adapter design for the 4 new observability slots.
7. `docs/adapters/PROTOCOL.md` + `docs/adapters/protocols/<slot>-v1.md` — adapter contracts (10 slots shipped; remaining observability slots designed in block 5-6 of the execution doc).

## What's done (shipped on this branch)

| Area | State |
|---|---|
| Adapter system | **10 Tier-1 slots** (sandbox, storage, notifications, secrets, billing, multimodal, llm-chat, auth, logs, traces) + shared remote HTTP client + capability registry + conformance harness + reference Python adapters. |
| Tests | 61 packages pass. 0 failures. 2 E2E tests (sandbox via Python adapter; LLM via real OpenRouter Kimi via OpenAI-compat proxy). |
| Dashboard shell | 48 routes registered with central navigation + catch-all renderer + seeded-fallback data loader pulling live runtime endpoints. |
| Docs | ARCHITECTURE, PROTOCOL, AUTHORING, CONFORMANCE, per-slot specs, dashboard spec, design patterns, gap registry, contract audit. |

## What's done and what's next

All outstanding work is consolidated in `development/execution-blocks-v1.md` — 9 ordered execution blocks. Blocks 1, 2, 3, and 4 have shipped; Block 5 is the next to dispatch.

| Order | Block | Status | Effort |
|---|---|---|---|
| 1 | Endpoint additions — adapter registry mount, /admin/services synth, /admin/db/health, provider-health poller, cron trigger, cache flush, key rotate, brand R/W, SQL Health tab, notifications mute | ✅ **DONE** | (~4 days) |
| 2 | **Foundation** — config schema (`backai.config.yaml`) + Layer 1/2 validators + capability-probe machinery + retention helper + `/api/v1/admin/features` + Block 1 consolidation | ✅ **DONE** | **~1.5 days** |
| 3 | **`logs` adapter slot** — ring-buffer default; Loki backend; remote SSE shim | ✅ **DONE** | ~2.5 days |
| 4 | **`traces` adapter slot** — empty default; Tempo backend; remote shim | ✅ **DONE** | ~2.5 days |
| 5 | **`metrics` adapter slot** — default Prometheus backend (operator-deployed); Cost charts + Container subsection | ⏭️ **NEXT** | ~2 days |
| 6 | **`errors` adapter slot** — default GlitchTip backend (operator-deployed); log-filter fallback | queued | ~3 days |
| 7 | Aggregation endpoints (reasoners analytics, tools usage, notifications channels CRUD, OAuth refresh history) | queued | ~3–4 days |
| 8 | Polish (adapter pills, Home Connected Services strip, capability-honest degradation) | queued | ~1 day |
| 9 | Unmapped gap indicators (harnesses Disable, modules Migrations, SQL History) | queued | ~1 day |

**Remaining: ~16–17 days.** Each block is independently shippable.

Blocks 3-6 follow the same adapter-slot scaffolding as the existing 8 slots (Go interface + per-slot HTTP protocol + remote-shim + registry row + conformance check). Block 2 (Foundation) introduces the shared machinery they all reuse. Full design for each in `development/execution-blocks-v1.md`.

## Locked decisions baked into the roadmap

- **No new admin nav items.** Every gap closes against an existing page via tab / section / data-source swap.
- **One central "Connected services" hub** at Operate → Health. Reads `/api/v1/admin/services` (synth from adapter registry + observability env). All "Open native UI" link-outs live here.
- **Per-page deep links only for specific entities** (e.g., Operate → Runs row → "Open in AgentField" for that run id).
- **Every new observability layer is an adapter slot.** Same scaffolding as existing 8 — protocol doc, Go interface, remote shim, registry row, conformance check. Third parties can swap backends.
- **Observability backends are operator-deployed and env-var-bound.** The runtime adapts to whatever the operator stands up (Loki at `AF_STACK_LOGS_LOKI_URL`, Tempo at `AF_STACK_TRACES_TEMPO_URL`, Prometheus at `AF_STACK_METRICS_PROMETHEUS_URL`, GlitchTip at `AF_STACK_ERRORS_GLITCHTIP_URL`). When unset, each slot stays on its default builtin.
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

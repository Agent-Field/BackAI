# BackAI Admin API Gap Registry v1

This file records the UI contract status for every page in `development/ui-plan-v1.md`. Update it when routes or adapter capabilities change.

## Status legend

- `backed` — endpoint exists and primary UI can be served end-to-end against the runtime.
- `derived` — primary data exists but UI computes grouping / forecast / status locally; runtime aggregation deferred.
- `degraded` — endpoint exists but active adapter or runtime may not expose the full data.
- `missing` — endpoint or adapter capability is absent.
- `slot-pending` — adapter-backed surface; works with builtin (degraded) today, swaps to full backend when observability profile or remote adapter is configured.

## Page status

| Page | Status | Current Source | Gap To Record |
|---|---|---|---|
| Home | backed | `/api/v1/home/overview`, `/api/v1/metrics/summary`, `/api/v1/activity`, `/health`, `/ready`, realtime | None |
| Runs | backed | `/api/v1/runs`, run events, AgentField link, run actions | None |
| Cost | derived | `/api/v1/cost`, `/api/v1/cost/events`, budgets, cache stats | **Time-series charts blocked on `metrics` slot.** Forecast + cache savings remain client-derived. |
| Errors | slot-pending | `/api/v1/logs?level=error,fatal` (builtin) | **Becomes `backed` once `errors` slot lands (GlitchTip default via observability profile).** Add `/api/v1/admin/errors` + mute/resolve endpoints. |
| Traces | slot-pending | runtime trace context (thin) | **Becomes `backed` once `traces` slot lands (Tempo default via observability profile).** Add `/api/v1/admin/traces` + `/admin/traces/{id}`. |
| Queue | backed | `/api/v1/queues/summary`, `/api/v1/jobs`, `/api/v1/jobs/definitions`, retry/enqueue | None — already covers River queue admin needs. |
| Cache | backed | `/api/v1/llm/cache/stats` | **Flush action needs `POST /api/v1/llm/cache/flush`.** |
| Sandbox runs | backed | `/api/v1/sandbox/runs`, detail, logs, delete | None |
| Webhook deliveries | backed | `/api/v1/webhooks/deliveries`, detail, retry | None |
| Notification deliveries | backed | `/api/v1/notifications`, detail, stats, send | **Mute future-notification action needs `POST /api/v1/notifications/{id}/mute`.** |
| Approvals | backed | `/api/v1/approvals`, detail, decide | None |
| Activity | backed | `/api/v1/activity` | CSV export can be client-side until backend export exists. |
| Health | derived → reorganised | `/health`, `/ready`, `/api/v1/metrics/summary`, service health | **Becomes the Connected Services hub.** Source: `/api/v1/admin/services` (synth from adapter registry + observability env). Subsections: backing services, observability stack, LLM provider availability, DB summary (link to SQL Health tab), Container metrics (when metrics slot active). |
| Logs | slot-pending | `/api/v1/logs` (in-memory ring) | **Becomes `backed` once `logs` slot lands (Loki via Vector default).** Add `/api/v1/admin/logs` + `/admin/logs/tail` (SSE). |
| Agents | backed | `/api/v1/agents`, invoke routes, realtime runs | None |
| Reasoners | derived | derived from `/api/v1/agents` | **Cross-agent latency/cost analytics: add `/api/v1/reasoners/analytics`.** |
| Tools | derived | `/api/v1/tools/native`, `/api/v1/tools/adapters`, `/api/v1/mcp/tools` | **Usage analytics: add `/api/v1/tools/usage`.** |
| Skills | backed | `/api/v1/skills`, MCP server/tool routes | None |
| Harnesses | backed | `/api/v1/harnesses`, provider probe | None |
| Crons | backed | `/api/v1/crons` | **Manual trigger: add `POST /api/v1/crons/{id}/trigger`.** |
| Sandboxes playground | backed | `/api/v1/sandbox/run`, pool, logs, cancel | None |
| Modules | backed | `/api/v1/modules` | Enable/disable writes config outside runtime API in v1; documented behaviour. |
| Data Tables | backed | DB table/detail/rows routes | None |
| Data SQL | backed → +tab | `/api/v1/db/sql` | **Gains a "Health" tab sourced from `/api/v1/admin/db/health` (pg_stat_* queries).** |
| Data Memory | backed | memory list/get/search/put/delete | None |
| Data Storage | backed | storage list/get/signed-url/upload/delete | None |
| Data Search | backed | search query and document upsert/delete | **Index stats: add `/api/v1/search/indexes`.** |
| Feature flags | backed | `/api/v1/config/flags` | Tenant override history endpoint deferred. |
| API Explorer | backed | `/openapi.json` | Type generation downloads are UI-side unless generator endpoint lands. |
| Shipwright | backed-conditional | `/api/v1/shipwright/tasks` | Omit from nav unless v1 product scope enables it. |
| Tenants | backed | admin tenant list/detail/drilldown/create/update/delete | None |
| API keys | backed | admin keys list/issue/spend/revoke | **Native rotate: add `POST /api/v1/admin/keys/{id}/rotate`.** |
| Members | backed | admin users, memberships, export, erase | Invite/disable account endpoints may need auth adapter support. |
| Sessions | degraded | better-auth DB/session data and auth logs | Adapter session-enumeration capability missing; depends on `auth.Provider` extensions. |
| Budgets | backed | admin budgets list/get/set | Delete budget represented by set-to-empty unless backend deletion lands. |
| Audit log | backed | `/api/v1/admin/audit` | Export can be client-side until backend export exists. |
| OAuth connections | backed | `/api/v1/oauth/connections`, providers, authorize, delete, token | **Refresh history: add `/api/v1/oauth/refresh-history`.** |
| Billing summary | backed | billing customers/meters/portal | Churn signals derived until backend flags them. |
| Setup Adapters | backed (handler exists, not yet mounted) | `/api/v1/admin/adapters` | **5-line wire-up needed in `cmd/af-stack/main.go`.** Synthesized capabilities for some built-in slots remain marked `contract_pending` until typed accessors land. |
| Auth providers | degraded | better-auth config, auth protocol docs | Runtime adapter capability endpoint pending. |
| LLM providers | degraded | `/api/v1/llm/models`, LiteLLM link-out | Adapter-aware gateway capabilities pending. |
| Sandbox adapter | backed | `/api/v1/sandbox/pool` | Runtime adapter switch is env-only (by design). |
| Webhook subscribers | backed | webhook endpoint routes, send test | Full event catalog lives in Svix native UI (link-out via Connected Services). |
| Notification channels | degraded | env display plus send test | **Channel CRUD: add `GET/POST/PATCH/DELETE /api/v1/notifications/channels`.** |
| Secrets | backed | secrets list/get/put/reveal/rotate/delete | None |
| Observability | derived | metrics endpoint and env config | **When `metrics` / `traces` / `logs` slots are configured, this page displays which OSS backend is active and exposes link-outs through the Connected Services hub.** Runtime config writes remain env-only. |
| Billing adapter | derived | billing customer/meter routes and env config | Runtime adapter switch is env-only (by design). |
| Deploy targets | missing | env/config conventions and provider link-outs | No runtime provisioning/status endpoint in v1. |
| Brand | missing | `brand.yaml` file read | **Add `GET /api/v1/admin/brand` (+ optional `PUT`).** |

## Observability adapter slots — new in this revision

These add four Tier-1 adapter slots to the platform. Each follows the existing 8-slot scaffolding (Go interface + remote shim + per-slot protocol doc + conformance harness check + registry row).

| Slot | Default builtin | Observability-profile backend | New backend endpoints |
|---|---|---|---|
| `logs` | runtime ring buffer (current) | **Loki** (collected via Vector) | `/api/v1/admin/logs`, `/api/v1/admin/logs/tail` |
| `traces` | empty | **Tempo** (collected via otel-collector; MinIO-backed storage) | `/api/v1/admin/traces`, `/api/v1/admin/traces/{id}` |
| `metrics` | none | **Prometheus** (scrapes runtime + cAdvisor) | `/api/v1/admin/metrics/query`, `/api/v1/admin/metrics/range` |
| `errors` | log-filter aggregation (current) | **GlitchTip** (open-source Sentry-compatible) | `/api/v1/admin/errors`, `/admin/errors/{id}`, `/admin/errors/{id}/mute`, `/admin/errors/{id}/resolve` |

When the observability profile is **not** enabled, every page above degrades gracefully to its builtin source — no broken UI.

## Compose changes

Single new compose profile: `--profile observability`. Adds:

```
vector (log shipper)
loki (log store)
otel-collector (trace receiver)
tempo (trace store; uses MinIO)
prometheus (metrics store)
cadvisor (container-metrics exporter)
glitchtip (error tracker; uses our postgres)
grafana (admin link-out for charts)
```

Each is opt-in. The base stack is unchanged.

## Cross-page conventions

- **Adapter pill** on every adapter-backed page: small "via Loki" / "via Tempo" / "via GlitchTip" indicator showing which backend the page is reading from. Clicking opens the slot's row in Operate → Health.
- **Connected Services** is the single source of "Open native UI" link-outs. No per-page sprinkling of "Open in LiteLLM" / "Open in MinIO" buttons.
- **Specific-entity deep links** are the only exception: Operate → Runs → row → "Open in AgentField" (deep link to a specific run); Customers → Billing → "Open in Stripe" (deep link to a customer).

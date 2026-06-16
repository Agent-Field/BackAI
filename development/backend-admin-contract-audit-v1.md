# BackAI Admin Backend Contract Audit v1

Date: 2026-06-16 (revised)

This audit scopes the backend work needed for the new admin console. The admin UI consumes BackAI **runtime APIs**, not OSS service APIs directly. OSS systems (LiteLLM, MinIO, Svix, AgentField, Postgres, River, Loki, Tempo, Prometheus, GlitchTip, …) stay behind BackAI middleware contracts. Third parties can replace any of those via the adapter-slot pattern.

## 0. Consumption Pattern (Locked)

Two distinct API classes; the admin only consumes one of them.

| Class | Path prefix | Who calls it | Examples |
|---|---|---|---|
| **SDK / customer-facing** | `/api/v1/agents`, `/api/v1/llm/*`, `/api/v1/embeddings`, `/api/v1/audio/*`, `/api/v1/images/*`, `/api/v1/sandbox/run`, `/api/v1/memory`, `/api/v1/storage`, `/api/v1/realtime`, etc. | Customer-app code, end-user clients via tenant key | Initiate work — chat completions, sandbox runs, embeddings, real-time event streams. |
| **Admin / operator-facing** | `/api/v1/admin/*`, `/api/v1/home/overview`, `/api/v1/cost`, `/api/v1/runs`, `/api/v1/queues/summary`, etc. | The admin dashboard only | Observe, configure, administer. Read-heavy; mutations require audit. |

The admin **never** initiates customer-shaped traffic. Admin "test" actions (test agent, test LLM, test sandbox) still flow through the SDK-class endpoints internally but originate from operator-scoped calls. Admin pages aggregate from ledgers (cost events, audit log, runs table) — not by re-running customer work.

Streaming pattern for admin:
- Live KPI ticks on Home / Cost / Health → WebSocket subscription via `/api/v1/realtime` (read-only filter).
- Log tail / trace tail → SSE on `/api/v1/admin/<resource>/tail`.
- Run progress (drill-down) → SSE on `/api/v1/runs/{id}/events`. (Already exists; admin reads it for the per-run drawer.)

## 1. Default OSS Deployment Services

### 1.1 Base stack (always on)

| Service | Purpose | Native UI port | Surface in admin |
|---|---|---|---|
| Postgres + pgvector | Runtime state, River queue, search, memory, audit, RLS | none (we own Build → Data) | Status row on Connected Services + Build → Data tabs + DB health tab (new) |
| AgentField | Agent execution, reasoner graph, runs | `:8081` | Status row + per-run "Open in AgentField" deep link |
| LiteLLM | LLM provider routing | `:4000/ui` | Status row + "Open LiteLLM" button on Connected Services |
| MinIO | Object storage | `:9001` | Status row + "Open MinIO" button on Connected Services |
| Svix + svix-postgres + svix-redis | Outbound webhook delivery | `:8071` | Status row + "Open Svix" button on Connected Services |
| Runtime | BackAI middleware | self | Status row (`/health`, `/ready`, build info) |
| Dashboard / customer-app / supportdesk-agent | UI + example app | n/a | Not surfaced as adapter; they're consumers |

### 1.2 Observability backends (operator-deployed; runtime is env-var-configured)

The runtime exposes 4 adapter slots (`logs`, `traces`, `metrics`, `errors`) that bind to whatever the operator deploys. The mechanism for deploying these services (compose, k8s, external SaaS) is the operator's choice — outside the scope of this contract. The runtime only cares about env vars pointing at the backend's HTTP API.

| Backend | Slot it serves | Native UI port | Runtime env vars |
|---|---|---|---|
| **Vector** (log shipper) | feeds Loki | none | (write-side; no runtime config needed) |
| **Loki** (log store) | `logs` slot | `:3100` (HTTP API only) | `AF_STACK_LOGS_BACKEND=loki`, `AF_STACK_LOGS_LOKI_URL` |
| **otel-collector** (trace receiver) | feeds Tempo | none | runtime exports via OTel SDK if `OTEL_EXPORTER_OTLP_ENDPOINT` set |
| **Tempo** (trace store) | `traces` slot | `:3200` (HTTP API) | `AF_STACK_TRACES_BACKEND=tempo`, `AF_STACK_TRACES_TEMPO_URL` |
| **Prometheus** (metrics store) | `metrics` slot | `:9090` (UI) | `AF_STACK_METRICS_BACKEND=prometheus`, `AF_STACK_METRICS_PROMETHEUS_URL` |
| **cAdvisor** (container metrics exporter) | feeds Prometheus | `:8080` (UI) | (scraped by Prometheus; no runtime config) |
| **GlitchTip** (error tracker) | `errors` slot | `:8000` (UI) | `AF_STACK_ERRORS_BACKEND=glitchtip`, `AF_STACK_ERRORS_GLITCHTIP_URL`, `..._ORG`, `..._TOKEN`, `SENTRY_DSN` |
| **Grafana** (optional charts UI) | none (just link-out) | `:3000` (UI) | `AF_STACK_GRAFANA_URL` (for Connected Services link-out) |

When an env var is unset, the corresponding slot stays on its default builtin (degraded mode); the admin UI gracefully shows zero-state or restricted feature set. See `development/execution-blocks-v1.md` for the full adapter design of each slot.

### 1.3 Explicitly NOT in scope

| Concern | Reason |
|---|---|
| LLM-specific observability (Langfuse, OpenLIT, Helicone) | Covered by generic Logs + Traces + Errors; LLM-aware UI deferred. |
| Full container management (Portainer) | Out of scope; cAdvisor metrics + Dozzle (future) is enough. |
| Sentry self-hosted | GlitchTip covers the API; lighter footprint. |
| Embedded River UI | Existing `/operate/queue` is sufficient; River UI is a v2 nice-to-have. |

## 2. Adapter Slots (the modularity spine — see `docs/adapters/PROTOCOL.md`)

Each row: BackAI Go interface + remote-shim contract. Operators swap adapters via env var; the admin UI does not care which backend is plugged in.

### 2.1 Slots already shipped

| Slot | BackAI interface | Builtin adapters | Remote-shim contract |
|---|---|---|---|
| `auth` | `auth.Provider` (VerifySession, RefreshSession, RevokeSession, GetUser) | better-auth | `docs/adapters/protocols/auth-v1.md` |
| `llm-chat` | `llmgateway.Provider` (Chat, ChatStream, Embeddings) | LiteLLM | `docs/adapters/protocols/llm-chat-v1.md` |
| `multimodal` | `MultimodalAdapter` (Speech, Transcribe, Image) | LiteLLM, ElevenLabs, Cartesia, fal, Flux | `docs/adapters/protocols/multimodal-v1.md` |
| `storage` | `storage.Storage` | MinIO, S3 | `docs/adapters/protocols/storage-v1.md` |
| `sandbox` | `sandbox.Sandbox` | docker, gvisor, firecracker, e2b | `docs/adapters/protocols/sandbox-v1.md` |
| `notifications` | `notifications.Adapter` | log, resend | `docs/adapters/protocols/notifications-v1.md` |
| `secrets` | `secrets.Store` | envelope-local | `docs/adapters/protocols/secrets-v1.md` |
| `billing` | `billing.Client` | Stripe, Lago | `docs/adapters/protocols/billing-v1.md` |

### 2.2 NEW observability slots (this audit's scope)

Each is a Tier-1 adapter slot (swappable). Default builtin = in-runtime degraded mode. Real backend = OSS sidecar plugged in via observability backend. Same modularity rule: third parties can ship alternative backends via the remote-shim pattern.

| Slot | BackAI interface (Go) | Default builtin | Observability-profile backend | Alternative remote backends |
|---|---|---|---|---|
| `logs` | `logs.Store { Query(filter), Tail(filter), Capabilities }` | runtime ring buffer (2048 lines, runtime-process only) | **Loki** (collected via Vector) | Elasticsearch, Quickwit, Datadog Logs (all via remote shim) |
| `traces` | `traces.Store { Search(filter), Get(id), Capabilities }` | empty (page shows zero-state) | **Tempo** (collected via otel-collector; storage on MinIO) | Jaeger, Honeycomb, Quickwit (remote) |
| `metrics` | `metrics.Store { Query(promql), QueryRange(promql, range), Capabilities }` | none (chart panels show "metrics backend not configured") | **Prometheus** | VictoriaMetrics, Mimir, Honeycomb (remote) |
| `errors` | `errors.Store { ListGroups, GetGroup, MuteGroup, Capabilities }` | log-filter aggregation (today's behavior) | **GlitchTip** | Sentry self-hosted, BugSink (remote) |

These slots get the same scaffolding as existing 8: protocol doc under `docs/adapters/protocols/<slot>-v1.md`, Go interface + remote shim, registry entry, conformance harness checks.

## 3. Backend APIs Still Missing (concrete TODO)

### 3.1 Observability slot endpoints (adapter-backed)

| Endpoint | Slot | Note |
|---|---|---|
| `GET /api/v1/admin/logs?q=&level=&service=&tenant=&from=&to=&limit=` | logs | Backend-agnostic; runtime translates filter to active adapter (Loki LogQL when Loki, table query when ring). |
| `GET /api/v1/admin/logs/tail?…` (SSE) | logs | Loki-backed when adapter supports tailing; otherwise no-op stream. |
| `GET /api/v1/admin/traces?service=&since=&duration_gt=&status=` | traces | Returns trace summaries. |
| `GET /api/v1/admin/traces/{trace_id}` | traces | Returns full span tree. |
| `GET /api/v1/admin/metrics/query?promql=` | metrics | Instant query. |
| `GET /api/v1/admin/metrics/range?promql=&from=&to=&step=` | metrics | Range query — powers Cost charts + Health graphs. |
| `GET /api/v1/admin/errors?status=open&limit=` | errors | Grouped errors. |
| `GET /api/v1/admin/errors/{id}` | errors | Group detail. |
| `POST /api/v1/admin/errors/{id}/mute` | errors | Mute group. |
| `POST /api/v1/admin/errors/{id}/resolve` | errors | Mark resolved. |

### 3.2 Non-adapter endpoints

| Endpoint | Notes | Effort |
|---|---|---|
| **Wire-up**: register existing `services/runtime/internal/adapters/registry/handler.go` at `GET /api/v1/admin/adapters` in `cmd/af-stack/main.go` | Already-written handler is not yet mounted. 5-line change. | trivial |
| `GET /api/v1/admin/services` | Synthesized "Connected services" view — every OSS service the runtime knows about, with status, version, port, native admin URL. **Sources from adapter registry + observability env vars.** Powers the Connected Services page. | 0.5 day |
| `GET /api/v1/admin/db/health` | PG slow queries, table sizes, cache hit ratio, vacuum status. Runs 5 standard `pg_stat_*` queries. Rendered as a tab on Build → Data → SQL. | 0.5 day |
| `GET /api/v1/admin/llm/provider-health` | Background poller writes provider availability to `suite_provider_health_log`; this endpoint reads. Surfaces on Operate → Health. | 0.5 day (poller + endpoint) |
| `POST /api/v1/crons/{id}/trigger` | Manual cron run. | trivial |
| `POST /api/v1/llm/cache/flush` (+ optional `?tenant=` / `?prompt_hash=`) | Cache management on Operate → Cache. | 0.5 day |
| `POST /api/v1/admin/keys/{id}/rotate` | Native key rotation (today: revoke + issue). | 0.5 day |
| `POST /api/v1/notifications/{id}/mute` + `GET/POST/PATCH/DELETE /api/v1/notifications/channels` | Mute + channel CRUD for Setup → Notifications. | 1 day |
| `GET /api/v1/reasoners/analytics` | Cross-agent cost/latency/error rollups. Pure aggregation over cost events + runs. | 0.5 day |
| `GET /api/v1/tools/usage` | Native + MCP tool usage analytics. | 0.5 day |
| `GET /api/v1/search/indexes` | FTS index stats (size, last vacuum, hit rate). | trivial |
| `GET /api/v1/admin/brand` + optional `PUT` | Runtime-owned brand.yaml read/write. | trivial |
| `GET /api/v1/oauth/refresh-history` | OAuth refresh debug log. | 0.5 day |
| Session list / force-logout APIs | Auth-adapter dependent. Deferred until adapter capability matures. | (deferred) |
| Feature flag override/history endpoints | Tenant rollout audit. | (deferred) |

## 4. Frontend Surface Mapping (where each gap lands in the UI)

Principle: **don't add new admin pages.** Extend existing ones via tabs or panels. The exception is when the operator's mental model genuinely warrants a separate page — usually for top-level concerns like Logs / Traces / Errors that are nav-prominent.

| Backend gap | Where it lands in the admin UI | Pattern |
|---|---|---|
| `/api/v1/admin/adapters` mount | Setup → Adapters (already wired in dashboard client; unblocks the page when the handler is mounted) | Existing page |
| `/api/v1/admin/services` | **Operate → Health** becomes the Connected Services hub | Same page, restructured: top section = "Connected services" with status pills + "Open UI" buttons; second section = LLM provider availability; third = DB health summary (link to Build → Data → SQL → Health tab) |
| `/api/v1/admin/db/health` | **Build → Data → SQL** gets a "Health" tab (slow queries, sizes, cache hit, locks) | Tab on existing page |
| `/api/v1/admin/llm/provider-health` | Operate → Health → "LLM Providers" subsection (uptime per provider, sparkline if metrics slot active) | Section on existing page |
| Container metrics (via Prometheus + cAdvisor) | Operate → Health → "Containers" subsection (CPU/memory/restarts per container, sparkline) | Section on existing page |
| `/api/v1/admin/logs` | **Operate → Logs** (existing page swaps data source when logs slot is non-builtin) | Existing page; gains advanced filters + tail when Loki active |
| `/api/v1/admin/traces` | **Operate → Traces** (existing page swaps data source; empty zero-state when no traces slot configured) | Existing page |
| `/api/v1/admin/errors` | **Operate → Errors** (existing page swaps data source; falls back to log filter when errors slot is builtin) | Existing page |
| `/api/v1/admin/metrics/range` | **Operate → Cost** gains time-series chart panel (spend over time, cache savings over time, error-rate over time) | Panel on existing page |
| `/api/v1/admin/metrics/range` (Health) | **Operate → Health** "Containers" + "LLM Providers" subsections render sparkline charts | Same page |
| `/api/v1/crons/{id}/trigger` | **Build → Crons** row action "Trigger now" | Row action |
| `/api/v1/llm/cache/flush` | **Operate → Cache** primary action "Flush" | Existing page action |
| `/api/v1/admin/keys/{id}/rotate` | **Customers → API keys** row action "Rotate" | Row action |
| `/api/v1/notifications/channels` | **Setup → Notifications** becomes editable (today read-only env display) | Existing page mutations |
| `/api/v1/reasoners/analytics` | **Build → Reasoners** gains cost/latency columns + sortable analytics | Existing page enrichment |
| `/api/v1/tools/usage` | **Build → Tools** gains call-frequency + error-rate columns | Existing page enrichment |
| `/api/v1/search/indexes` | **Build → Data → Search** gains index-stats panel | Existing page panel |
| `/api/v1/admin/brand` | **Brand** page gains editable form (today read-only file parse) | Existing page mutations |
| `/api/v1/oauth/refresh-history` | **Customers → OAuth connections** row drawer "Refresh history" tab | Drawer tab |

**No new top-level admin pages introduced.** Every gap closes against an existing nav item.

## 5. Connected Services page (clarified design)

`Operate → Health` is the **single hub** for OSS service link-outs. The dashboard renders sections; each is sourced from `/api/v1/admin/services`.

```
┌─ Connected services ───────────────────────────────────────────────┐
│ Service        Status   Version    Port   Purpose      Action     │
│ ─────────────  ──────   ────────   ─────  ──────────   ─────────  │
│ postgres       ●ok      16.4       5432   data         —          │
│ minio          ●ok      RELEASE.…  9001   storage     [Open ↗]    │
│ litellm        ●ok      1.40       4000   llm gateway [Open ↗]    │
│ agentfield     ●ok      0.5        8081   reasoning   [Open ↗]    │
│ svix           ●ok      1.40       8071   webhooks    [Open ↗]    │
│                                                                    │
│ ─── observability (profile: observability) ──                      │
│ loki           ●ok      3.0        3100   logs        —            │
│ tempo          ●ok      2.5        3200   traces      —            │
│ prometheus     ●ok      2.50       9090   metrics    [Open ↗]      │
│ cadvisor       ●ok      0.49       8080   containers —             │
│ grafana        ●ok      11.0       3000   charts     [Open ↗]      │
│ glitchtip      ●ok      4.0        8000   errors     [Open ↗]      │
└────────────────────────────────────────────────────────────────────┘
┌─ LLM provider availability (last 24h) ─────────────────────────────┐
│ openai         ●ok     99.97%      median ttft 220ms               │
│ anthropic      ●ok     99.94%      median ttft 410ms               │
│ openrouter     ●warn   98.10%      median ttft 880ms               │
└────────────────────────────────────────────────────────────────────┘
┌─ Database (summary; deep dive: Build → Data → SQL → Health tab) ──┐
│ Connections  Idle  Active  Slow queries (24h)  Cache hit ratio    │
│ 14           11    3       2                    98.4%              │
└────────────────────────────────────────────────────────────────────┘
┌─ Containers (when observability backend enabled) ──────────────────┐
│ runtime       cpu 4.2%   mem 320MB   restarts 0   uptime 4d       │
│ litellm       cpu 1.1%   mem 180MB   restarts 0   uptime 4d       │
│ agentfield    cpu 0.8%   mem 240MB   restarts 0   uptime 4d       │
└────────────────────────────────────────────────────────────────────┘
```

Direct deep-links (from other pages into specific OSS entities) are limited to:
- Operate → Runs → row → "Open in AgentField" (specific run id)
- Customers → Billing summary → "Open in Stripe" (specific customer id)

Everything else goes through the Connected Services page.

## 6. Roadmap — ordered execution

Each block is independently shippable. Order is "most-operator-value-first".

| Order | Block | Items | Effort |
|---|---|---|---|
| 1 | **Quick wins (no new OSS)** | Wire `/admin/adapters` mount · `/admin/services` synth · `/admin/db/health` · `/llm/provider-health` poller · cron trigger · cache flush · key rotate · brand read/write · DB Health tab on SQL page | ~2 days |
| 2 | **Logs slot** | Define `logs.Store` interface · builtin = current ring · remote shim · protocol spec (logs-v1) · observability compose profile with Loki + Vector · conformance harness adds logs slot · Operate → Logs swaps data source | ~2 days |
| 3 | **Traces slot** | Define `traces.Store` · uncomment otel-collector in compose · add Tempo (uses MinIO storage) · protocol spec (traces-v1) · conformance · Operate → Traces swaps data source | ~2 days |
| 4 | **Metrics slot** | Define `metrics.Store` · add Prometheus + cAdvisor to observability backend · protocol spec (metrics-v1) · Operate → Cost chart panel · Operate → Health container subsection | ~2 days |
| 5 | **Errors slot** | Define `errors.Store` · add GlitchTip to observability backend + SDK wiring in runtime · protocol spec (errors-v1) · Operate → Errors swaps data source | ~2 days |
| 6 | **Aggregation endpoints** | reasoners analytics · tools usage · search index stats · notifications channels CRUD · OAuth refresh history | ~2 days |
| 7 | **Polish** | Adapter pill on each adapter-backed page showing active backend · Connected Services widget on Home as a compact strip · Grafana link-outs on Cost / Health when observability backend is on | ~1 day |

**Total: ~13 days.**

## 7. Pattern enforcement (for the implementer)

1. Every new admin endpoint lives under `/api/v1/admin/*` and has an entry in `services/runtime/internal/openapi/`. Drift is treated as a contract bug.
2. Every adapter-backed admin endpoint reads from the **registry's currently active slot**, never from a hardcoded backend. Operators swap by env var.
3. Every mutation gets an audit row before returning (`internal/audit.Write`).
4. Real-time / streaming uses SSE on `…/tail` for one-shot pushes; WebSocket on `/api/v1/realtime` for KPI subscriptions. Both honour cancellation.
5. Admin endpoints **never** invoke SDK-class paths internally; they aggregate from durable tables. "Test" buttons in the UI dispatch to SDK endpoints with the operator's session credentials.
6. Frontend `api` client validates every response with zod; no silent coercion.
7. New adapter slots follow the existing 8-slot scaffolding: protocol doc, Go interface, remote shim under `<slot>/adapters/remote/` (or `providers/remote/` if package convention prefers), conformance harness entry, registry registration.

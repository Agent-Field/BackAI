# BackAI v1 — Execution Blocks

> **The 7-block roadmap to close every outstanding gap in the admin UI.**
> Each block is independently shippable as its own PR.
>
> Read alongside these source-of-truth docs:
>
> - `docs/ARCHITECTURE.md` — system architecture, 8 bands, request lifecycle.
> - `docs/adapters/PROTOCOL.md` — universal adapter contract.
> - `docs/adapters/protocols/<slot>-v1.md` — per-slot HTTP contracts (8 existing).
> - `development/ui-plan-v1.md` — admin product spec (every page).
> - `development/admin-design-patterns-v1.md` — visual + interaction contract.
> - `development/admin-api-gap-registry-v1.md` — per-page status.
> - `development/backend-admin-contract-audit-v1.md` — backend-side gap catalogue.
> - `development/audits/audit-block{1,2-3,4-5,6-7-frontend}-v1.md` — code-grounded audits that produced the corrections in this doc.

---

## 0. Locked decisions (do not relitigate)

1. **Every new observability layer is a Tier-1 adapter slot** following the existing 8-slot scaffolding (Go interface + per-slot HTTP protocol + remote-shim + registry row + conformance harness check). Same modularity rule: third parties can ship alternative backends.
2. **OSS service deployment is out of scope here.** Operators bring up Loki / Tempo / Prometheus / GlitchTip / Vector / otel-collector / cAdvisor via their own chosen mechanism. The runtime is configured purely via env vars — it doesn't care how the backend got there.
3. **No new admin nav items.** Every backend gap closes against an existing page via tab / section / data-source swap.
4. **One central "Connected services" hub** at `Operate → Health`. The only place "Open native OSS UI" buttons exist. Per-page deep links exist only for *specific entities* (e.g., "Open this run in AgentField").
5. **Admin never invokes SDK-class paths.** Admin reads aggregates from durable tables; "test" actions dispatch through SDK paths with operator credentials.

---

## 0.5. Cross-cutting corrections (applied across all blocks)

These are pattern-level fixes the audits surfaced. Each block's body has the slot-specific consequences baked in.

### 0.5.1 Env var convention: `_ADAPTER`, not `_BACKEND`

All existing 8 slots use `AF_STACK_<SLOT>_ADAPTER=<name>` (e.g., `AF_STACK_SANDBOX_ADAPTER`, `AF_STACK_S3_ADAPTER`, `AF_STACK_BILLING_ADAPTER`). The 4 new observability slots MUST follow the same convention. The doc previously used `_BACKEND`; the corrected names are:

- `AF_STACK_LOGS_ADAPTER` = `ring` (default) | `loki` | `remote`
- `AF_STACK_TRACES_ADAPTER` = `empty` (default) | `tempo` | `remote`
- `AF_STACK_METRICS_ADAPTER` = `none` (default) | `prometheus` | `remote`
- `AF_STACK_ERRORS_ADAPTER` = `logfilter` (default) | `glitchtip` | `remote`

### 0.5.2 Config wiring through `internal/config`, not `os.Getenv` in main

All 8 existing slots route their env via the typed `internal/config` package. The new slots add fields to the `Config` struct and read them in main; do not sprinkle `os.Getenv("AF_STACK_LOGS_ADAPTER")` directly in `cmd/af-stack/main.go`.

### 0.5.3 Frontend pattern is data-driven — no new React components for Blocks 1-6

Confirmed shape (`apps/dashboard/src/app/(admin)/[...slug]/page.tsx:15-21` + `lib/new-admin/data.ts:500-545` + `lib/new-admin/page-model.ts`):

- One catch-all route renders `OperatorPage(model)` where `model` is built by `buildPageModel(path, snapshot)`.
- `getOperatorSnapshot()` runs N (~44 today) `settle(() => api.X())` calls in parallel and falls to `seededSnapshot` if all null.
- Per-endpoint cost = **5 edits**: zod schema + `api.X.Y()` method in `apps/dashboard/src/lib/api.ts` + `settle()` line in `data.ts` + snapshot field map + page-model entry consumes `snapshot.<field>`.
- Only the **brand editor** plausibly needs a new component. Everything else flips by editing `page-model.ts` + `api.ts`.

### 0.5.4 Gap-indicator flip rule (behavior-based, not doc-based)

`page-model.ts` has 24 static `kpi(..., "missing"|"gap"|"deferred"|"thin", ...)` indicators. **Do NOT flip them until:**

1. The runtime endpoint actually responds with the documented shape, AND
2. The zod schema in `api.ts` validates a real response (not the seed), AND
3. The `settle()` call in `data.ts` consistently returns non-null in a live run.

This makes the block checklist behavior-based — a block isn't "done" because the doc text claims so; it's done when the corresponding KPIs flip from `"missing"` → `"backed"` against a running runtime.

### 0.5.5 Registry-driven probe reuse

The adapter registry (`services/runtime/internal/adapters/registry/registry.go:179-204`) already exposes per-slot status / version / admin_ui via `SlotView` and caches probes via `StatusTTL`. The new `/api/v1/admin/services` endpoint (Block 1.2) **reuses** this; it does not build a parallel probe system.

### 0.5.6 Reframe "Quick wins" as Block 1's billing

Audits show every Block 1.5 item carries non-trivial implementation hazards (cache flush needs Cache API extension; keys rotate needs serialization against revoke's async cleanup; notifications mute needs a new table + filter logic; brand R/W must reconcile with build-time `brand.yaml`). Effort estimates corrected: **Block 1 = 3–4 days**, not 2.

---

## Block 1 — Endpoint additions (no new OSS) · **~3–4 days** (revised up from 2d)

Closes 8 known gaps. Most items are small but several carry hidden cost (Cache API extension, key rotation race, brand-yaml build-time gotcha, postgres config change for pg_stat_statements).

### 1.1 ~~Wire~~ Verify `/api/v1/admin/adapters` handler (no work — already mounted)

**Status: DONE.** Verified in code:

- Handler: `services/runtime/internal/server/admin_adapters.go:13`
- OpenAPI registration: `admin_adapters.go:25`
- Tests: `admin_adapters_test.go:29, 53, 83`
- Dashboard client method: `apps/dashboard/src/lib/api.ts:2448` (`api.admin.adapters.list()`)

Action for this PR: tick this item off; do not re-add a duplicate route. Verify the existing endpoint serves a non-empty slot list once Blocks 2-5 register the new slots.

### 1.2 `GET /api/v1/admin/services` — Connected Services synth

The Connected Services page (a renaming of Operate → Health) needs a single endpoint synthesising every OSS service the runtime knows about.

**File**: `services/runtime/internal/server/services.go` (new)

**Response shape**:

```json
{
  "services": [
    {
      "id": "postgres",
      "name": "Postgres",
      "kind": "data",
      "status": "healthy",
      "version": "16.4",
      "host": "postgres",
      "port": 5432,
      "admin_url": null,
      "purpose": "primary database"
    },
    {
      "id": "litellm",
      "name": "LiteLLM",
      "kind": "llm-gateway",
      "status": "healthy",
      "version": "1.40",
      "host": "litellm",
      "port": 4000,
      "admin_url": "http://localhost:4000/ui",
      "purpose": "LLM provider routing"
    },
    {
      "id": "loki",
      "name": "Loki",
      "kind": "observability/logs",
      "status": "healthy",
      "version": "3.0.0",
      "host": "loki",
      "port": 3100,
      "admin_url": null,
      "purpose": "log store (backs logs adapter slot)"
    }
  ]
}
```

**Source of truth**: synthesised from
- adapter registry (`internal/adapters/registry/`) — **reuse the existing `SlotView` + `Probe` + `StatusTTL` cache at `registry.go:179-274`. Do not build a parallel probe system.**
- env vars for observability backends (`AF_STACK_LOGS_LOKI_URL`, `AF_STACK_TRACES_TEMPO_URL`, `AF_STACK_METRICS_PROMETHEUS_URL`, `AF_STACK_ERRORS_GLITCHTIP_URL`)
- the runtime's own self-info

If an env var is unset, the corresponding service is absent from the list. The UI gracefully degrades.

**Status probe**: registry already caches probes (default 30s TTL). Reuse, don't replace.

### 1.3 `GET /api/v1/admin/db/health`

5 standard `pg_stat_*` queries wrapped behind one endpoint.

**File**: `services/runtime/internal/server/db_health.go` (new)

**Response shape**:

```json
{
  "connections": {"active": 3, "idle": 11, "max": 100},
  "cache_hit_ratio": 0.984,
  "slow_queries": [
    {"query": "SELECT ...", "calls": 12, "mean_ms": 1820, "total_ms": 21840}
  ],
  "largest_tables": [
    {"schema": "public", "table": "suite_cost_events", "size_bytes": 458752000, "row_count": 1240000}
  ],
  "vacuum_status": [
    {"table": "suite_audit_log", "last_vacuum": "2026-06-15T08:00:00Z", "dead_tuples": 120}
  ],
  "locks": []
}
```

**Required PG setup** (audit hazard): `pg_stat_statements` is a shared library, not just an extension. It requires:

1. `shared_preload_libraries = 'pg_stat_statements'` in `postgresql.conf` **before** the postgres image starts. The current compose uses `pgvector/pgvector:pg16` which does NOT set this by default — adding a postgres config file mount is part of this block.
2. `CREATE EXTENSION IF NOT EXISTS pg_stat_statements` run once (migration).

If step 1 is skipped, step 2 silently succeeds but no stats accumulate. Runtime checks `pg_extension` AND probes a sample stat at boot; if either fails, endpoint returns `{"available": false, "reason": "pg_stat_statements not loaded; see configuration note"}` and the dashboard renders an "extension needed" notice with the exact remediation.

### 1.4 LLM provider availability poller

Background goroutine polls LiteLLM's `/health` and writes to a new table `suite_provider_health_log` (provider, status, latency_ms, observed_at).

**Polling cadence hazard** (from audit): LiteLLM's `/health` iterates every configured upstream provider serially and can take 10–60s end-to-end. A naïve 60-second cadence will stack tickers behind each other. Two acceptable approaches:

- **(a) Coarser interval + concurrent guard**: poll every 5 minutes; the poller uses `sync.Mutex.TryLock()` to skip if the prior tick is still running.
- **(b) Per-provider parallel polling**: enumerate `/model/info` to discover providers, then poll each upstream individually with a per-provider goroutine + jittered interval. Lower latency but more code.

For Block 1, prefer (a). Plan (b) as a follow-up if operators report stale data.

**Files**:
- `services/runtime/internal/db/migrations/<n>_provider_health_log.sql`
- `services/runtime/internal/llmgateway/health_poller.go`
- `services/runtime/internal/server/llm_provider_health.go`

**Endpoint**: `GET /api/v1/admin/llm/provider-health?window=24h` — returns availability % and latency histogram per provider.

**UI surface**: section on the Connected Services page. **Read directly from this endpoint** (Postgres-backed) — do NOT route through Block 4's PromQL metrics adapter. The earlier doc carried both paths; the Postgres path wins because the poller is the system of record.

### 1.5 Endpoint additions (5 items — audit-corrected scope)

Six endpoints originally listed together; one (`/api/v1/search/indexes`) was a duplicate with Block 6.3 — kept here, dropped from Block 6. Each item below has audit notes about hidden cost.

#### 1.5.1 `POST /api/v1/crons/{id}/trigger`

**HAZARD (audit)**: do NOT trigger by mutating `next_run_at`. The cron scheduler's `ClaimDue` (`internal/crons/scheduler.go:113`) is the only path that enqueues due rows — bumping `next_run_at` introduces a race with the scheduler's own due-detection.

**Correct approach**: call the existing `crons.JobEnqueuer.Enqueue(ctx, name, args, tenantID)` directly (`internal/crons/interface.go`). The handler:

1. Loads the cron row (validate `id` + tenant).
2. Calls `enqueuer.Enqueue(ctx, cron.Name, cron.Args, cron.TenantID)`.
3. Writes an audit log entry `cron.triggered_manually`.
4. Returns the new job id.

Reuses the same `JobEnqueuer` the scheduler uses; no second code path.

**File**: `internal/server/crons.go` (add to existing handlers).

#### 1.5.2 `POST /api/v1/llm/cache/flush` + tenant / prompt_hash filters

**HAZARD (audit)**: `llmcache.Cache.Evict` (`cache.go:265-275`) only deletes *expired* rows. There is no current API to flush by tenant or by prompt hash. This block requires extending the `Cache` interface, not just wrapping it.

**Correct approach**:
1. Add `Cache.Flush(ctx context.Context, opts FlushOpts) (int64, error)` where `FlushOpts` has `TenantID` and `PromptHash` fields (empty = match-all).
2. Implement against the existing storage (PG today).
3. Wire the new HTTP handler to it.

**Files**: `internal/llmcache/cache.go` (extend interface), `internal/server/llm.go` (new handler).

#### 1.5.3 `POST /api/v1/admin/keys/{id}/rotate`

**HAZARD (audit)**: today's revoke (`tenancy/manager.go:1267`) kicks off an **async** cleanup goroutine that removes the LiteLLM virtual-key mirror. A naïve "revoke + reissue" handler races that goroutine and can leave the new key un-mirrored OR the old key half-deleted.

**Correct approach**: add `tenancy.Manager.Rotate(ctx, keyID) (newKey, error)` that:
1. Acquires the same per-key serialization the manager uses for `Revoke` + `Issue`.
2. Inside the lock: issues new key (with LiteLLM mirror), revokes old key (with synchronous LiteLLM cleanup), commits both audit rows.
3. Returns the new key (one-time reveal).

The dashboard's existing "revoke + issue" flow stays as-is for callers that don't need the atomicity guarantee.

**Files**: `internal/tenancy/manager.go` (new `Rotate` method), `internal/server/admin_keys.go` (handler).

#### 1.5.4 Notifications mute (NEW table + filter logic — not a single endpoint)

**HAZARD (audit)**: "Mute future matching notifications" cannot be a single endpoint pinned to a sent notification. Notifications today are an outbox (`notifications/interface.go:1-17`) — a row exists only after delivery is queued. To mute a pattern, the runtime must consult mute rules *before* enqueuing.

**Correct approach**:
1. New migration: `suite_notification_mutes` with columns (id, pattern_match (channel/recipient/template/category), tenant_id, expires_at, created_by, created_at).
2. New endpoints:
   - `GET /api/v1/notifications/mutes`
   - `POST /api/v1/notifications/mutes` — body: `{pattern}` returns id
   - `DELETE /api/v1/notifications/mutes/{id}`
3. Modify `notifications.Service.Send` to check the mute table before dispatch; skip + log `notification.muted` on match.

This is materially larger than the other 1.5 items — alone it's ~half a day. Plan accordingly.

**Files**: `internal/db/migrations/<n>_notification_mutes.sql`, `internal/notifications/{interface.go, service.go}`, `internal/server/notifications.go`.

#### 1.5.5 `GET /api/v1/admin/brand` + optional `PUT`

**HAZARD (audit)**: `brand.yaml` is BUILD-TIME for the customer-app. `apps/customer-app/package.json:6-8` runs `generate-brand.mjs` as `predev` + `prebuild`. The Dockerfile (`apps/customer-app/Dockerfile:27-28`) copies `brand.yaml` into the image. A PUT that mutates the file on disk DOES NOT live-update the running customer-app container.

**Correct approach** (two-layer):
1. **GET** reads `brand.yaml` from disk — fine for the dashboard's read-only view today.
2. **PUT** writes to a new DB row `suite_brand_override` (single-row table, last-write-wins). The customer-app reads this row at boot (next deploy/restart) and merges it over `brand.yaml`. Until the next customer-app restart, the running app uses the build-time YAML.
3. The UI surfaces the "restart customer-app to apply" caveat explicitly.

This avoids a PUT that mutates a build-time file while still letting operators iterate without rebuilds.

**Files**: `internal/db/migrations/<n>_brand_override.sql`, `internal/server/admin_brand.go`, `apps/customer-app/src/lib/brand.ts` (read DB row on boot).

#### 1.5.6 `GET /api/v1/search/indexes`

FTS index stats. Reads from `pg_indexes` + `pg_stat_user_indexes` for FTS / GIN indexes. Small.

**File**: `internal/server/search.go`.

### 1.6 Dashboard wiring for Block 1 (data-driven; no new components except possibly the brand editor)

Per the cross-cutting pattern (§0.5.3): per-endpoint cost is 5 edits — schema + api.ts method + settle() + snapshot map + page-model entry. Concretely for Block 1:

| Endpoint | api.ts addition | data.ts settle line | page-model.ts edit |
|---|---|---|---|
| `GET /api/v1/admin/services` | `api.admin.services.list()` + `ServiceListSchema` | `settle(() => api.admin.services.list())` | `"/operate/health"` row source switches from `seededServices` to `snapshot.services`; flip "Deep stats" KPI from `missing` to `live` |
| `GET /api/v1/admin/db/health` | `api.admin.db.health()` + `DbHealthSchema` | new settle | `"/build/data/sql"` gains a "Health" tab fed from `snapshot.dbHealth`; new tab entry in page-model |
| `GET /api/v1/admin/llm/provider-health` | `api.admin.llm.providerHealth()` | new settle | section on `"/operate/health"` page rendering `snapshot.providerHealth` |
| `POST /api/v1/crons/{id}/trigger` | `api.crons.trigger(id)` mutation | n/a | `"/build/crons"` rows gain "Trigger now" action; flip "Trigger now" KPI |
| `POST /api/v1/llm/cache/flush` | `api.llm.cache.flush(opts)` mutation | n/a | `"/operate/cache"` page primary action; flip "Flush" KPI from `gap` to `live` |
| `POST /api/v1/admin/keys/{id}/rotate` | `api.admin.keys.rotate(id)` mutation | n/a | `"/customers/api-keys"` row action; flip "Rotate" KPI |
| Notifications mute (multiple endpoints) | `api.notifications.mutes.{list,create,delete}` | new settle for list | new "Mutes" tab on `"/operate/notifications"` or section on `"/setup/notifications"`; flip "Mute" KPI |
| `GET /api/v1/search/indexes` | `api.search.indexes()` + `SearchIndexesSchema` | new settle | `"/build/data/search"` gains an Index Stats panel; flip "Stats" KPI |
| `GET /api/v1/admin/brand` (+ `PUT`) | `api.admin.brand.{get,update}` | new settle for get | `"/brand"` page becomes editable (this is the ONLY new React component in Block 1) |

**Gap-indicator flip rule** (§0.5.4): each KPI flip requires the endpoint to be live AND zod to validate AND `settle()` to return non-null. Doc text alone doesn't count.

**Verification**: every page listed in `admin-api-gap-registry-v1.md` that has a "missing endpoint" call-out either flips to `backed` OR retains its `missing` status if the audit-corrected scope deferred it.

---

## Block 2 — `logs` adapter slot · **~2.5 days** (revised up from 2d)

**Why this slot**: today logs are a 2048-line in-memory ring inside the runtime process. We can't see agent-container logs, customer-app logs, LiteLLM logs, or anything past 2048 entries. Operators running real workloads need cross-service log query.

**Approach**: introduce a `logs` adapter slot following the existing 8-slot pattern. The default builtin is the current ring buffer (no behaviour change). The first real adapter is **Loki** (queried via its HTTP API). Third parties can ship alternative backends (Elasticsearch, Quickwit, Datadog) using the remote-shim contract.

**Audit corrections baked in** (see `development/audits/audit-block2-3-v1.md`):
- Env var: `AF_STACK_LOGS_ADAPTER` (not `_BACKEND`).
- Wire field is `msg` (matches existing `LogLineSchema` zod in `apps/dashboard/src/lib/api.ts`), with the existing `agent` field preserved — do NOT introduce a new `message` field that breaks dashboard validation.
- Ring buffer has only `Append` / `Recent` (`internal/logger/ring.go:27`); `Tail` requires building a Subscribe + channel + backpressure layer on top.
- Loki `/loki/api/v1/tail` is genuinely WebSocket — need `nhooyr.io/websocket` or `gorilla/websocket` added to `go.mod`.
- LogQL queries must use `| logfmt` parser stage before post-pipeline filtering on `level`; default Vector/Promtail Docker scrape config doesn't promote `level` to a label.
- For substring search parity with the ring buffer's `SupportsFullText`, default the line filter to `|=` (literal); use `|~` only when caller passes an explicit regex flag.

### 2.1 Go interface

**File**: `services/runtime/internal/observability/logs/interface.go` (new)

```go
package logs

import (
    "context"
    "time"
)

// Entry is one log line, normalised across backends.
// Field names match the existing LogLineSchema zod on the dashboard side
// (see apps/dashboard/src/lib/api.ts) — do not rename `msg` to `message`
// or drop `agent`.
type Entry struct {
    TS        time.Time      `json:"ts"`
    Level     string         `json:"level"`     // "debug"|"info"|"warn"|"error"|"fatal"
    Service   string         `json:"service"`   // "runtime"|"agentfield"|"litellm"|"supportdesk-agent"|...
    Msg       string         `json:"msg"`
    Agent     string         `json:"agent,omitempty"`
    TenantID  string         `json:"tenant_id,omitempty"`
    RequestID string         `json:"request_id,omitempty"`
    TraceID   string         `json:"trace_id,omitempty"`
    Fields    map[string]any `json:"fields,omitempty"`
}

// Filter is the admin-facing query. The adapter translates it to its
// backend's native query language (LogQL for Loki, Lucene for ES, etc.).
type Filter struct {
    Services []string  // include only these services (empty = all)
    Levels   []string  // include only these levels (empty = all)
    TenantID string
    RequestID string
    TraceID  string
    Search   string    // free-text, backend handles as best-effort
    From, To time.Time // empty To = "now"
    Limit    int       // backend caps; default 200
    Cursor   string    // pagination
}

// Page is one window of entries.
type Page struct {
    Entries    []Entry
    NextCursor string // "" when no more
    HasMore    bool
}

// Capabilities is what this adapter declares.
type Capabilities struct {
    SupportsTail      bool `json:"supports_tail"`        // adapter can stream live
    SupportsFullText  bool `json:"supports_full_text"`   // Search field is honoured
    SupportsTraceID   bool `json:"supports_trace_id"`    // TraceID filter is honoured
    NativeQueryLang   string `json:"native_query_lang"`  // "logql"|"lucene"|"sql"|""
    RetentionDays     int    `json:"retention_days"`     // 0 = unknown
    MaxEntriesPerPage int    `json:"max_entries_per_page"`
}

// Store is the adapter contract. All methods respect context cancellation.
type Store interface {
    Query(ctx context.Context, f Filter) (Page, error)
    Tail(ctx context.Context, f Filter) (<-chan Entry, error)
    Capabilities() Capabilities
}
```

### 2.2 Default builtin: ring buffer

The existing `internal/logger.Ring` becomes the basis for the `RingStore` impl satisfying `logs.Store`. No change in write path (slog still writes to the ring).

**HAZARD (audit)**: `Ring` only exposes `Append` and `Recent`. There is no Subscribe / channel API. `RingStore.Tail(...)` is NOT a one-liner — it requires:

1. A subscriber registry on `Ring` (slice of channels guarded by a mutex).
2. Append fans out to each subscriber.
3. Backpressure policy: drop oldest in the channel if the consumer is slow, or close the channel after a bounded drop count.
4. Unsubscribe on context cancel.

This is ~80 lines of new code in `internal/logger/ring.go`, with tests.

```go
// services/runtime/internal/observability/logs/ring/ring.go
type RingStore struct { ring *logger.Ring }
func (s *RingStore) Query(ctx, f Filter) (Page, error) { ... iterate ring with filter ... }
func (s *RingStore) Tail(ctx, f Filter) (<-chan Entry, error) { ... uses new Ring.Subscribe ... }
func (s *RingStore) Capabilities() Capabilities { return ... }
```

`SupportsFullText: true` (we do substring), `NativeQueryLang: ""`, `RetentionDays: 0` (volatile), `MaxEntriesPerPage: 1000`.

### 2.3 First real adapter: Loki

**File**: `services/runtime/internal/observability/logs/adapters/loki/loki.go` (new)

Loki's relevant HTTP API:

| Loki endpoint | Used by Store method |
|---|---|
| `GET /loki/api/v1/query_range?query=&start=&end=&limit=&direction=backward` | `Query` (LogQL filter built from `Filter`) |
| `GET /loki/api/v1/tail?query=&delay_for=&limit=&start=` (WebSocket) | `Tail` — adapter consumes WebSocket, re-emits as Go channel |
| `GET /ready` | health probe |
| `GET /loki/api/v1/labels?start=&end=` | future autocomplete (not in v1 interface) |

**LogQL translation rule** (audit-corrected):

```go
func filterToLogQL(f Filter) string {
    // Stream selector with label match
    var labels []string
    if len(f.Services) > 0 { labels = append(labels, `service=~"`+strings.Join(f.Services,"|")+`"`) }
    if f.TenantID != ""    { labels = append(labels, `tenant_id="`+f.TenantID+`"`) }
    if f.TraceID != ""     { labels = append(labels, `trace_id="`+f.TraceID+`"`) }
    selector := "{" + strings.Join(labels, ",") + "}"
    expr := selector

    // Default: literal substring (parity with ring buffer's SupportsFullText).
    // |~ regex only when caller passes an explicit regex flag.
    if f.Search != "" {
        if f.SearchIsRegex {
            expr += ` |~ ` + strconv.Quote(f.Search)
        } else {
            expr += ` |= ` + strconv.Quote(f.Search)
        }
    }

    // Post-pipeline level filter REQUIRES a parser stage. Default Vector /
    // Promtail Docker scrape configs ship JSON / logfmt to Loki; pick the
    // one your shipper uses. We standardise on logfmt for BackAI services
    // (slog default).
    if len(f.Levels) > 0 {
        expr += ` | logfmt | level=~"` + strings.Join(f.Levels,"|") + `"`
    }
    return expr
}
```

Pitfall: the `| level=~"..."` clause without `| logfmt` / `| json` returns nothing on a vanilla Loki because `level` is not a stream label — it's a parsed field. The fix is to always run a parser stage. BackAI runtime + agents use slog's logfmt output; pick another parser if your shipper sends JSON.

**Capabilities**:

```go
Capabilities{
    SupportsTail: true,
    SupportsFullText: true,        // |= literal substring
    SupportsRegexSearch: true,     // |~ when caller opts in
    SupportsTraceID: true,
    NativeQueryLang: "logql",
    RetentionDays: probeRetention(),  // parse Loki's /config YAML; multi-tenant aware
    MaxEntriesPerPage: 5000,
}
```

**Env vars consumed** (corrected to existing `_ADAPTER` convention):

- `AF_STACK_LOGS_ADAPTER` — `ring` (default) | `loki` | `remote`
- `AF_STACK_LOGS_LOKI_URL` — e.g., `http://loki:3100`
- `AF_STACK_LOGS_LOKI_TENANT` — optional `X-Scope-OrgID` for multi-tenant Loki

**New dependency**: `nhooyr.io/websocket` (or `gorilla/websocket`) — Loki tail is a WebSocket endpoint. Add to `go.mod`; the remote-adapter `Client` package's existing SSE parser doesn't speak WebSocket.

### 2.4 Remote-shim contract (for third-party backends)

**File**: `docs/adapters/protocols/logs-v1.md` (new, mirrors existing per-slot specs)

**Endpoints the remote sidecar exposes**:

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/logs/query` | body=`Filter` (JSON), response=`Page` |
| `GET` | `/v1/logs/tail?service=&level=&...` | SSE: each event is one `Entry` JSON; terminator `data: [DONE]` |
| `GET` | `/v1/capabilities` | declare `Capabilities` |
| `GET` | `/healthz` | health |
| `GET` | `/v1/info` | optional metadata |

Same universal envelope as the other 8 slots (auth, idempotency, RFC 7807 errors, `X-Backai-*` headers).

**Remote shim Go file**: `services/runtime/internal/observability/logs/adapters/remote/remote.go` (uses the shared `services/runtime/internal/adapters/remote/Client`).

### 2.5 Adapter selection

`services/runtime/cmd/af-stack/main.go`:

```go
var logsStore logs.Store
switch os.Getenv("AF_STACK_LOGS_BACKEND") {
case "loki":
    logsStore = lokilogs.New(os.Getenv("AF_STACK_LOGS_LOKI_URL"), ...)
case "remote":
    logsStore = remotelogs.New(remote.Config{
        BaseURL: os.Getenv("AF_STACK_LOGS_ADAPTER_URL"),
        Token:   os.Getenv("AF_STACK_LOGS_ADAPTER_TOKEN"),
    })
default:
    logsStore = ringlogs.New(logRing)
}
adapterRegistry.Register(registry.Slot{
    ID: "logs", Tier: 1, Kind: kind, Name: logsStore.Capabilities().NativeQueryLang or "ring",
    ...
})
```

### 2.6 Admin endpoints (read-side)

| Endpoint | Maps to |
|---|---|
| `GET /api/v1/admin/logs?services=&levels=&tenant=&search=&from=&to=&limit=&cursor=` | `logsStore.Query` |
| `GET /api/v1/admin/logs/tail?...` (SSE) | `logsStore.Tail` |
| `GET /api/v1/admin/logs/capabilities` | returns `Capabilities` for the dashboard's adapter pill |

### 2.7 Conformance harness

Extend `services/runtime/cmd/backai-adapter-conformance/main.go` with `runLogsChecks(...)`:

- `GET /v1/capabilities` declares the logs slot
- `POST /v1/logs/query` with empty filter returns a Page (possibly empty)
- `GET /v1/logs/tail` (SSE) starts a stream and terminates on disconnect
- Capability honesty: if `supports_tail=false`, `/v1/logs/tail` returns `422 unsupported_capability`

### 2.8 Admin UI surface

- `Operate → Logs` (existing page): data source switches from `/api/v1/logs` to `/api/v1/admin/logs`. Adapter pill shows "via Loki" / "via ring buffer" / "via remote: <name>".
- No new nav item.

### 2.9 Files this block creates / modifies

```
NEW:
  docs/adapters/protocols/logs-v1.md
  services/runtime/internal/observability/logs/interface.go
  services/runtime/internal/observability/logs/ring/ring.go
  services/runtime/internal/observability/logs/ring/ring_test.go
  services/runtime/internal/observability/logs/adapters/loki/loki.go
  services/runtime/internal/observability/logs/adapters/loki/loki_test.go
  services/runtime/internal/observability/logs/adapters/remote/remote.go
  services/runtime/internal/observability/logs/adapters/remote/remote_test.go
  services/runtime/internal/server/admin_logs.go
MODIFIED:
  services/runtime/cmd/af-stack/main.go
  services/runtime/cmd/backai-adapter-conformance/main.go
  docs/adapters/README.md  (add logs row)
  docs/adapters/PROTOCOL.md (§14 — add logs slot)
  apps/dashboard/src/lib/api.ts (point `api.logs.list` to /admin/logs)
```

---

## Block 3 — `traces` adapter slot · **~2.5 days** (revised up from 2d)

**Why this slot**: the runtime has the OpenTelemetry SDK wired and **does honor `OTEL_EXPORTER_OTLP_ENDPOINT`** (`internal/config/config.go:208-210`). Nothing receives the spans today. The admin's Traces page is empty. Operators need a span store + query.

**Approach**: `traces` adapter slot. Default builtin is empty (admin shows zero-state). First real adapter is **Tempo** (queried via its HTTP API; spans are pushed to it by otel-collector, configured separately by the operator).

**Audit corrections baked in**:
- Env var: `AF_STACK_TRACES_ADAPTER` (not `_BACKEND`).
- Tempo `/api/search?tags=` expects a **single logfmt-encoded string** for `tags`, not per-tag query params. Fix translator.
- Decode legacy `batches[].resource_spans[]` JSON shape from `/api/traces/{id}`, OR pin to `/api/v2/traces/{id}` (newer Tempo) — pick one and document.
- Root span has `parent_span_id == "0000000000000000"` (Tempo's all-zeros) OR `""` depending on version. Normalize to `""` in the Go struct.
- Config wiring via `internal/config`, not `os.Getenv` in main (§0.5.2).

### 3.1 Go interface

**File**: `services/runtime/internal/observability/traces/interface.go` (new)

```go
package traces

import (
    "context"
    "time"
)

type TraceSummary struct {
    TraceID       string        `json:"trace_id"`
    RootService   string        `json:"root_service"`
    RootOperation string        `json:"root_operation"`
    StartTime     time.Time     `json:"start_time"`
    Duration      time.Duration `json:"duration"`
    SpanCount     int           `json:"span_count"`
    Status        string        `json:"status"` // "ok"|"error"|"unset"
}

type SearchFilter struct {
    Service     string
    Operation   string
    Tag         map[string]string  // attribute filters
    MinDuration time.Duration
    MaxDuration time.Duration
    Status      string
    From, To    time.Time
    Limit       int
}

type SearchResult struct {
    Traces  []TraceSummary
    HasMore bool
}

type Span struct {
    SpanID        string         `json:"span_id"`
    ParentSpanID  string         `json:"parent_span_id"`
    Service       string         `json:"service"`
    Operation     string         `json:"operation"`
    StartTime     time.Time      `json:"start_time"`
    Duration      time.Duration  `json:"duration"`
    Status        string         `json:"status"`
    Attributes    map[string]any `json:"attributes"`
    Events        []SpanEvent    `json:"events"`
}

type SpanEvent struct {
    TS         time.Time      `json:"ts"`
    Name       string         `json:"name"`
    Attributes map[string]any `json:"attributes"`
}

type Trace struct {
    TraceID string `json:"trace_id"`
    Spans   []Span `json:"spans"`
}

type Capabilities struct {
    SupportsTraceQL    bool   `json:"supports_traceql"`
    SupportsTagSearch  bool   `json:"supports_tag_search"`
    NativeQueryLang    string `json:"native_query_lang"` // "traceql"|"jql"|""
    RetentionHours     int    `json:"retention_hours"`
    MaxResultsPerQuery int    `json:"max_results_per_query"`
}

type Store interface {
    Search(ctx context.Context, f SearchFilter) (SearchResult, error)
    Get(ctx context.Context, traceID string) (Trace, error)
    Capabilities() Capabilities
}
```

### 3.2 Default builtin: empty store

```go
// services/runtime/internal/observability/traces/empty/empty.go
type EmptyStore struct{}
func (EmptyStore) Search(...) (SearchResult, error) { return SearchResult{}, nil }
func (EmptyStore) Get(_, id) (Trace, error) { return Trace{}, ErrNoBackend }
func (EmptyStore) Capabilities() Capabilities { return Capabilities{} }
```

Admin renders zero-state: "No traces backend configured. See docs/observability.md."

### 3.3 First real adapter: Tempo

**File**: `services/runtime/internal/observability/traces/adapters/tempo/tempo.go` (new)

Tempo's HTTP API:

| Tempo endpoint | Used by Store method |
|---|---|
| `GET /api/search?tags=&start=&end=&minDuration=&maxDuration=&limit=` | `Search` (when no TraceQL) |
| `GET /api/v2/search?q=<TraceQL>&start=&end=&limit=` | `Search` (when TraceQL preferred) |
| `GET /api/traces/{traceID}` | `Get` — returns full trace |
| `GET /ready` | health |

**Capabilities probe**: hit `/status/version`. Tempo ≥ 2.0 supports TraceQL; older versions use legacy tag search.

**Env vars** (corrected):

- `AF_STACK_TRACES_ADAPTER` — `empty` (default) | `tempo` | `remote`
- `AF_STACK_TRACES_TEMPO_URL` — e.g., `http://tempo:3200`
- `AF_STACK_TRACES_TEMPO_TENANT` — optional `X-Scope-OrgID`

**Tempo translator pitfalls** (audit):

```go
// tags param is a single logfmt-encoded string:
//   tags=service.name=runtime operation=POST%20chat status=error
// NOT a repeated query param.
func searchURL(f SearchFilter) string {
    var kv []string
    if f.Service != ""   { kv = append(kv, `service.name=`+f.Service) }
    if f.Operation != "" { kv = append(kv, `operation=`+f.Operation) }
    for k, v := range f.Tag { kv = append(kv, k+`=`+v) }
    if f.Status != ""    { kv = append(kv, `status=`+f.Status) }
    u := url.Values{}
    u.Set("tags", strings.Join(kv, " "))
    u.Set("start", strconv.FormatInt(f.From.Unix(), 10))
    u.Set("end",   strconv.FormatInt(f.To.Unix(),   10))
    if f.MinDuration > 0 { u.Set("minDuration", f.MinDuration.String()) }
    if f.MaxDuration > 0 { u.Set("maxDuration", f.MaxDuration.String()) }
    if f.Limit > 0       { u.Set("limit", strconv.Itoa(f.Limit)) }
    return "/api/search?" + u.Encode()
}
```

**Trace decode**: `/api/traces/{id}` returns `{batches: [{resource: {...}, scopeSpans: [{spans: [...]}]}]}` (OTLP shape). Flatten to our `Trace.Spans`. Normalize:

```go
if span.ParentSpanID == "0000000000000000" { span.ParentSpanID = "" }
```

### 3.4 Remote-shim contract

**File**: `docs/adapters/protocols/traces-v1.md` (new)

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/traces/search` | body=`SearchFilter`, response=`SearchResult` |
| `GET` | `/v1/traces/{trace_id}` | response=`Trace` |
| `GET` | `/v1/capabilities` | declare `Capabilities` |
| `GET` | `/healthz` | health |

### 3.5 Admin endpoints

| Endpoint | Maps to |
|---|---|
| `GET /api/v1/admin/traces?service=&from=&to=&duration_gt=&status=&limit=` | `tracesStore.Search` |
| `GET /api/v1/admin/traces/{trace_id}` | `tracesStore.Get` |
| `GET /api/v1/admin/traces/capabilities` | declares adapter |

### 3.6 Conformance harness

Extend with `runTracesChecks(...)`:
- Search with empty filter returns a result envelope
- Capabilities echoes back at least one supported feature
- Get for an unknown trace returns `404 trace_not_found`

### 3.7 Admin UI surface

- `Operate → Traces` (existing): data source switches to `/api/v1/admin/traces`. Empty-state when default. Adapter pill: "via Tempo" / "via remote: <name>".

### 3.8 Files

```
NEW:
  docs/adapters/protocols/traces-v1.md
  services/runtime/internal/observability/traces/interface.go
  services/runtime/internal/observability/traces/empty/empty.go
  services/runtime/internal/observability/traces/adapters/tempo/tempo.go
  services/runtime/internal/observability/traces/adapters/tempo/tempo_test.go
  services/runtime/internal/observability/traces/adapters/remote/remote.go
  services/runtime/internal/observability/traces/adapters/remote/remote_test.go
  services/runtime/internal/server/admin_traces.go
MODIFIED:
  services/runtime/cmd/af-stack/main.go
  services/runtime/cmd/backai-adapter-conformance/main.go
  docs/adapters/README.md
  docs/adapters/PROTOCOL.md (§14)
  apps/dashboard/src/lib/api.ts
```

> **Note on write-side**: spans reach Tempo via an `otel-collector` that receives OTLP from the runtime + agents. Wiring the collector is an operator-deployment concern (out of scope for this slot). The runtime's OTel SDK is already configured to export to `OTEL_EXPORTER_OTLP_ENDPOINT` if set.

---

## Block 4 — `metrics` adapter slot · ~2 days

**Why this slot**: KPIs on Home, Cost, and Health are point-in-time. The dashboard has no time-series — operators can't see spend over the last 24h, error rate trends, container resource trajectories. Cost forecasting is client-side regression.

**Approach**: `metrics` adapter slot. Default builtin is **none** (chart panels show "metrics backend not configured"). First real adapter is **Prometheus** (queried via PromQL). Container metrics come from **cAdvisor** scraped by the same Prometheus instance — no separate slot.

**Audit corrections baked in**:
- Env var: `AF_STACK_METRICS_ADAPTER` (not `_BACKEND`).
- `/metrics` is mounted (`server.go:776`) but only Go runtime + process collectors — Prometheus dashboards will be sparse until app-specific counters/gauges are added (cost-per-tenant, runs-per-minute, etc.). This block should also export a starter set of app metrics.
- `kube_pod_container_status_restarts_total` is from **kube-state-metrics**, NOT cAdvisor. Use cAdvisor's own series for Compose deployments.
- **Provider availability** is sourced from Block 1.4's Postgres `suite_provider_health_log` via `/api/v1/admin/llm/provider-health` — NOT via PromQL on this slot. Earlier doc had both paths; corrected.
- Prometheus API param formats: `time` is Unix seconds (float allowed) OR RFC3339; `step` is duration string (`"30s"`) or float seconds. Document both formats in the adapter doc.

### 4.1 Go interface

**File**: `services/runtime/internal/observability/metrics/interface.go` (new)

```go
package metrics

import (
    "context"
    "time"
)

type InstantSample struct {
    Labels map[string]string `json:"labels"`
    Value  float64           `json:"value"`
    TS     time.Time         `json:"ts"`
}

type RangeSample struct {
    Labels  map[string]string `json:"labels"`
    Samples []TimedValue      `json:"samples"`
}

type TimedValue struct {
    TS    time.Time `json:"ts"`
    Value float64   `json:"value"`
}

type Capabilities struct {
    NativeQueryLang     string `json:"native_query_lang"` // "promql"|""
    SupportsInstant     bool   `json:"supports_instant"`
    SupportsRange       bool   `json:"supports_range"`
    SupportsContainerMx bool   `json:"supports_container_metrics"` // adapter is fed by cAdvisor (or equivalent)
    RetentionDays       int    `json:"retention_days"`
    MinStep             time.Duration `json:"min_step"`
}

type Store interface {
    Query(ctx context.Context, expr string, at time.Time) ([]InstantSample, error)
    QueryRange(ctx context.Context, expr string, from, to time.Time, step time.Duration) ([]RangeSample, error)
    Capabilities() Capabilities
}
```

### 4.2 Default builtin: none

```go
// services/runtime/internal/observability/metrics/none/none.go
type NoneStore struct{}
func (NoneStore) Query(...) ([]InstantSample, error) { return nil, ErrNoBackend }
func (NoneStore) QueryRange(...) ([]RangeSample, error) { return nil, ErrNoBackend }
func (NoneStore) Capabilities() Capabilities { return Capabilities{} }
```

Admin pages that depend on metrics show a notice: "configure a metrics backend to see time-series charts."

### 4.3 First real adapter: Prometheus

**File**: `services/runtime/internal/observability/metrics/adapters/prometheus/prometheus.go` (new)

Prometheus HTTP API:

| Prometheus endpoint | Used by Store method |
|---|---|
| `GET /api/v1/query?query=&time=` | `Query` |
| `GET /api/v1/query_range?query=&start=&end=&step=` | `QueryRange` |
| `GET /-/ready` | health |

**Capabilities**:

```go
Capabilities{
    NativeQueryLang: "promql",
    SupportsInstant: true,
    SupportsRange:   true,
    SupportsContainerMx: cadvisorScraped, // probe: does series container_cpu_usage_seconds_total exist?
    RetentionDays:   probeRetention(),
    MinStep:         15 * time.Second,
}
```

**Env vars** (corrected):

- `AF_STACK_METRICS_ADAPTER` — `none` (default) | `prometheus` | `remote`
- `AF_STACK_METRICS_PROMETHEUS_URL` — e.g., `http://prometheus:9090`

**Container metric series** (corrected — cAdvisor only):

- `container_cpu_usage_seconds_total{name=~"backai-.*"}` (cAdvisor)
- `container_memory_usage_bytes{name=~"backai-.*"}` (cAdvisor)
- `container_network_receive_bytes_total{name=~"backai-.*"}` (cAdvisor)
- For restart counts: cAdvisor exposes `container_start_time_seconds`; restarts are derived as a delta over time. (`kube_pod_container_status_restarts_total` is kube-state-metrics — Kubernetes-only.)

**App metrics to add this block** (so Prometheus dashboards aren't empty):

- `backai_cost_usd_total{tenant,model,agent}` — counter, increments per cost event
- `backai_llm_requests_total{tenant,model,status}` — counter
- `backai_llm_ttft_seconds{model}` — histogram
- `backai_runs_total{agent,status}` — counter
- `backai_sandbox_runs_total{adapter,status}` — counter

Wired in the existing observability hooks — small additions to `internal/cost/`, `internal/llmgateway/`, `internal/server/runs.go`, `internal/sandbox/`.

### 4.4 Remote-shim contract

**File**: `docs/adapters/protocols/metrics-v1.md` (new)

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/metrics/query` | body=`{"query": "...", "time": "..."}`, response=`[InstantSample]` |
| `POST` | `/v1/metrics/query_range` | body=`{"query","from","to","step"}`, response=`[RangeSample]` |
| `GET` | `/v1/capabilities` | declare |
| `GET` | `/healthz` | health |

### 4.5 Admin endpoints

| Endpoint | Maps to |
|---|---|
| `GET /api/v1/admin/metrics/query?promql=&at=` | `metricsStore.Query` |
| `GET /api/v1/admin/metrics/range?promql=&from=&to=&step=` | `metricsStore.QueryRange` |
| `GET /api/v1/admin/metrics/capabilities` | declares adapter |

### 4.6 Admin UI surface

- `Operate → Cost`: gains a time-series chart panel (spend over time, cache savings, model mix) — backed by `backai_cost_usd_total` series.
- `Operate → Health → Connected Services`: "Containers" subsection rendering per-container CPU / mem sparklines via cAdvisor queries: `rate(container_cpu_usage_seconds_total{name=~"backai-.*"}[5m])`, `container_memory_usage_bytes{name=~"backai-.*"}`. Restart count derived from `container_start_time_seconds` changes.
- `Operate → Health → LLM Providers`: latency / availability sparkline. **Source: `/api/v1/admin/llm/provider-health` (Block 1.4), NOT PromQL.** The Postgres-backed poller is the system of record.

### 4.7 Conformance harness

Extend with `runMetricsChecks(...)`:
- Query with `up{}` returns at least one InstantSample (or graceful empty)
- Capabilities echoes `native_query_lang`

### 4.8 Files

```
NEW:
  docs/adapters/protocols/metrics-v1.md
  services/runtime/internal/observability/metrics/interface.go
  services/runtime/internal/observability/metrics/none/none.go
  services/runtime/internal/observability/metrics/adapters/prometheus/prometheus.go
  services/runtime/internal/observability/metrics/adapters/prometheus/prometheus_test.go
  services/runtime/internal/observability/metrics/adapters/remote/remote.go
  services/runtime/internal/observability/metrics/adapters/remote/remote_test.go
  services/runtime/internal/server/admin_metrics.go
MODIFIED:
  services/runtime/cmd/af-stack/main.go
  services/runtime/cmd/backai-adapter-conformance/main.go
  docs/adapters/README.md
  docs/adapters/PROTOCOL.md (§14)
  apps/dashboard/src/lib/api.ts
```

> **Note on write-side**: the runtime already exposes `/metrics` (`client_golang`). Prometheus is configured (in operator's deployment) to scrape `runtime:8080/metrics` + `cadvisor:8080/metrics`. The runtime doesn't push.

---

## Block 5 — `errors` adapter slot · **~3 days** (revised up from 2d)

**Why this slot**: today's "errors" page client-side filters logs by level (`apps/dashboard/src/lib/new-admin/data.ts:1158-1166`). There's no server-side deduplication, grouping, resolution state, or alerting. Operators need a Sentry-shaped error tracker.

**Approach**: `errors` adapter slot. Default builtin is a log-filter aggregation **(new code — does not exist server-side today; today's behaviour is purely client-side)**. First real adapter is **GlitchTip** (Sentry-API-compatible, MIT licensed). Runtime + agents push events via the standard Sentry SDK; admin reads grouped issues via GlitchTip's REST API.

**Audit corrections baked in**:
- Env var: `AF_STACK_ERRORS_ADAPTER` (not `_BACKEND`).
- "Default builtin = log-filter aggregation" is net-new server code, not an existing fallback. Frame the work accordingly.
- **GlitchTip license is MIT** (not AGPL/MIT).
- **Sentry SDK is NOT in `go.mod` or python requirements.** Adding `github.com/getsentry/sentry-go` to the Go runtime + `sentry-sdk` to each Python agent is part of this block's cost — ~half a day across all containers.
- GlitchTip mutation: canonical path is `PUT /api/0/issues/{id}/` (no org slug). Org-scoped variant works on some versions but isn't universal.
- Status mapping: internal `"muted"` → GlitchTip/Sentry `"ignored"`. Make the translation explicit in code, not implicit.

### 5.1 Go interface

**File**: `services/runtime/internal/observability/errors/interface.go` (new)

```go
package errors

import (
    "context"
    "time"
)

type Group struct {
    ID         string         `json:"id"`
    Title      string         `json:"title"`
    Culprit    string         `json:"culprit"`
    Service    string         `json:"service"`
    Status     string         `json:"status"` // "open"|"muted"|"resolved"
    Count      int            `json:"count"`
    UserCount  int            `json:"user_count"`
    FirstSeen  time.Time      `json:"first_seen"`
    LastSeen   time.Time      `json:"last_seen"`
    Permalink  string         `json:"permalink,omitempty"`
    SampleEvent map[string]any `json:"sample_event,omitempty"`
}

type ListFilter struct {
    Status   string    // open|muted|resolved (default open)
    Service  string
    TenantID string
    Since    time.Time
    Limit    int
    Cursor   string
}

type Page struct {
    Groups     []Group
    NextCursor string
    HasMore    bool
}

type Update struct {
    Status string  // "resolved"|"muted"|"open"
}

type Capabilities struct {
    SupportsMute      bool   `json:"supports_mute"`
    SupportsResolve   bool   `json:"supports_resolve"`
    SupportsIngestion bool   `json:"supports_ingestion"`   // adapter accepts events written by runtime/agents
    SupportsAlerting  bool   `json:"supports_alerting"`
    RetentionDays     int    `json:"retention_days"`
}

type Store interface {
    ListGroups(ctx context.Context, f ListFilter) (Page, error)
    GetGroup(ctx context.Context, id string) (Group, error)
    UpdateGroup(ctx context.Context, id string, u Update) error
    Capabilities() Capabilities
}
```

### 5.2 Default builtin: log-filter

```go
// services/runtime/internal/observability/errors/logfilter/logfilter.go
type LogFilterStore struct { logs logs.Store }
// Groups = error-level entries fingerprinted by (service, top stack frame).
// Mute/resolve = in-process map (volatile).
```

This keeps today's behaviour as the default, no regression.

### 5.3 First real adapter: GlitchTip

**File**: `services/runtime/internal/observability/errors/adapters/glitchtip/glitchtip.go` (new)

GlitchTip's API (Sentry-compatible) — corrected to canonical Sentry paths:

| GlitchTip endpoint | Used by Store method |
|---|---|
| `GET /api/0/organizations/{org}/issues/?query=is:unresolved&statsPeriod=&limit=&cursor=` | `ListGroups` (org-scoped list IS necessary for filtering by org) |
| `GET /api/0/issues/{id}/` | `GetGroup` (no org slug — canonical Sentry path) |
| `PUT /api/0/issues/{id}/` | `UpdateGroup` (body: `{"status":"resolved"|"ignored"|"unresolved"}`) |
| `GET /api/0/projects/` | future autocomplete |

Status translation in the GlitchTip adapter:

```go
// internal -> GlitchTip
switch update.Status {
case "muted":     payload.Status = "ignored"
case "resolved":  payload.Status = "resolved"
case "open":      payload.Status = "unresolved"
}
// GlitchTip -> internal (when reading)
switch g.Status {
case "ignored":    out.Status = "muted"
case "resolved":   out.Status = "resolved"
case "unresolved": out.Status = "open"
}
```

**Auth**: `Authorization: Bearer <token>` (GlitchTip auth token issued by operator).

**Env vars** (corrected):

- `AF_STACK_ERRORS_ADAPTER` — `logfilter` (default) | `glitchtip` | `remote`
- `AF_STACK_ERRORS_GLITCHTIP_URL` — e.g., `http://glitchtip:8000`
- `AF_STACK_ERRORS_GLITCHTIP_ORG` — organisation slug
- `AF_STACK_ERRORS_GLITCHTIP_TOKEN` — bearer (managed via Setup → Secrets)
- `SENTRY_DSN` — the WRITE-path: runtime + agents read this to initialise Sentry SDK and report errors to GlitchTip

**Net-new dependencies for write side**:

- Go runtime: add `github.com/getsentry/sentry-go` to `go.mod`. Initialise in `internal/observability/observability.go` when `SENTRY_DSN` is set. Hook into the existing slog handler so error-level logs become Sentry events.
- Python agents: add `sentry-sdk>=2.0` to each agent's `requirements.txt` / `pyproject.toml` (`apps/backend/agents/supportdesk/`, etc.). Initialise in each agent's entry point.

Without the SDKs, GlitchTip stays empty even if the read-side adapter is configured. Plan ~0.5 day for SDK wiring across all containers.

### 5.4 Remote-shim contract

**File**: `docs/adapters/protocols/errors-v1.md` (new)

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/errors/list` | body=`ListFilter`, response=`Page` |
| `GET` | `/v1/errors/{group_id}` | response=`Group` |
| `PATCH` | `/v1/errors/{group_id}` | body=`Update` |
| `GET` | `/v1/capabilities` | declare |
| `GET` | `/healthz` | health |

### 5.5 Admin endpoints

| Endpoint | Maps to |
|---|---|
| `GET /api/v1/admin/errors?status=open&limit=` | `errorsStore.ListGroups` |
| `GET /api/v1/admin/errors/{id}` | `errorsStore.GetGroup` |
| `POST /api/v1/admin/errors/{id}/mute` | `errorsStore.UpdateGroup(status=muted)` |
| `POST /api/v1/admin/errors/{id}/resolve` | `errorsStore.UpdateGroup(status=resolved)` |
| `POST /api/v1/admin/errors/{id}/reopen` | `errorsStore.UpdateGroup(status=open)` |

### 5.6 Conformance harness

Extend with `runErrorsChecks(...)`:
- `POST /v1/errors/list` with empty filter returns a `Page`
- Capabilities declares either `supports_mute` or `supports_resolve`

### 5.7 Admin UI surface

- `Operate → Errors` (existing): data source switches to `/api/v1/admin/errors`. Adapter pill: "via GlitchTip" / "via log-filter (builtin)". Mute/resolve row actions wire to the new endpoints.

### 5.8 Files

```
NEW:
  docs/adapters/protocols/errors-v1.md
  services/runtime/internal/observability/errors/interface.go
  services/runtime/internal/observability/errors/logfilter/logfilter.go
  services/runtime/internal/observability/errors/logfilter/logfilter_test.go
  services/runtime/internal/observability/errors/adapters/glitchtip/glitchtip.go
  services/runtime/internal/observability/errors/adapters/glitchtip/glitchtip_test.go
  services/runtime/internal/observability/errors/adapters/remote/remote.go
  services/runtime/internal/observability/errors/adapters/remote/remote_test.go
  services/runtime/internal/server/admin_errors.go
MODIFIED:
  services/runtime/cmd/af-stack/main.go
  services/runtime/cmd/backai-adapter-conformance/main.go
  docs/adapters/README.md
  docs/adapters/PROTOCOL.md (§14)
  apps/dashboard/src/lib/api.ts
```

> **Note on write-side**: when `SENTRY_DSN` is set, runtime + agents initialise the Sentry SDK and report errors automatically. The SDK works against any Sentry-compatible backend (GlitchTip, Sentry self-hosted, BugSink, etc.) — write-path is decoupled from the read-side adapter.

---

## Block 6 — Aggregation endpoints · **~3–4 days** (revised up from 2d)

Four aggregation endpoints. Two of them require new DB schema + write-path hooks, not just read-side aggregation. Search-indexes was moved to Block 1.5 (was duplicate).

### 6.1 `GET /api/v1/reasoners/analytics` (LARGER THAN BILLED)

Cross-agent reasoner stats: cost, latency, error rate, call count, top callers.

**HAZARD (audit)**: `suite_cost_events` has columns for `model`, `provider`, `agent` but **NO `reasoner` column** (migration `00005_cost.sql:20-34`). The earlier doc claimed "where agent + reasoner are tagged" — false.

**Correct approach**:

1. Migration: `ALTER TABLE suite_cost_events ADD COLUMN reasoner text`.
2. Hook in `internal/cost/recorder.go`: when AgentField returns the reasoner path on a run, write the leaf reasoner name to the cost-event row.
3. Hook in agent containers (Python AgentField SDK): pass `reasoner` in the LLM call context so the gateway can stamp it on the cost event.
4. Aggregation handler queries grouped by `(agent, reasoner, time-bucket)`.

Admin lands: enrichment on `Build → Reasoners` page.

**Effort**: ~1 day (migration + recorder + handler + dashboard column).

### 6.2 `GET /api/v1/tools/usage` (LARGER THAN BILLED)

Native + MCP tool usage: call frequency, error rate, average duration, top callers.

**HAZARD (audit)**: no `suite_tool_calls` log table exists. Only config tables (`suite_tool_adapters`, `suite_tenant_tools`). The earlier doc said "aggregated over tool invocation logs" — those logs don't exist.

**Correct approach**:

1. Migration: `suite_tool_calls (id, tenant_id, agent_id, tool_name, transport ['native'|'mcp'], duration_ms, status, error_code, called_at)`.
2. Hook in `internal/tools/registry.go: (Registry).Call` and `internal/mcp/bridge.go: (Bridge).Call` to INSERT a row on every invocation (best-effort, async, never blocks the call).
3. Aggregation handler queries grouped by `(tool_name, transport, time-bucket)`.

Admin lands: `Build → Tools` page.

**Effort**: ~1 day (migration + 2 hooks + handler + dashboard columns).

### 6.3 ~~Search indexes~~ — moved to Block 1.5.6

Was a duplicate. Block 1.5 handles it.

### 6.4 Notification channels CRUD

`GET / POST / PATCH / DELETE /api/v1/notifications/channels` — durable channel configuration (replacing env-only).

**Approach** (audit-corrected): channels today are configured via env in `notifications/factory.go`. This block needs:

1. Migration: `suite_notification_channels (id, kind, config_json, enabled, created_at, updated_at)`.
2. Refactor `notifications/factory.go` to merge env defaults with DB rows at boot AND on config-reload signal.
3. New handlers + audit on each mutation.

**Effort**: ~0.5 day.

### 6.5 `GET /api/v1/oauth/refresh-history?provider=&tenant_id=&limit=`

**HAZARD (audit)**: `suite_oauth_tokens` is **current-state only** — no history of refresh attempts. The earlier doc said "recent OAuth refresh attempts and outcomes" — those records don't exist.

**Correct approach**:

1. Migration: `suite_oauth_refresh_log (id, tenant_id, provider, user_id, status ['success'|'failed'], error_code, attempted_at)`.
2. Hook in `internal/oauth/manager.go` on every refresh attempt (success + failure).
3. Read handler returns last N rows filtered by provider/tenant.

Admin lands: `Customers → OAuth connections` row drawer.

**Effort**: ~0.5 day.

### 6.6 Frontend wiring

Each of the above is a single new column / panel / drawer-tab on the existing page. Pattern: api.ts method + settle() + snapshot map + page-model edit. No new components.

---

## Block 7 — Polish · **~1 day** (lighter than billed in earlier draft)

Cross-cutting refinements. Audit revealed two of three items are mostly already wired.

### 7.1 Adapter pill on every adapter-backed page (mostly done)

**Already rendered** at `apps/dashboard/src/components/new-admin/operator-page.tsx:95-100` — reads `model.adapter`. Work in this block is:

1. Populate `model.adapter` on the page-model entries for the new adapter-backed pages (Logs, Traces, Errors, Cost, Health). Each is ~5 lines.
2. Clicking the pill opens the Connected Services row for that slot — needs a small router action.

### 7.2 Home page — Connected Services strip (mostly done)

`snapshot.services` is ALREADY consumed by the Home entry in `page-model.ts:168`. Today it reads `seededServices` from `data.ts:437`. This block:

1. Confirms Block 1.2's `/api/v1/admin/services` endpoint returns a real list.
2. The strip lights up automatically once the snapshot wires through.

### 7.3 Capability-honest UI degradation (genuinely new infrastructure)

Audit confirmed there are no existing `if (capabilities.supports_X)` patterns in the new admin. This block adds:

1. A `useCapability(slot, feature)` hook in `apps/dashboard/src/lib/new-admin/capabilities.ts`.
2. Convention: every page that depends on an adapter capability checks it at render and shows a neutral "configure backend X to enable" notice when the capability is absent.

Example: when `metrics.Store.Capabilities().SupportsRange` is false, the Cost page hides the chart panel and shows a small "configure a metrics backend" hint.

---

## Block 8 — Remaining unmapped gap indicators · **~1 day** (NEW)

Audit extracted 24 static `kpi(..., "missing"|"gap"|"deferred", ...)` indicators from `page-model.ts`. Blocks 1-7 cover 21 of them. The remaining 3 land here.

### 8.1 `/build/harnesses` — "Disable" KPI

Per-agent disable of harness probing. Today's harness probe runs across all configured agents indiscriminately.

**Endpoint**: `POST /api/v1/harnesses/{provider}/disable` — body: `{agent_id}` ; idempotent.

**Files**: `internal/harnesses/manager.go` (track disabled set), `internal/server/harnesses.go`, dashboard adds row action.

### 8.2 `/build/modules` — "Migrations" KPI

Surface which workload-module migrations are applied vs pending. Today module CRUD writes config but doesn't expose migration state.

**Endpoint**: `GET /api/v1/modules/migrations` — returns per-module `{applied: [...], pending: [...]}`.

**Files**: `internal/modules/loader.go` (introspect migration manifest), `internal/server/modules.go`.

### 8.3 `/build/data/sql` — "History" KPI

Persist a per-operator history of SQL queries (currently UI-only local state in the dashboard).

**Endpoint**: `GET /api/v1/db/sql/history?limit=` + `POST /api/v1/db/sql/history` (record-on-execute).

**Files**: migration `suite_sql_history (id, user_id, query, executed_at)`, `internal/server/db_sql.go` (record after each successful query).

---

## Effort summary (audit-corrected)

| Block | Pre-audit days | **Post-audit days** | Driver of change |
|---|---|---|---|
| 1 — Endpoint additions | 2 | **3–4** | Cache API extension; rotate serialization; notification-mute new table; brand build-time reconciliation; pg_stat_statements postgres config |
| 2 — `logs` adapter slot | 2 | **2.5** | Ring needs new Subscribe/channel layer; WebSocket dep |
| 3 — `traces` adapter slot | 2 | **2.5** | Tempo decoder shape variations; tags translator fix |
| 4 — `metrics` adapter slot | 2 | **2** | Mostly correction (env var, cAdvisor metric names, provider-health source); +0 net days because adding app metrics was implicit |
| 5 — `errors` adapter slot | 2 | **3** | Sentry SDK wiring in runtime + each Python agent |
| 6 — Aggregation endpoints | 2 | **3–4** | reasoner column + tools log table + OAuth refresh log are new migrations + write-path hooks |
| 7 — Polish | 1 | **1** | Lighter than billed but capability hook is real |
| 8 — Unmapped gap indicators | (new) | **1** | 3 small gaps not covered by Blocks 1-7 |
| **Total** | **13** | **~18–19** | |

Each block is one PR. Blocks 2–5 share the same adapter-slot scaffolding pattern (Go interface + builtin + first concrete adapter + remote shim + protocol doc + admin endpoint + conformance check + dashboard wiring).

---

## Pattern checklist (for each adapter-slot block)

A block isn't done until every box is ticked.

- [ ] Go interface defined in `services/runtime/internal/observability/<slot>/interface.go`
- [ ] Default builtin implementation in `<slot>/<default-name>/`
- [ ] First concrete backend adapter in `<slot>/adapters/<backend>/`
- [ ] Remote-shim implementation in `<slot>/adapters/remote/` using shared `internal/adapters/remote.Client`
- [ ] Per-slot HTTP protocol doc in `docs/adapters/protocols/<slot>-v1.md` (matching existing 8 slots' shape)
- [ ] Slot row added to `services/runtime/internal/adapters/registry/` (in `main.go` wiring)
- [ ] Admin HTTP endpoints in `services/runtime/internal/server/admin_<slot>.go`
- [ ] OpenAPI registration
- [ ] Conformance harness extended with per-slot checks in `services/runtime/cmd/backai-adapter-conformance/main.go`
- [ ] Unit tests for the backend adapter (httptest mocks)
- [ ] Dashboard `api.ts` switched from old endpoint to new `/admin/<slot>/...`
- [ ] Adapter pill rendered on the page header
- [ ] Capability-honest degradation when builtin is active
- [ ] Slot row in `docs/adapters/README.md` and `docs/adapters/PROTOCOL.md` §14

---

## Out of scope (do not implement in v1)

| Concern | Reason |
|---|---|
| LLM-specific observability (Langfuse / Helicone / OpenLIT) | Covered by generic Logs + Traces + Errors |
| River UI (embedded job queue UI) | Existing `Operate → Queue` page is sufficient |
| Sentry self-hosted | GlitchTip covers the API; lighter footprint |
| Full container management UI (Portainer / Dozzle) | cAdvisor metrics + Connected Services links cover it |
| Compose / deployment mechanism | Operator chooses how to bring up Loki / Tempo / Prometheus / GlitchTip / Vector / otel-collector / cAdvisor; the runtime is fully env-var-driven |
| pgHero | DB Health tab on Build → Data → SQL covers the same need |
| Session adapter capabilities (full enumeration / impersonation) | Auth slot interface needs extension first |
| Feature flag override history endpoint | Deferred |
| Deploy-target provisioning | Out of scope for v1 |

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

---

## 0. Locked decisions (do not relitigate)

1. **Every new observability layer is a Tier-1 adapter slot** following the existing 8-slot scaffolding (Go interface + per-slot HTTP protocol + remote-shim + registry row + conformance harness check). Same modularity rule: third parties can ship alternative backends.
2. **OSS service deployment is out of scope here.** Operators bring up Loki / Tempo / Prometheus / GlitchTip / Vector / otel-collector / cAdvisor via their own chosen mechanism. The runtime is configured purely via env vars — it doesn't care how the backend got there.
3. **No new admin nav items.** Every backend gap closes against an existing page via tab / section / data-source swap.
4. **One central "Connected services" hub** at `Operate → Health`. The only place "Open native OSS UI" buttons exist. Per-page deep links exist only for *specific entities* (e.g., "Open this run in AgentField").
5. **Admin never invokes SDK-class paths.** Admin reads aggregates from durable tables; "test" actions dispatch through SDK paths with operator credentials.

---

## Block 1 — Quick wins (no new OSS) · ~2 days

Closes 9 known gaps without touching the adapter system or introducing new dependencies.

### 1.1 Wire `/api/v1/admin/adapters` handler

The handler exists in `services/runtime/internal/adapters/registry/handler.go` but isn't mounted.

**Change**: in `services/runtime/cmd/af-stack/main.go`, add

```go
mux.Handle("GET /api/v1/admin/adapters", registry.Handler(rt.adapterRegistry))
```

**Verification**: `curl http://localhost:8080/api/v1/admin/adapters` returns the slot inventory. Setup → Adapters page in the dashboard light up.

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
- adapter registry (`internal/adapters/registry/`)
- env vars for observability backends (`AF_STACK_LOGS_LOKI_URL`, `AF_STACK_TRACES_TEMPO_URL`, `AF_STACK_METRICS_PROMETHEUS_URL`, `AF_STACK_ERRORS_GLITCHTIP_URL`)
- the runtime's own self-info

If an env var is unset, the corresponding service is absent from the list. The UI gracefully degrades.

**Status probe**: each service has a fast `/healthz`-style check; results cached 30s.

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

**Required PG extension**: `pg_stat_statements`. Runtime checks `pg_extension` at boot; if missing, endpoint returns `{"available": false, "reason": "..."}` and the dashboard renders an "extension needed" notice.

### 1.4 LLM provider availability poller

Background goroutine polls LiteLLM's `/health` every 60s, writes to a new table `suite_provider_health_log` (provider, status, latency_ms, observed_at).

**Files**:
- `services/runtime/internal/db/migrations/<n>_provider_health_log.sql`
- `services/runtime/internal/llmgateway/health_poller.go`
- `services/runtime/internal/server/llm_provider_health.go`

**Endpoint**: `GET /api/v1/admin/llm/provider-health?window=24h` — returns availability % and latency histogram per provider.

**UI surface**: section on the Connected Services page.

### 1.5 Single-shot endpoints (closes 6 specific admin UI gaps)

| Endpoint | What it does | File |
|---|---|---|
| `POST /api/v1/crons/{id}/trigger` | Manual cron execution | `internal/server/crons.go` |
| `POST /api/v1/llm/cache/flush` (+ optional `?tenant=` `?prompt_hash=`) | Flush LLM cache | `internal/server/llm.go` |
| `POST /api/v1/admin/keys/{id}/rotate` | Native key rotation (today: revoke + re-issue) | `internal/server/admin_keys.go` |
| `POST /api/v1/notifications/{id}/mute` | Mute future notifications matching pattern | `internal/server/notifications.go` |
| `GET /api/v1/search/indexes` | FTS index stats (size, last vacuum, hit rate) | `internal/server/search.go` |
| `GET /api/v1/admin/brand` + optional `PUT` | Read/write `brand.yaml` | `internal/server/brand.go` |

All small. Each lands one UI action.

### 1.6 Dashboard wiring for Block 1

- `Operate → Health` is restructured as the Connected Services hub (reads `/api/v1/admin/services`).
- `Build → Data → SQL` gains a "Health" tab (reads `/api/v1/admin/db/health`).
- Connected Services page shows LLM provider availability section (reads `/admin/llm/provider-health`).
- Row actions wire up on Crons / Cache / API keys / Notifications / Search.
- Brand page becomes editable.

**Verification**: every page listed in `admin-api-gap-registry-v1.md` that has a "missing endpoint" call-out flips to `backed`.

---

## Block 2 — `logs` adapter slot · ~2 days

**Why this slot**: today logs are a 2048-line in-memory ring inside the runtime process. We can't see agent-container logs, customer-app logs, LiteLLM logs, or anything past 2048 entries. Operators running real workloads need cross-service log query.

**Approach**: introduce a `logs` adapter slot following the existing 8-slot pattern. The default builtin is the current ring buffer (no behaviour change). The first real adapter is **Loki** (queried via its HTTP API). Third parties can ship alternative backends (Elasticsearch, Quickwit, Datadog) using the remote-shim contract.

### 2.1 Go interface

**File**: `services/runtime/internal/observability/logs/interface.go` (new)

```go
package logs

import (
    "context"
    "time"
)

// Entry is one log line, normalised across backends.
type Entry struct {
    TS        time.Time      `json:"ts"`
    Level     string         `json:"level"`     // "debug"|"info"|"warn"|"error"|"fatal"
    Service   string         `json:"service"`   // "runtime"|"agentfield"|"litellm"|"supportdesk-agent"|...
    Message   string         `json:"message"`
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

The existing `internal/logger.Ring` becomes the `RingStore` impl satisfying `logs.Store`. No change in write path (slog still writes to the ring). The Store wraps the ring.

```go
// services/runtime/internal/observability/logs/ring/ring.go
type RingStore struct { ring *logger.Ring }
func (s *RingStore) Query(ctx, f Filter) (Page, error) { ... iterate ring with filter ... }
func (s *RingStore) Tail(ctx, f Filter) (<-chan Entry, error) { ... subscribe to ring ... }
func (s *RingStore) Capabilities() Capabilities { return ... }
```

`SupportsFullText: true` (we already do substring), `NativeQueryLang: ""`, `RetentionDays: 0` (volatile), `MaxEntriesPerPage: 1000`.

### 2.3 First real adapter: Loki

**File**: `services/runtime/internal/observability/logs/adapters/loki/loki.go` (new)

Loki's relevant HTTP API:

| Loki endpoint | Used by Store method |
|---|---|
| `GET /loki/api/v1/query_range?query=&start=&end=&limit=&direction=backward` | `Query` (LogQL filter built from `Filter`) |
| `GET /loki/api/v1/tail?query=&delay_for=&limit=&start=` (WebSocket) | `Tail` — adapter consumes WebSocket, re-emits as Go channel |
| `GET /ready` | health probe |
| `GET /loki/api/v1/labels?start=&end=` | future autocomplete (not in v1 interface) |

**LogQL translation rule**:

```go
func filterToLogQL(f Filter) string {
    // {service=~"a|b|c"} |~ "search" | level=~"warn|error" | tenant_id="..."
    var labels []string
    if len(f.Services) > 0 { labels = append(labels, `service=~"`+strings.Join(f.Services,"|")+`"`) }
    if f.TenantID != ""    { labels = append(labels, `tenant_id="`+f.TenantID+`"`) }
    if f.TraceID != ""     { labels = append(labels, `trace_id="`+f.TraceID+`"`) }
    selector := "{" + strings.Join(labels, ",") + "}"
    expr := selector
    if f.Search != "" { expr += ` |~ ` + strconv.Quote(f.Search) }
    if len(f.Levels) > 0 { expr += ` | level=~"` + strings.Join(f.Levels,"|") + `"` }
    return expr
}
```

**Capabilities**:

```go
Capabilities{
    SupportsTail: true,
    SupportsFullText: true,
    SupportsTraceID: true,
    NativeQueryLang: "logql",
    RetentionDays: probeRetention(),  // poll Loki /config endpoint
    MaxEntriesPerPage: 5000,
}
```

**Env vars consumed**:

- `AF_STACK_LOGS_BACKEND` — `ring` (default) | `loki` | `remote`
- `AF_STACK_LOGS_LOKI_URL` — e.g., `http://loki:3100`
- `AF_STACK_LOGS_LOKI_TENANT` — optional `X-Scope-OrgID` for multi-tenant Loki

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

## Block 3 — `traces` adapter slot · ~2 days

**Why this slot**: the runtime has the OpenTelemetry SDK wired but nothing receives the spans. The admin's Traces page is empty. Operators need a span store + query.

**Approach**: `traces` adapter slot. Default builtin is empty (admin shows zero-state). First real adapter is **Tempo** (queried via its HTTP API; spans are pushed to it by otel-collector, configured separately by the operator).

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

**Env vars**:

- `AF_STACK_TRACES_BACKEND` — `empty` (default) | `tempo` | `remote`
- `AF_STACK_TRACES_TEMPO_URL` — e.g., `http://tempo:3200`
- `AF_STACK_TRACES_TEMPO_TENANT` — optional `X-Scope-OrgID`

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

**Env vars**:

- `AF_STACK_METRICS_BACKEND` — `none` (default) | `prometheus` | `remote`
- `AF_STACK_METRICS_PROMETHEUS_URL` — e.g., `http://prometheus:9090`

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

- `Operate → Cost`: gains a time-series chart panel (spend over time, cache savings, model mix).
- `Operate → Health → Connected Services`: gains a "Containers" subsection rendering per-container CPU / mem / restart sparklines, sourced from a curated set of PromQL queries (`rate(container_cpu_usage_seconds_total[5m])`, `container_memory_usage_bytes`, `kube_pod_container_status_restarts_total`).
- `Operate → Health → LLM Providers`: latency / availability sparkline using metrics from the provider-health poller.

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

## Block 5 — `errors` adapter slot · ~2 days

**Why this slot**: today's "errors" page client-side filters logs by level. There's no deduplication, no grouping, no resolution state, no alerting. Operators need a Sentry-shaped error tracker.

**Approach**: `errors` adapter slot. Default builtin is the current log-filter aggregation. First real adapter is **GlitchTip** (Sentry-API-compatible, AGPL/MIT, lightweight). Runtime + agents push events via the standard Sentry SDK; admin reads grouped issues via GlitchTip's REST API.

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

GlitchTip's API (Sentry-compatible):

| GlitchTip endpoint | Used by Store method |
|---|---|
| `GET /api/0/organizations/{org}/issues/?query=&statsPeriod=&limit=&cursor=` | `ListGroups` (filter mapped to `query`: `is:unresolved` etc.) |
| `GET /api/0/organizations/{org}/issues/{id}/` | `GetGroup` |
| `PUT /api/0/organizations/{org}/issues/{id}/` | `UpdateGroup` (body: `{"status":"resolved"|"ignored"|"unresolved"}`) |
| `GET /api/0/projects/` | future autocomplete |

**Auth**: `Authorization: Bearer <token>` (GlitchTip auth token issued by operator).

**Env vars**:

- `AF_STACK_ERRORS_BACKEND` — `logfilter` (default) | `glitchtip` | `remote`
- `AF_STACK_ERRORS_GLITCHTIP_URL` — e.g., `http://glitchtip:8000`
- `AF_STACK_ERRORS_GLITCHTIP_ORG` — organisation slug
- `AF_STACK_ERRORS_GLITCHTIP_TOKEN` — bearer (managed via Setup → Secrets)
- `SENTRY_DSN` — the WRITE-path: runtime + agents read this to initialise Sentry SDK and report errors to GlitchTip

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

## Block 6 — Aggregation endpoints · ~2 days

Six small endpoints that close the "derived" rows in the gap registry. None require new OSS; all aggregate from durable tables.

### 6.1 `GET /api/v1/reasoners/analytics`

Cross-agent reasoner stats: cost, latency, error rate, call count, top callers. Aggregated over `suite_cost_events` (where `agent` + `reasoner` are tagged) joined with `suite_runs`.

Admin lands: enrichment on `Build → Reasoners` page (existing).

### 6.2 `GET /api/v1/tools/usage`

Native + MCP tool usage: call frequency, error rate, average duration. Aggregated over tool invocation logs.

Admin lands: `Build → Tools` page.

### 6.3 `GET /api/v1/search/indexes`

FTS index stats: size, last vacuum, hit rate. PG queries against `pg_indexes` + `pg_stat_user_indexes`.

Admin lands: `Build → Data → Search` page.

### 6.4 Notification channels CRUD

`GET / POST / PATCH / DELETE /api/v1/notifications/channels` — durable channel configuration (replacing env-only display).

Admin lands: `Setup → Notifications` page.

### 6.5 `GET /api/v1/oauth/refresh-history?provider=&tenant_id=&limit=`

Recent OAuth refresh attempts and outcomes.

Admin lands: `Customers → OAuth connections` row drawer.

### 6.6 Frontend wiring

Each of the above is a single new column / panel / drawer-tab on the existing page.

---

## Block 7 — Polish · ~1 day

Cross-cutting refinements.

### 7.1 Adapter pill on every adapter-backed page

A small badge top-right of the page header: `via Loki` / `via Tempo` / `via Prometheus` / `via GlitchTip` / `via builtin ring` / `via remote: my-sidecar`. Clicking opens the Connected Services row for that slot.

Affected pages: Operate → Logs, Traces, Errors, Cost (when metrics slot active), Health.

### 7.2 Home page — Connected Services strip

Compact strip at the top showing the 4 most-critical service statuses (postgres, litellm, agentfield, runtime). Pulls `/api/v1/admin/services`.

### 7.3 Capability-honest UI degradation

Every page that depends on an adapter capability checks it on load and renders a clear, neutral notice when the capability is absent. No spinning loaders that never resolve.

Example: when `metrics.Store.Capabilities().SupportsRange` is false, the Cost page hides the chart panel and shows a small "configure a metrics backend" hint linking to docs.

---

## Effort summary

| Block | Days |
|---|---|
| 1 — Quick wins | 2 |
| 2 — `logs` adapter slot | 2 |
| 3 — `traces` adapter slot | 2 |
| 4 — `metrics` adapter slot | 2 |
| 5 — `errors` adapter slot | 2 |
| 6 — Aggregation endpoints | 2 |
| 7 — Polish | 1 |
| **Total** | **13 days** |

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

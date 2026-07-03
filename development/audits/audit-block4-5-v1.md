# Audit — Blocks 4 (`metrics`) & 5 (`errors`)

Findings against actual codebase. File paths absolute. Line refs from current `supportdesk-first-dx` branch.

---

## Block 4 — `metrics` adapter slot

### 4.1 `/metrics` endpoint exists?

**Confirmed.** Mounted at `services/runtime/internal/server/server.go:776`:

```go
s.mux.Handle("GET /metrics", s.tel.MetricsHandler())
```

Handler defined at `services/runtime/internal/observability/observability.go:116`. But — **only Go runtime + process collectors are registered** (`observability.go:76-80`: `NewGoCollector` + `NewProcessCollector`). No app-specific metrics (no LLM call counters, request histograms, cost gauges, etc.). The claim "the runtime already exposes `/metrics` (`client_golang`)" is technically true but understates the gap: without app-level instrumentation, a Prometheus adapter has very little BackAI-specific data to chart. Doc should note that app instrumentation is a prerequisite for the proposed Cost-over-time chart.

### 4.2 Sensible default builtin (other than "none")

**Mild contradiction.** The runtime *does* hold its own `prometheus.Registry` (`observability.go:44, 76`). An in-process builtin that ran PromQL against the local registry would be cheaper than punting to `none`. But `client_golang` ships no embedded PromQL engine — only registry + exposition. To answer PromQL queries in-process requires an extra dep (e.g., `github.com/prometheus/prometheus/promql` — heavyweight). Conclusion: the doc's choice of `none` as the default builtin is defensible — it just deserves a one-line justification ("no embedded PromQL engine; would require pulling in `prometheus/prometheus`").

### 4.3 Prometheus API claims

- `time` parameter: Prometheus accepts **Unix timestamp (float, optionally with fractional ms) OR RFC3339**. Doc is silent — should specify "Unix timestamp; RFC3339 also accepted".
- `step` parameter: accepts **duration string (`30s`, `1m`) OR float seconds**. Doc is silent.
- Response shape: doc does not declare it. Real shape is `{"status":"success","data":{"resultType":"vector"|"matrix"|"scalar"|"string","result":[...]}}`. The adapter must handle all four `resultType` values. The proposed `InstantSample` / `RangeSample` shapes assume vector/matrix only — fine, but the protocol doc should declare the mapping explicitly.

### 4.4 cAdvisor metric names

**Block 4 is wrong on one claim.** `execution-blocks-v1.md:667`:

> `rate(container_cpu_usage_seconds_total[5m])`, `container_memory_usage_bytes`, `kube_pod_container_status_restarts_total`

- `container_cpu_usage_seconds_total` — correct, cAdvisor.
- `container_memory_usage_bytes` — correct, cAdvisor.
- `kube_pod_container_status_restarts_total` — **from kube-state-metrics, not cAdvisor.** In a non-k8s Compose deployment (which is the local-dev target per `platform/CLAUDE.md`), this series will not exist. cAdvisor exposes `container_start_time_seconds` per container; restart count must be derived (`changes(container_start_time_seconds[1h])`) or pulled from a different source.

Fix: either drop the restart metric for the v1 Compose case, replace with `changes(container_start_time_seconds[<window>])`, or scope the restart claim to "k8s deployments with kube-state-metrics".

### 4.5 Provider-availability PromQL contradiction

**Real contradiction.** Block 1 (`execution-blocks-v1.md:130-138`) defines `suite_provider_health_log` as a **Postgres** table. Block 4 (`execution-blocks-v1.md:668`) then says:

> Latency / availability sparkline using metrics from the provider-health poller.

…in a section whose data source is PromQL against Prometheus. PromQL can't query Postgres. Two coherent paths:

1. Have the poller also export gauges into the runtime's Prometheus registry (e.g., `backai_provider_health{provider="..."} 0|1` + `backai_provider_latency_seconds{provider="..."}`). Then PromQL works.
2. Keep it Postgres-only — render the sparkline from `/api/v1/admin/llm/provider-health` (Block 1.4) directly, no metrics adapter involvement.

Doc should pick one. Option 1 has the side benefit of providing real app-level metrics for the broader claim in 4.1.

### 4.6 Env var naming consistency

**Inconsistent with existing pattern.** Existing slots use `_ADAPTER` (`af-stack/main.go:183, 212, 228, 244, 278, 305, 328, 346`):

- `AF_STACK_S3_ADAPTER`, `AF_STACK_SANDBOX_ADAPTER`, `AF_STACK_LLM_GATEWAY_ADAPTER`, `AF_STACK_NOTIFICATIONS_ADAPTER`, `AF_STACK_BILLING_ADAPTER`, `AF_STACK_SECRETS_ADAPTER`, `AF_STACK_AUTH_ADAPTER`, `AF_STACK_MULTIMODAL_ADAPTER`.

Blocks 2–5 use `_BACKEND` (`AF_STACK_METRICS_BACKEND`, `AF_STACK_ERRORS_BACKEND`, `AF_STACK_LOGS_BACKEND`, `AF_STACK_TRACES_BACKEND`). Suggest renaming to `_ADAPTER` for consistency, or document the rationale for the new pattern.

### 4.7 cAdvisor dependency disclosure

**Yes, doc should call this out.** Without an operator-configured Prometheus that scrapes cAdvisor (and ideally kube-state-metrics for k8s), the "per-container CPU / mem / restart sparklines" section degrades to "configure backend" notices. The doc already says deployment is out of scope (line 21) but should be explicit that the Connected Services container-metrics sub-panel requires a specific scrape config — not just "any Prometheus".

---

## Block 5 — `errors` adapter slot

### 5.1 "Current log-filter aggregation" — does it exist?

**Partially true.** Today's errors page is **client-side** filter, not server-side. See `apps/dashboard/src/lib/new-admin/data.ts:1158-1166`:

```ts
errors: logRows.filter((line) => line.status === "error").slice(0, 20).map(...)
```

And `page-model.ts:215-227`: KPI label "Endpoint gap: 1, admin errors endpoint, missing, fail". So there is no current Go-side "log-filter aggregation" — the builtin proposed in Block 5.2 (fingerprint by `(service, top stack frame)`, in-process mute/resolve map) is **net-new code**, not a refactor of existing logic. Doc should say "new builtin matching today's client-side behaviour" rather than implying it lifts existing code.

### 5.2 GlitchTip API claims

- `GET /api/0/organizations/{org}/issues/?query=...&statsPeriod=...&limit=...&cursor=...` — **correct**, GlitchTip mirrors Sentry's API. `cursor` pagination works (GlitchTip uses Sentry-style `Link` header cursors).
- `PUT /api/0/organizations/{org}/issues/{id}/` with `{"status":"resolved"|"ignored"|"unresolved"}` — **partially wrong.** Sentry/GlitchTip's actual canonical mutation is `PUT /api/0/issues/{id}/` (no organisation slug in path; some versions also accept the org-scoped path). Bulk update on issues uses `PUT /api/0/projects/{org}/{project}/issues/?id=<id>&id=<id>`. The status enum is `resolved | ignored | unresolved` — confirmed.
- Auth header: **`Authorization: Bearer <token>`** is correct for GlitchTip's auth tokens (user/org-scoped tokens issued in the GlitchTip UI). DSNs are only for the *ingest* (write) path, not the issues API.

### 5.3 SDK write-side wiring

**Net-new dependencies, not mentioned as such.**

- Go runtime: `grep sentry go.mod` → **no match.** `getsentry/sentry-go` is not currently a dependency. Adding it (plus its slog/http middleware) is part of Block 5's cost.
- Python agents: `apps/backend/pyproject.toml`, `apps/backend/agents/supportdesk/requirements.txt`, `apps/backend/agents/sample/requirements.txt` → **no `sentry-sdk` anywhere.**

The doc's note ("runtime + agents use Sentry SDK") implies pre-existing wiring. It's actually new work. Surface this in Block 5 acceptance criteria.

### 5.4 GlitchTip license

**Wrong / vague.** GlitchTip server is **MIT-licensed** (`https://gitlab.com/glitchtip/glitchtip-backend/-/blob/master/LICENSE` is MIT). The hosted-service trademark and some assets are restricted, but the codebase itself is not AGPL. Doc says "AGPL/MIT" — drop the AGPL.

### 5.5 Status mapping — "mute" → `ignored`

**Correct.** Sentry/GlitchTip statuses are `unresolved | resolved | ignored`. There is no `muted` status. The admin-side label "mute" mapping to `status=ignored` is the right call. Doc should be explicit that internal labels (`open|muted|resolved`) translate to Sentry semantics (`unresolved|ignored|resolved`).

### 5.6 Conformance harness extensibility

**Yes, additive.** `services/runtime/cmd/backai-adapter-conformance/main.go` is structured as:

```go
runCommonChecks(...)                 // line 57
runSandboxChecks(...)                // line 61
runStorageChecks(...)                // line 63
runNotificationsChecks(...)          // line 65
runSecretsChecks(...)                // line 67
runBillingChecks(...)                // line 69
runMultimodalChecks(...)             // line 71
runLLMChatChecks(...)                // line 73
runAuthChecks(...)                   // line 75
```

A `--slot` flag drives which checks run. Adding `runMetricsChecks` / `runErrorsChecks` (and `runLogsChecks` / `runTracesChecks` for Blocks 2/3) is mechanical. Note the existing harness actually has **8 slot check functions** in code, not 6 — recount in the doc if it claims "6".

---

## Cross-block

### Slot name collisions

Existing registered slot IDs (`af-stack/main.go:176, 205, 223, 239, 272, 299, 323, 341, 365, 383, 401, 418`): `storage, sandbox, llm-chat, multimodal, notifications, billing, secrets, auth, database, reasoning, job-queue, webhooks`. **No collision** with `metrics` or `errors` (or `logs` / `traces`).

### `apps/dashboard/src/lib/api.ts` collisions

`api.ts:1875-1892` currently defines `api.logs.list` against `/api/v1/logs` (no `/admin/`). No existing `/api/v1/admin/errors`, `/api/v1/admin/metrics/...`, `/api/v1/admin/traces` clients. Block 5 plan to point `api.errors.list` → `/api/v1/admin/errors` is clean. Doc should note the dashboard wiring change for Block 2 (logs) is a path change, not a new client.

### Current Operate → Errors page

`page-model.ts:215-227` + `data.ts:1158-1166` together: KPIs all flagged `derived`/`client`/`warn`/`missing`; rows are client-side log filter; mute/resolve are visual-only. The Block 5 backend endpoints close every KPI gap noted on that page (no leftover gap remains).

---

_Audit produced 2026-06-15 against branch `supportdesk-first-dx`._

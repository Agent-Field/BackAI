# Required Backend Gaps — Surfaced During UX Grooming

> Append-only ledger of backend gaps discovered while writing Page Briefs.
> Each row: what we need · why we need it · whether the data is computable
> today (just not exposed in the right shape) · suggested fix.
>
> Updated whenever a Page Brief surfaces a missing/insufficient endpoint.

---

## Severity legend

- **Blocking** — page can't ship without this. Backend work required.
- **Inefficient** — page works via multiple calls / client-side computation,
  but a single endpoint would be cleaner. Defer if needed.
- **Cosmetic** — minor improvement. Backlog.

---

## Gaps discovered

### Gap 1 — Live queue depth scalar
- **Surfaced by**: Page Brief — Home (KPI tile "Queue depth")
- **What we need**: A single integer for current queue depth (jobs queued
  right now).
- **What we have**: `home/overview.QueueSparkline` (24-hour hourly) and
  `GET /api/v1/queues/summary` (broader summary).
- **Data is computable**: YES — sparkline last bucket, or queues/summary
  total.
- **Severity**: Inefficient
- **Suggested fix**: Add `QueueDepth int` field to `homeOverviewResponse`
  (cheap query: `select count(*) from suite_jobs where status='queued'`).

### Gap 2 — Live running runs scalar
- **Surfaced by**: Home (KPI tile "Live runs")
- **What we need**: Current count of runs in `running` status.
- **What we have**: `GET /api/v1/runs?status=running` returns a list with
  total but requires a separate call.
- **Data is computable**: YES.
- **Severity**: Inefficient
- **Suggested fix**: Add `LiveRuns int` field to `homeOverviewResponse`.

### Gap 3 — Failed runs in last 24h scalar
- **Surfaced by**: Home (KPI tile "Failed 24h")
- **What we need**: Count of runs that failed in the last 24h.
- **What we have**: `ErrorSparkline` (24-hour buckets) sums to this; or
  filter `/api/v1/runs?status=failed&from=24h`.
- **Data is computable**: YES.
- **Severity**: Inefficient
- **Suggested fix**: Add `FailedRunsLast24h int` field to
  `homeOverviewResponse`, OR document that client sums `ErrorSparkline`.

### Gap 4 — Budget consumed % across all tenants
- **Surfaced by**: Home (KPI tile "Budget consumed %")
- **What we need**: Single percentage representing aggregate budget
  consumption across tenants (or a clear "tenants near cap" signal).
- **What we have**: `GET /api/v1/admin/budgets` returns per-tenant budgets
  + spend; client would have to fetch all and aggregate.
- **Data is computable**: YES (aggregate).
- **Severity**: Inefficient (could be Blocking if we want this tile on Home
  reliably).
- **Suggested fix**: Add `BudgetsAggregate { TenantsAtRisk int,
  AvgConsumedPct float }` to `homeOverviewResponse`; "at risk" = consumed
  >= alert_threshold.

### Gap 5 — Unified activity feed merge
- **Surfaced by**: Home (Activity feed section)
- **What we need**: A chronologically-ordered feed mixing: run completions,
  errors, tenant.created, budget alerts, config changes, deploys.
- **What we have**: Three separate sources — `home/overview.RecentRuns`,
  `home/overview.Alerts`, `GET /api/v1/activity` (customer-facing
  mutations only). No unified merge.
- **Data is computable**: YES — dashboard merges client-side, but it's
  expensive (3 calls + client-side sort + filtering).
- **Severity**: Inefficient
- **Suggested fix**: Either (a) extend `home/overview` to include a
  `RecentEvents []event` field that pre-merges these, or (b) add a
  dedicated `GET /api/v1/admin/events?limit=20` endpoint with a typed
  union of event shapes. (b) is more reusable.

### Gap 6 — Anchor live values unified endpoint
- **Surfaced by**: Home (top-bar anchor values: Inbox count, Cost daily,
  Health dot)
- **What we need**: Single endpoint serving the three persistent top-bar
  anchor values so every page can fetch them cheaply.
- **What we have**: Three separate endpoints —
  `/api/v1/approvals?status=pending` (Inbox count),
  `/api/v1/home/overview` (Cost daily), `/api/v1/admin/services`
  (Health summary).
- **Data is computable**: YES.
- **Severity**: Inefficient (matters because anchors are on EVERY page).
- **Suggested fix**: Add `GET /api/v1/admin/anchors` returning
  `{inbox_pending: int, cost_today_usd: float, health: "healthy"|"degraded"|"down"}`.
  WebSocket-pushable.

### Gap 8 — Unified Inbox endpoint
- **Surfaced by**: Page Brief — Inbox
- **What we need**: A single endpoint returning all pending-decision items
  across types (approvals, system alerts, budget alerts, error spikes,
  provider degraded).
- **What we have**: Two separate sources merged client-side:
  `GET /api/v1/approvals?status=pending` + `home/overview.Alerts`.
- **Data is computable**: YES — for v1 sources only (approvals + 2 system
  alerts).
- **Severity**: Inefficient (acceptable v1)
- **Suggested fix**: `GET /api/v1/admin/inbox` returning typed union
  `{kind, id, severity, title, context, age, actions, source}`. v0.2.

### Gap 9 — Inbox-emitted events (budget / error / provider / queue)
- **Surfaced by**: Page Brief — Inbox
- **What we need**: Runtime emitters that surface as Inbox items:
  - Budget threshold crossed (80% / 90% / 100%) per tenant
  - Error rate spike above baseline
  - LLM provider degraded transition
  - Queue backpressure (jobs stuck > N minutes)
- **What we have**: Underlying data exists (`admin/budgets`,
  `admin/llm/provider-health`, `queues/summary`) but no event emission
  feeding Inbox.
- **Data is computable**: YES — but requires emitter logic + persistence
  of "active alert" rows.
- **Severity**: **Blocking** for full Inbox value
- **Suggested fix**: Add `suite_inbox_items` table + emitter goroutines
  per signal type + `GET/POST /api/v1/admin/inbox` endpoints. v0.2.
- **v1 mitigation**: Ship Inbox with approvals + 2 system alerts only;
  brief notes the partial scope.

### Gap 10 — Acknowledge / dismiss mutation for non-approval items
- **Surfaced by**: Page Brief — Inbox
- **What we need**: `POST /api/v1/admin/inbox/{id}/acknowledge` so
  operator can dismiss a system alert without taking remedial action.
- **What we have**: Approvals have decide endpoint; system alerts in
  `home/overview.Alerts` re-compute on every call, so "acknowledging" them
  has no persistence.
- **Data is computable**: NO — requires persistence layer.
- **Severity**: **Blocking** if v1 ships system alerts as ack-able items.
- **Suggested fix**: Pairs with Gap 9 implementation.
- **v1 mitigation**: System alerts in v1 cannot be acknowledged — they
  vanish only when the underlying condition resolves. UI must communicate
  this.

### Gap 11 — Mobile single-item fetch by composite id
- **Surfaced by**: Page Brief — Inbox (Journey 6 mobile push deep link)
- **What we need**: Push notification deep-links to `/inbox/<item_id>`
  where `item_id` could be `approval:abc123` or `alert:xyz`. The mobile
  route fetches just that single item.
- **What we have**: `GET /api/v1/approvals/{id}` exists for approvals;
  no equivalent for system alerts.
- **Data is computable**: YES.
- **Severity**: **Blocking** for Journey 6 mobile path.
- **Suggested fix**: Either `GET /api/v1/admin/inbox/{kind}/{id}` or
  per-type GETs. Pairs with Gap 8.

### Gap 7 — Backing services strip "admin URL" field
- **Surfaced by**: Home (Backing services strip click → opens OSS UI)
- **What we need**: Each `adminService` row needs an `admin_url` so the
  dashboard knows where to send the operator on click.
- **What we have**: `adminService` exposes Host + Port. `appendEnvService`
  uses some `_UI_URL` env vars (e.g., `AF_STACK_PROMETHEUS_UI_URL`,
  `AF_STACK_GRAFANA_URL`) but not for all services. The slot-derived
  services from `adapterRegistry` may include `admin_ui` already; need to
  verify the JSON shape exposes it.
- **Data is computable**: YES — Host + Port + scheme assumption.
- **Severity**: Cosmetic for Block 1 services; Blocking for click-out UX.
- **Suggested fix**: Audit `adminService` JSON shape to confirm `AdminURL`
  field exists or add it. Default constructor for known OSS (`postgres` →
  none, `litellm` → `:4000/ui`, `agentfield` → `:8081`, `minio` →
  `:9001`, `svix` → `:8071`, etc.).

---

## How this doc is used

When a Page Brief surfaces a gap:
1. Add an entry here with the page that surfaced it.
2. Set severity.
3. Suggest the fix.
4. Continue grooming.

The backend team uses this list to prioritize Block-N additions. The UX
team uses this list to know what to mock vs. defer.

If a gap is **Blocking** and the backend team can't ship in v1 timeline,
the corresponding Page Brief should mark that surface as **TODO / deferred**
rather than over-promise in UI.

---

_Last updated: 2026-06-16. First populated during Home page grooming._

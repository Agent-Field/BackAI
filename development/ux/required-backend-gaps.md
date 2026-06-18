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

## Status legend

- ✅ **Closed** — backend shipped, frontend consuming.
- ⏸ **Deferred** — surfaces with a different page brief.
- ⬜ **Open** — still on the backlog.

## Snapshot after Home v1 (2026-06-17)

Gaps **1, 2, 3, 4, 5, 7** closed by the Home implementation. Webhook
deliveries on `home/overview` also wired (was the "Phase 10" placeholder).
Detail in `implementation-status.md`.

## Snapshot after Inbox v1 (2026-06-17)

Gap **6** closed (anchors extended with `inbox_has_critical`, count now
cross-tenant). Gaps **8, 9, 10, 11** explicitly deferred to v0.2 per the
brief's partial-scope plan, with v1 mitigations recorded in each entry
below.

## Snapshot after Cost v1 (2026-06-17)

Gaps **12–19** explicitly deferred to v0.2 per the Cost brief's
partial-scope plan. Each is recorded below with the v1 mitigation
(client-side heuristic, per-tenant fan-out, "v0.2 note inline", or
hidden surface). No new endpoints introduced; the existing
`/api/v1/cost`, `/api/v1/admin/budgets`, and `/api/v1/llm/cache/stats`
were sufficient.

## Snapshot after Health v1 (2026-06-17)

Gaps **20–24** explicitly deferred to v0.2 per the Health brief's
v1-SHIPS / v0.2-DEFERS table. v1 mitigation for each gap recorded
below (current-only language, hidden surfaces, static fallback link,
or "—" placeholders). No new endpoints — `/api/v1/admin/services`,
`/api/v1/admin/llm/provider-health`, `/api/v1/admin/db/health`, and
`/api/v1/metrics/summary` were all in place.

---

## Gaps discovered

### Gap 1 — Live queue depth scalar ✅ Closed (Home v1, 2026-06-17)
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
- **Shipped**: `homeOverviewResponse.QueueDepth` was already declared but
  unused; now populated from `s.jobs.Summary(ctx, 0).Pending` in
  `dashboard.go:handleHomeOverview`.

### Gap 2 — Live running runs scalar ✅ Closed (Home v1, 2026-06-17)
- **Shipped**: `homeOverviewResponse.LiveRuns` populated from
  `s.jobs.Summary().Running`. Field name is `live_runs` (not `live_jobs`)
  so the JSON shape matches the tile semantics.
- **Surfaced by**: Home (KPI tile "Live runs")
- **What we need**: Current count of background work actively executing.
- **DECISION (2026-06-16)**: Tile tracks **River background jobs in running
  state** (`s.jobs.Summary().Running`). Sync traffic is captured by
  Requests/min; queued async by Queue depth. Live runs = running async.
  Tooltip: "Background jobs currently being processed. For sync traffic
  see Requests/min."
- **What we have**: `s.jobs.Summary()` is callable internally; not exposed
  on `home/overview` as a scalar today.
- **Data is computable**: YES — `s.jobs.Summary().Running` is the source.
- **Severity**: Inefficient
- **Suggested fix**: Add `LiveJobs int` field to `homeOverviewResponse`
  sourced from `s.jobs.Summary().Running`. Tile displays this as "Live
  runs" with the tooltip above.

### Gap 3 — Failed runs in last 24h scalar ✅ Closed (Home v1, 2026-06-17)
- **Shipped**: `homeOverviewResponse.FailedRunsLast24h` populated by a
  direct count query on `suite_gateway_requests` scoped to
  `endpoint LIKE '/api/v1/execute/%'` so admin traffic doesn't pollute
  the run-failure signal.
- **Surfaced by**: Home (KPI tile "Failed 24h")
- **What we need**: Count of runs that failed in the last 24h.
- **What we have**: `ErrorSparkline` (24-hour buckets) sums to this; or
  filter `/api/v1/runs?status=failed&from=24h`.
- **Data is computable**: YES.
- **Severity**: Inefficient
- **Suggested fix**: Add `FailedRunsLast24h int` field to
  `homeOverviewResponse`, OR document that client sums `ErrorSparkline`.

### Gap 4 — Budget consumed % across all tenants ✅ Closed (Home v1, 2026-06-17)
- **Shipped**: `homeOverviewResponse.BudgetsAggregate { tenants_at_risk,
  avg_consumed_pct, tenant_count }` computed by `computeBudgetsAggregate`
  in `dashboard.go`. "At risk" = `spent / monthly_usd * 100 >= alert_threshold_pct`,
  matching the gateway's own definition.
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

### Gap 5 — Unified activity feed merge ✅ Closed (Home v1, 2026-06-17)
- **Shipped**: New `GET /api/v1/admin/events?limit=N&kind=...` endpoint
  (`admin_events.go`). Server-side-merges runs, webhook deliveries,
  system alerts, and the activity log into a typed union sorted by
  `occurred_at DESC`. Decision: separate endpoint (option **b**) over
  extending `home/overview` (option a) so other surfaces (Activity page,
  Tenant detail) can reuse the same merged stream.
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

### Gap 6 — Anchor live values unified endpoint ✅ Closed (Inbox v1, 2026-06-17)
- **Shipped**: `GET /api/v1/admin/anchors` returns
  `{inbox_pending, inbox_has_critical, cost_today_usd, health}`.
  `inbox_pending` now folds active system alerts into the tally and
  counts pending approvals cross-tenant via a short `app.bypass_rls=on`
  transaction (prior store call was tenant-scoped and silently returned
  zero). `inbox_has_critical` drives the badge colour.
- **Originally deferred**: Top-bar anchors belong to the layout shell,
  not the Home page brief. Home v1 ships without anchors; the three sources
  remain separate calls until the layout brief is groomed.
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

### Gap 8 — Unified Inbox endpoint ⏸ Deferred (Inbox v1 ships client-side merge)
- **v1 mitigation shipped**: Dashboard merges approvals +
  `home/overview.alerts` in `apps/dashboard/src/lib/inbox/derive.ts`. The
  shape is ready to be replaced by a server response when a
  `GET /api/v1/admin/inbox` endpoint lands — no client work required
  beyond swapping the merge call for a fetch.
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

### Gap 9 — Inbox-emitted events (budget / error / provider / queue) ⏸ Deferred (Inbox v1 ships partial scope)
- **v1 mitigation shipped**: Inbox renders approvals + AF/DB unhealthy
  probes only, per the brief's "partial v1 ship without" plan. The
  filter chips and severity-grouped list already accommodate richer
  sources when emitters land.
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

### Gap 10 — Acknowledge / dismiss mutation for non-approval items ⏸ Deferred (Inbox v1 documents the constraint)
- **v1 mitigation shipped**: System-alert rows in Inbox are
  non-interactive and carry the inline copy "resolves when condition
  clears" so the operator knows there's no ack action available yet.
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

### Gap 11 — Mobile single-item fetch by composite id ⏸ Deferred (Inbox v1 is desktop-only)
- **v1 mitigation shipped**: Inbox v1 is desktop-only; the mobile push
  Journey 6 path waits on v0.2 along with the mobile route. No
  regression to existing surfaces.
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

### Gap 12 — Anomaly detection inputs for Cost page ⏸ Deferred (Cost v1 ships client-side heuristic)
- **v1 mitigation shipped**: `apps/dashboard/src/lib/cost/derive.ts`
  computes top-share-spike (tenant whose share ≥2× expected) +
  forecast-vs-budget overrun client-side. AnomalyCard rows surface in
  Zone 1 with primary/secondary action buttons.
- **Surfaced by**: Page Brief — Cost (Zone 1)
- **What we need**: Backend emits anomaly events (X is N× its baseline)
  ready for surfacing in Zone 1 anomaly strip and Inbox.
- **What we have**: Client computes anomalies from `/cost` `by_tenant` ×
  `by_day` arrays. Coarse, no per-hour resolution.
- **Data is computable**: YES (heuristic v1).
- **Severity**: Inefficient
- **Suggested fix**: v1 client computes; v0.2 add server-side anomaly
  emitter that writes to `suite_anomaly_events` and surfaces in Inbox.

### Gap 13 — Nested hierarchy endpoint for Cost ⏸ Deferred (Cost v1 fan-outs per-tenant)
- **v1 mitigation shipped**: CostShell calls `/api/v1/cost?tenant=X` for
  each of the top-5 tenants and caches the result; Zone 2 (Tenant →
  Model) and Zone 3 (stacked area) both reuse the same dictionary so we
  pay the fan-out cost once.
- **Surfaced by**: Cost (Zone 2)
- **What we need**: Single endpoint returning Tenant → Agent → Reasoner
  → Model breakdown in one nested response.
- **What we have**: Flat `by_tenant` / `by_agent` / `by_model` arrays;
  per-tenant drill via separate `/cost?tenant=X` call.
- **Severity**: Inefficient (v1 makes N+1 calls; acceptable up to top-5).
- **Suggested fix**: `GET /api/v1/cost/breakdown?root=...&depth=N` v0.2.

### Gap 14 — Reasoner-tagged cost ⏸ Deferred (Cost v1 ships Tenant → Model only)
- **v1 mitigation shipped**: Zone 2 ships a 2-level hierarchy (Tenant →
  Model) and surfaces "Agent + Reasoner in v0.2" inline so the operator
  knows the depth is intentional, not broken.
- **Surfaced by**: Cost (Zone 2 reasoner level, future Reasoners page)
- **What we need**: Cost events tagged with `reasoner_name` so we can
  attribute spend to specific reasoners within an agent.
- **What we have**: `suite_cost_events` has `tenant`, `model`, `agent`
  (when run-tied), but no `reasoner`.
- **Severity**: **Blocking** for hierarchy completeness + Reasoners page.
- **Suggested fix**: Add `reasoner_name` column to `suite_cost_events`.
  AgentField SDK must pass reasoner identifier to the LLM gateway call so
  the cost ledger picks it up.

### Gap 15 — Per-hierarchy-node sparkline + delta ⏸ Deferred (Cost v1 ships share-bar only)
- **v1 mitigation shipped**: Zone 2 tenant + model rows render a
  GaugeBar (share of parent) instead of a per-node sparkline + delta.
  Sparklines + deltas land alongside the nested-hierarchy endpoint.
- **Surfaced by**: Cost (Zone 2 hierarchy rows)
- **What we need**: 24h time-series + prior-period total for EACH node
  in the tenant/agent/reasoner/model hierarchy.
- **What we have**: Only top-level prior period and aggregate by_day.
- **Severity**: Inefficient (v1 ships without; row shows aggregate only).
- **Suggested fix**: Per-node breakdown endpoint (pairs with Gap 13).
  v0.2.

### Gap 16 — ByDayByDimension for stacked area chart ⏸ Deferred (Cost v1 fan-outs for tenant only)
- **v1 mitigation shipped**: Zone 3 stacks the top-5 tenants over time
  by reusing the per-tenant fan-out from Gap 13. Group-by chips
  (agent/model/tool/day) are not shipped in v1 — they wait on the
  composite endpoint so we don't fan out unbounded calls.
- **Surfaced by**: Cost (Zone 3)
- **What we need**: For a given group-by dimension (tenant/agent/model),
  return `[{date, [{dimension_id, cost}]}]` so the stacked area chart can
  render directly.
- **What we have**: `by_day` (total) and `by_tenant` (totals) — but not
  combined.
- **Severity**: **Blocking for Zone 3 quality** (multi-call workaround is
  expensive at >5 dimensions).
- **Suggested fix**: `GET /api/v1/cost/timeseries?groupBy=tenant&top=10`
  returning the composite. v0.2.

### Gap 17 — MRR per tenant for Cost÷MRR unit economics ⏸ Deferred (Cost v1 hides the scatter)
- **v1 mitigation shipped**: Zone 4 ships without the Cost÷MRR scatter
  per brief §24. Cache donut + cost-by-model bars + budgets carry the
  zone in v1.
- **Surfaced by**: Cost (Zone 4 break-even scatter)
- **What we need**: Per-tenant MRR from billing adapter to plot vs cost.
- **What we have**: Billing adapter interface exists; Stripe/Lago
  adapters exist; MRR extraction not surfaced in
  `/api/v1/billing/customers`.
- **Severity**: **Blocking** for the killer Zone 4 chart.
- **Suggested fix**: Extend `GET /api/v1/billing/customers/{tenantId}`
  to return `monthly_recurring_usd` field. Compute from billing adapter's
  subscription state. v0.2.

### Gap 18 — Token totals per model in cost summary ⏸ Deferred (Cost v1 ships $/share bars)
- **v1 mitigation shipped**: Zone 4 model row renders share-of-spend
  bars instead of $/1k token bars. Honest in copy ("v0.2: $/1k tokens
  once token totals land").
- **Surfaced by**: Cost (Zone 4 $/1k tokens bars; future Models page)
- **What we need**: For each model in `by_model`, include
  `{tokens_in_total, tokens_out_total, calls_total}` so we can render
  $/1k tokens and $/call alongside total cost.
- **What we have**: `suite_cost_events` has token columns per event;
  summary `/cost` only sums cost.
- **Severity**: Inefficient (v1 shows $/call as substitute).
- **Suggested fix**: Add token totals to `by_model` entries in `/cost`
  response. Cheap aggregation. v0.2.

### Gap 19 — Tool-tagged cost ⏸ Deferred (Cost v1 hides per-tool ROI)
- **v1 mitigation shipped**: Per-tool ROI panel is not rendered in v1
  per brief §24. The page footer adapter pill still links operators to
  the LiteLLM admin where raw tool calls are visible.
- **Surfaced by**: Cost (Zone 4 per-tool ROI), future Tools page
- **What we need**: Cost events tagged with `tool_name` when the cost is
  attributable to a tool call (MCP or native tool with external API
  cost).
- **What we have**: No tool tagging on cost events.
- **Severity**: **Blocking** for per-tool ROI.
- **Suggested fix**: Add `tool_name` column to `suite_cost_events`. Tool
  adapters must register costs with the cost ledger when they hit
  external paid APIs. v0.2.

### Gap 20 — Service status transition log ⏸ Deferred (Health v1 uses current-only language)
- **v1 mitigation shipped**: IncidentBanner copy avoids "since X" /
  "for Nm" phrasing and shows the freshness via the "last sweep N ago"
  timestamp. Banner stays in the same slot regardless of state so the
  page doesn't jump on transitions.
- **Surfaced by**: Page Brief — Health (Zone 0 "degraded since 14:32")
- **What we need**: Persistent log of status transitions per service so
  the Incident banner can show "degraded since X (Nm ago)" instead of
  just current state.
- **What we have**: `suite_provider_health_log` exists for LLM providers
  but not for backing services generally; only current status is read.
- **Severity**: Inefficient (v1 shows current; v0.2 adds since-when).
- **Suggested fix**: Generalize provider health log pattern to
  `suite_service_status_log` covering all backing services. v0.2.

### Gap 21 — Worker / River status surface ⏸ Deferred (Health v1 hides Workers)
- **v1 mitigation shipped**: Workers section is intentionally absent
  from Health v1 per brief §"v1 SHIPS". Operators who care about jobs
  can still inspect the Queue link in the sidebar (also v0.2).
- **Surfaced by**: Health (Workers section)
- **What we need**: Endpoint surfacing River worker count, healthy/paused,
  per-queue throughput, restart rate.
- **What we have**: `/api/v1/queues/summary` for depth, not worker state.
- **Severity**: **Blocking** for Workers section on Health.
- **Suggested fix**: `GET /api/v1/admin/workers` returning per-queue
  worker metadata. v0.2.

### Gap 22 — TLS / cert expiry surface ⏸ Deferred (Health v1 hides TLS)
- **v1 mitigation shipped**: TLS / cert section is hidden in v1.
- **Surfaced by**: Health (TLS / domain section)
- **What we need**: Per-domain cert expiry, DNS verification state.
- **What we have**: Caddy manages certs; admin API at `:2019` not proxied.
- **Severity**: Inefficient (v1 hides; v0.2 adds it).
- **Suggested fix**: Proxy Caddy admin via runtime, or probe via
  `tls.Dial`. v0.2.

### Gap 23 — Suggested recovery actions per status ⏸ Deferred (Health v1 links to PLATFORM)
- **v1 mitigation shipped**: ProviderHealthCard surfaces a
  "Switch fallback" link to `/platform/adapters#llm` when the provider
  is degraded or down. v0.2 replaces the static link with smart
  per-state suggestions.
- **Surfaced by**: Health + Errors page
- **What we need**: When provider degrades → "Switch fallback to Y"; when
  DB cache low → "Run VACUUM"; etc.
- **What we have**: Status surfaced; no recommendation engine.
- **Severity**: Inefficient (v1 links to PLATFORM page; v0.2 smart).
- **Suggested fix**: Rule table in runtime mapping
  `(slot, status_pattern) → suggested_action`. v0.2.

### Gap 24 — DB previous-period cache hit ratio ⏸ Deferred (Health v1 shows current only)
- **v1 mitigation shipped**: Zone C cache donut renders the current
  ratio; no delta. Donut still renders at zero traffic (placeholder
  slice + "—" hit ratio) per the brief's structure-visible principle.
- **Surfaced by**: Health (Zone C cache hit ratio delta)
- **What we need**: Prior-period cache hit ratio for delta indicator.
- **What we have**: Current only.
- **Severity**: Cosmetic.
- **Suggested fix**: Periodic snapshot table `suite_db_health_snapshot`
  rolling 30d. v0.2.

### Gap 25 — Inbound webhook detail GET endpoint ⬜ Open
- **Surfaced by**: Drawer primitive (inbound webhook drill)
- **What we need**: `GET /api/v1/admin/webhooks/inbound/{id}` returning a
  single received inbound webhook with payload, headers, HMAC status,
  dedup status, downstream action.
- **What we have**: The receiving endpoint `POST /webhooks/in/{slug}`
  exists; observability for received inbound webhooks doesn't.
- **Severity**: **Blocking** for inbound webhook drawer.
- **Suggested fix**: Persist inbound webhooks to
  `suite_webhook_inbound_log` + expose list + detail endpoints. v0.2.
- **v1 mitigation**: Substitute via Logs filter; no drawer.

### Gap 26 — Error drawer composite endpoint ⬜ Open
- **Surfaced by**: Drawer primitive (error drill)
- **What we need**: `GET /api/v1/admin/errors/{id}` returning stack,
  sample run/job, pattern matches, audit — pre-composed for the drawer.
- **What we have**: Client merges `/api/v1/logs?correlation_id=X` +
  `/runs/{id}` per drill.
- **Severity**: Inefficient (v1 ships client merge; v0.2 single endpoint).
- **Suggested fix**: Add composed error detail endpoint v0.2.

### Gap 7 — Backing services strip "admin URL" field ✅ Closed (verified, 2026-06-17)
- **Verified shipped**: `adminService.AdminURL *string` already exists in
  `services.go:27` and is populated by both `serviceFromSlot` (slot
  adapters) and `appendEnvService` (observability services). No code
  change required; gap was a documentation gap.
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

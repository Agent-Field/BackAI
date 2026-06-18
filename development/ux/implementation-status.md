# UX Implementation Status

Running ledger of what's been built against the Page Briefs in
`development/ux/pages/`. Updated whenever a page ships.

---

## Home — DONE (v1)

**Branch**: `feat/ui-redesign`
**Page Brief**: `development/ux/pages/home.md`
**Route**: `/` (dashboard) — middleware-gated, sidebar-08 shell

### Frontend — built

| Surface | File | Notes |
|---|---|---|
| Server-rendered snapshot | `apps/dashboard/src/lib/home/data.ts` | `Promise.allSettled` over 6 endpoints; partial failure renders degraded sections |
| View types | `apps/dashboard/src/lib/home/types.ts` | `HomeSnapshot`, `KpiTileModel`, `StatusState`, `DataState` |
| Client derivations | `apps/dashboard/src/lib/home/derive.ts` | Status thresholds (error rate, failed runs, budget) + 8 tile models |
| Shell composition | `apps/dashboard/src/components/home/home-shell.tsx` | Welcome → KPI strip → Activity feed → Quick actions → Services strip |
| KPI strip | `apps/dashboard/src/components/home/kpi-strip.tsx` + `kpi-tile.tsx` + `kpi-sparkline.tsx` | 8 tiles, monochrome `chart-1` sparkline, identical structure across all 4 data states |
| Activity feed | `apps/dashboard/src/components/home/activity-feed.tsx` | 20-event scroll, server-merged via `/admin/events`, severity-only colour accent |
| Quick actions | `apps/dashboard/src/components/home/quick-actions.tsx` | 4 cards (Issue key, Add tenant, Test agent, API explorer) |
| Backing services strip | `apps/dashboard/src/components/home/backing-services-strip.tsx` | Pills with status dot, version, `admin_url` deep-link |
| Welcome block stub | `apps/dashboard/src/components/home/welcome-block.tsx` | Returns `null` — deferred per home.md §20 |
| Runtime-unreachable banner | `apps/dashboard/src/components/home/states/runtime-unreachable.tsx` | Shown only when every endpoint failed |
| Page entry | `apps/dashboard/src/app/page.tsx` | Server component, `force-dynamic`, replaces the sidebar-08 placeholder |

### Backend — built or verified

| Endpoint | Status | File |
|---|---|---|
| `GET /api/v1/home/overview` | **Extended** with `live_runs`, `failed_runs_last_24h`, `budgets_aggregate`; populated `queue_depth` + `recent_webhook_deliveries` | `services/runtime/internal/server/dashboard.go` |
| `GET /api/v1/cost` | Verified — MTD default range | `services/runtime/internal/server/cost.go` |
| `GET /api/v1/admin/services` | Verified — `admin_url` already in shape | `services/runtime/internal/server/services.go` |
| `GET /api/v1/approvals?status=pending` | Verified — `total` is the badge count | `services/runtime/internal/server/approvals.go` |
| `GET /api/v1/modules` | Verified — `multi_tenancy_enabled` flag | `services/runtime/internal/server/admin.go` |
| `GET /api/v1/admin/events` | **NEW** — unified events feed across runs, webhooks, alerts, activity log | `services/runtime/internal/server/admin_events.go` |

### Schema mirroring (zod)

`apps/dashboard/src/lib/api.ts` updated:
- `HomeOverviewSchema` extended with `live_runs`, `failed_runs_last_24h`,
  `budgets_aggregate`
- `BudgetsAggregateSchema` added
- `AdminEventSchema`, `AdminEventListSchema` added
- `api.admin.events.list({limit, kind})` method added

### Test coverage

| Test | File | Status |
|---|---|---|
| `TestHomeOverviewEmptyDeps` — extended with new field checks | `services/runtime/internal/server/dashboard_test.go` | passing |
| `TestAdminEventsEmptyDeps` | `services/runtime/internal/server/admin_events_test.go` | passing |
| `TestAdminEventsRespectsLimitParam` | same | passing |
| `TestAdminEventsKindFilterEmpty` | same | passing |

Full server suite: **265 tests, all passing**.

### Theme system

| Surface | File |
|---|---|
| CSS vars + `@theme inline` Tailwind bindings | `apps/dashboard/src/app/globals.css` |
| TS mirror for JS-side values (motion, chart sizing, polling, layout) | `apps/dashboard/src/lib/theme.ts` |

**Token surface introduced** (all consumed by JSX via class utilities):

- Spacing: `--spacing-page-x`, `--spacing-page-y`, `--spacing-section`,
  `--spacing-stack`, `--spacing-inline`, `--spacing-tile`,
  `--spacing-tile-tight`, `--spacing-pill-x`, `--spacing-pill-y`
- Radii: `--radius-tile`, `--radius-pill` (plus shadcn's `--radius-{sm,md,lg,xl,2xl,3xl,4xl}`)
- Type scale: `--text-eyebrow`, `--text-meta`, `--text-body`, `--text-kpi-value`
- Status colours: `--success`, `--success-foreground`, `--warning`,
  `--warning-foreground` (plus shadcn's `--destructive`)

**Rules enforced**:
1. No hardcoded `p-[Npx]` or `gap-[Npx]` or `text-[Npx]` anywhere in the home tree.
2. Status colour only appears on KPI status dots, severity badges, and service pills. Everything else is monochrome zinc.
3. Motion durations are in `lib/theme.ts` only — kept out of `@theme inline` because they need to flow into JS APIs, not class names.

### Deferred / out of scope (v1)

Per the page brief's §20 and the constraint list at start of session:

- **Welcome block** — content/dismiss/compact-mode all deferred (component stubbed)
- **Demo-mode toggle UI** — backend exists at `/admin/features`, UI deferred
- **Top-bar anchors** (Inbox / Cost / Health) — belong to layout, not this page brief
- **Cmd+K palette** — same reason
- **WebSocket live ticks** — page server-renders per request; WS upgrade tracked at home.md §11
- **Animated value transitions** — `theme.motion.tick` token exists; CSS transitions can be added once anchors and live ticks land

---

## Backend gaps — status after Home

See `development/ux/required-backend-gaps.md` for the originating list. Each gap's status after the Home v1 implementation:

| Gap | Description | Status after Home v1 |
|---|---|---|
| **1** | Live queue depth scalar | ✅ **Closed.** `home/overview.queue_depth` populated from `s.jobs.Summary().Pending` |
| **2** | Live running runs scalar | ✅ **Closed.** Added `home/overview.live_runs` from `s.jobs.Summary().Running` (counts River jobs currently executing — labelled "Live runs" with a tooltip explaining the semantics) |
| **3** | Failed runs in last 24h | ✅ **Closed.** Added `home/overview.failed_runs_last_24h` via a new count query on `suite_gateway_requests` scoped to `/api/v1/execute/%` endpoints |
| **4** | Budget consumed % aggregate | ✅ **Closed.** Added `home/overview.budgets_aggregate { tenants_at_risk, avg_consumed_pct, tenant_count }` via `cost.Budgets.List()` aggregation |
| **5** | Unified events feed | ✅ **Closed via new endpoint.** Built `GET /api/v1/admin/events` that server-side-merges runs + webhook deliveries + system alerts + activity log into a typed union. Decision: separate endpoint (not extending home/overview) for future reusability — recorded in this commit |
| **6** | Anchor unified endpoint | ⏸ **Deferred.** Top-bar anchors are out of scope for the Home page brief (they belong to layout). Will close when grooming the layout shell |
| **7** | Backing services `admin_url` | ✅ **Closed (verification only).** `adminService.AdminURL` field already exists in `services.go:27`; was already populated by `serviceFromSlot` and `appendEnvService`. No code change needed |
| **8** | Unified Inbox endpoint | ⏸ Belongs to Inbox page brief, not Home |
| **9** | Inbox-emitted events | ⏸ Belongs to Inbox page brief, not Home |
| **10** | Acknowledge/dismiss for non-approval items | ⏸ Belongs to Inbox page brief, not Home |
| **11** | Mobile single-item fetch | ⏸ Belongs to Inbox page brief, not Home |

**Net result for Home**: 0 gaps left blocking. 0 mocked data anywhere — every value comes from a real handler with a real query.

### Webhook deliveries

Not numbered in the gap ledger but addressed inline: `home/overview.recent_webhook_deliveries` was declared but never populated ("Phase 10" placeholder). Wired to read the latest 10 rows from `suite_webhook_deliveries` (cross-tenant) and map DB enums (`inbound`/`outbound`, `succeeded`/`failed`/`queued`/...) to the schema vocabulary (`in`/`out`, `delivered`/`failed`/`pending`).

---

---

## Inbox — DONE (v1)

**Branch**: `feat/ui-redesign`
**Page Brief**: `development/ux/pages/inbox.md`
**Route**: `/inbox` (dashboard) — middleware-gated, sidebar-08 shell
**Scope (per brief §20)**: Approvals + 2 system alerts only. Gaps 9/10/11
deferred to v0.2.

### Decisions locked in this round

- **Live data**: 30s polling (no WebSocket v1). Token at
  `theme.polling.inbox`.
- **Filter chips**: severity + kind, URL-persistent (`?severity=`, `?kind=`).
- **Badge rule**: total pending count; red when any inbox item is
  critical-severity.
- **Detail surface**: right-side shadcn Sheet drawer, URL state
  `?item=approval:<id>`.
- **Sort**: severity-tiered (critical → warning → info), newest first
  within tier.
- **Card density**: expanded (title + context + meta), brief
  recommendation.
- **Empty state**: affirmative "All clear" panel; never a sad "no items"
  message.

### Frontend — built

| Surface | File | Notes |
|---|---|---|
| Server-rendered snapshot | `apps/dashboard/src/lib/inbox/data.ts` | `Promise.allSettled` over approvals + home/overview |
| Types | `apps/dashboard/src/lib/inbox/types.ts` | `InboxItem` discriminated union, `InboxFilters` |
| Merge + sort + filter helpers | `apps/dashboard/src/lib/inbox/derive.ts` | Severity-tiered sort, by-kind counts |
| Shell | `apps/dashboard/src/components/inbox/inbox-shell.tsx` | Polling, URL-state filters + drawer, decide handler |
| Filter chips | `apps/dashboard/src/components/inbox/filter-chips.tsx` | Severity + kind, counts on each chip |
| Severity-grouped list | `apps/dashboard/src/components/inbox/item-group.tsx` | One group per severity tier with item rows |
| Approval drawer | `apps/dashboard/src/components/inbox/approval-drawer.tsx` | shadcn Sheet, approve/deny/cancel with optional note |
| All-clear state | `apps/dashboard/src/components/inbox/all-clear.tsx` | Affirmative empty state |
| Degraded banner | `apps/dashboard/src/components/inbox/inbox-banner.tsx` | Surfaces partial-source failures |
| Page entry | `apps/dashboard/src/app/(dashboard)/inbox/page.tsx` | Server component, `force-dynamic` |
| Top-bar anchor | `apps/dashboard/src/components/layout/top-bar.tsx` | `AnchorPill` now optionally renders as a `<Link>`; Inbox pill points at `/inbox`; critical → red dot, pending → yellow, else green |

### Backend — built or extended

| Endpoint | Status | File |
|---|---|---|
| `GET /api/v1/admin/anchors` | **Extended**: added `inbox_has_critical`; `inbox_pending` now folds system alerts into the tally and counts approvals cross-tenant via `app.bypass_rls=on` (the prior store call returned 0 because the store enforces tenant scope). | `services/runtime/internal/server/admin_anchors.go` |
| `GET /api/v1/approvals?status=pending` | Already shipped — consumed for both initial fetch and polling | `services/runtime/internal/server/approvals.go` |
| `GET /api/v1/home/overview.alerts` | Already shipped — source for system-alert inbox items | `services/runtime/internal/server/dashboard.go` |
| `POST /api/v1/approvals/{id}/decide` | Already shipped — wired through the drawer's Approve/Deny/Cancel buttons | `services/runtime/internal/server/approvals.go` |

### Schema mirroring (zod)

`apps/dashboard/src/lib/api.ts`:
- `AdminAnchorsSchema` gained `inbox_has_critical: z.boolean()`

### Test coverage

| Test | File | Status |
|---|---|---|
| `TestAdminAnchorsEmptyDeps` | `services/runtime/internal/server/admin_anchors_test.go` | passing — locks down the `inbox_has_critical` boolean field, healthy default health, and well-formed zero values |

Full server suite: **266 tests, all passing**.

### Deferred / out of scope (v1)

- **Mobile route** (`/inbox/<id>`) — Gap 11. Inbox is desktop-only in v1.
- **Acknowledge mutation** for non-approval items — Gap 10. System alerts
  vanish only when the underlying probe recovers; rows in v1 carry the
  copy "resolves when condition clears".
- **Rich Inbox sources** (budget / error / provider / queue alerts) —
  Gap 9. Approvals + AF/DB probes are the only v1 signals.
- **Unified inbox endpoint** — Gap 8. v1 merges client-side; the merge
  helper lives in `lib/inbox/derive.ts` and is ready to be replaced by a
  server response when Gap 8 ships.
- **WebSocket subscription** — page brief §11 calls it out as a v0.2
  enhancement; 30s polling carries v1.

---

## Backend gaps — status after Inbox

See `development/ux/required-backend-gaps.md` for the originating list.

| Gap | Description | Status after Inbox v1 |
|---|---|---|
| **6** | Anchor unified endpoint | ✅ **Closed in scope.** Extended with `inbox_has_critical`; counts now reflect approvals + system alerts cross-tenant |
| **8** | Unified Inbox endpoint | ⏸ **Deferred** — merged client-side in `lib/inbox/derive.ts`. Acceptable per brief; revisit in v0.2 |
| **9** | Inbox-emitted events | ⏸ **Deferred to v0.2** — v1 ships with approvals + 2 system alerts only, per brief §20 |
| **10** | Acknowledge/dismiss non-approval items | ⏸ **Deferred to v0.2** — v1 makes system-alert rows non-interactive with copy explaining they resolve when the condition clears |
| **11** | Mobile single-item fetch | ⏸ **Deferred to v0.2** — Inbox is desktop-only in v1 |

**Net result for Inbox**: 0 gaps blocking the v1 surface; remaining gaps
match the brief's explicit "partial scope" plan.

---

---

## Cost — DONE (v1)

**Branch**: `feat/ui-redesign`
**Page Brief**: `development/ux/pages/cost.md`
**Route**: `/cost` (dashboard)
**Scope (per brief §24)**: Zone 1 (anomaly strip + forecast) + Zone 2
(Tenant → Model hierarchy, 2 levels) + Zone 3 (stacked area + top-N) +
Zone 4 (cache donut + model bars + budgets + edit dialog) + adapter
footer. Gaps 12–19 deferred per brief's v0.2 plan.

### Decisions locked

- **Live data**: 10s polling (theme.polling.services) — cost numbers move
  slower than KPIs but the page benefits from background freshness.
- **Range chips**: Today / 7d / 30d / 90d, URL-persistent (`?range=`).
  Reuses the standardised `FilterChip` primitive so contrast + density
  match the Inbox chips.
- **Anomaly heuristic** (Gap 12 mitigation): top-share-spike detection
  client-side (≥2× expected share triggers a callout) + forecast >
  budget overrun. v0.2 swaps for backend emitters.
- **Zone 2 v1 depth**: Tenant → Model only. Agent + Reasoner levels
  surface a v0.2 chip inline. Per-tenant data reused from Zone 3
  fan-out so the zone costs zero extra requests.
- **Zone 3 series**: Top 5 tenants stacked + "other"; per-tenant `/cost`
  calls fan out client-side (Gap 16 mitigation).
- **Budgets edit**: shadcn Dialog + Sonner toast on save. Validates
  positive cap + 0–100% threshold before submit.
- **Adapter footer**: LiteLLM pill + DropdownMenu (Open LiteLLM admin,
  Change adapter). Per framework Part 9.

### Frontend — built

| Surface | File | Notes |
|---|---|---|
| Server-rendered snapshot | `apps/dashboard/src/lib/cost/data.ts` | `Promise.allSettled` over cost + budgets + cache + tenants |
| Range helpers | `apps/dashboard/src/lib/cost/range.ts` | `today/7d/30d/90d` → UTC ISO range |
| Types | `apps/dashboard/src/lib/cost/types.ts` | `CostSnapshot`, `CostAnomaly`, `TenantSeriesPoint` |
| Derivations | `apps/dashboard/src/lib/cost/derive.ts` | Anomaly heuristic, top-N tenants, stacked-area series builder, model rows, formatters |
| Shell | `apps/dashboard/src/components/cost/cost-shell.tsx` | Polling, URL-state range, per-tenant fan-out for Zone 3 |
| Range chips | `apps/dashboard/src/components/cost/range-chips.tsx` | Reuses `FilterChip` primitive |
| Degraded banner | `apps/dashboard/src/components/cost/degraded-banner.tsx` | Same shape as Inbox banner |
| Adapter footer | `apps/dashboard/src/components/cost/adapter-footer.tsx` | LiteLLM pill + dropdown |
| Zone 1 | `apps/dashboard/src/components/cost/zone1-anomaly-strip.tsx` | Period-total tile + forecast tile + AnomalyCard list |
| AnomalyCard | `apps/dashboard/src/components/cost/anomaly-card.tsx` | Severity-bordered card + sparkline + 1-2 action buttons |
| Zone 2 | `apps/dashboard/src/components/cost/zone2-hierarchy.tsx` | Tenant rows with model-leaf expand |
| Zone 3 | `apps/dashboard/src/components/cost/zone3-explorer.tsx` | Stacked area (top-5 + other) + top-N tenant list |
| Zone 4 | `apps/dashboard/src/components/cost/zone4-economics.tsx` | Cache donut + cost-by-model bars + budgets table |
| Budget edit dialog | `apps/dashboard/src/components/cost/budget-edit-dialog.tsx` | shadcn Dialog + Sonner toast |
| Page entry | `apps/dashboard/src/app/(dashboard)/cost/page.tsx` | Server component, `force-dynamic` |
| Top-bar anchor | `apps/dashboard/src/components/layout/top-bar.tsx` | Cost AnchorPill now points at `/cost` |

### Primitives introduced (in `components/ui/`)

These are tokenised, theme-aware, and reused across pages. All colour
goes through semantic CSS variables — no raw hex, no per-component
overrides.

| Primitive | File | Reused on |
|---|---|---|
| `Sparkline` | `apps/dashboard/src/components/ui/sparkline.tsx` | Cost zones, AnomalyCard, future Errors/Tenant detail |
| `DeltaIndicator` | `apps/dashboard/src/components/ui/delta-indicator.tsx` | Cost Period-total tile, future KPIs |
| `ForecastBar` | `apps/dashboard/src/components/ui/forecast-bar.tsx` | Cost Forecast tile, future Budgets page |
| `GaugeBar` | `apps/dashboard/src/components/ui/gauge-bar.tsx` | Cost budgets rows, Cost hierarchy share bars, future Quota strips |
| `FilterChip` / `FilterChipGroup` | `apps/dashboard/src/components/ui/filter-chip.tsx` | Inbox filters, Cost range chips |

### Backend — no new endpoints

Existing endpoints are sufficient for v1:

| Endpoint | Powers | Status |
|---|---|---|
| `GET /api/v1/cost?from=&to=&tenant=` | All zones | ✅ existing |
| `GET /api/v1/admin/budgets` | Zone 4 budgets table | ✅ existing |
| `PUT /api/v1/admin/budgets` | Budget edit dialog | ✅ existing |
| `GET /api/v1/llm/cache/stats` | Zone 4 cache donut | ✅ existing |

`api.cost(params)` extended with optional `tenant` (was already
backend-supported; the client wrapper just didn't expose it).

### Deferred / out of scope (v1)

Per brief §24 and §20:

- **Hierarchy Agent + Reasoner levels** — Gap 14 blocking; surfaced
  inline as "v0.2".
- **Per-node sparkline + delta in hierarchy** — Gap 15.
- **Group-by chips on Zone 3** (agent/model/tool/day) — needs
  ByDayByDimension (Gap 16) to be tractable for more than tenant.
- **Cost ÷ MRR scatter** — Gap 17 (no billing adapter MRR).
- **$/1k tokens bars** — Gap 18 (token totals not in summary).
- **Per-tool $/call** — Gap 19 (tools not tagged on cost events).
- **WebSocket subscription** — page brief §11 calls for WS but v1 polls;
  v0.2 will upgrade.
- **Mobile** — desktop-primary; mobile budget alerts land in Inbox
  (page brief §16).

---

## Backend gaps — status after Cost

| Gap | Description | Status after Cost v1 |
|---|---|---|
| **12** | Anomaly detection inputs | ⏸ **Deferred to v0.2** — v1 ships client-side top-share-spike + forecast-vs-budget heuristics. Honest about limitations in code comments |
| **13** | Nested hierarchy endpoint | ⏸ **Deferred to v0.2** — v1 fan-outs per-tenant `/cost?tenant=X` (max 5 calls); shared with Zone 3 |
| **14** | Reasoner-tagged cost | ⏸ **Blocking for full hierarchy; deferred to v0.2** — v1 ships Tenant → Model only and surfaces the v0.2 note inline |
| **15** | Per-node sparkline / delta | ⏸ **Deferred to v0.2** — Zone 2 rows currently render a share bar instead |
| **16** | ByDayByDimension for stacked area | ⏸ **Deferred to v0.2** — v1 fan-outs per top-N tenant (acceptable for N=5) |
| **17** | MRR per tenant | ⏸ **Deferred to v0.2** — break-even scatter not shipped in v1 |
| **18** | Token totals per model | ⏸ **Deferred to v0.2** — v1 ships `$/share` bars instead of `$/1k tokens` |
| **19** | Tool-tagged cost | ⏸ **Deferred to v0.2** — per-tool ROI not shipped in v1 |

---

---

## Health — DONE (v1)

**Branch**: `feat/ui-redesign`
**Page Brief**: `development/ux/pages/health.md`
**Route**: `/health` (dashboard)
**Scope (per brief §"v1 SHIPS")**: Zone 0 (Incident / All-clear banner) +
Zone A (LLM providers) + Zone B (Connected services grid) + Zone C
(Database) + Zone D (Runtime). Workers + TLS deferred to v0.2 (Gaps
21/22). Status transition log + smart recovery + cache delta deferred
(Gaps 20/23/24).

### Decisions locked

- **Live data**: 10s polling (theme.polling.services). Brief calls for
  WebSocket; deferred to v0.2 alongside the live-tick framework upgrade.
- **Provider window chips**: 24h / 7d / 30d, URL-persistent (`?window=`).
  Reuses the standardised `FilterChip` primitive so contrast + density
  match Inbox / Cost chips.
- **Manual refresh**: Refresh button in the header runs the same fetch
  loop and toasts on success.
- **Overall summary computation**: client-side via
  `lib/health/derive.ts::summarise()`. Drives the Incident banner; v0.2
  swaps for backend-emitted incident events.
- **Service grouping**: by `kind` (Runtime / Data / Intelligence /
  Storage / Queue / Delivery / Observability / Other). Order locked in
  derive.ts so the page reads top-down by criticality.
- **Empty states**: every sub-card renders its frame even at zero
  data — slow queries shows the table header + "First period of data"
  row; cache donut shows "—" hit ratio when no traffic; provider grid
  uses dashed-border affordance when no providers configured.

### Frontend — built

| Surface | File | Notes |
|---|---|---|
| Server-rendered snapshot | `apps/dashboard/src/lib/health/data.ts` | `Promise.allSettled` over services + providers + db + metrics |
| Types | `apps/dashboard/src/lib/health/types.ts` | `HealthSnapshot`, `OverallSummary`, `ProviderWindowKind`, `isProviderWindowKind` |
| Derivations | `apps/dashboard/src/lib/health/derive.ts` | Status classifiers, overall summariser, service grouping, sparkline + formatters (latency / bytes / duration / relative time) |
| Shell | `apps/dashboard/src/components/health/health-shell.tsx` | Polling, URL-state provider window, manual refresh, overall summary |
| Incident banner | `apps/dashboard/src/components/health/incident-banner.tsx` | All-clear + degraded variants, same height across both |
| Zone A — providers | `apps/dashboard/src/components/health/zone-a-providers.tsx` | Grid (3/2/1 cols) + window chips + EmptyProvidersCard fallback |
| ProviderHealthCard | `apps/dashboard/src/components/health/provider-health-card.tsx` | Status dot + uptime + p95 + sparkline + switch-fallback link when degraded |
| Zone B — services | `apps/dashboard/src/components/health/zone-b-services.tsx` | Grouped by kind, each group its own sub-card |
| ServiceHealthRow | `apps/dashboard/src/components/health/service-health-row.tsx` | Dot + name + purpose + host + version + checked time + click-out arrow |
| Zone C — database | `apps/dashboard/src/components/health/zone-c-database.tsx` | Five sub-cards (Connections, Cache, Slow queries, Largest tables, Vacuum) |
| Zone D — runtime | `apps/dashboard/src/components/health/zone-d-runtime.tsx` | 4 tiles (Version / Uptime / Memory / Goroutines) + HTTP summary + Top routes table |
| Page entry | `apps/dashboard/src/app/(dashboard)/health/page.tsx` | Server component, `force-dynamic` |
| Top-bar anchor | `apps/dashboard/src/components/layout/top-bar.tsx` | Health AnchorPill points at `/health` |
| Sidebar | `apps/dashboard/src/components/app-sidebar.tsx` | Health removed from `comingSoon` set |

### Primitives promoted to `components/ui/`

| Primitive | Notes |
|---|---|
| `ZoneCard` / `ZoneCardHeader` | Moved out of `components/cost/` into `components/ui/zone-card.tsx` so Cost + Health share the same primitive. Cost re-export shim keeps the old import path working |

### Backend — no new endpoints

All four sources existed in Block 1:

| Endpoint | Powers |
|---|---|
| `GET /api/v1/admin/services` | Zone B |
| `GET /api/v1/admin/llm/provider-health?window=` | Zone A |
| `GET /api/v1/admin/db/health` | Zone C |
| `GET /api/v1/metrics/summary` | Zone D |

### Deferred / out of scope (v1)

- **Workers section** — Gap 21
- **TLS / cert expiry section** — Gap 22
- **"Degraded since X" transition log** — Gap 20
- **Smart recovery suggestions** — Gap 23
- **DB cache hit ratio delta** — Gap 24
- **WebSocket subscription** — brief calls for WS; v1 polls at 10s
- **HoverCard for slow queries** — table renders truncated query; full
  text via title tooltip in v1, HoverCard in v0.2

---

## Backend gaps — status after Health

| Gap | Description | Status after Health v1 |
|---|---|---|
| **20** | Service status transition log | ⏸ **Deferred to v0.2** — IncidentBanner uses current-only language; "since X" deltas await Gap 20 |
| **21** | Worker / River status | ⏸ **Deferred to v0.2** — Workers section hidden in v1 per brief |
| **22** | TLS / cert expiry | ⏸ **Deferred to v0.2** — TLS section hidden in v1 |
| **23** | Suggested recovery actions | ⏸ **Deferred to v0.2** — ProviderHealthCard surfaces a "Switch fallback" link to PLATFORM → Adapters when degraded |
| **24** | DB previous-period cache hit ratio | ⏸ **Deferred to v0.2** — Cache sub-card shows current ratio only |

---

_Last updated: 2026-06-17. Next: drawer primitive design._

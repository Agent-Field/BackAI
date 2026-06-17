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

_Last updated: 2026-06-17. Next page brief to groom: Cost._

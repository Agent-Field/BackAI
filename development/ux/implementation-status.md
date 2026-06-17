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

_Last updated: 2026-06-17. Next page brief to groom: Inbox._

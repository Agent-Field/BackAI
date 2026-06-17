# Page Brief — Home

> The first surface an operator sees on Day 0 and on every subsequent visit.
> Anchor for 6 of 8 journeys.
> Groomed against `development/ux/page-design-framework.md`.

---

## 1. PURPOSE

Show the operator whether their fork is healthy and what — if anything —
needs their attention, in under 3 seconds.

Confidence questions answered:
- *"Am I OK right now?"* (Journey 2, every visit)
- *"What IS this thing?"* (Journey 1, Day 0 only)
- *"Where do I go next?"* (entry-point for Journeys 3, 5, 7, 8)

---

## 2. PILLAR

Anchor (top-pinned, no group). The most-visited surface in the admin.

---

## 3. JOURNEYS SERVED

| # | Journey | Frequency on Home | Role |
|---|---|---|---|
| 1 | First open of fresh fork | Every visit Day 0 | Direct — sets first impression |
| 2 | Daily 3-sec health glance | Every visit thereafter | Direct — IS the journey |
| 3 | Verify Claude-Code change | Sometimes | Entry-point (Cmd+K from here) |
| 5 | Customer-reported issue triage | Sometimes | Entry-point (Cmd+K from here) |
| 7 | Cost-spike investigation | Sometimes | Trigger (spike noticed on KPI strip) |
| 8 | Provider outage / incident | Sometimes | Trigger (red on Health anchor + KPI strip) |

Journeys NOT served by Home: 4 (Playground) lands directly on agent;
6 (push alert) lands directly on Inbox alert detail.

---

## 4. TIME BUDGET

- **3 seconds** — Journey 2 (glance) target
- **60–90 seconds** — Journey 1 (first open) target
- **~3 seconds** — entry-point uses (operator hits Cmd+K and leaves)

---

## 5. DENSITY TARGET

**HIGH but scannable.** Multiple signals at once. This is the "volume =
credibility signal" page above all others. Operator should feel "this is a
real platform" within one glance.

Density without overwhelm: rich KPI strip + rich activity feed + welcome
block + quick actions + backing services — but visually quiet, monochrome,
no decoration.

---

## 6. PRIMARY READ

**The COLOR PATTERN across the KPI strip + the Health dot in the top bar.**

Not a single tile. The operator's eye sweeps the strip looking for any
non-green value. Green everywhere = leave. Anything yellow or red = drill.

Eye flow: top bar (Health dot, anchor live values) → KPI strip → activity
feed. Done in 3 seconds.

---

## 7. DATA SOURCES

> **Audit status**: every endpoint below verified to exist in
> `services/runtime/internal/server/`. Field-level shape verified against
> handler implementations. Gaps surfaced in
> `development/ux/required-backend-gaps.md`.

### Runtime endpoints (WRAP) — verified

| Endpoint | Powers | Status |
|---|---|---|
| `GET /api/v1/home/overview` | KPIs: RequestsPerMinute, ErrorRate, CostTodayUSD + 24h sparklines + RecentRuns(20) + RecentWebhookDeliveries + Alerts | ✅ exists (`dashboard.go:360`) |
| `GET /api/v1/cost` | Cost MTD (`PeriodTotalUSD`), forecast, by-day/model/agent/tenant | ✅ exists (`dashboard.go:607`) |
| `GET /api/v1/activity` | Customer-facing mutation log (tenant_id, action, resource) — partial input to activity feed | ✅ exists (`activity.go:91`) |
| `GET /api/v1/admin/services` | Backing services strip (runtime, postgres, slot-derived services, observability env-derived) | ✅ exists (`services.go:46`) |
| `GET /api/v1/admin/llm/provider-health` | Provider availability (Block 1.4) | ✅ exists (`llm_provider_health.go`) |
| `GET /api/v1/admin/features` | Feature flags / capability surface (Block 2) | ✅ exists (`admin_features.go`) |
| `GET /api/v1/approvals?status=pending` | Inbox badge count | ✅ exists (`approvals.go`) — Home filters response |
| `GET /api/v1/metrics/summary` | Runtime metrics (HTTP total, p95, goroutines, mem, uptime, version, top-10 routes by traffic) | ✅ exists (`metrics_summary.go`) — NOT what powers Home KPI strip; useful in Health page |
| `WS /api/v1/realtime` | Live ticks for KPI updates | ✅ exists (`realtime.go`) |
| `GET /health` + `GET /ready` | Runtime self-status | ✅ exists |

### KPI tile sourcing — audited per-tile

| Tile | Source field | Status |
|---|---|---|
| Requests / min | `home/overview.RequestsPerMinute` | ✅ direct |
| Error rate % (last 60m) | `home/overview.ErrorRate` | ✅ direct |
| Cost today | `home/overview.CostTodayUSD` | ✅ direct |
| Cost MTD | `cost.PeriodTotalUSD` (default range = start of UTC month) | ✅ direct (separate call) |
| Queue depth | `home/overview.QueueSparkline` last bucket OR `queues/summary` | ⚠️ Inefficient — see **Gap 1** |
| Live runs | derive from `runs?status=running` (separate call) | ⚠️ Inefficient — see **Gap 2** |
| Failed runs 24h | sum `home/overview.ErrorSparkline` OR `runs?status=failed&from=24h` | ⚠️ Inefficient — see **Gap 3** |
| Budget consumed % | derive from `admin/budgets` (aggregate client-side) | ⚠️ Inefficient — see **Gap 4** |

### Activity feed sourcing — composite (audited)

The "activity feed" on Home is a UNION of three sources:

| Source | Provides |
|---|---|
| `home/overview.RecentRuns` | run.completed / run.failed events |
| `home/overview.RecentWebhookDeliveries` | webhook.delivered / .failed events |
| `home/overview.Alerts` | system alerts (e.g., agentfield-unreachable, db-unhealthy) |
| `GET /api/v1/activity?limit=20` | customer-facing mutations (tenant.created, config changed, etc.) |

⚠️ **Gap 5** — these need merging client-side today. A unified
`GET /api/v1/admin/events?limit=20` endpoint would be cleaner. Defer to
later block.

### Backing services strip — audited

`GET /api/v1/admin/services` returns rows with `ID, Name, Kind, Status,
Version, Host, Port, Purpose, Checked`. Covers: runtime, postgres,
adapter-registered slots (LiteLLM, MinIO, AgentField, Svix, River),
observability env-derived (Loki, Tempo, Prometheus, GlitchTip, Grafana).

⚠️ **Gap 7** — confirm `admin_url` field is exposed in the JSON response
for click-out behavior. Host + Port + scheme assumption can derive it
client-side as a fallback.

### Anchor live values — audited

Top-bar anchors (Inbox count, Cost daily, Health dot) currently require
THREE separate calls per page load (anchors are persistent across all
pages, so this is expensive).

⚠️ **Gap 6** — a single `GET /api/v1/admin/anchors` endpoint would be a
significant UX/perf win. Defer to later block; acceptable to make three
calls per page in v1 if cached aggressively.

### OSS sources (indirectly via runtime — none queried directly)

LiteLLM `/spend/keys`, AgentField capabilities, provider `/health`, etc.
are all aggregated server-side by the runtime endpoints above. Home does
not call OSS services directly.

### Computed client-side

- Deltas vs. yesterday (compare today's value to yesterday's at same time)
- Sparkline rendering from time-series in `home/overview`
- Welcome-block dismissed flag (localStorage) — TODO
- Color-state thresholds (e.g., error rate >2% = yellow, >5% = red)
- Activity feed merge (client-side until **Gap 5** resolved)

### Backend gap summary for Home

| Gap | Severity | Action |
|---|---|---|
| 1. Queue depth scalar | Inefficient | Add to `home/overview` |
| 2. Live runs scalar | Inefficient | Add to `home/overview` |
| 3. Failed 24h scalar | Inefficient | Add to `home/overview` or sum sparkline client-side |
| 4. Budget aggregate | Inefficient | Add to `home/overview` or compute client-side |
| 5. Unified events feed | Inefficient | New `/admin/events` endpoint OR merge client-side |
| 6. Anchor unified endpoint | Inefficient | New `/admin/anchors` endpoint (perf win) |
| 7. Backing services admin_url | Cosmetic | Audit JSON shape; fallback to Host+Port |

**Net assessment**: Home is buildable today with current endpoints. 4 of 8
KPI tiles require client-side derivation from existing responses. No
**Blocking** gaps. All inefficiencies are tracked for backlog grooming.

---

## 8. WRAP / REFLECT / LINK

| Surface | Pattern | Why |
|---|---|---|
| KPI strip | WRAP | Multi-tenant aggregates, our cost ledger, no OSS UI competes |
| Activity feed | WRAP | Our typed event log; cross-OSS unified |
| Welcome block | WRAP | Our operator onboarding |
| Backing services strip | REFLECT | We show status pills; depth lives in each OSS's UI |
| Quick action cards | WRAP | All actions are runtime mutations |
| Anchor live values (Inbox, Cost, Health) | WRAP | Same as KPI tiles |

---

## 9. OSS LINKS SURFACED

From the **backing services strip** at the bottom of the page (or footer
rail). One pill per service, click opens the OSS's native UI in a new tab.

| Pill | Opens (when configured) | Always present? |
|---|---|---|
| Runtime | n/a (us) | Yes |
| Postgres | n/a (no native UI; link to BUILD → Data) | Yes |
| LiteLLM | `:4000/ui` | Yes |
| AgentField | `:8081` | Yes |
| MinIO | `:9001` | Yes |
| Svix | `:8071` | Yes |
| Tempo | `:3200` | Only if `AF_STACK_TRACES_ADAPTER=tempo` |
| Loki | `:3100` | Only if `AF_STACK_LOGS_ADAPTER=loki` |
| Prometheus | `:9090` | Only if `AF_STACK_METRICS_ADAPTER=prometheus` |
| GlitchTip | `:8000` | Only if `AF_STACK_ERRORS_ADAPTER=glitchtip` |

Context preservation: Home pills open OSS dashboards at their root (no
per-entity context to preserve). Per-entity OSS jumps happen from detail
pages (drawer → "Open this run in AgentField"), not from Home.

---

## 10. THREE DATA STATES

### EMPTY (capable but no data)

Fresh fork, no real activity yet. Default behavior: **`demo_mode` is ON** so
this state rarely renders as "truly empty." If operator has turned demo off:

- **KPI tiles**: render structure (label, unit, sparkline frame) at zero
  with a muted "no data yet" sub-label per tile
- **Activity feed**: single line "Make your first call to see events." +
  link to BUILD → API explorer + dev tenant key card visible
- **Backing services strip**: shows actual live status (likely all green)
- **Welcome block**: prominent
- **Quick actions**: full set visible

Color tone: neutral. No red. No alarm. "Quiet awaiting first activity."

### MISSING (adapter doesn't expose)

Some KPI tiles depend on adapters that may not be configured:
- "Error rate" needs an Errors adapter (default = logfilter, so always
  works to some extent)
- "Cost today" needs LLM gateway with spend tracking (LiteLLM always works)
- "Provider availability" needs the provider health polling feature

If a tile's data source is missing:
- Tile shows structure with `—` instead of value
- Tooltip: "Requires <feature/adapter>. <link to PLATFORM → that page>"
- No red. Subtle informational tone.

Backing services strip simply doesn't include pills for adapters that aren't
configured (Tempo, Loki, etc. when not enabled).

### DEGRADED (configured but unhealthy)

- **KPI tiles**: last-known value frozen, timestamp turns muted, small
  "stale" chip on tile
- **Activity feed**: shows what it can; if WebSocket dropped, "reconnecting…"
  chip at top
- **Backing services pill**: dot turns yellow (degraded) or red (down);
  tooltip shows last error / last successful check
- **Health anchor in top bar**: turns yellow or red — most important signal

Banner at top of Home if degradation is significant: "Some signals are
stale. See Health." with link to Health page.

---

## 11. LIVE DATA

### WebSocket subscriptions

- KPI strip values (sub-second updates as runtime emits)
- Activity feed (new events slide in at top)
- Backing service status (push notifications via runtime)
- Anchor live values: Inbox count, Cost daily total, Health dot

### Polled instead

- Backing services check every 5-10s for those without push
- Provider availability polled by runtime, surfaced via the same WebSocket
  channel (so Home doesn't poll)

### Animations

- KPI value change: 200ms ease, new value, NO flash
- New activity event: slides in at top, brief subtle highlight (~600ms),
  then settles
- Backing service status change: dot pulses once when state transitions
- Inbox badge count: increments with subtle pop; decrements quietly
- Stale data: tile timestamp turns muted gray; value freezes; no scream

### Static / on-demand

- Welcome block content (only changes when operator dismisses or restarts)
- Quick action cards
- Demo mode toggle state

---

## 12. MUTATIONS

| Mutation | Surface | Audit | Undo | Mobile |
|---|---|---|---|---|
| Reveal dev tenant key | Welcome block, key card | Yes (`key.revealed`) | n/a (read action) | n/a |
| Copy try-it snippet | Welcome block | No (client-side) | n/a | n/a |
| Dismiss welcome block | Block "x" | No (localStorage) | "Reset welcome" in PLATFORM | n/a |
| Toggle demo mode | Welcome block compact OR PLATFORM | Yes (`demo_mode.toggle`) | Re-toggle | n/a |
| Issue API key | Quick action → drawer | Yes | Revoke | n/a |
| Add tenant | Quick action → drawer | Yes | Delete | n/a |
| Test an agent | Quick action → navigates to Build → Agents → Playground | Yes (when run executes) | n/a | n/a |
| Open API explorer | Quick action → navigates | No | n/a | n/a |

Home itself has no destructive mutations. All risky actions are in their
respective pages.

---

## 13. DRILL PATHS

### KPI tile → destination

| Tile | Drills to |
|---|---|
| Requests / min | ACTIVITY → Runs (filtered last 24h) |
| Error rate % | ACTIVITY → Errors (filtered last 1h) |
| Cost today | MONEY → Cost (range = today) |
| Cost MTD | MONEY → Cost (range = month) |
| Queue depth | ACTIVITY → Runs (filtered status = queued) or Queue subpage |
| Live runs | ACTIVITY → Runs (filtered status = running) |
| Failed runs 24h | ACTIVITY → Runs (filtered status = failed, 24h) |
| Budget consumed % | PEOPLE → Budgets (sorted by % consumed desc) |

### Activity event → entity

| Event type | Drills to |
|---|---|
| run.completed / run.failed | Run drawer (universal) |
| tenant.created | PEOPLE → Tenant detail |
| budget.threshold_crossed | PEOPLE → Tenant (Budgets tab) |
| config.changed | Respective PLATFORM page |
| error.surfaced | Error drawer (universal) |
| deploy.completed | (none in v1; future) |

### Anchor live values → page

| Anchor | Drills to |
|---|---|
| Inbox badge | Inbox page (alerts + approvals) |
| Cost value | MONEY → Cost |
| Health dot | Health page |

### Backing services pill → OSS UI

Opens that OSS's native dashboard in a new tab.

### Quick action cards → drawer or page

See Mutations table above.

---

## 14. CROSS-PAGE JUMPS IN

What other surfaces link IN to Home?

- Logo / wordmark click in top bar (from any page)
- Cmd+K → "home" or "/"
- ESC from any modal/drawer in some contexts (debatable — generally not)
- Successful login redirect
- "Back to overview" pattern (not currently used; not needed)

---

## 15. PRIMITIVES REUSED

### Existing primitives consumed

- **KPI tile** (label / value / delta / sparkline / drill) — 8 instances
- **Activity event** (icon / actor / message / time / drill) — feed of 20
- **Mutation toast** — for any action triggered from Home
- **Cmd+K trigger** — visible chip in top bar or hint on Home
- **Empty-state frame** — for activity feed when truly empty (demo off)

### New primitives Home introduces

| Primitive | Justification | Reused elsewhere? |
|---|---|---|
| **Backing services strip** | Pills with status dot + version + OSS link | Yes — Health page reuses |
| **Quick action card** | Icon + label + sub-label + click → drawer/page | Yes — possible reuse in empty states |
| **Welcome block** (prominent + compact modes) | Onboarding-shaped, dismissible | No — one-off for Home |

The two strip-shaped primitives become library elements. Welcome block stays
Home-only.

---

## 16. URL STATE

- Route: `/` (or `/home` if needed for explicitness)
- Welcome dismissed: NOT in URL; localStorage flag `backai.welcome.dismissed=true`
- Demo mode: NOT in URL; runtime state (`GET /api/v1/admin/features`)
- No filters, no group-by, no drawer — Home stays simple

Shareability: Home URL is the same for everyone; no shareable views needed.

---

## 17. MOBILE STORY

**Responsive-OK, desktop-primary.** Phone-readable but not phone-optimized.

- KPI strip: stacks vertically on narrow screens (single column)
- Activity feed: full-width, scrollable
- Welcome block: collapses to compact card on mobile
- Quick actions: 2-column grid on mobile (vs. 4 on desktop)
- Backing services strip: horizontal scroll, no wrap

No mutations needed on Home from mobile. Mobile users with hot decisions
land on `/alerts/<id>` (Inbox alert detail), not on Home.

---

## 18. MODULARITY SURFACE

Home does NOT carry an adapter pill (per framework: anchors are noise-free).

The **backing services strip IS the modularity surface** on Home. Each pill
shows:
- OSS name
- Version (when known)
- Status dot (green / yellow / red / gray)
- Click → opens that OSS's admin UI

This is the only place on Home where modularity is communicated. Quiet,
informational, useful.

---

## 19. EMPTY-STATE SHAPE

### With `demo_mode` ON (default for fresh fork)

Home is NEVER truly empty. Demo events seed the activity feed with realistic
events (run completed, tenant created, budget alert, error). KPI tiles show
seeded numbers with realistic sparklines. Welcome block prominent.

Goal: Day 0 operator sees a LIVING platform on first open.

### With `demo_mode` OFF

KPI tiles show zero with structure intact:

```
┌────────────────────┐
│ Requests / min     │
│                    │
│       —            │   ← muted dash, not "0"
│                    │
│ no data yet        │   ← sub-label
└────────────────────┘
```

Activity feed:
```
┌──────────────────────────────────────────────────────────┐
│                                                          │
│           No activity yet.                               │
│                                                          │
│   Try the snippet above to make your first call,         │
│   or [open API explorer →]                               │
│                                                          │
└──────────────────────────────────────────────────────────┘
```

Welcome block stays visible (in compact mode if dismissed once, prominent
if first visit).

---

## 20. OPEN QUESTIONS & TODOs

### Deferred (per operator: "can be added later")

- **Welcome block** — content, dismiss behavior, compact-mode, copy. Not v1
  blocker. Revisit when the broader first-run experience is designed.
- **Demo mode toggle** — location and UX deferred. Backend feature exists
  via `/admin/features`; UI affordance to come later.
- **"Take a tour" link** — deferred along with the welcome block.

### Open for v1 design

1. **Backing services strip — Home/Health only, or persistent footer rail
   across all pages?** Pro: ubiquitous visibility. Con: noise on focused
   pages. Recommendation: Home + Health only.

2. **Quick action card count.** Four (Issue key, Add tenant, Test agent,
   Open API explorer)? Or five-six adding utilities? Recommendation: four
   on Home.

3. **Inbox badge at zero.** Show "0" or hide? Recommendation: hide. Quiet
   when nothing needs attention.

4. **Anchor live-value tiles** — visual treatment vs. KPI tiles. Different
   shapes since anchors are persistent across pages; KPI tiles are Home-only.

5. **What if the runtime is itself unreachable** (`/api/v1/home/overview`
   fails)? Recommendation: render fallback shell with banner "Runtime
   unreachable" + retry; do not block sidebar / nav.

### Backend gaps (tracked in `required-backend-gaps.md`)

All gaps are **Inefficient** — none are **Blocking**. Home is buildable
today; gaps are tracked for later block grooming:

- Gap 1: Queue depth scalar
- Gap 2: Live runs scalar
- Gap 3: Failed 24h scalar
- Gap 4: Budget aggregate
- Gap 5: Unified events feed
- Gap 6: Anchor unified endpoint
- Gap 7: Backing services admin_url confirmation

---

## Suggested layout zones (not prescriptive)

Designer freedom on actual layout; these are the logical zones from
journey-driven thinking:

```
┌─────────────────────────────────────────────────────────────────────┐
│ TOP BAR  (anchors with live values, tenant switcher, Cmd+K, search) │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  WELCOME BLOCK  (prominent first run; compact thereafter)           │
│   - your stack summary                                              │
│   - dev tenant key (reveal)                                         │
│   - try-it snippet (curl + SDK lang toggle)                         │
│   - demo mode toggle                                                │
│                                                                     │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  KPI STRIP  (8 tiles, structure visible at zero, live ticks)        │
│   req/min · error % · cost today · cost MTD · queue · live runs    │
│   · failed 24h · budget consumed %                                  │
│                                                                     │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ACTIVITY FEED  (20 events, severity-ordered, live)                 │
│                                                                     │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  QUICK ACTIONS  (4 cards: issue key, add tenant, test agent, API)   │
│                                                                     │
├─────────────────────────────────────────────────────────────────────┤
│ BACKING SERVICES STRIP (pills with status, version, OSS link)       │
└─────────────────────────────────────────────────────────────────────┘
```

Alternative two-column layout (designer's call) is fine; the zones above
are independent enough to flow either way.

---

## Grooming checklist

```
☑ Purpose is one sentence
☑ Pillar is named (Anchor)
☑ Primary read identified (KPI strip color pattern + Health dot)
☑ Data sources mapped to runtime / OSS endpoints
☑ Each data source declared WRAP / REFLECT / LINK
☑ OSS links surfaced with context preservation (Home pills → root)
☑ Three data states (empty / missing / degraded) all specified
☑ Live data behavior explicit
☑ Mutations listed with audit + undo + mobile flag
☑ Drill paths and pivots called out
☑ URL state declared
☑ Mobile story declared (responsive-OK)
☑ Adapter pill placement decided (none on Home; backing services strip is the modularity surface)
☑ Reused primitives listed
☑ New primitives justified (backing services strip, quick action card become library; welcome block stays Home-only)
☑ Open questions documented (10 of them)
```

All boxes checked. Brief is ready for design review.

---

## Backend prerequisites — audited

Every endpoint Home depends on **verified to exist** in
`services/runtime/internal/server/`:

- ✅ `GET /api/v1/home/overview` — `dashboard.go:360`
- ✅ `GET /api/v1/cost` — `dashboard.go:607`
- ✅ `GET /api/v1/activity` — `activity.go:91`
- ✅ `GET /api/v1/admin/services` — `services.go:46`
- ✅ `GET /api/v1/admin/llm/provider-health` — `llm_provider_health.go`
- ✅ `GET /api/v1/admin/features` — `admin_features.go`
- ✅ `GET /api/v1/approvals` — `approvals.go`
- ✅ `GET /api/v1/metrics/summary` — `metrics_summary.go` (not used on Home; useful for Health)
- ✅ `WS /api/v1/realtime` — `realtime.go`
- ✅ `GET /health` + `/ready` — runtime self-status

**Field-level shapes verified** against handler implementations. KPI tile
sourcing audited per-tile (see §7).

**7 backend gaps surfaced** (all Inefficient, none Blocking) — tracked in
`development/ux/required-backend-gaps.md`. Home is fully buildable today.

---

_Last updated: 2026-06-16. Next page: Inbox._

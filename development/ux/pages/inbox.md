# Page Brief — Inbox

> The operator's "things needing my attention" queue. Anchor for Journey 6
> (push alert) and incident-funnel for Journey 8.
> Groomed against `development/ux/page-design-framework.md`.

---

## 1. PURPOSE

Show every pending decision or active alert that requires the operator's
attention, in one ordered list, so they can act on what's load-bearing
before it costs them.

Confidence questions answered:
- *"What needs me right now?"* (primary — Journey 6)
- *"Did anything fire while I was away?"* (Journey 8 fall-through)

---

## 2. PILLAR

Anchor (top-pinned, no group). Sibling to Home / Cost / Health.

---

## 3. JOURNEYS SERVED

| # | Journey | Frequency on Inbox | Role |
|---|---|---|---|
| 6 | Push alert: budget cap looming | Every push → mobile deep link | Direct — IS the journey |
| 8 | Provider outage / incident | Sometimes | Funnel — shows aggregated alerts |
| 2 | Daily 3-sec glance | Whenever Inbox badge > 0 | Entry-point (badge in top bar) |
| 5 | Customer-reported issue triage | Sometimes | Entry-point (Inbox surfaces tenant-touching events) |

Journeys NOT served: 1 (first-run rarely has Inbox items), 3 / 4 (dev
loops don't trigger Inbox), 7 (cost spike is a Cost-page journey, but
budget cap alerts surface here too).

---

## 4. TIME BUDGET

- **30–60 seconds** — Journey 6, mobile push → tap → decide → done
- **2–5 minutes** — desktop Inbox sweep with multiple items
- **<10 seconds** — confirm at-a-glance "nothing needs me"

---

## 5. DENSITY TARGET

**MEDIUM, list-shaped.** This is not a dashboard. It is a decision queue.
Each item has clear shape: severity / context / one or two action buttons.

Volume target: Inbox shows everything but is rarely overflowing. If the
operator has 20+ items here regularly, something is wrong (alert tuning
or platform issue).

---

## 6. PRIMARY READ

**The top item in the list.** Items are severity- + age-ordered, so the
top entry is what's most demanding of attention. Operator's eye goes to
position 1; if it's not relevant, scan downward.

For mobile (`/inbox/<id>` deep link): the PRIMARY READ is the item detail
view itself — single-focus, decision-shaped.

---

## 7. DATA SOURCES

> **Audit status**: HITL approvals confirmed; system alerts confirmed via
> `home/overview.Alerts`; broader alert types (budget thresholds, error
> spikes, provider health changes) NOT YET emitted as Inbox items. Gaps
> tracked in `required-backend-gaps.md` (Gaps 8–11).

### Runtime endpoints (WRAP) — verified

| Endpoint | Powers | Status |
|---|---|---|
| `GET /api/v1/approvals?status=pending` | HITL approvals queue | ✅ exists (`approvals.go`) |
| `GET /api/v1/approvals/{id}` | Approval detail (drawer + mobile route) | ✅ exists |
| `POST /api/v1/approvals/{id}/decide` | Approve / deny mutation | ✅ exists |
| `GET /api/v1/home/overview` | `Alerts` array — system-level alerts (AgentField unreachable, DB unhealthy currently) | ✅ exists; **limited scope** — see Gap 9 |
| `WS /api/v1/realtime` | Live updates: new items appear, decided items vanish | ✅ exists |

### Item type sourcing — audited per type

| Item type | Source | Status |
|---|---|---|
| HITL approval | `approvals` table via `/api/v1/approvals` | ✅ shipping |
| AgentField unreachable | `home/overview.Alerts` (probed on each call) | ✅ shipping |
| DB unhealthy | `home/overview.Alerts` (probed on each call) | ✅ shipping |
| Budget threshold crossed (tenant at 80% / 90% / 100%) | ❌ not emitted as inbox item today | **Gap 9** |
| Error spike (rate jumps above baseline) | ❌ not detected today | **Gap 9** |
| LLM provider degraded | `admin/llm/provider-health` exists but not surfaced as Inbox item | **Gap 9** |
| Queue backpressure (jobs stuck) | ❌ not detected today | **Gap 9** |
| Abuse signal (suspicious tenant) | ❌ not detected today | future |

### Composite Inbox endpoint

⚠️ **Gap 8** — Inbox currently requires the dashboard to merge two
sources client-side (`approvals` + `home/overview.Alerts`). A unified
`GET /api/v1/admin/inbox` would simplify both desktop + mobile clients.

### Mobile detail route

⚠️ **Gap 11** — mobile push notification deep-links to
`/inbox/<item_id>` need a small endpoint or extension to fetch a single
inbox item by its composite id (e.g., `approval:abc123` or `alert:xyz`).

### OSS sources (indirectly via runtime — none queried directly)

None. Inbox is fully WRAP.

### Computed client-side

- Severity-aware ordering (critical → warn → info, then by age within tier)
- Age display ("2m ago" / "3h ago" / "yesterday")
- Decision-shape per item type (approve/deny vs. acknowledge/extend)
- Empty-state affirmative copy

### Backend gap summary for Inbox

| Gap | Severity | Action |
|---|---|---|
| 8. Unified Inbox endpoint | Inefficient | Merge client-side in v1; add `/admin/inbox` later |
| 9. Inbox-emitted events (budget / error / provider / queue) | **Blocking for full Inbox value** | Need runtime emitters to surface these as Inbox items |
| 10. Acknowledge / dismiss mutation for non-approval items | Blocking for mute UX | Need an "alerts.ack" endpoint |
| 11. Mobile single-item fetch by composite id | Blocking for Journey 6 | Either extend `/admin/inbox?id=X` or per-type fetches |

**Net assessment**: Inbox can ship in v1 with **approvals + 2 system
alerts**. Without Gaps 9–11 it's a partial surface (HITL only).
Recommended: ship as "Approvals + system alerts" in v1, expand to richer
Inbox in v0.2 once emitters land.

---

## 8. WRAP / REFLECT / LINK

| Surface | Pattern | Why |
|---|---|---|
| Approval items | WRAP | We own the workflow; mutation must audit |
| System alerts | WRAP | Our health probes; our event |
| Budget alerts (future) | WRAP | Our cost ledger emits |
| Item → tenant detail (drill) | WRAP (internal nav) | Stays in admin |
| Item → run detail (drill) | WRAP (internal nav) | Stays in admin |
| Item → AgentField for run depth | REFLECT | Only when operator wants step-tree depth on the run |

---

## 9. OSS LINKS SURFACED

None directly from Inbox itself. Inbox items drill INTO admin pages
(tenant detail, run drawer) which carry their own adapter pills + OSS
links.

---

## 10. THREE DATA STATES

### EMPTY (nothing needs the operator)

This is the **happiest state**. Render an affirmative "all clear" rather
than a sad empty page:

```
┌─────────────────────────────────────────┐
│                                         │
│              ✓  All clear               │
│                                         │
│   Nothing needs your attention now.     │
│   Last action: 2 hours ago              │
│                                         │
└─────────────────────────────────────────┘
```

Tone: positive, quiet, dignified. No "no items" / "you're empty."

### MISSING (subsystem disabled)

If `approvals` feature is disabled (`admin/features`), Inbox still works
for system alerts. If both are disabled, Inbox is hidden from the
sidebar (no badge).

### DEGRADED (Inbox source unhealthy)

Banner at top: "Inbox may be incomplete — approval service unavailable.
[Retry]"

Last successful fetch timestamp visible. List shows what cached / last
fetched.

---

## 11. LIVE DATA

### WebSocket subscriptions

- New Inbox items appear at top with subtle entry animation
- Decided / dismissed items vanish with subtle exit animation
- Badge count on top-bar anchor updates immediately

### Polled fallback

- 30s polling if WebSocket lost

### Animations

- New item: slides in from top, brief highlight (~600ms), then settles
- Item resolved (approved/denied/dismissed): fades and collapses; brief
  toast confirms
- Badge: subtle pop on increment; quiet decrement

---

## 12. MUTATIONS

| Mutation | Surface | Audit | Undo | Mobile |
|---|---|---|---|---|
| Approve approval | Item primary action | Yes (`approval.decided`) | No (forwards to workflow) | **Yes — primary mobile path** |
| Deny approval | Item primary action | Yes (`approval.decided`) | No (forwards to workflow) | **Yes** |
| Cancel approval | Item secondary action | Yes | No | Yes |
| Acknowledge system alert | Item action | Yes (`alert.acknowledged`) | Yes (re-open) | Yes |
| Extend budget (when budget-alert item) | Item action → opens tenant drawer | Yes (via budget endpoint) | Yes | Yes — single tap |
| Suspend tenant (when abuse-alert item) | Item action → confirm modal | Yes | Yes | No (destructive — desktop only) |
| Open in admin (drill) | Item secondary action | n/a (nav) | n/a | n/a |

Mobile-critical: approve / deny / extend / acknowledge. All single-tap with
confirmation.

---

## 13. DRILL PATHS

### Item → entity detail (in-admin)

| Item type | Drills to |
|---|---|
| HITL approval | Approval detail drawer (right-side, full payload + decision form) |
| Budget alert (future) | PEOPLE → Tenant detail (Budgets tab) |
| Provider degraded | Health page → Providers section |
| Error spike | ACTIVITY → Errors filtered to time window + pattern |
| Run-related approval | Run drawer with `approval` highlighted |

### Item → OSS (rare, REFLECT only)

| When | Drills to |
|---|---|
| Need full DAG of a blocked run | "Open run in AgentField ↗" preserves trace ID |

### Mobile route

| Push notification | Opens `/inbox/<item_id>` mobile-optimized page |

---

## 14. CROSS-PAGE JUMPS IN

What other surfaces link IN to Inbox?

- Top-bar anchor "Inbox" badge from any page
- Push notification → `/inbox/<id>` mobile route
- Cmd+K → "inbox" or "approvals" or "/inbox"
- Activity feed events of type `approval.requested` → linked to Inbox

---

## 15. PRIMITIVES REUSED

### Existing primitives consumed

- **Activity event** (close cousin) — but Inbox items are decisions, not
  events. Different shape: severity / context / action(s).
- **Drawer** — approval detail drawer (right-side)
- **Mutation toast** — confirm + audit ref
- **Empty-state frame** — affirmative variant (positive copy)

### New primitives Inbox introduces

| Primitive | Justification | Reused elsewhere? |
|---|---|---|
| **Inbox item card** | Severity badge / title / context / age / one-or-two action buttons | Yes — feed-of-decisions pattern; reusable on tenant detail's "open decisions" tab |
| **Mobile item route** (`/inbox/<id>`) | Single-purpose mobile decision view | Yes — pattern for other `/alerts/<id>` paths |
| **Affirmative empty state** | "All clear" tone, distinct from "no data" | Yes — used wherever absence-is-good (resolved errors, etc.) |

---

## 16. URL STATE

- List: `/inbox`
- Filter chip (severity / kind): `?severity=critical&kind=approval` URL-persistent
- Item detail (desktop drawer): `/inbox?item=<id>` opens drawer
- Item detail (mobile full page): `/inbox/<id>` standalone route

URL-state ensures: badge → tap → land on exact item, shareable links between
operator teammates (when teams ship), survives reload.

---

## 17. MOBILE STORY

**Inbox IS the mobile-critical surface.** First — and currently only —
admin route that needs first-class mobile design.

### Mobile list (`/inbox`)

- Stacked cards, one per item
- Severity + age front-and-center
- Tap card → mobile item detail

### Mobile item detail (`/inbox/<id>`)

- Single-focus, decision-shaped
- Context up top (tenant name, what triggered)
- Recent metric (last 7d trend / current state)
- 1-3 action buttons (Approve / Deny / Cancel; OR Extend / Hold; OR
  Acknowledge / Investigate)
- Each button taps once → confirmation modal → single tap to commit
- Confirmation written; back to mobile inbox

No deep drill, no pivots, no group-by. **One question, decisive answer.**

---

## 18. MODULARITY SURFACE

Inbox does NOT carry an adapter pill (per framework: anchors are
noise-free, and Inbox aggregates events from many subsystems).

The Health anchor + the items themselves communicate modularity (e.g., a
"Tempo unreachable" item names its adapter).

---

## 19. EMPTY-STATE SHAPE

See §10. Affirmative tone, last-action timestamp, no CTA needed.

Distinct from "no data yet" (which is sad / informational). Inbox empty
is GOOD.

---

## 20. OPEN QUESTIONS & TODOs

### Deferred to v0.2

- **Rich Inbox sources** (budget / error / provider / queue alerts) —
  requires runtime emitters (Gap 9). Ship v1 with approvals + 2 system
  alerts; expand in v0.2.
- **Inbox history / dismissed view** — "show what I've already handled"
  view. Helpful for audit but not v1 critical.
- **Alert tuning UI** — threshold sliders for what becomes an Inbox
  item. v0.2.
- **Push channel preferences** — where alerts get pushed (email / Slack /
  SMS) — config lives in `PLATFORM → Notifications` (already groomed).
  Mobile push specifically is a v0.2 add.

### Open for v1 design

1. **Empty-state copy.** "All clear" + timestamp? Or "0 pending" + tenant
   stats? Recommendation: the affirmative one — operators should feel
   GOOD when Inbox is empty.

2. **Sort order.** Severity first then age? Or strict chronological?
   Recommendation: severity-tiered (critical / warn / info), then
   newest-first within tier.

3. **Item card density.** Compact (1-line) or expanded (3-line with
   context)? Recommendation: expanded — operator needs context before
   deciding.

4. **Per-item severity colors.** Critical = red, warn = yellow, info =
   gray. Confirm against framework Part 8.

5. **Inbox badge counting behavior.** Does it count only `critical` items?
   All pending? Recommendation: count all pending; severity is conveyed
   via badge color (or always-red badge for any pending — debate).

6. **Approval payload depth on mobile.** How much of the approval payload
   shows before requiring a "view full" expand? Recommendation: 3-4
   key fields + "view full" expand for the JSON blob.

### Backend gaps (tracked in `required-backend-gaps.md`)

- Gap 8 — Unified Inbox endpoint (Inefficient; merge client-side v1)
- Gap 9 — Inbox-emitted events (**Blocking** for full Inbox value;
  partial v1 ship without)
- Gap 10 — Acknowledge / dismiss mutation for non-approval items
  (**Blocking** if we ship alerts in v1)
- Gap 11 — Mobile single-item fetch by composite id (**Blocking** for
  Journey 6)

---

## Suggested layout zones (not prescriptive)

```
┌─────────────────────────────────────────────────────────────────────┐
│ TOP BAR  (anchors; Inbox shows badge for own page count)            │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  FILTER CHIPS  (severity, kind, mute-status)                        │
│                                                                     │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ITEM LIST  (severity-tiered, newest-first within tier)             │
│                                                                     │
│  ┌─ CRITICAL ─────────────────────────────────────────────┐         │
│  │ AgentField unreachable                            14s  │         │
│  │ [Acknowledge]  [Open Health →]                         │         │
│  └─────────────────────────────────────────────────────────┘         │
│                                                                     │
│  ┌─ WARN ─────────────────────────────────────────────────┐         │
│  │ Approval: refund $128 for acme                    2m   │         │
│  │ Customer note: "charged twice for invoice INV-04812"   │         │
│  │ [Approve]  [Deny]  [Cancel]                            │         │
│  └─────────────────────────────────────────────────────────┘         │
│                                                                     │
│  ... more items ...                                                 │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

Mobile layout: single column, full-width cards, tap → `/inbox/<id>`.

---

## Grooming checklist

```
☑ Purpose is one sentence
☑ Pillar is named (Anchor)
☑ Primary read identified (top of severity-ordered list)
☑ Data sources mapped to runtime endpoints
☑ Each data source declared WRAP / REFLECT / LINK
☑ OSS links surfaced (none direct; drills carry their own)
☑ Three data states (empty / missing / degraded) all specified
☑ Live data behavior explicit
☑ Mutations listed with audit + undo + mobile flag
☑ Drill paths and pivots called out
☑ URL state declared
☑ Mobile story declared — Inbox IS mobile-critical
☑ Adapter pill placement decided (none)
☑ Reused primitives listed
☑ New primitives justified (Inbox item card, mobile item route,
  affirmative empty state)
☑ Open questions documented
☑ Backend gaps surfaced (8, 9, 10, 11) and severity flagged
```

All boxes checked. Brief is ready for design review with the explicit
note: **v1 ships Inbox as Approvals + system alerts only**, expanding
once runtime emitters land for budget / error / provider / queue events.

---

_Last updated: 2026-06-16. Next page: Cost._

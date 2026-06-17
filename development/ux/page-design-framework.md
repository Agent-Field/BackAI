# BackAI Admin — Page Design Framework

> The lens we apply BEFORE grooming any admin page.
> Output of using this framework on a page = a Page Brief that becomes input
> to design + dev.
> Companion to `development/ux/journeys-v1.md`.

---

## How to use this doc

When we're about to groom a specific page (next steps will groom Home,
Errors, Cost, Tenant detail, etc. one-by-one), we walk through this framework
first. The result is a consistent, comparable Page Brief. The Page Brief is
input to design; design is input to build.

**The framework is the philosophy. The Page Brief is the artifact.**

---

## Part 1 — The Page Brief template

Every grooming session produces a Page Brief that answers these in order.
No skipping. If we don't know an answer, that gap is the question to resolve.

```
PAGE BRIEF — <page name>

1. PURPOSE
   One-sentence job. What this page exists to do.
   What confidence question does it answer?

2. PILLAR
   Which sidebar pillar: anchor / ACTIVITY / PEOPLE / BUILD / PLATFORM

3. JOURNEYS SERVED
   Which of the 8 canonical journeys land here.
   Annotate each with frequency (every visit / sometimes / rare).

4. TIME BUDGET
   How long the operator should spend here.
   3 sec assess / 30 sec drill / 5 min investigate / 15 min build.

5. DENSITY TARGET
   high  — page is a wall of structured info (Cost, Errors, Logs, Tenant)
   medium — focused but populated (Agent detail, Run drawer)
   low   — single-action surface (Mobile alert detail, Confirmation)

6. PRIMARY READ
   The ONE thing on the page that satisfies most visits.
   Above-the-fold; eyes hit it first; communicates the answer in <3 sec.

7. DATA SOURCES
   - Runtime endpoints (path + verb)
   - OSS service endpoints (LiteLLM /spend, Tempo /api/search, etc.)
   - Computed client-side (forecast regressions, deltas, etc.)

8. WRAP vs REFLECT vs LINK  (per data source)
   For each source, declare:
   - WRAP    = we present in our UI, full control
   - REFLECT = we present a summary; depth lives in OSS UI
   - LINK    = we don't present at all; button to OSS UI
   See Part 2 for criteria.

9. OSS LINKS SURFACED
   What OSS UIs we link to from this page, with what context.
   Example: "Open this run in AgentField (preserves trace ID)"
   Example: "Open LiteLLM admin for advanced routing"

10. THREE DATA STATES
   How the page renders when:
   - empty    = adapter capable, no data yet
   - missing  = adapter doesn't expose this capability
   - degraded = adapter unhealthy / response stale
   See Part 4.

11. LIVE DATA
   What ticks in real-time (WebSocket / polling).
   What animation conveys updates.

12. MUTATIONS
   What can be changed from this page.
   For each: form-shape, audit entry, undo affordance, mobile-needed.

13. DRILL PATHS
   Where rows / cells / cards lead to.

14. CROSS-PAGE JUMPS
   What other pages link IN to here (from Activity feed, Cmd+K, etc.).

15. PRIMITIVES REUSED
   From the shared primitives library:
   Drawer / KPI tile / group-by control / filter chip / mutation toast /
   tenant scope switcher / adapter pill / empty-state frame / activity event /
   live-tick / etc.

16. URL STATE
   What's persistent in the URL so the view is shareable + reload-safe.
   Filters, group-by, drawer-open, tab.

17. MOBILE STORY
   desktop-only — most pages
   mobile-detail — alert detail pages
   responsive    — Home (read-only on phone is fine)

18. MODULARITY SURFACE
   Where the adapter pill appears.
   Which OSS link sits in the footer.

19. EMPTY-STATE SHAPE
   UI-shaped frame (KPI structure visible at zero).
   Try-it snippet OR demo-data button OR explanatory link.
   Never just "no data."

20. OPEN QUESTIONS
   What we don't know yet. Things to verify before design starts.
```

---

## Part 2 — The Wrap / Reflect / Link spectrum

Our biggest design lever. We don't replicate OSS UIs. We use them, link to
them, or wrap them — three distinct patterns.

### The spectrum

```
WRAP                          REFLECT                       LINK
─────                         ───────                       ────
We own the UI.                We show an index +            We show nothing.
We own the data shape.        a deep link.                  Button → OSS UI.
80% of operator time.         20% — quick scan +            <5% — monthly visit.
                              occasional deep dive.

EXAMPLES:                     EXAMPLES:                     EXAMPLES:
- Cost page                   - Agent detail (we show       - Open MinIO console
- Runs list                     name + reasoners + cost;    - Open Stripe dashboard
- Errors page                   AgentField has DAG /        - Open Prometheus
- Tenant detail                 step inspector / source)
- Inbox                       - Run drawer (we show input /
- Health                        output / cost; AgentField
- Playground                    has full span tree)
                              - Webhook flow (we show
                                deliveries; Svix has
                                replay archive)
                              - Trace summary (we show
                                summary; Tempo has full
                                TraceQL)
```

### Decision criteria

Use WRAP when ANY of these are true:
- Multi-tenant filtering must apply (cost, runs, audit)
- Our audit log must record the mutation
- It's a daily/hourly surface (>80% of operator time)
- The OSS doesn't have a UI worth linking to

Use REFLECT when ALL of these are true:
- We have the index data (list, summary, recent rows)
- OSS has DEEPER data we don't replicate (DAG, span tree, replay archive)
- Operator wants quick scan in admin + occasional deep dive in OSS
- The jump must preserve context (trace_id, run_id, tenant)

Use LINK when ANY of these are true:
- We have no index data (just an env var saying it exists)
- OSS UI is genuinely better than anything we'd build
- Operator goes monthly, not daily (MinIO console, Stripe deep billing)
- The OSS has its own auth model we don't want to dual-auth

### Reflect — the design pattern

When we REFLECT, the row / card has 4 elements:

```
┌─────────────────────────────────────────────────────────────────────┐
│ <our summary data>                                  [ Open in X ↗ ] │
│ id · name · status · recent-metric                                  │
│ subtle adapter pill: via <OSS> v<N>                                 │
└─────────────────────────────────────────────────────────────────────┘
```

- Our summary (what we own)
- Deep-link button with `↗` (preserves entity ID in destination)
- Adapter pill (modularity signal)
- That's it. No replication of OSS internals.

### Example: Agent execution detail

User's specific example. This is REFLECT, not WRAP.

| Element | Where it lives |
|---|---|
| Agent registry, reasoner list, recent runs, cost trend | WRAP (our admin) |
| Per-reasoner schema preview | WRAP (small summary) |
| Full DAG with step-by-step execution | LINK ("Open in AgentField") |
| Reasoner source code | LINK |
| Memory store inspection per run | LINK |
| Trace span tree for the run | REFLECT (we show summary; "Open full trace in Tempo") |

Operator stays in admin for 80% of cases. When they need to debug a weird
reasoner step, one click jumps them to AgentField with the run_id preselected.

---

## Part 3 — The cross-link philosophy

Pages talk to each other. The journeys revealed which paths matter.

### Three jump types

1. **DRILL** — list row → detail drawer (same page, no navigation)
   - Runs row → run drawer
   - Errors row → error drawer
   - Tenant row → tenant detail page (this one IS a page nav)

2. **PIVOT** — cell value → another page filtered to that value
   - Cost cell (tenant=acme) → ACTIVITY → Runs filtered to acme
   - Errors row (model=claude-sonnet) → MONEY → Cost filtered to model
   - Agent name in any drawer → BUILD → Agents → that agent

3. **JUMP OUT** — our row → OSS UI with context
   - Run drawer "Open in AgentField" → AgentField with trace_id selected
   - Cost row "Open in LiteLLM" → LiteLLM admin filtered to virtual key
   - Storage file → MinIO console for that bucket

### Context preservation

When we jump (pivot or out), we MUST preserve context. No "land on the OSS
home page and search again." Examples:

- Drilling into a tenant → URL has `?tenant_id=X` and that filter applies on
  the destination page
- Opening a run in AgentField → URL has the run/trace ID; AgentField opens
  on that specific record
- Opening LiteLLM admin from Cost → if LiteLLM supports it, pass the virtual
  key or tag as query string

If the OSS doesn't support context-preserving deep links, we still link out,
but the brief should call out this friction.

### Cmd+K as universal jump

Cmd+K resolves any entity by string match:
- tenant email / id / name → tenant detail
- run id → run drawer (from anywhere)
- "acme costs" → MONEY → Cost filtered to acme
- "anthropic errors" → ACTIVITY → Errors filtered to anthropic models
- "rotate keys" → action shortcut

Every page brief should list **which Cmd+K queries land here** (this drives
the palette's resolver design).

---

## Part 4 — The three data states

The biggest UX trap is treating these as the same. They are not.

```
┌─────────────────────────────────────────────────────────────────────┐
│ EMPTY                                                                │
│   Adapter is capable. No data yet (fresh fork, no activity).        │
│   Render: UI-shaped frame with structure + try-it snippet +          │
│           "demo data on" hint.                                       │
│   Tone:   "Make your first call to see this come alive."             │
│   Color:  Neutral. No alarm.                                         │
├─────────────────────────────────────────────────────────────────────┤
│ MISSING                                                              │
│   Adapter doesn't expose this feature at all.                        │
│   Render: Capability notice + adapter name + alternative path.       │
│   Tone:   "Your current <slot> adapter doesn't support X."           │
│   Color:  Subtle, informational. NOT red.                            │
├─────────────────────────────────────────────────────────────────────┤
│ DEGRADED                                                             │
│   Adapter is configured but unhealthy / stale / failing.             │
│   Render: Warning banner + last-known data + retry / view-health.    │
│   Tone:   "Data may be stale — adapter health check failing."        │
│   Color:  Yellow (warning), not red (alarm).                         │
└─────────────────────────────────────────────────────────────────────┘
```

Each page brief must specify the rendering for all three.

The TRAP: rendering the same "No data" text in all cases makes operators
think their setup is broken when really their adapter just doesn't do that
thing. Visual + verbal differentiation matters.

---

## Part 5 — Shared primitives library

Build once, use everywhere. Each page brief lists which it reuses.

| Primitive | Used by | Shape |
|---|---|---|
| **Drawer** | Runs, Errors, Deliveries, Sandbox runs, Jobs, Approvals, Tenant rows | Header (id, status, when) / overview tile / collapsible details / actions footer |
| **KPI tile** | Home, Cost, Health, Tenant detail | Label / value / delta / sparkline / drill |
| **Group-by pill row** | Cost, Errors, Runs, Activity | Single-click switches dimension |
| **Filter chip set** | Every list view | URL-persistent, shareable |
| **Mutation toast** | Every mutation | Confirm + audit ref + undo where reversible |
| **Tenant scope switcher** | Top bar | Re-scopes ACTIVITY / PEOPLE / BUILD pages |
| **Adapter pill** | Every wrapped page | Subtle "via X" badge; click reveals admin link |
| **Empty-state frame** | Every page | UI-shaped structure even at zero |
| **Activity event** | Activity feed, audit, alerts | Timestamp / icon / actor / message / drill |
| **Live-tick** | Anchors, Cost, Errors during incident, Playground | Value updates with subtle pulse, no flash |
| **Cmd+K palette** | Global | Categories: jump / action / docs / external |
| **Owner ribbon** *(future)* | Setup pages only | "Backed by X" — not on operating pages |

Adding a new primitive is a design decision. Page briefs that propose new
primitives must justify why the existing library isn't enough.

---

## Part 6 — Information density per journey-mode

Density matches the journey, not a global default.

| Journey mode | Density | Why |
|---|---|---|
| Glance (Journey 2) | High but scannable | Operator wants single-screen status; multiple signals at once |
| Drill (Journeys 5, 7) | High and pivot-heavy | Operator is investigating; multi-axis data + group-by |
| Build (Journeys 3, 4) | Medium, focused | Operator is testing; clear input → output flow, no clutter |
| Incident (Journey 8) | High + live | Operator needs everything visible + real-time updates |
| Decision (Journey 6) | Low + decisive | Mobile / time-pressed; ONE question, ONE-tap answer |

**The volume principle (from journeys-v1) applies globally**: pages should
feel full, empty states should look alive. But within "full," there's a
spectrum from glance to deep-investigate.

---

## Part 7 — Live data conventions

What ticks in real-time and how it feels.

### What gets WebSocket subscriptions

- Home KPI strip (req/min, error rate, cost, queue)
- Anchor live values (Cost daily total, Inbox count, Health dot)
- Cost page during active period
- Errors page during incident
- Runs list during active workload
- Playground during a run

### What gets polled instead

- Health page service checks (5-10s polling)
- Adapter status pills (30s polling)

### What's static / on-demand

- Audit log, historical Cost views, settled Runs
- Configuration pages (Setup)

### Animation conventions

- Value change: subtle 200ms ease, no flash
- New row appears: top of list, brief highlight, no jump
- Delta arrow: animates between ▴ / ▾ / ▬ as direction changes
- Connection drop: small "reconnecting…" chip, not a banner
- Stale data: timestamp turns muted; no visual scream

The principle: live data should be *noticeable peripheral motion*, not the
center of attention. The operator looks AT a number, not THROUGH animations.

---

## Part 8 — Numerical + time conventions

Consistency across pages so operator's eye learns the patterns.

### Time

- "now" — within 30s
- "2m ago" / "14m ago" — within 1h, relative
- "2h ago" / "5h ago" — within 24h, relative
- "yesterday 14:32" / "Mon 09:18" — within 7d
- "Mar 14" — within 365d
- "2025-03-14" — older
- Hover any of these → full ISO timestamp tooltip

### Cost

- USD always
- Display precision: $0.0001 (4 decimals) at run level; $0.01 at daily aggregate; $1 at monthly
- Delta as ▴18% (green up = bad for cost), ▾18% (green down = good for cost)
  - INVERTED meaning for cost vs. revenue — color carries semantic
- Daily total in anchor always 4 decimals if <$1, 2 decimals otherwise

### Counts

- Whole numbers, comma-separated >999
- "1.2k" / "342" / "42" / "0" — pick precision matching the scale

### Status

- Use color sparingly. Green / yellow / red carry meaning. Avoid gratuitous color.
- Three states per signal: ok / watch / act
- Gray = no data, not "ok"

### Percentages

- 1 decimal at small values (12.3%), whole at large (78%)
- Threshold-aware color: 80% budget green, 90% yellow, 100% red

---

## Part 9 — Modularity awareness on pages

When a page is backed by an adapter, it shows a pill. Quietly.

### Adapter pill spec

```
                                              via LiteLLM ▾
                                              ─────────────
```

- Top-right of page content area
- Type-secondary, no background fill, just a small chip
- Click reveals dropdown:
  - "View adapter docs"
  - "Open <adapter> admin ↗"
  - "Change adapter → PLATFORM → Adapters"

### When the pill is required

- Cost (LiteLLM), Runs (AgentField), Storage (MinIO), Queue (River),
  Webhook flow (Svix), Sandboxes (Docker/etc.), Traces (Tempo), Logs (Loki),
  Metrics (Prometheus), Errors (GlitchTip)

### When the pill is NOT used

- Anchors (Home, Inbox, Cost anchor, Health) — would be noisy
- Setup pages — they ARE the adapter config
- Pages without a primary adapter (Audit, Activity feed)

### Three data states + the pill

- Empty → pill is muted ("via <adapter> · no data yet")
- Missing → no pill (slot not configured)
- Degraded → pill is yellow ("via <adapter> · stale 14m")

---

## Part 10 — Mobile considerations

The default is desktop. Mobile is a specific surface for specific journeys.

### What needs mobile

- **Inbox alert detail (`/alerts/<id>`)** — Journey 6, the only critical mobile path
- **Single-action confirmation pages** — extend budget, suspend, approve

### What's responsive-OK but desktop-primary

- Home — readable on phone, no mutations needed
- Tenant detail header — read-only snapshot is fine
- Errors page — for triage on the go

### What's desktop-only

- Cost page (multi-axis, pivot-heavy)
- Playground (substantial input/output)
- Data → SQL (code editor)
- API explorer (Scalar embed)
- Build pages (Agents, Modules, etc.)
- Setup pages

### Mobile design rules

- Inbox alert pages are SINGLE-PURPOSE
- One question, 2-3 tap options
- Tap option → confirmation modal with single tap to commit
- No deep drill, no pivots, no group-by

---

## Part 11 — The grooming checklist

After producing a Page Brief, run through these before accepting it:

```
☐ Purpose is one sentence
☐ Pillar is named
☐ Primary read is identified (what answers most visits in <3 sec)
☐ Data sources mapped to runtime / OSS endpoints
☐ Each data source declared WRAP / REFLECT / LINK
☐ OSS links surfaced with context preservation
☐ Three data states (empty / missing / degraded) all specified
☐ Live data behavior explicit
☐ Mutations listed with audit + undo + mobile flag
☐ Drill paths and pivots called out
☐ URL state declared
☐ Mobile story declared
☐ Adapter pill placement decided
☐ Reused primitives listed
☐ New primitives (if any) justified
☐ Open questions documented
```

If any box can't be checked, that's the next question. Don't ship to design
with open gaps; the Page Brief is the contract.

---

## Part 12 — How grooming pages will go from here

The intended cadence:

1. **Pick a page**, ideally in dependency order (Home before Cost; Cost
   before Tenant detail; Tenant detail before Compose-issue drawer).
2. **Walk this framework** to produce a Page Brief in a new markdown file
   at `development/ux/pages/<page-name>.md`.
3. **Review the brief** before moving to design — the framework's purpose
   is to surface gaps and decisions BEFORE pixels are placed.
4. **Repeat** for the next page. Cross-reference pages where they share
   primitives or drill paths.

The brief is the contract between product + design + dev. The framework is
the lens that produces consistent briefs.

---

## Suggested grooming order

Prioritized by impact + dependency:

| Order | Page | Rationale |
|---|---|---|
| 1 | **Home** | Anchor for 6 of 8 journeys. Sets density tone. Most-trafficked surface. |
| 2 | **Inbox** | Highest-stakes anchor. Drives mobile design + push channels. |
| 3 | **Cost (anchor + page)** | Existential for indie hackers. Drives MONEY pillar. |
| 4 | **Health (anchor + page)** | Drives PLATFORM observability + recovery actions. |
| 5 | **Drawer primitive** | Reused across 7+ surfaces. Designing it first unblocks Runs / Errors / etc. |
| 6 | **Runs** (ACTIVITY) | Most-common drill surface; tests the Drawer primitive. |
| 7 | **Tenant detail** (PEOPLE) | Customer-care anchor. Tests multi-tab + mutation patterns. |
| 8 | **Errors** (ACTIVITY) | Tests suggested-action / pattern-cluster patterns. |
| 9 | **Agent detail + Playground** (BUILD) | Tests REFLECT pattern (link to AgentField) + substantial Playground surface. |
| 10 | **PLATFORM → Adapters** | Tests modularity surface + three-state rendering. |

Other pages (Logs, Traces, Sandboxes, Data, Modules, etc.) inherit patterns
from the first 10.

---

_Last updated: 2026-06-16. Framework precedes individual page briefs at
`development/ux/pages/<page>.md`._

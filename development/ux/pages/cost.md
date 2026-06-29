e tra# Page Brief — Cost

> The platform's USP page. Inference economics for an AI backend.
> Anchor for live spend awareness; rich page for explain/explore/optimize.
> Groomed against `development/ux/page-design-framework.md`.

---

## 1. PURPOSE

Show the operator where their inference spend is going, surface anomalies
with paired actions, and make budget/optimization decisions one-click.

Confidence questions answered:
- *"Where's my money going right now?"* (Journey 7 — primary)
- *"Will I run out of budget?"* (planning mode, monthly)
- *"Is anything spiking?"* (Journey 2 anchor + glance)
- *"How do my unit economics look?"* (occasional, indie-hacker math)

---

## 2. PILLAR

Anchor (top-pinned live tile `$X.XX ▴N%`) + full page reached via click.

---

## 3. JOURNEYS SERVED

| # | Journey | Frequency | Role |
|---|---|---|---|
| 7 | Cost-spike investigation | Reactive | Direct — IS the journey |
| 2 | Daily glance | Every visit (anchor only) | Anchor live tile |
| 6 | Budget cap alert | Reactive (mobile alert detail) | Drill destination from Inbox |
| 8 | Incident — provider mix change | Sometimes | Cross-cut from Errors |

Journeys NOT served: 1 (first-run shows zero), 3/4 (dev loops), 5
(customer triage uses tenant detail).

---

## 4. TIME BUDGET

- **3 seconds** — anchor live tile in nav (Journey 2)
- **5-15 minutes** — Journey 7 (cost-spike detective)
- **20-30 minutes** — monthly planning + budget setting
- **30 seconds** — mobile alert detail (drill from Inbox)

---

## 5. DENSITY TARGET

**HIGH and structured.** The most data-rich page in the admin. Four zones
serve three operator modes. Each zone is dense; zones are visually
separated.

---

## 6. PRIMARY READ

**The Zone 1 anomaly strip + the Forecast bar.** Operator opens Cost,
eyes go to the top, reads:
- Today's number + delta vs same time yesterday
- Whether forecast blows budget
- 2-3 callouts highlighting WHAT is unusual

If nothing's anomalous and forecast stays under budget → operator leaves
in 5 seconds. If something's anomalous → operator drills into Zone 2.

---

## 7. STRATEGIC SHAPE (locked from prior ideation)

**Cost is an inference economics page**, not a billing dashboard. The
core abstraction is the **inference event**, which has lineage:

```
Tenant → User → Agent → Reasoner → Model + Tool + Sandbox
```

Every $0.0001 has this lineage. The page exposes it as a hierarchy.

Four zones serve three operator modes:

| Zone | Mode | Purpose |
|---|---|---|
| 1 — Anomaly Strip | Glance | "Is anything spiking?" + 3-sec answer |
| 2 — Explain-Spend Hierarchy | Detective | "Where's my money going?" lineage tree |
| 3 — Slice-Over-Time Explorer | Detective | Pivot by any dimension over time |
| 4 — Inference Economics | Planning | Unit economics + budgets + cache |

---

## 8. DATA SOURCES

> **Audit status**: `/api/v1/cost`, `/api/v1/cost/events`,
> `/api/v1/admin/budgets`, `/api/v1/admin/budgets/{tenantId}`, and
> `PUT /api/v1/admin/budgets` verified to exist
> (`services/runtime/internal/server/cost.go`). Field-level shapes
> verified against handler implementations. **Several rich-design
> features require backend gaps; see §8.6.**

### 8.1 Runtime endpoints (WRAP) — verified

| Endpoint | Powers | Status |
|---|---|---|
| `GET /api/v1/cost?from=&to=&tenant=` | Zone 1 totals · Zone 2 root level · Zone 3 base aggregates · Zone 4 totals | ✅ exists (`cost.go:607`) |
| `GET /api/v1/cost/events?tenant=&model=&request_id=&from=&to=&limit=&offset=` | Drill from chart cell to raw events; Zone 2 leaf inspection | ✅ exists (`cost.go:213`) |
| `GET /api/v1/admin/budgets` | Zone 4 budgets table | ✅ exists (`cost.go:273`) |
| `GET /api/v1/admin/budgets/{tenantId}` | Per-tenant budget edit | ✅ exists |
| `PUT /api/v1/admin/budgets` | Set/update budget (upsert) | ✅ exists |
| `GET /api/v1/home/overview` | `CostTodayUSD` for anchor live tile | ✅ exists |
| `GET /api/v1/llm/cache/stats` | Zone 4 cache donut | ✅ exists (`server.go`) |
| `WS /api/v1/realtime` | Live ticks for anchor + Zone 1 today number | ✅ exists |

### 8.2 `/api/v1/cost` response shape (verified)

```ts
{
  period_total_usd: number,
  previous_total_usd: number,
  budget_usd: number | null,
  forecast_usd: number,
  by_day: [{ date: string, cost_usd: number }],
  by_model: [{ model: string, cost_usd: number }],
  by_agent: [{ agent: string, cost_usd: number }],
  by_tenant: [{ tenant_id: string, tenant_name: string, cost_usd: number }],
}
```

Default period = month-to-date UTC. Accepts `from`, `to`, `tenant` filters.

### 8.3 Per-zone source mapping

| Zone | Source | Status |
|---|---|---|
| Zone 1 — Today total + delta | `/home/overview.CostTodayUSD` + `/cost` `period_total_usd` / `previous_total_usd` | ✅ |
| Zone 1 — Forecast vs budget | `/cost` `forecast_usd` / `budget_usd` | ✅ |
| Zone 1 — Anomaly callouts | Compute client-side from `/cost` `by_tenant` × `by_day` | ⚠️ partial — see **Gap 12** |
| Zone 2 — Tenant root | `/cost` `by_tenant` | ✅ |
| Zone 2 — Agent under tenant | requires `?tenant=X` call + that response's `by_agent` | ⚠️ inefficient — see **Gap 13** |
| Zone 2 — Reasoner under agent | reasoner-tagged events | ❌ — see **Gap 14** |
| Zone 2 — Model leaf | `/cost?tenant=X` `by_model` | ✅ (but no per-day per-tenant) |
| Zone 2 — Sparkline per node | Per-node 24h time-series | ❌ — see **Gap 15** |
| Zone 2 — Delta per node | Per-node prior-period total | ❌ — see **Gap 15** |
| Zone 3 — Stacked area chart | `by_day` exists, but not `by_day_by_dimension` | ⚠️ — see **Gap 16** |
| Zone 3 — Top-N table | `by_tenant` / `by_agent` / `by_model` | ✅ |
| Zone 3 — Cell drill to runs | `/runs?tenant=X&from=&to=` | ✅ |
| Zone 4 — Cost÷MRR scatter | MRR from billing adapter | ⚠️ — see **Gap 17** |
| Zone 4 — Cost per 1k tokens | Token totals not in `/cost` response | ❌ — see **Gap 18** |
| Zone 4 — Cache donut | `/llm/cache/stats` | ✅ |
| Zone 4 — Per-tool $/call | Tools not tagged on cost events | ❌ — see **Gap 19** |
| Zone 4 — Budgets table | `/admin/budgets` | ✅ |

### 8.4 Anchor live tile

Powers: `/home/overview.CostTodayUSD` for value; `WS /api/v1/realtime` for
live updates. Delta computed client-side from yesterday's same-window
snapshot (or use `/cost?from=yesterday-start&to=yesterday-now`).

### 8.5 OSS sources

No direct OSS queries from the dashboard. All cost data flows through our
runtime (which aggregates LiteLLM `/spend/*` server-side into
`suite_cost_events`).

### 8.6 Backend gap summary for Cost

| Gap | Severity | Action |
|---|---|---|
| 12. Anomaly detection inputs | Inefficient | Compute client-side v1 from existing data; backend anomaly emitter v0.2 |
| 13. Nested hierarchy endpoint | Inefficient | Multiple `/cost?tenant=X` calls v1; single nested endpoint v0.2 |
| 14. Reasoner-tagged cost | **Blocking** for hierarchy completeness | Add `reasoner` column to cost events; agents must tag |
| 15. Per-node sparkline / delta | Inefficient | Per-node 24h queries v0.2 |
| 16. ByDayByDimension for stacked area | **Blocking** for Zone 3 quality | Need `by_day` keyed by dimension |
| 17. MRR per tenant | **Blocking** for Cost÷MRR scatter | Billing adapter must expose MRR |
| 18. Token totals per model | Inefficient | Sum from events or add to summary |
| 19. Tool-tagged cost | **Blocking** for per-tool ROI | Add `tool` column to cost events |

**Net assessment**: Cost can ship in v1 with **Zone 1 + simplified Zone 2
(2 levels: Tenant → Agent or Tenant → Model) + Zone 3 limited to day-only
stack + Zone 4 with budgets + cache only**. Full hierarchy, reasoner
level, unit-economics scatter, and per-tool ROI are **v0.2**. Brief calls
out each TODO inline.

---

## 9. WRAP / REFLECT / LINK

| Surface | Pattern | Why |
|---|---|---|
| All Cost data | WRAP | Our cost ledger; LiteLLM is upstream we aggregate |
| Drill to filtered Runs | WRAP (internal nav) | ACTIVITY → Runs |
| Drill to tenant | WRAP (internal nav) | PEOPLE → Tenant detail |
| "Open in LiteLLM admin" (footer) | LINK | Adapter pill → `:4000/ui` for virtual-key debug |

---

## 10. THREE DATA STATES

### EMPTY (no inference events yet — fresh fork)

All zones render with structural integrity:
- Zone 1: anchor reads `$0.00`, sparkline frame visible, forecast bar at 0
- Zone 2: hierarchy empty-state "No spend yet. Make your first LLM call →"
- Zone 3: chart frame visible, "No data — try the API explorer" overlay
- Zone 4: budgets table empty with "Set first budget" CTA

Demo mode (when enabled) seeds realistic cost events so this state is rare.

### MISSING (cost ledger feature disabled or LLM gateway not configured)

If cost feature is off via `/admin/features`:
- Anchor live tile hidden
- Cost in sidebar dimmed with tooltip "Cost ledger disabled. Configure in PLATFORM → LLM providers."
- Page shows informational empty state

### DEGRADED (LLM gateway unhealthy or cost aggregator failing)

- Banner at top: "Cost data may be stale — last successful aggregate 14m ago"
- Anchor live tile renders last-known with stale chip
- Zones render last-known data with muted timestamps

---

## 11. LIVE DATA

### WebSocket subscriptions

- Anchor live tile (today's running total)
- Zone 1 today number + delta
- Zone 1 forecast bar (recomputed every 5 min server-side)

### Polled

- Zone 2 hierarchy: refresh on dimension change + 60s background poll
- Zone 3 chart: refresh on filter change
- Zone 4 widgets: refresh on tab focus + 5 min poll

### Animation

- Anchor: value rolls subtly (200ms ease) when changed
- Sparklines: redraw on live update, no flash
- Bars: animate width changes on filter (300ms ease)
- Hierarchy expand/collapse: 250ms smooth
- Anomaly card: slides in from top (~600ms)

---

## 12. MUTATIONS

| Mutation | Surface | Audit | Undo | Mobile |
|---|---|---|---|---|
| Set tenant budget | Zone 4 row inline "Edit" → Dialog | Yes (`budget.set`) | Yes (set back) | Yes |
| Cap tenant from anomaly | Zone 1 anomaly action button → Dialog | Yes | Yes | Yes |
| Switch default model | Zone 1 / Zone 4 inline → Sheet | Yes (`llm.default_model.set`) | Yes | No |
| Open LiteLLM admin (link out) | Zone 4 footer / adapter pill | n/a | n/a | n/a |

Mobile-critical: budget set, cap tenant. Both single-tap from anomaly action.

---

## 13. DRILL PATHS

### Hierarchy node → entity

- Click tenant name → PEOPLE → Tenant detail
- Click agent name → BUILD → Agents → that agent
- Click reasoner name → BUILD → Agents → that agent → Reasoners tab
- Click model name → PLATFORM → LLM providers → that model

### Chart cell / table row → runs

- Click any chart series segment → ACTIVITY → Runs filtered to time +
  dimension value
- Click any Top-N table row → same

### Anchor tile → Cost page

- Click anchor → Cost page top zone (Zone 1)

---

## 14. CROSS-PAGE JUMPS IN

- Anchor live tile from any page (top bar)
- Tenant detail "View cost trend →" link
- Inbox budget-alert item action button
- Cmd+K: "cost", "spend", "budget"
- Errors page "Cost during incident" link (Journey 8)

---

## 15. URL STATE

- Route: `/cost`
- Time range: `?range=today|7d|30d|90d|custom&from=...&to=...`
- Tenant filter: `?tenant=<id>` (re-scopes whole page; matches top-bar tenant switcher)
- Zone 3 group-by: `?groupBy=tenant|agent|model|day`
- Zone 2 expanded nodes: `?expanded=acme,acme/refund-agent`
- Hierarchy drilled-down root: `?root=acme/refund-agent`

URL state ensures: shareable views ("send this Cost link to my cofounder"), reload-safe, browser back works on drill.

---

## 16. MOBILE STORY

**Mobile-detail only**, not full mobile-responsive.

- Anchor live tile renders responsively in mobile top bar
- Cost page is desktop-only for detective/planning modes
- Mobile users with budget alerts land on `/inbox/<alert_id>` (Inbox mobile detail), NOT on Cost page
- The Inbox alert detail page renders the necessary context (tenant 7d trend, budget status, single-tap action) without needing the full Cost page

If operator on mobile DOES open Cost: render Zone 1 only in stacked
layout, hide Zones 2-4 behind "View on desktop for full breakdown" link.

---

## 17. MODULARITY SURFACE

**Adapter pill in page footer**: `via LiteLLM ▾`. Click reveals:
- "View adapter docs"
- "Open LiteLLM admin ↗" (deep link to `:4000/ui` for raw spend / virtual key debug)
- "Change LLM adapter → PLATFORM → Adapters"

Adapter pill is the ONLY OSS modularity signal on Cost.

---

# 18. ZONE-BY-ZONE DESIGN + COMPONENT SPEC

All zones use **shadcn/ui** primitives + **Recharts** (wrapped by
shadcn `Chart` where shipped) + **lucide-react** icons + **tailwindcss**.
No external chart libraries beyond Recharts.

Library decisions locked:

| Library | Use |
|---|---|
| **shadcn/ui** | All UI primitives: Card, Button, Badge, Progress, Tabs, Tooltip, Collapsible, Select, ToggleGroup, Table, ScrollArea, Skeleton, Separator, Dialog, Sheet, HoverCard, DropdownMenu, Chart |
| **Recharts** | All data visualizations (AreaChart, BarChart, PieChart, LineChart, ScatterChart) — wrapped through shadcn `<Chart>` for theming consistency |
| **lucide-react** | All icons (consistent with shadcn) |
| **tailwindcss + cn** | Styling + class merging |
| **date-fns** | Time formatting |
| **@radix-ui/react-collapsible** | Hierarchy expand/collapse (already via shadcn) |

No D3, no Nivo, no Visx, no Plotly. Recharts is enough.

---

## Zone 1 — Anomaly Strip

**Purpose**: 3-second answer to "is anything spiking + will I blow budget."

### Component layout

```
┌──────────────────────────────────────────────────────────────────────┐
│  [Card]                                                              │
│  ┌──────────────────────┐  ┌────────────────────────────────────┐    │
│  │ TODAY                │  │ MONTH FORECAST                     │    │
│  │  $52.18              │  │  ████████████░░░░░░░░░░░░░░░░░░░░  │    │
│  │  ▴18% vs yesterday   │  │  $632 spent · $1,215 projected     │    │
│  │  ▁▂▃▂▄▅▆▆██████░░░  │  │  vs $2,000 budget · 7% over ⚠      │    │
│  └──────────────────────┘  └────────────────────────────────────┘    │
│                                                                      │
│  ┌─── ANOMALY ────────────────────────────────────────────────┐      │
│  │ ⚠ acme is 5× its 7d average                                 │      │
│  │ ▁▁▁▂▂▂▃▃▃▃▄▆█████   spike started 2h ago                  │      │
│  │ [Cap budget at $50]  [Open acme ↗]                          │      │
│  └─────────────────────────────────────────────────────────────┘      │
│                                                                      │
│  ┌─── ANOMALY ────────────────────────────────────────────────┐      │
│  │ ⚠ claude-sonnet calls 2× this week                         │      │
│  │ ▂▃▄▆█████████████   ramped over 3 days                    │      │
│  │ [Switch fallback]  [Open LiteLLM ↗]                         │      │
│  └─────────────────────────────────────────────────────────────┘      │
└──────────────────────────────────────────────────────────────────────┘
```

### Component map (Zone 1)

| Element | shadcn | Recharts | Notes |
|---|---|---|---|
| Outer container | `Card` + `CardContent` | — | Per-zone wrapper |
| Today tile | `Card` (nested borderless variant) | `LineChart` (mini, no axes) | Sparkline = `<Sparkline />` (custom — see below) |
| Forecast bar | Custom div + tailwind | — | 3-segment progress: spent/projected/buffer/over |
| Delta indicator | Custom — `Badge` + `lucide-react ArrowUp/Down/Minus` | — | `<DeltaIndicator />` primitive (see below) |
| Anomaly card | `Card` w/ `border-l-4 border-warning` | `LineChart` (mini) | `<AnomalyCard />` primitive |
| Action buttons | `Button` (size="sm", variant="outline" / "default") | — | Open `Dialog` for mutations |
| Warning icon | `lucide-react AlertTriangle` | — | Used in anomaly Badge + tile |

### Primitives introduced (Zone 1)

#### `<Sparkline />`

```tsx
<Sparkline
  data={number[]}            // 24-48 points
  width={120} height={24}
  strokeColor="currentColor" // inherits text color → adapts to anomaly state
  ariaLabel="Cost over last 24 hours"
/>
```

Built with Recharts `<LineChart>`:
- `width=120, height=24`
- No `<XAxis>` / `<YAxis>` / `<CartesianGrid>` / `<Tooltip>` rendered
- `<Line dot={false} strokeWidth={1.5} />`
- Theme-aware stroke (`text-foreground` by default; `text-warning` when in anomaly card; `text-success` for downtrend; `text-destructive` for spike)
- On hover (outer wrapper): shadcn `Tooltip` shows last-N values + timestamps

#### `<DeltaIndicator />`

```tsx
<DeltaIndicator
  current={52.18} previous={44.20}
  format="currency"               // currency | percent | number
  semantic="cost"                 // cost (▴=bad,red) | revenue (▴=good,green) | neutral
  showAbsolute={true}             // ▴$8.18 in addition to ▴18%
/>
```

Renders: `▴18%` or `▴$8.18 (18%)`. Arrow from `lucide-react`. Color via tailwind tokens (`text-destructive` / `text-success` / `text-muted-foreground`).

For large deltas (>200%) renders as `▴3×` instead of `▴300%`.

#### `<ForecastBar />`

```tsx
<ForecastBar
  spent={632} projected={1215} budget={2000}
  warnThresholdPct={90}
  className="h-2"
/>
```

Custom div composition:
- Total width = container
- Three segments stacked horizontally: spent (solid `bg-primary`), projected (`bg-primary/40` with striped pattern via CSS), remaining or over (red `bg-destructive/30` if over)
- Markers above bar at 50%, 80%, 100% via small ticks
- Annotation below: `$632 spent · $1,215 projected vs $2,000 budget`

Uses shadcn `Progress` as base, but customized for multi-segment.

#### `<AnomalyCard />`

```tsx
<AnomalyCard
  severity="warn"                 // info | warn | critical
  title="acme is 5× its 7d average"
  spikeSparkline={number[]}       // last 24h showing the spike
  sparklineAriaLabel="..."
  metaText="spike started 2h ago"
  actions={[
    { label: "Cap budget at $50", variant: "default", onClick: () => ... },
    { label: "Open acme", variant: "outline", onClick: () => ... },
  ]}
/>
```

Built from shadcn `Card`:
- Left border accent (`border-l-4 border-warning`)
- `<AlertTriangle />` icon in header
- `<Sparkline />` shows the spike (severity-colored)
- Action `Button` row in footer
- On click "Cap budget" → opens shadcn `Dialog` with budget form

---

## Zone 2 — Explain-Spend Hierarchy

**Purpose**: The lineage view. The page's centerpiece. Operator sees
where money flows through Tenant → Agent → Reasoner → Model.

### Component layout

```
┌──────────────────────────────────────────────────────────────────────┐
│  EXPLAIN SPEND                              [▼ Root: All tenants]    │
│  Range: this month                                                   │
│ ──────────────────────────────────────────────────────────────────── │
│ ▼ [TENANT] acme                            $31.42  60%  ▴42% ⚠       │
│    ████████████████████████░░░░░░░░░░░░░░░░  ▁▂▃▅█▅▆█████          │
│   │                                                                  │
│   ▼ [AGENT] refund-agent                   $25.13  80%  ▴3×          │
│      ████████████████████████████████░░░░░░  ▁▁▂▅█▆█▇████           │
│     │                                                                │
│     ▼ [REASONER] reply_plan                $18.85  75%  ▴2×          │
│        ██████████████████████████████░░░░░  ▁▂▃▅▆██████             │
│       │                                                              │
│       [MODEL] claude-sonnet-4-6            $18.85  100%             │
│       12.4k in · 4.2k out · 30 calls · cache 12%                    │
│                                                                      │
│     ▶ [REASONER] classify_issue            $4.16   16%  ▾2%          │
│        ████████░░░░░░░░░░░░░░░░░░░░░░░░░  ▂▁▁▂▃▂▂▃▂▂              │
│                                                                      │
│   ▶ [AGENT] supportdesk-agent              $6.29   20%  ▾5%          │
│      ████████░░░░░░░░░░░░░░░░░░░░░░░░░░░  ▂▂▂▂▂▁▁▁▁▁             │
│                                                                      │
│ ▶ [TENANT] globex                          $13.45  26%  ▾5%          │
│    ████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░  ▂▃▂▂▂▂▂▂▂▂              │
│                                                                      │
│ ▶ [TENANT] initech                          $7.31  14%  ▬           │
│    ████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  ▁▁▁▁▁▁▁▁▁▁              │
└──────────────────────────────────────────────────────────────────────┘

  TODO v1: hierarchy renders 2 levels deep (tenant → agent) using
  `/cost` and `/cost?tenant=X` composition. Reasoner + model levels
  marked "[v0.2 — requires reasoner tagging]" with placeholder rows.

  Sparseness chip when reasoner level partial: "63% tagged"
```

### Component map (Zone 2)

| Element | shadcn | Recharts | Notes |
|---|---|---|---|
| Outer container | `Card` + `CardHeader` + `CardContent` | — | |
| Root selector | `Select` ("All tenants" or selected root node) | — | |
| Hierarchy tree | custom `<HierarchyTree>` recursive | — | Built on `Collapsible` |
| Each node row | custom `<HierarchyRow>` | inline `<Sparkline>` | See below |
| Level pill | `Badge` (small, variant by level) | — | TENANT / AGENT / REASONER / MODEL — color-coded subtly |
| Inline proportion bar | `Progress` (h-1.5) — value=% of parent | — | Subtle, low contrast |
| Cost number | text styled `font-mono tabular-nums text-right` | — | |
| Delta indicator | `<DeltaIndicator />` from Zone 1 | — | |
| Warning icon | `lucide-react AlertTriangle` (text-warning) | — | When anomalous |
| Sparkline | `<Sparkline />` from Zone 1 | `LineChart` | 80×16, subtle |
| Chevron toggle | `Button` variant="ghost" size="icon" with `lucide-react ChevronRight/Down` | — | Rotates on expand |
| Drill out | Click name → router navigation | — | Tenant/agent/model names are clickable links |
| Model leaf metadata | text `<Muted>` line below row | — | Token counts in/out/calls/cache |
| Sparseness chip | `Badge` variant="outline" text-xs | — | "63% tagged" when reasoner level partial |

### Primitive introduced (Zone 2)

#### `<HierarchyRow />`

```tsx
<HierarchyRow
  level="agent"               // tenant | user | agent | reasoner | model
  name="refund-agent"
  cost={25.13}
  pctOfParent={80}
  delta={{ current, previous, semantic: "cost" }}
  sparkline={number[]}        // 24h
  anomaly={null | "spike" | "drop" | "new"}
  expanded={true}
  hasChildren={true}
  childrenSparsenessPct={null}
  depth={1}                   // for indentation
  onToggle={() => ...}
  onDrillOut={() => ...}      // click name → drills to entity page
  modelMeta={null | {
    tokensIn, tokensOut, calls, cacheRatePct
  }}
/>
```

Composes: `Collapsible` + `Badge` + `Progress` + `Sparkline` +
`DeltaIndicator` + `Button` (ghost chevron). Indented via padding-left
based on `depth`. Hover any cell → `Tooltip` with hourly breakdown.

#### `<HierarchyTree />`

```tsx
<HierarchyTree
  root={treeNode}             // recursive structure
  defaultExpandedLevels={1}   // tenant + agent open
  onNodeDrill={(node) => ...}
  onNodeToggle={(id, expanded) => ...} // syncs to URL state
/>
```

Recursive render of `<HierarchyRow>`. Manages expand state.

### Zone 2 v1 TODOs (called out in UI)

Wherever data is missing, surface honestly:
- Reasoner level: `<Alert variant="outline">` row "Reasoner-level cost
  requires agent code to tag LLM calls. [Learn how →]"
- Per-node sparkline: degrade to delta-only if no time-series
- Per-node delta: degrade to N/A with subtle dash

---

## Zone 3 — Slice-Over-Time Explorer

**Purpose**: Pivot any dimension over time. Detective work.

### Component layout

```
┌──────────────────────────────────────────────────────────────────────┐
│  EXPLORE                                                             │
│  Range: [last 7d ▾]   Compare: [vs prior 7d ▾]                       │
│  Group by:  [tenant] agent  reasoner  model  tool  day               │
│ ──────────────────────────────────────────────────────────────────── │
│                                                                      │
│  ┌─ Stacked area ──────────────────────────┐  ┌── Top 5 by total ─┐  │
│  │  $60  ┌─────────────────────────────┐   │  │ ● acme     $214   │  │
│  │       │   ▒▒▒acme▒▒▒                │   │  │ ● globex     $88   │  │
│  │  $40  │ ▒▒▒▒▒▒▒▒▒▒▒▒▒  ░░globex░░  │   │  │ ● initech     $61   │  │
│  │       │ ▒▒▒▒▒▒▒▒▒▒▒▒░░░░░░░░░░░░░░ │   │  │ ● widgetco    $44   │  │
│  │  $20  │ ▒▒▒▒▒▒▒▒▒▒▒▒░░░░░░░ initech│   │  │ ● other       $19   │  │
│  │       │ ▒▒▒▒▒▒▒▒░░░░░░░░░░░░░░░░░░ │   │  └────────────────────┘  │
│  │   $0  └─────────────────────────────┘   │                          │
│  │       Mon  Tue  Wed  Thu  Fri  Sat  Sun  │                          │
│  └─────────────────────────────────────────┘                          │
└──────────────────────────────────────────────────────────────────────┘

  Hover chart point → Tooltip shows full stack at that time
  Hover/click table row → highlights matching chart series
  Click chart segment → drills to ACTIVITY → Runs (filtered)
```

### Component map (Zone 3)

| Element | shadcn | Recharts | Notes |
|---|---|---|---|
| Outer container | `Card` + `CardHeader` + `CardContent` | — | |
| Range picker | `Select` | — | today/7d/30d/90d/custom |
| Compare picker | `Select` | — | vs prior period / vs last week / off |
| Group-by chips | `ToggleGroup` (variant="outline", single) | — | tenant/agent/reasoner/model/tool/day |
| Stacked area chart | shadcn `<Chart>` wrapper around | `AreaChart` + `Area` per series | — |
| Chart tooltip | shadcn `<ChartTooltip>` | — | Shows full stack at hovered time |
| Chart legend | shadcn `<ChartLegend>` | — | |
| Top-N table | `Table` + `TableHeader` + `TableBody` + `TableRow` | — | Sortable columns |
| Table row hover-link | `onMouseEnter` highlights matching `Area` (CSS class on series) | — | |
| "Other" expand | Click → loads next N + re-renders chart | — | |

### Chart specifics

```tsx
<ChartContainer config={chartConfig}>
  <AreaChart data={byDayByDimension}>
    <CartesianGrid vertical={false} strokeDasharray="3 3" />
    <XAxis
      dataKey="date"
      tickFormatter={(d) => formatDate(d, "MMM d")}
      tickLine={false}
      axisLine={false}
    />
    <YAxis
      tickFormatter={(v) => `$${v}`}
      tickLine={false}
      axisLine={false}
    />
    <ChartTooltip content={<ChartTooltipContent />} />
    <ChartLegend content={<ChartLegendContent />} />
    {topN.map((series) => (
      <Area
        key={series.id}
        type="monotone"
        dataKey={series.id}
        stackId="cost"
        fill={`var(--chart-${series.colorIndex})`}
        stroke={`var(--chart-${series.colorIndex})`}
        fillOpacity={0.6}
      />
    ))}
  </AreaChart>
</ChartContainer>
```

Uses shadcn chart token colors `--chart-1` through `--chart-5` for top 5
+ neutral for "other." Categorical palette muted to not compete with
state colors (red/yellow/green).

### Zone 3 v1 TODOs

- **Gap 16**: Backend returns `by_day` and `by_tenant` separately; need
  `by_day_by_tenant` composite. Workaround v1: dashboard makes one call
  per top-N tenant (`/cost?tenant=X&from=&to=`) and merges. Acceptable
  for N≤5. Document the perf cost.

---

## Zone 4 — Inference Economics

**Purpose**: Unit economics + budgets + cache. Planning mode.

### Component layout

```
┌──────────────────────────────────────────────────────────────────────┐
│  INFERENCE ECONOMICS                                                 │
│  ┌─────────────────────────┐  ┌────────────────────────────────────┐  │
│  │ COST ÷ MRR per tenant   │  │ COST PER 1K TOKENS by model        │  │
│  │ [Scatter chart]          │  │ gpt-4-32k    ██████████  $0.42  12 │  │
│  │   ●           ●          │  │ claude-opus  ████████   $0.31   8  │  │
│  │              ●           │  │ gpt-4o       █████      $0.22  41  │  │
│  │     ╱ break-even         │  │ claude-sonnet ███       $0.14 312  │  │
│  │   ╱   ●  ●               │  │ gpt-4o-mini  █          $0.05  89  │  │
│  │ ●  ●●●                  │  └────────────────────────────────────┘  │
│  └─────────────────────────┘                                          │
│  ┌─── CACHE ────────────────┐  ┌── PER-TOOL $/CALL ──────────────────┐ │
│  │  [Donut]                  │  │ google-search  ████  $0.02 · 412/d │ │
│  │   hit 43%  Saved $24.18   │  │ db-query       ██    $0.01 · 1.2k/d│ │
│  │   ▴4% trend              │  │ web-fetch      █     $0.005 · 88/d │ │
│  └──────────────────────────┘  └─────────────────────────────────────┘ │
│  ┌─── BUDGETS ────────────────────────────────────────────────────────┐ │
│  │ acme    $87 / $100  ████████████████████░░░  87% ⚠ near    [Edit]│ │
│  │ globex  $42 / $100  ████████░░░░░░░░░░░░░░  42%             [Edit]│ │
│  │ initech $103 / $100 ████████████████████  100% 🔴 over!     [Edit]│ │
│  │ nimbus  $12 / $50   █████░░░░░░░░░░░░░░░░  24%             [Edit]│ │
│  └────────────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────────┘
```

### Component map (Zone 4)

#### Break-even scatter (`Cost ÷ MRR`)

| Element | shadcn | Recharts | Notes |
|---|---|---|---|
| Container | `Card` | — | |
| Chart | shadcn `<Chart>` wrapping | `ScatterChart` + `Scatter` | One scatter point per tenant |
| Diagonal line | — | `<ReferenceLine />` | y = x (cost = MRR break-even) |
| Tooltip | `<ChartTooltip>` | — | Tenant card on hover (`HoverCard`) |
| Dot encoding | — | `<Scatter shape={CustomDot} />` | size = call volume; color = age |
| Axis labels | — | `<XAxis label="MRR ($)" />` etc. | |

```tsx
<ChartContainer>
  <ScatterChart>
    <CartesianGrid strokeDasharray="3 3" />
    <XAxis type="number" dataKey="mrr" label="MRR ($)" />
    <YAxis type="number" dataKey="cost" label="Cost ($)" />
    <ReferenceLine
      segment={[{x: 0, y: 0}, {x: maxMrr, y: maxMrr}]}
      stroke="var(--muted-foreground)"
      strokeDasharray="4 4"
      label={{ value: "break-even", position: "insideBottomRight" }}
    />
    <Scatter data={tenants} shape={<CustomDot />} fill="var(--chart-1)" />
    <ChartTooltip content={<TenantHoverCard />} />
  </ScatterChart>
</ChartContainer>
```

**v1 TODO**: Hidden when MRR data not available (Gap 17). Show placeholder
Card with copy "Connect a billing adapter in PLATFORM → Billing to see
unit economics."

#### Cost per 1k tokens by model (horizontal bars)

| Element | shadcn | Recharts | Notes |
|---|---|---|---|
| Container | `Card` | — | |
| Chart | shadcn `<Chart>` wrapping | `BarChart layout="vertical"` | One bar per model |
| Right annotation | Custom `<XAxis orientation="top">` or label | — | calls/day count |
| Bar fill | — | `<Bar fill="var(--chart-N)" />` | Color band on left = provider |

**v1 TODO**: Token totals (Gap 18) not in `/cost` summary today. Either
add `tokens_in_total`, `tokens_out_total` to `by_model`, OR sum from
`/cost/events` (expensive). v1 ships with `$/call avg` instead of `$/1k
tokens` if tokens not available.

#### Cache donut

| Element | shadcn | Recharts | Notes |
|---|---|---|---|
| Container | `Card` | — | |
| Chart | shadcn `<Chart>` wrapping | `PieChart` + `Pie innerRadius={60}` | hit + miss segments |
| Center label | Custom div with `absolute inset-0 flex` | — | Savings amount + hit-rate |
| Trend below | `<Sparkline />` (Zone 1 primitive) | — | hit rate over time |

#### Per-tool $/call (horizontal bars — same primitive as models)

Reuses the horizontal bars component. Different data source.

**v1 TODO**: Tools not tagged on cost events (Gap 19). Surface as empty
state "Tool-level cost requires the agent to tag tool calls. v0.2."

#### Budgets table — `<BudgetRow />` primitive

```tsx
<BudgetRow
  tenant={{ id, name, currentSpend, monthlyCap, alertThresholdPct }}
  onEdit={() => ...}
/>
```

Renders:
- `TableRow` with tenant name cell
- Gauge bar = custom div composition (similar to ForecastBar but single-segment with color):
  - `bg-success` if <70%
  - `bg-warning` if 70-90%
  - `bg-destructive` if >90%
- Cost numbers `$X / $Y` in `font-mono tabular-nums`
- Status text + icon (`AlertTriangle` near, `AlertCircle` over)
- `Button variant="ghost"` for "Edit" → opens `Dialog` with shadcn Form

### Edit-budget Dialog (mutation)

Triggered from BudgetRow "Edit" or anomaly action "Cap budget at $X":

```tsx
<Dialog>
  <DialogContent>
    <DialogHeader>
      <DialogTitle>Set budget for {tenant.name}</DialogTitle>
    </DialogHeader>
    <Form>
      <FormField name="monthlyCapUSD" label="Monthly cap (USD)">
        <Input type="number" min={1} />
      </FormField>
      <FormField name="alertThresholdPct" label="Alert at (%)">
        <Slider min={50} max={100} step={5} defaultValue={80} />
      </FormField>
    </Form>
    <DialogFooter>
      <Button variant="outline">Cancel</Button>
      <Button>Save</Button>
    </DialogFooter>
  </DialogContent>
</Dialog>
```

Calls `PUT /api/v1/admin/budgets`. On success → `<Toaster />` notifies with
audit ref + undo affordance.

---

## 19. PRIMITIVES INTRODUCED (full list)

Adding to shared library (from §15 of framework):

| Primitive | Built from | Reused on |
|---|---|---|
| `<Sparkline />` | Recharts `LineChart` (mini) | Cost (all zones), Home (KPIs), Errors, Tenant detail |
| `<DeltaIndicator />` | `Badge` + `lucide-react` arrows | Cost, Home, Anchor live tile, Tenant detail |
| `<ForecastBar />` | Custom div + `Progress` styling | Cost Zone 1, future Budgets page |
| `<AnomalyCard />` | `Card` + `Sparkline` + `Button` row | Cost Zone 1, Errors page, Inbox |
| `<HierarchyRow />` | `Collapsible` + `Badge` + `Progress` + `Sparkline` + `DeltaIndicator` | Cost Zone 2, future Tenant detail spend explorer |
| `<HierarchyTree />` | recursive `<HierarchyRow>` | Cost Zone 2 |
| `<StackedAreaWithTable />` | `<Chart>` + `Table` linked | Cost Zone 3, Errors page time-series, Runs analytics |
| `<BreakEvenScatter />` | `<Chart>` + `ScatterChart` + `ReferenceLine` | Cost Zone 4, future Tenant detail |
| `<SortedBars />` | `<Chart>` + `BarChart layout="vertical"` | Cost Zone 4 (models, tools), Reasoners page |
| `<MetricDonut />` | `<Chart>` + `PieChart` + center label | Cost Zone 4 cache, future Cache page, success/fail ratios |
| `<GaugeBarRow />` | Custom div + `Progress` + `Badge` | Cost Zone 4 budgets, Rate limits, Resource quotas |

11 primitives added. All shadcn/Recharts-based; zero external chart libs.

---

## 20. OPEN QUESTIONS & TODOs

### Deferred (per operator: "can be added later")

- **Reasoner-level hierarchy** — requires `reasoner` column on
  `suite_cost_events` + agents tagging. v0.2.
- **Per-node sparkline + delta in hierarchy** — requires per-node 24h
  time-series queries. v0.2.
- **Causal investigation** ("acme spike because refund-agent ran 30x
  because customer X uploaded 30 tickets") — v0.2.
- **Predictability / variance signal** per tenant. v0.2.
- **Animated bar race** for spend over time — never (bad for daily use).

### Open for v1 design

1. **Color palette for hierarchy level pills.** TENANT = neutral, AGENT =
   subtle blue, REASONER = subtle green, MODEL = subtle purple. Tones
   must be muted enough to not compete with anomaly red/yellow.

2. **Anomaly threshold tuning.** Heuristic v1: >2× 7d avg over 1h window.
   Tunable in PLATFORM → Observability later.

3. **"Other" bucket expansion in Zone 3.** Click loads next 5 + re-renders
   chart. Or always show top-5 fixed? Recommendation: click-to-expand.

4. **Forecast confidence intervals.** v1 = point estimate. v0.2 could add
   p10/p50/p90 cone. Defer.

5. **"Compare" picker default.** vs prior period (same window) recommended.
   "vs last week" alternative for short windows.

6. **Edit-budget Dialog — show projected impact?** "Setting $50 cap would
   have capped 12 calls last week." Useful but requires lookup; defer v0.2.

### Backend gaps (tracked in `required-backend-gaps.md`)

- Gap 12 — Anomaly detection inputs (Inefficient; client v1)
- Gap 13 — Nested hierarchy endpoint (Inefficient; multi-call v1)
- Gap 14 — Reasoner-tagged cost (**Blocking**; partial v1, full v0.2)
- Gap 15 — Per-node sparkline / delta (Inefficient; v0.2)
- Gap 16 — ByDayByDimension for stacked area (Inefficient; multi-call v1)
- Gap 17 — MRR per tenant (**Blocking** for scatter; placeholder v1)
- Gap 18 — Token totals per model (Inefficient; $/call alternative v1)
- Gap 19 — Tool-tagged cost (**Blocking** for ROI; placeholder v1)

---

## Grooming checklist

```
☑ Purpose is one sentence
☑ Pillar is named (Anchor + page)
☑ Primary read identified (Zone 1 strip + forecast)
☑ Data sources mapped + verified to runtime/server
☑ Each data source WRAP / REFLECT / LINK declared
☑ OSS link surfaced (LiteLLM admin via footer adapter pill)
☑ Three data states (empty / missing / degraded) specified
☑ Live data behavior explicit
☑ Mutations listed (budget set, cap, switch model)
☑ Drill paths + pivots called out (hierarchy → entity; chart cell → runs)
☑ URL state declared (range, tenant, groupBy, expanded, root)
☑ Mobile story declared (mobile-detail only via Inbox; desktop-primary)
☑ Adapter pill placement decided (footer LiteLLM)
☑ Reused primitives listed
☑ New primitives justified + spec'd (11 new)
☑ Library choices locked (shadcn + Recharts + lucide-react)
☑ Open questions documented
☑ Backend gaps surfaced (12-19) and severity flagged
```

All boxes checked. Brief ready for design with **explicit v1 vs v0.2
scope per zone**.

---

## v1 SHIPS / v0.2 DEFERS — at a glance

```
SHIPS in v1:
  ✓ Anchor live tile (today + delta)
  ✓ Zone 1: anomaly strip with heuristic anomalies (tenant/agent/model)
  ✓ Zone 1: forecast bar with budget overlay
  ✓ Zone 2: hierarchy 2 levels (Tenant → Agent OR Tenant → Model)
  ✓ Zone 3: stacked area + top-N table for top-5 dimensions
  ✓ Zone 4: cache donut
  ✓ Zone 4: budgets table with gauge bars + edit Dialog
  ✓ Zone 4: $/call by model (substitute for $/1k tokens until tokens land)
  ✓ Adapter pill footer link to LiteLLM admin

DEFERS to v0.2:
  ✗ Hierarchy reasoner level (needs Gap 14)
  ✗ Hierarchy model leaf with token meta (needs Gap 18)
  ✗ Per-node sparkline + delta in hierarchy (needs Gap 15)
  ✗ Break-even Cost÷MRR scatter (needs Gap 17)
  ✗ $/1k tokens bars (needs Gap 18)
  ✗ Per-tool $/call (needs Gap 19)
  ✗ Causal investigation
  ✗ Predictability / variance signal
  ✗ Forecast confidence intervals
```

---

_Last updated: 2026-06-16. Next page: Health._

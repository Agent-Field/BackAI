# Page Brief — Runs

> The platform's main debugging surface. List + Drawer composition.
> The first page that puts the `_primitive-drawer.md` to work.
> Groomed against `development/ux/page-design-framework.md`.

---

## 1. PURPOSE

Show every agent + handler run that's happened on the platform, with
fast filters + drill into any row. The main "what just happened?"
investigation surface.

Confidence questions answered:
- *"Did my Claude-Code change actually run?"* (Journey 3)
- *"What did my customer see?"* (Journey 5)
- *"What's burning cost?"* (Journey 7 drill destination)
- *"What's failing during this incident?"* (Journey 8)

---

## 2. PILLAR

ACTIVITY group. Often the highest-traffic page in the admin after Home.

---

## 3. JOURNEYS SERVED

| # | Journey | Frequency | Role |
|---|---|---|---|
| 3 | Verify Claude change | Every dev iteration | Direct — operator confirms run logged |
| 5 | Customer issue triage | Reactive | Direct — find the suspect run |
| 7 | Cost-spike investigation | Reactive | Drill destination (from Cost cell) |
| 8 | Provider outage | Reactive | Filter to failed runs to triage |
| 2 | Daily glance | Sometimes | Drilled from Home KPI tiles |
| 4 | Playground iteration | Sometimes | Playground runs surface here too |

5+ journeys = highest-traffic ACTIVITY page.

---

## 4. TIME BUDGET

- **30 sec - 5 min** — typical investigation
- **5 sec** — confirm a run logged (Journey 3)
- **15 min** — incident triage (Journey 8)

---

## 5. DENSITY TARGET

**HIGH.** Dense table. Operators scan many rows fast. ~30-50 rows
visible per screen at normal density.

---

## 6. PRIMARY READ

**The top of the table.** Time-desc sort means newest runs at top.
Operators scan status dots down the left edge looking for non-green.

---

## 7. STRATEGIC SHAPE

A list + drawer page is mostly **composition over Drawer primitive**.
The brief specs:

1. List view (table + filters + group-by)
2. Reference `_primitive-drawer.md` for the drill — NOT re-spec the drawer
3. URL state across filter + drawer
4. Live updates pattern
5. Backend gap documentation

This is the template for Errors / Webhook flow / Sandbox runs / Queue /
Notifications — same shape, different entity.

---

## 8. DATA SOURCES

> **Audit status**: `GET /api/v1/runs` verified
> (`dashboard.go:88`); filter limitations documented; per-row drawer
> data covered in `_primitive-drawer.md`.

### 8.1 Runtime endpoints (WRAP) — verified

| Endpoint | Powers | Status |
|---|---|---|
| `GET /api/v1/runs?agent=&tenant=&status=&limit=&offset=` | Table rows | ✅ exists |
| `GET /api/v1/executions/{id}` + `/api/v1/runs/{id}/events` | Drawer detail | ✅ (Drawer primitive) |
| `WS /api/v1/realtime/runs` | Live updates | ✅ exists |
| `POST /api/v1/runs/{id}/cancel` | Cancel running | ✅ |
| `POST /api/v1/runs/{id}/pause` + `/resume` | Pause control | ✅ |

### 8.2 `/api/v1/runs` row shape (verified — abbreviated)

```ts
{
  runs: [{
    id: string,
    agent: string,          // "supportdesk.reply_plan"
    tenant_id: string,
    tenant_name: string,
    status_code: number | null,
    cost_usd: number | null,  // best-effort join with cost events
    duration_ms: number,
    tokens_in: number | null,
    tokens_out: number | null,
    model: string | null,
    started_at: string,
    error_summary: string | null,
    trigger_source: "http" | "webhook" | "job" | "cron" | "playground",
  }],
  total: number,
  has_more: boolean,
}
```

### 8.3 Filter limitations (v1 backend reality)

The endpoint accepts: `agent`, `tenant`, `status`, `limit`, `offset`.

**NOT supported today** (surface as Gaps 27-31):

| Filter | Status | Workaround v1 |
|---|---|---|
| Time range (`from`, `to`) | ❌ Gap 27 | Client-side filter on rendered page only |
| Search by run id / error message | ❌ Gap 28 | Filter client-side after fetch |
| Multi-select status | ❌ Gap 29 | Server only takes "succeeded" OR "failed" — UI shows multi-select but issues one of two calls |
| Status `running` / `queued` / `cancelled` / `timeout` | ❌ Gap 30 | Status today is binary 2xx/non-2xx; richer status enum needs DB schema work |
| Trigger source filter | ❌ Gap 31 | Trigger derived client-side from endpoint path; can't filter server-side |

### 8.4 Backend gap summary

| Gap | Severity | Action |
|---|---|---|
| 27. Runs `from`/`to` time range filter | **Blocking** for time-windowed views | Add params |
| 28. Runs search by id/error | Inefficient | v1 client-filter |
| 29. Multi-select status | Inefficient | v1 UI shows multi-select, requests intersection |
| 30. Richer status enum (running/queued/cancelled/timeout) | **Blocking** for live runs filter | Schema work |
| 31. Trigger source filter | Inefficient | v1 client-filter on visible rows |

### 8.5 Per-row drawer data

See `_primitive-drawer.md` §4 (Run drawer config). All endpoints verified
there.

---

## 9. WRAP / REFLECT / LINK

| Surface | Pattern |
|---|---|
| Table rows | WRAP — our gateway request ledger |
| Drawer detail | WRAP w/ AgentField REFLECT link (drawer footer "Open in AgentField ↗") |
| Drill to filtered runs from Cost cell | WRAP (internal nav) |
| Drill to tenant | WRAP (internal nav) |

No OSS pills on the list page — Drawer carries the adapter pill via
"Open in AgentField" action.

---

## 10. THREE DATA STATES

### EMPTY (no runs yet — fresh fork)

```
┌──────────────────────────────────────────────────────────────────┐
│ FILTERS  [Status] [Agent] [Tenant] [Time range] [Search]         │
├──────────────────────────────────────────────────────────────────┤
│ Time      Agent.Reasoner    Tenant   Duration   Cost   Trigger   │   ← header always visible
├──────────────────────────────────────────────────────────────────┤
│                                                                  │
│  No runs yet.                                                    │
│  Make your first agent call to see runs appear here.             │
│                                                                  │
│  curl localhost:8080/api/v1/agents/sample.echo \                 │
│    -H "Authorization: Bearer $BACKAI_KEY" -d '{"input":...}'    │
│                                                                  │
│  [Open API explorer →]                                           │
└──────────────────────────────────────────────────────────────────┘
```

Table header always visible (Cost critique). Try-it snippet inline.

### MISSING (DB unavailable, runs feature disabled)

Top banner: "Run history unavailable — database not configured." Table
hidden.

### DEGRADED (runs feature available but recent rows stale)

Banner: "Showing cached runs — connection unstable." Existing rows shown
with stale chip; live tick paused.

---

## 11. LIVE DATA

### WebSocket

Subscribe to `WS /api/v1/realtime/runs` (with current filters as query
params if backend supports).

- **New run** → slides in at top of table with subtle highlight (~600ms)
- **Status transition** (running → succeeded/failed) → status dot color
  tween + row brief highlight
- **Counts in filter chips** update live (status counts)
- **"N new runs"** chip appears at top when operator has scrolled down +
  new runs arrive; click to scroll-to-top + clear chip

### Animation

- New row slide-in: 250ms ease-out from top, 600ms highlight, settle
- Status transition: 200ms color tween on dot
- Filter change: rows re-stack with stagger (100ms, max 200ms total)

---

## 12. MUTATIONS

| Mutation | Surface | Audit | Undo |
|---|---|---|---|
| Cancel run (if running) | Row hover → kebab menu, or Drawer action | Yes | No |
| Re-run | Drawer action | Yes (creates new run audit) | n/a |
| Pause / Resume | Drawer action (if running) | Yes | Resume / Pause |
| Bulk cancel selected | Selection mode → footer bar | Yes (per-row) | No |

No destructive mutations on Runs list itself; structural changes happen
via Drawer.

---

## 13. DRILL PATHS

### Row → drawer

Single click on row → opens `<DrillDrawer type="run" id={id} />`.
URL updates to `/operate/runs?<filters>&drawer=run/<id>`.

### Cell content → cross-page jumps

- Click **agent name** in row (NOT the row body) → BUILD → Agents → that agent
- Click **tenant name** in row → PEOPLE → Tenants → that tenant
- Click **trigger pill** → no jump; filters table to that trigger
- Click **cost number** → MONEY → Cost filtered to that tenant + time

Row-body click goes to drawer; specific cell clicks jump cross-page.
Both patterns coexist via `<a>` inside cells with `e.stopPropagation()`.

---

## 14. CROSS-PAGE JUMPS IN

What lands here?

- Home → KPI tile "Live runs" / "Failed runs 24h" → Runs filtered
- Cost → group-by-tenant cell → Runs filtered to tenant + time
- Cost → top expensive runs table row → Runs filtered + drawer open
- Cmd+K → "runs" → Runs default view
- Cmd+K → run id (`abc123`) → Runs with `?drawer=run/abc123`
- Tenant detail → Runs tab is itself the Runs page filtered to tenant
- Agent detail → Recent runs section drills here filtered
- Drawer → "Parent run" link → Runs with new drawer at parent

---

## 15. URL STATE

```
/operate/runs
  ?status=failed,running       (comma-separated multi)
  &agent=supportdesk.reply_plan,refund.evaluate
  &tenant=acme,globex
  &from=2026-06-17T00:00:00Z
  &to=2026-06-17T23:59:59Z
  &trigger=http,webhook
  &search=connection+timeout
  &groupBy=tenant              (when group view active)
  &drawer=run/abc123           (drawer open at run abc123)
  &sort=cost_desc              (alternative sort)
```

Every filter and drawer state URL-encoded. Shareable. Reload-safe. Back
arrow works.

---

## 16. MOBILE STORY

**Desktop-primary, responsive read.**

- List collapses to cards (one row = one card) on narrow viewports
- Cards show: time + agent.reasoner + tenant + status pill (collapsed)
- Tap card → Drawer in full-screen takeover (Drawer mobile spec)
- No bulk-select on mobile

For mobile incident response: operator goes through Inbox alert detail,
not Runs page.

---

## 17. MODULARITY SURFACE

List page: no adapter pill in header. Drawer carries the modularity
signal via "Open in AgentField" action.

---

# 18. LAYOUT + COMPONENT SPEC

Stack: **shadcn/ui** + **Recharts** (for histogram in group-by) +
**lucide-react** + **tailwindcss**. Same as previous pages.

---

## Layout zones

```
┌──────────────────────────────────────────────────────────────────────┐
│ PAGE HEADER                                                          │
│   Runs                                          [Refresh] [Live ●]   │
│   Activity from the last 24h                                         │
├──────────────────────────────────────────────────────────────────────┤
│ FILTER BAR                                                           │
│   Status: [● succeeded ▾]  Agent: [supportdesk ▾]  Tenant: [All ▾]   │
│   Time: [Last 24h ▾]  Trigger: [Any ▾]                               │
│   [Search runs...]                                       [Group: off]│
├──────────────────────────────────────────────────────────────────────┤
│ (optional) GROUP BAR  (when groupBy != off)                           │
│   tenant   acme  $214  ▾                                              │
├──────────────────────────────────────────────────────────────────────┤
│ TABLE                                                                │
│  Time  Agent.Reasoner    Tenant   Duration  Cost    Trigger  Status  │
│  2m ago  supportdesk...  acme      2.4s     $0.018  http     ● ok    │
│  4m ago  supportdesk...  globex    1.2s     $0.009  http     ● ok    │
│  6m ago  refund...        acme      8.3s     $0.052  webhook  ● fail │
│  ...                                                                 │
│                                                                      │
│  [Drawer slides over from right when row clicked]                    │
└──────────────────────────────────────────────────────────────────────┘
```

---

## Component map

### Page header

| Element | shadcn | Notes |
|---|---|---|
| Title | `CardTitle text-lg font-semibold` | "Runs" (primary tier) |
| Subtitle | `text-sm text-muted-foreground` | "Activity from the last 24h" — reflects time range |
| Refresh button | `Button variant="ghost" size="icon"` | `RefreshCw` icon |
| Live indicator | custom span with green pulsing dot | "Live ●" — visible when WS connected |

### Filter bar — `<RunsFilterBar />`

Sticky just below page header. Multi-select dropdowns for primary
filters; search at right.

| Element | shadcn | Notes |
|---|---|---|
| Filter container | `div` with `border-b sticky top-0 bg-background` | Sticks during scroll |
| Status filter | `Select` (multi via `DropdownMenuCheckboxItem`) | Counts displayed inline per status |
| Agent filter | `Select` (multi) | Searchable when many agents |
| Tenant filter | `Select` (multi) | Disabled if top-bar tenant scope active |
| Time range | `Select` with custom option opening `Calendar` | Today / 24h / 7d / 30d / Custom |
| Trigger filter | `ToggleGroup` (multi) | http / webhook / job / cron / playground |
| Search | `Input` with `Search` icon | Debounced 200ms |
| Group-by toggle | `Select` | off / tenant / agent / status / hour |
| Active filter chip row | `Badge` row below dropdowns with ✕ to clear each | Visible only when filters applied |

### Filter chip row pattern (when filters active)

```
Active: [● succeeded ✕] [agent: supportdesk ✕] [Last 24h ✕]  Clear all
```

Visual confirmation of what's filtering. Clear individual or all.

### Table — `<RunsTable />`

Uses shadcn `<Table>` with sticky header.

```tsx
<Table>
  <TableHeader className="sticky top-0 bg-background z-10">
    <TableRow>
      <TableHead className="w-2"></TableHead> {/* status dot col */}
      <TableHead>Time</TableHead>
      <TableHead>Agent.Reasoner</TableHead>
      <TableHead>Tenant</TableHead>
      <TableHead className="text-right tabular-nums">Duration</TableHead>
      <TableHead className="text-right tabular-nums">Cost</TableHead>
      <TableHead>Trigger</TableHead>
      <TableHead></TableHead> {/* row actions kebab */}
    </TableRow>
  </TableHeader>
  <TableBody>
    {runs.map(run => <RunRow key={run.id} run={run} onClick={...} />)}
  </TableBody>
</Table>
```

### Row — `<RunRow />`

| Element | shadcn | Notes |
|---|---|---|
| Status dot | custom span theme-tone | left edge — 8px circle |
| Time cell | `formatAge(started_at)` | "2m ago" — HoverCard shows full ISO |
| Agent.Reasoner | font-mono text-sm — clickable link | Click name → BUILD → Agents → that agent (`stopPropagation`); row body click → drawer |
| Tenant | text-sm — clickable link | Click → PEOPLE → Tenant detail |
| Duration | `font-mono tabular-nums text-right` | "2.4s" or "240ms" |
| Cost | `font-mono tabular-nums text-right` | "$0.018" — 4 decimals, gray if 0 |
| Trigger | `Badge variant="outline" size="sm"` | http / webhook / job / cron / playground — color subtly per kind |
| Status text | `text-xs` quiet when ok, loud when failed | "ok" / "running" / "failed: timeout" |
| Row kebab | `Button variant="ghost" size="icon"` with `MoreHorizontal` | Reveals on row hover; Cancel / Re-run options |

Row click anywhere except cell-links → opens Drawer.

### Failed row highlight

```tsx
<TableRow className={cn(
  "cursor-pointer hover:bg-muted/50",
  run.status === "failed" && "border-l-4 border-destructive",
  run.status === "running" && "border-l-4 border-warning",
)}>
```

Left-border accent on non-ok rows. Scanning the column tells you in 1
second how many failed.

### Live "N new runs" chip

When live runs arrive while operator is scrolled away from top:

```tsx
{newRunsCount > 0 && hasScrolled && (
  <button
    onClick={scrollToTop}
    className="sticky top-12 mx-auto px-3 py-1 rounded-full
               bg-primary text-primary-foreground text-sm"
  >
    {newRunsCount} new runs ↑
  </button>
)}
```

### Group-by mode

When `groupBy=tenant` (or agent / status / hour):

```tsx
<Collapsible defaultOpen>
  <CollapsibleTrigger className="w-full">
    <div className="flex items-center gap-2 px-4 py-2 hover:bg-muted/50">
      <ChevronDown />
      <span className="font-medium">tenant</span>
      <span className="font-mono">acme</span>
      <span className="text-muted-foreground ml-auto">
        {runCount} runs · ${groupCost}
      </span>
    </div>
  </CollapsibleTrigger>
  <CollapsibleContent>
    {/* RunsTable rendered for this group only */}
  </CollapsibleContent>
</Collapsible>
```

Sub-tables nested inside group sections. Operator collapses
uninteresting groups, focuses on hotspots.

### Empty / loading / error states

- **Empty**: see §10 — header always visible, snippet inline
- **Loading**: shadcn `<Skeleton>` rows (10 placeholder rows with same
  column layout)
- **Error**: banner at top + "Retry" button; preserve filters

---

## 19. PRIMITIVES INTRODUCED

Adding to shared library:

| Primitive | Built from | Reused on |
|---|---|---|
| `<RunsFilterBar />` | Composition of Select / Input / ToggleGroup / Badge | Likely reused on Errors / Webhook flow / Sandbox runs with same shape — parametrize the dimensions |
| `<EntityListTable />` | `Table` + sticky header + skeleton + group-by + failed-row accent | Generic form usable on Errors / Webhook flow / Sandbox runs / Queue / Notifications — the shape is identical |
| `<EntityRow />` | TableRow + cells + status dot + kebab | Same — generic row contract |
| `<LiveNewItemsChip />` | sticky button | Reused on Errors / Webhook flow whenever live updates land while scrolled |
| `<GroupByCollapsible />` | shadcn `Collapsible` | Same — generic |

**Strategic move**: `<EntityListTable />` + `<EntityRow />` should be
generic primitives parametrized by `EntityConfig` (matching Drawer's
config map). Adding a list page for a new entity type = adding a config
entry, not writing a new table.

Reuses from previous briefs: `<KeyValueTile />` (in drawer),
`<DeltaIndicator />`, `formatAge()`, all status conventions, shadcn
`Skeleton` / `Toaster` / etc.

---

## 20. OPEN QUESTIONS & TODOs

### Deferred to v0.2

- **Bulk cancel** — selection mode + bulk action footer. v1 single-row only.
- **Saved filter views** — operator saves "my failed runs from acme this
  week" as a named view. v0.2.
- **Compare runs side-by-side** — open 2 drawers split-view. v0.2.
- **Cost-bar in row** — visual encoding of cost magnitude per row.
  Considered for v1 but adds noise; defer.
- **Cost histogram in group-by hour** — sparkline-style. v0.2.

### Open for v1 design

1. **Default sort.** Time-desc is correct. Other sorts available via
   header click: cost-desc (find expensive), duration-desc (find slow).

2. **Pagination vs infinite scroll.** Recommend infinite scroll with
   "Load more" button at bottom. Pagination feels stale for a live list.

3. **Filter chip dropdown counts.** Should each `[Status ▾]` dropdown
   show counts per option live? Recommendation: yes — `succeeded (1,234)`.

4. **Row density toggle.** Compact (1 line) vs comfortable (with error
   summary line)? Recommendation: comfortable default; compact toggle
   via Cmd+Shift+D for power users.

5. **Trigger pill colors.** Subtle differentiation by source:
   - http → neutral
   - webhook → blue tint
   - job → purple tint
   - cron → orange tint
   - playground → green tint
   Avoid loud colors that compete with status signal.

6. **Run id truncation in row.** Don't show run id by default (it's in
   drawer). Saves a column.

### Backend gaps (in `required-backend-gaps.md`)

- Gap 27 — Runs `from`/`to` filter (Blocking for time-windowed views)
- Gap 28 — Runs search filter (Inefficient; v1 client-filter)
- Gap 29 — Multi-select status (Inefficient; v1 UI workaround)
- Gap 30 — Richer status enum (Blocking for live/running filter)
- Gap 31 — Trigger source filter (Inefficient; v1 client-filter)

---

## v1 SHIPS / v0.2 DEFERS

```
SHIPS in v1:
  ✓ Table + sticky filter bar
  ✓ Single-select status (succeeded / failed only — backend limit)
  ✓ Agent + tenant multi-filter via repeated calls (or UI shows multi, request intersection)
  ✓ Search + trigger filters as CLIENT-SIDE filters on rendered rows
  ✓ Time range as CLIENT-SIDE filter (until Gap 27)
  ✓ Live updates via WebSocket
  ✓ Drawer drill (per _primitive-drawer.md)
  ✓ Group-by tenant / agent / status / hour (client-side aggregation)
  ✓ Failed/running row border accent
  ✓ Cell-level cross-page jumps
  ✓ Skeleton loading + empty state + error banner
  ✓ URL state for all filters + drawer

DEFERS to v0.2 (mostly backend gap-bound):
  ✗ Time range as proper server-side filter (Gap 27)
  ✗ Server-side search (Gap 28)
  ✗ Multi-select status server-side (Gap 29)
  ✗ Status "running"/"queued"/"cancelled"/"timeout" (Gap 30)
  ✗ Server-side trigger filter (Gap 31)
  ✗ Bulk cancel
  ✗ Saved filter views
  ✗ Compare runs side-by-side
```

---

## Grooming checklist

```
☑ Purpose is one sentence
☑ Pillar named (ACTIVITY)
☑ Primary read identified (top of table)
☑ Data sources mapped + verified (runs endpoint shape)
☑ WRAP/REFLECT/LINK declared (REFLECT in drawer only)
☑ Three data states specified
☑ Live data behavior explicit
☑ Mutations listed
☑ Drill paths called out (row → drawer; cells → cross-page)
☑ Cross-page jumps IN documented
☑ URL state declared (filters + drawer composed)
☑ Mobile story declared (cards + Drawer takeover)
☑ Reused primitives from previous briefs (Drawer, formatAge, KeyValueTile, status conventions)
☑ New primitives justified (FilterBar, EntityListTable, EntityRow, LiveNewItemsChip, GroupByCollapsible)
☑ Cost critique lessons applied (header always visible, friendly names, font-mono cost, status consistency, weight hierarchy)
☑ Backend gaps documented (27-31)
☑ v1/v0.2 split called out
```

All checked.

---

_Last updated: 2026-06-17. Next: Tenant detail (composition over many
primitives)._

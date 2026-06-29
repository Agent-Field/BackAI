# Page Brief — Health

> The incident landing page. The "Connected Services" hub (Block 0 decision #4).
> Anchor for "is the platform itself healthy?" + full page for Journey 8.
> Groomed against `development/ux/page-design-framework.md`.

---

## 1. PURPOSE

Show the operator whether their fork's infrastructure (runtime, DB, backing
services, LLM providers) is healthy — and when something's degraded,
surface what + since when + the link to recovery.

Confidence questions answered:
- *"Is the platform itself healthy?"* (Journey 2 — anchor dot answers)
- *"What broke, where, and how do I fix it?"* (Journey 8 — primary)
- *"Did I install this right?"* (Journey 1 — fresh fork verification)

---

## 2. PILLAR

Anchor (top-pinned status dot ●) + full page reached via click.

Per Block 0 decision #4: Health is the **ONE central "Open native OSS UI"
hub**. Other pages get adapter pills with contextual deep links; Health is
where all OSS service admin UIs are listed in one place.

---

## 3. JOURNEYS SERVED

| # | Journey | Frequency on Health | Role |
|---|---|---|---|
| 8 | Provider outage / incident | Reactive | **Direct — incident landing** |
| 2 | Daily 3-sec glance | Every visit (anchor only) | Anchor dot in top bar |
| 1 | First open of fresh fork | Sometimes | Verification — "all green, install worked" |
| 7 | Cost spike | Sometimes | Cross-cut when spike is provider-driven |

Journeys NOT served: 3 / 4 (dev loops don't use Health), 5 (customer
issues don't usually hit infra), 6 (mobile alerts route through Inbox).

---

## 4. TIME BUDGET

- **3 seconds** — anchor dot color (Journey 2)
- **15-30 minutes** — Journey 8 incident sweep
- **30 seconds** — Journey 1 first-open verify

---

## 5. DENSITY TARGET

**HIGH and structured.** Health is dense by nature — many services × many
signals. Layout uses Cards to group concerns; operator scans by container.

---

## 6. PRIMARY READ

**The incident banner (if any) + the LLM Provider Availability section.**

During incidents (Journey 8): operator opens Health asking "which provider
is degraded?" If a provider is down, they need to see WHICH + LATENCY +
TREND immediately. That section sits at the top.

When everything's green: a top-of-page "✓ All services healthy" card,
quick visual scan of the grid below confirms.

---

## 7. STRATEGIC SHAPE

Health is the **infrastructure status board + recovery surface**.

Five zones serve three modes:

| Zone | Mode | Purpose |
|---|---|---|
| 0 — Incident banner | Incident | Visible only when something's wrong |
| A — LLM provider availability | Incident + Glance | Topmost because providers drive incidents |
| B — Backing services grid | Glance + Verify | At-a-glance "is everything up" |
| C — Database health | Diagnostic | Connections, slow queries, cache, vacuum |
| D — Runtime self | Verify | Version, uptime, mem, goroutines |

Workers + TLS deferred to v0.2 (see gaps).

---

## 8. DATA SOURCES

> **Audit status**: All Health endpoints verified to exist
> (`services/runtime/internal/server/services.go`,
> `llm_provider_health.go`, `db_health.go`,
> `metrics_summary.go`). Block 1 shipped them.

### 8.1 Runtime endpoints (WRAP) — verified

| Endpoint | Powers | Status |
|---|---|---|
| `GET /api/v1/admin/services` | Zone B — backing services grid | ✅ (`services.go:46`) |
| `GET /api/v1/admin/llm/provider-health?window=24h|7d|30d` | Zone A — provider availability + sparklines | ✅ (`llm_provider_health.go:27`) |
| `GET /api/v1/admin/db/health` | Zone C — DB connections, slow queries, tables, vacuum, locks | ✅ (`db_health.go:73`) |
| `GET /api/v1/metrics/summary` | Zone D — runtime self (HTTP total, p95, goroutines, heap, uptime, version) | ✅ (`metrics_summary.go:56`) |
| `GET /health` + `GET /ready` | Zone D — health probes | ✅ |
| `WS /api/v1/realtime` | Live updates on Zones A, B (status transitions) | ✅ |

### 8.2 `/api/v1/admin/services` response shape (verified)

```ts
{
  services: [{
    id: string,
    name: string,
    kind: string,             // "runtime" | "data" | "llm-gateway" | "storage" | "observability/*" | ...
    status: "healthy" | "degraded" | "offline",
    version: string,
    host: string | null,
    port: number | null,
    purpose: string,
    admin_url: string | null, // for click-out
    checked: string,          // RFC3339 timestamp
  }]
}
```

### 8.3 `/api/v1/admin/llm/provider-health` response shape (verified)

```ts
{
  window: "24h" | "7d" | "30d",
  providers: [{
    provider: string,                   // "openai", "anthropic", "openrouter", ...
    status: "healthy" | "degraded" | "down" | "unknown",
    median_latency_ms: number,
    p95_latency_ms: number,
    latency_buckets: [{ ts, latency }], // for sparkline
    sample_count: number,
    last_check_at: string,
  }]
}
```

### 8.4 `/api/v1/admin/db/health` response shape (verified)

```ts
{
  available: boolean,
  reason: string | null,
  connections: { active, idle, waiting, total },
  cache_hit_ratio: number,       // 0.0 - 1.0
  slow_queries: [{ query, calls, mean_ms, total_ms, rows }],
  largest_tables: [{ schema, table, total_bytes, rows }],
  vacuum_status: [{ schema, table, last_vacuum, last_autovacuum, dead_tuples }],
  locks: [{ relation, mode, granted, waiting }],
  checked_at: string,
}
```

Note: `slow_queries` requires `pg_stat_statements` extension. If not
installed, `available=false` + `reason` explains. Render Zone C with a
banner explaining the prereq.

### 8.5 OSS sources (link-out only)

Health is the ONLY page where these admin UIs surface as click-outs:
- LiteLLM admin (`:4000/ui`)
- AgentField control plane (`:8081`)
- MinIO console (`:9001`)
- Svix dashboard (`:8071`)
- River UI (when shipped)
- Prometheus (`:9090`)
- Grafana (`:3001`)
- Tempo / Loki / GlitchTip when their adapters are configured

### 8.6 Computed client-side

- Overall health = `healthy` if all services healthy AND all providers healthy;
  `degraded` if any service or provider degraded; `down` if any critical
  service offline. Anchor dot color uses this.
- Provider availability % = `sample_count - degraded_count) / sample_count` over window
- Cache hit ratio rendered as percentage with delta vs. prior period (if available)
- DB connection saturation = `active / total` rendered as gauge
- "Since when" deltas: for v1 we use `checked_at` timestamps; v0.2 adds true
  "degraded since 14:32" via state transition log (Gap 20)

### 8.7 Backend gap summary for Health

| Gap | Severity | Action |
|---|---|---|
| 20. Service status transition log ("degraded since 14:32") | Inefficient | v1 shows current status only; v0.2 add log |
| 21. Worker / River status | **Blocking** for Workers section | v1 hides Workers; v0.2 adds River-pkg surfaces |
| 22. TLS / cert expiry surface | Inefficient | v1 hides; v0.2 Caddy admin proxy |
| 23. Suggested recovery actions per status | Inefficient | v1 inline link to PLATFORM page; v0.2 smart suggestions |
| 24. DB previous-period cache hit ratio for delta | Cosmetic | v1 shows current only; v0.2 adds delta |

**Net assessment**: Health ships in v1 with Zones 0+A+B+C+D. Workers
section deferred. All ship-blocking endpoints already exist.

---

## 9. WRAP / REFLECT / LINK

| Surface | Pattern | Why |
|---|---|---|
| Backing services grid | REFLECT | We show status + version; admin UI lives on each OSS |
| LLM provider availability | WRAP | Our `suite_provider_health_log`; LiteLLM `/health` aggregated |
| DB health | WRAP | Direct `pg_stat_*` queries |
| Runtime self | WRAP | Our metrics |
| Each service → admin UI | LINK | Per Block 0 decision #4 — Health is the single hub |

---

## 10. THREE DATA STATES

### EMPTY (fresh fork, just installed)

- Zone 0: hidden
- Zone A: providers list empty until first provider call OR shows configured providers with status "unknown" + "First check in 2 min"
- Zone B: backing services grid shows all configured services with their actual status
- Zone C: DB available, slow queries empty ("first period of data")
- Zone D: runtime version, uptime "2m", goroutines, etc.

Fresh fork should LOOK alive (services green) — this is one of the most
satisfying first-run experiences.

### MISSING

- `pg_stat_statements` not installed → Zone C shows banner:
  "Slow query data requires `pg_stat_statements`. Add to
  `shared_preload_libraries` and restart Postgres."
- Provider health polling disabled → Zone A shows banner:
  "Provider health polling disabled. Enable in PLATFORM → Features."

### DEGRADED

- Zone 0 incident banner appears at top
- Affected service / provider gets red border + warning icon
- Last-check timestamp goes muted
- WebSocket reconnecting → chip at top

---

## 11. LIVE DATA

### WebSocket subscriptions

- Service status changes (push when transition occurs)
- Provider status changes
- Runtime probe results

### Polled

- DB health: 60s (queries are expensive)
- LLM provider check: per `health_poller.go` schedule (configurable)
- Backing services: 5-10s per service

### Animation

- Status dot transition: 200ms color tween, brief pulse on change
- New incident banner: slides in from top, brief highlight
- Sparkline updates: redraw on tick
- Provider availability percentage rolls subtly

---

## 12. MUTATIONS

Health is mostly **read-only**. The few mutations:

| Mutation | Surface | Audit | Undo | Mobile |
|---|---|---|---|---|
| "Switch fallback" (provider degraded) | Zone A inline button → Sheet | Yes (`llm.fallback.set`) | Yes | n/a |
| "Open in X" link out | Zone B card button | n/a | n/a | n/a |
| Refresh probes now | Page header | n/a | n/a | n/a |
| Acknowledge incident (when emitter ships) | Zone 0 banner | Yes | Yes | n/a |

---

## 13. DRILL PATHS

### Service card → admin UI

Each backing service Card has `admin_url` (when present) opens that OSS's
native dashboard in a new tab. Per Block 0 decision: Health is the ONLY
page with generic "Open OSS UI" buttons; other pages have contextual deep
links to specific entities only.

### Provider card → fallback config

"Switch fallback" inline action → opens Sheet rooted at PLATFORM → LLM
providers → fallback chain editor.

### Slow query row → SQL runner

Click a slow query → opens BUILD → Data → SQL with that query pasted in
(read-only).

### Cache hit ratio → guidance

Click → opens contextual info: "Cache hit ratio drives LLM gateway
savings. See Cost page Cache section for savings."

---

## 14. CROSS-PAGE JUMPS IN

- Anchor dot click from any page
- Cmd+K → "health" or "providers"
- Errors page "Provider degraded? Open Health" link during incidents
- Cost page anomaly card linking to Health when degradation correlates

---

## 15. URL STATE

- Route: `/health`
- Provider window: `?window=24h|7d|30d` (matches API param)
- Tab focus when zones are tabbed: `?tab=providers|services|database|runtime`
- Specific service drawer: `?service=<id>` opens detail drawer
- Expanded query row: `?query=<hash>` opens slow query detail

---

## 16. MOBILE STORY

**Desktop-primary, responsive read-only.**

- Anchor dot in top bar always visible on mobile
- If operator opens Health on mobile: stacked single-column with each zone
  collapsible. No mutations.
- Mobile users with incident alerts land on `/inbox/<alert_id>`, NOT
  Health page. The Inbox alert detail surfaces the necessary provider
  context + one-tap action.

---

## 17. MODULARITY SURFACE

Health is itself **the modularity surface** for the whole platform. Per
Block 0 decision #4, every backing service is a Card on this page with:
- Adapter name + version
- Status pill
- Last-check timestamp
- Admin URL (when present)

No single adapter pill in the page header — the entire page is the pill
collection.

---

## 17.5 SECTION HIERARCHY — visual weight differentiation (Cost critique)

Cost critique surfaced: all section headers were uppercase + same weight,
which flattened the page. Health must differentiate primary from
secondary headers explicitly:

| Level | Element | Style | Where |
|---|---|---|---|
| **Primary** (page-level zone) | `CardTitle` + `CardDescription` | `text-lg font-semibold` | "LLM PROVIDERS" / "CONNECTED SERVICES" / "DATABASE HEALTH" / "RUNTIME" |
| **Secondary** (sub-section inside zone) | `CardTitle` size="sm" | `text-sm font-medium` | "Connections" / "Cache" / "Slow Queries" / inside DB Card |
| **Tertiary** (label inside row / card) | `Label` uppercase muted | `text-xs uppercase tracking-wider text-muted-foreground` | "VERSION" / "UPTIME" labels |

Operator's eye gradient: BIG → medium → small. Three weights. Don't use
the same `text-xs uppercase` everywhere — it creates the Cost flatness.

---

## 17.6 ANCHOR DOT — interaction + animation spec (Cost critique applied)

Top-bar Health anchor is a single status dot. But it does more than just
display color.

### Color tokens

```tsx
const dotTone = {
  healthy:  "bg-success",
  degraded: "bg-warning",
  down:     "bg-destructive",
  unknown:  "bg-muted-foreground",
}
```

### Hover state

```tsx
<HoverCard>
  <HoverCardTrigger>
    <span className={cn("h-2 w-2 rounded-full", dotTone[overallStatus])} />
    <span>HEALTH</span>
    <span className="text-muted-foreground">{overallStatus}</span>
  </HoverCardTrigger>
  <HoverCardContent className="w-[320px]">
    <div className="space-y-1 text-sm">
      <div>● <strong>Services</strong>: N healthy / N degraded / N down</div>
      <div>● <strong>Providers</strong>: N healthy / N degraded</div>
      <div>● <strong>Database</strong>: connections N/N, cache N%</div>
      {hasIncident && (
        <div className="pt-2 mt-2 border-t">
          <Button size="sm" variant="default">View incidents →</Button>
        </div>
      )}
    </div>
  </HoverCardContent>
</HoverCard>
```

Operator hovers anchor → sees breakdown WITHOUT navigating. Click →
Health page.

### Transition animation

- healthy → degraded: dot color tween 200ms; brief 1.4× pulse (300ms)
- degraded → down: same pulse pattern, color red
- → healthy: color tween, NO pulse (no alarm on recovery)
- Updates pushed via `WS /api/v1/realtime` — no polling needed

### Status text next to dot

The word after the dot ("healthy" / "degraded" / "down") matches the dot:
- healthy: `text-muted-foreground` (quiet — good news is no news)
- degraded: `text-warning font-medium`
- down: `text-destructive font-medium`

Operator's eye is drawn to text only when something's wrong. Matches the
"color is signal, not decoration" framework principle.

---

# 18. ZONE-BY-ZONE DESIGN + COMPONENT SPEC

Stack: **shadcn/ui** + **Recharts** + **lucide-react** + **tailwindcss**.
Same as Cost page.

---

## Zone 0 — Incident banner (conditional)

**Purpose**: When something's degraded, this is what operator sees first.
Renders only when there's an issue.

> **v1 honesty**: We don't yet have the service transition log (Gap 20),
> so we **cannot show "since 14:32 (12m ago)"** in v1. Banner copy must
> use **current-only** language until v0.2 lands the log.

### Component layout (v1)

```
┌──────────────────────────────────────────────────────────────────────┐
│ ⛔  Anthropic is degraded                          last check 2s ago  │
│ p95 latency 4,200ms · 18% of recent calls failing                    │
│ [Switch fallback]  [Open LiteLLM ↗]                                  │
└──────────────────────────────────────────────────────────────────────┘
```

Note: NO "since X" or "for Nm" — those need Gap 20 (v0.2).

### Component map

| Element | shadcn | Notes |
|---|---|---|
| Banner container | `Alert` variant="destructive" | Theme tokens: `border-destructive bg-destructive/10` |
| Icon | `lucide-react Octagon` (filled) | `text-destructive` |
| Title | `AlertTitle` | `text-base font-semibold` — bigger than DB section labels |
| Meta line | `AlertDescription` | `font-mono tabular-nums` for the latency / % numbers |
| Action buttons | `Button` size="sm" variant="default" / "outline" | "Switch fallback" primary, "Open X" outline |
| Last-check chip | `text-muted-foreground text-xs` right-aligned | live-tick on each successful probe |

### Empty-state

When NO incidents: banner renders nothing. Page top instead carries the
**affirmative "all clear" tile**:

```
┌──────────────────────────────────────────────────────────────────────┐
│ ✓  All systems operational                  last full sweep 3s ago   │
└──────────────────────────────────────────────────────────────────────┘
```

Use `Alert` variant="default" with `border-l-4 border-success` and
`CheckCircle2` icon (success-colored). This card stays — same height as
the incident variant so layout doesn't jump on transitions.

### Animation on transition

- Status flip (healthy → degraded): banner slides in from top (300ms ease),
  brief shake (subtle), then settles
- Status flip (degraded → healthy): degraded banner fades out (200ms),
  "All clear" card fades in (200ms), no jump

### Multiple concurrent incidents

Stack as separate `<Alert>` instances, ordered by severity (down > degraded).
Each carries its own action row.

---

## Zone A — LLM Provider Availability

**Purpose**: The primary read during incidents. Which provider is
healthy, what's the latency, what's the trend.

### Component layout

```
┌──────────────────────────────────────────────────────────────────────┐
│  LLM PROVIDERS                              Window: [last 24h ▾]     │
│ ──────────────────────────────────────────────────────────────────── │
│                                                                      │
│  ┌────────────────────┐ ┌────────────────────┐ ┌────────────────────┐│
│  │ ● openai            │ │ ● anthropic         │ │ ● google            ││
│  │   100% uptime       │ │   82% uptime  ⚠     │ │   100% uptime       ││
│  │   p95  420ms        │ │   p95  4,200ms ⚠   │ │   p95  680ms        ││
│  │   ▁▁▂▁▁▁▂▁▁▁▂▁     │ │   ▁▁▁▁▂▆█████     │ │   ▁▁▂▁▁▁▂▁▁▁▂▁     ││
│  │   3,412 samples     │ │   3,212 samples     │ │   2,891 samples     ││
│  │                     │ │   [Switch fallback] │ │                     ││
│  │   [Open LiteLLM ↗]  │ │   [Open LiteLLM ↗]  │ │   [Open LiteLLM ↗]  ││
│  └────────────────────┘ └────────────────────┘ └────────────────────┘│
└──────────────────────────────────────────────────────────────────────┘
```

### Component map

| Element | shadcn | Recharts | Notes |
|---|---|---|---|
| Outer container | `Card` + `CardHeader` + `CardContent` | — | |
| Window picker | `Select` | — | 24h / 7d / 30d |
| Provider grid | CSS grid `grid-cols-3` responsive | — | Wraps to 2 cols on tablet, 1 on mobile |
| Each provider | `Card` (nested, variant subtle) | — | `<ProviderHealthCard>` primitive |
| Status dot | Custom span with `bg-success` / `bg-warning` / `bg-destructive` | — | 8px circle |
| Uptime % | `font-mono tabular-nums` | — | |
| p95 latency | `font-mono tabular-nums` | — | yellow if >1s, red if >3s |
| Sparkline | `<Sparkline />` from Cost | `LineChart` | shows degradation visually |
| Action buttons | `Button` size="sm" variant="outline" / "default" | — | "Switch fallback" only when degraded |
| Sample count | `text-muted-foreground text-xs` | — | |

### New primitive `<ProviderHealthCard />`

```tsx
<ProviderHealthCard
  provider="anthropic"
  status="degraded"
  uptimePct={82}
  p95LatencyMs={4200}
  medianLatencyMs={1800}
  sparkline={number[]}         // latency_buckets over window
  sampleCount={3212}
  lastCheckAt="2026-06-17T14:44:12Z"
  adminUrl="http://litellm:4000/ui"
  onSwitchFallback={() => ...}   // opens Sheet for PLATFORM → LLM providers
/>
```

Conditional rendering: "Switch fallback" Button only when
`status !== "healthy"`.

### Empty-state per card (Cost critique applied)

When `sampleCount === 0` (fresh fork, polling hasn't reached threshold
yet) — render structure, not blank:

```
┌────────────────────┐
│ ◌  anthropic       │   ← gray dot (not green) — status unknown
│   awaiting first    │
│   check             │
│   p95  —            │   ← em-dash, not "0ms"
│   ▁▁▁▁▁▁▁▁▁▁       │   ← flat sparkline frame, not missing
│   0 samples         │
└────────────────────┘
```

This is the framework principle: **structure visible even at zero**.

### Empty grid state

When NO providers configured (zero rows in
`/api/v1/admin/llm/provider-health`):

```
┌──────────────────────────────────────────────────────────────┐
│ No providers configured.                                      │
│ Add a provider in PLATFORM → LLM providers, or set            │
│ OPENROUTER_API_KEY in .env to start with OpenRouter.         │
│                                                               │
│ [Open PLATFORM → LLM providers]                              │
└──────────────────────────────────────────────────────────────┘
```

Card has `border-dashed border-muted` — distinct from active provider cards.

### Sparseness handling (one provider only)

When `providers.length === 1`, render as full-width card (not 3-col grid
with empty space). Grid only kicks in at N>=2.

### Provider name friendly display (Cost critique applied)

Raw provider ids (`openrouter`, `anthropic`, `openai`) are already
friendly. But model rows on hover should follow Cost's truncate-friendly
convention:
- `openrouter/qwen/qwen-2.5-72b-instruct` → "qwen 2.5 72b (via openrouter)"
- `anthropic/claude-3-5-sonnet` → "claude 3.5 sonnet (anthropic)"

Apply via shared `formatModelName(id)` util.

### Live-tick

- Latency numbers update via WebSocket — roll animation only on >50ms change
- Sparkline appends new bucket on tick, oldest drops off — smooth slide
- Status dot color tween 200ms on transition; brief pulse (1.2× scale,
  300ms) on degraded → healthy or vice versa

---

## Zone B — Backing Services Grid

**Purpose**: At-a-glance "all my OSS services up?" View. Click any →
opens that OSS's admin UI.

### Component layout

```
┌──────────────────────────────────────────────────────────────────────┐
│  CONNECTED SERVICES                              [Refresh] [Group: kind ▾] │
│ ──────────────────────────────────────────────────────────────────── │
│                                                                      │
│  RUNTIME                                                             │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │ ● BackAI runtime  v1.2.0  ── port 8080 ── 3 days uptime         │  │
│  │   Runtime API and admin middleware                              │  │
│  └────────────────────────────────────────────────────────────────┘  │
│                                                                      │
│  DATA                                                                │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │ ● Postgres  16.4  ── postgres:5432 ── checked 2s ago           │  │
│  │   Primary database                                              │  │
│  └────────────────────────────────────────────────────────────────┘  │
│                                                                      │
│  INTELLIGENCE                                                        │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │ ● LiteLLM  v1.40  ── litellm:4000 ── 2s ago     [Open ↗]       │  │
│  │   LLM provider routing                                          │  │
│  └────────────────────────────────────────────────────────────────┘  │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │ ● AgentField  v0.5  ── agentfield:8081 ── 2s ago  [Open ↗]     │  │
│  │   Agent execution and reasoner graph                            │  │
│  └────────────────────────────────────────────────────────────────┘  │
│                                                                      │
│  STORAGE / DELIVERY / OBSERVABILITY                                  │
│  ... grouped sections ...                                            │
└──────────────────────────────────────────────────────────────────────┘

  Each row is `<ServiceHealthRow>` — REUSABLE on Adapters page in PLATFORM
```

### Component map

| Element | shadcn | Notes |
|---|---|---|
| Outer container | `Card` | |
| Group header | `Separator` + `Label` uppercase | "RUNTIME", "DATA", etc. — grouped by `kind` |
| Service row | `<ServiceHealthRow />` | Built from Card variant subtle |
| Status dot | Same pattern as Provider | green/yellow/red |
| Open button | `Button variant="outline" size="sm"` | Visible only when `admin_url !== null` |
| Group-by switcher | `Select` | "kind" / "status" / "alphabetical" |
| Refresh button | `Button variant="ghost" size="icon"` | `lucide-react RefreshCw` |

### New primitive `<ServiceHealthRow />`

```tsx
<ServiceHealthRow
  service={{
    id, name, kind, status, version,
    host, port, purpose, admin_url, checked
  }}
  onOpenAdmin={() => ...}
/>
```

Renders one row from `/api/v1/admin/services`. Reused on PLATFORM →
Adapters page.

### Status dot color tokens (explicit — Cost critique applied)

Not raw colors — theme tokens:

```tsx
const statusTone = {
  healthy:  "bg-success",          // green
  degraded: "bg-warning",          // yellow
  offline:  "bg-destructive",      // red
  unknown:  "bg-muted-foreground", // gray
}
```

Dot size: 8px circle (`h-2 w-2 rounded-full`). When degraded/offline:
add subtle 1.4× scale pulse animation, 1.5s loop, halt on hover.

### Friendly host display (Cost critique applied)

Don't dump `host:port` raw. Format:

- Show: `litellm` (with `:4000` muted, smaller)
- Show: `agentfield` (with `:8081` muted)
- On hover: full `litellm:4000` revealed in `Tooltip`

```tsx
<span className="font-mono text-sm">
  {service.host}
  <span className="text-muted-foreground ml-1">:{service.port}</span>
</span>
```

### Empty-state for group

If an entire `kind` group has zero services (e.g., no observability
services configured):

```
OBSERVABILITY
┌──────────────────────────────────────────────────────────────┐
│ No observability backends configured.                         │
│ Configure Loki / Tempo / Prometheus / GlitchTip via env vars. │
│ See PLATFORM → Observability.                                 │
└──────────────────────────────────────────────────────────────┘
```

Dashed border, muted text. Distinct from active service cards.

### Live-tick

- Status dot color tween 200ms on transition
- `checked` timestamp updates in place ("2s ago" → "3s ago"); never re-renders
  the whole row
- Version string update (on rolling restart) → brief highlight

### Card containment

Each `kind` group renders in its OWN sub-Card with subtle border, NOT
piled into one big Card. Sub-Card layout:

```tsx
<Card className="border-muted">
  <CardHeader className="py-2">
    <Label className="text-xs uppercase tracking-wider text-muted-foreground">
      INTELLIGENCE
    </Label>
  </CardHeader>
  <CardContent className="space-y-2">
    {/* ServiceHealthRow instances */}
  </CardContent>
</Card>
```

---

## Zone C — Database Health

**Purpose**: Deep DB diagnostics. Connections, queries, tables, vacuum,
locks. The "is Postgres OK" view.

### Component layout

```
┌──────────────────────────────────────────────────────────────────────┐
│  DATABASE HEALTH                                                     │
│ ──────────────────────────────────────────────────────────────────── │
│  ┌─ CONNECTIONS ────────────┐  ┌─ CACHE HIT ────────────────────────┐│
│  │ 12 active  78 idle  0 wait│  │   ┌──────────────┐                ││
│  │ ████░░░░░░░░░░░░░░░░░░░░ │  │   │      ▓▓▓     │  98.4%          ││
│  │ 13% saturated · 90 total │  │   │   ▓▓▓▓▓▓▓    │  hit ratio       ││
│  └──────────────────────────┘  │   │   ▓▓▓▓▓▓▓    │  ▴0.3% vs 24h   ││
│                                 │   │      ▓▓▓     │                  ││
│                                 │   └──────────────┘                  ││
│                                 └──────────────────────────────────────┘│
│                                                                      │
│  SLOW QUERIES  (top 10, last 24h, requires pg_stat_statements)       │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │ Query                          Calls   Mean    Total           │  │
│  │ select * from suite_co...      12,892  42.1ms  543s   [Open]   │  │
│  │ insert into suite_acti...       8,213  18.4ms  151s   [Open]   │  │
│  │ ...                                                            │  │
│  └────────────────────────────────────────────────────────────────┘  │
│                                                                      │
│  LARGEST TABLES                                                      │
│  suite_cost_events       ███████████████████████████  4.2 GB         │
│  suite_runs              ████████████  1.8 GB                        │
│  suite_logs              ████████      980 MB                        │
│  ...                                                                 │
│                                                                      │
│  VACUUM STATUS                                                       │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │ Table              Last vacuum    Dead tuples   Status         │  │
│  │ suite_cost_events  2h ago         1,234         ok              │  │
│  │ suite_logs         12d ago        892,131       ⚠ needs vacuum │  │
│  └────────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────────┘
```

### Component map

Each sub-section sits in its OWN nested `Card` so they don't pile up
visually (Cost critique applied — flat sectioning was a problem). Five
sub-Cards: Connections · Cache · Slow Queries · Largest Tables · Vacuum.

| Element | shadcn | Recharts | Notes |
|---|---|---|---|
| Outer container | `Card` (page-level) | — | |
| Sub-section Card | `Card` (nested, subtle border) | — | Each widget gets its own container |
| Sub-section header | `CardHeader` + `CardTitle` size="sm" | — | Smaller than zone header — visual hierarchy |
| Connections widget | Custom div + multi-segment `Progress` | — | Active/idle/waiting stacked; `h-3` minimum |
| Cache hit donut | `<MetricDonut />` from Cost | `PieChart innerRadius={50}` | Center number = hit % big; trend sparkline below |
| Cache hit delta | `<DeltaIndicator />` | — | Hidden when prior data absent (don't fake) |
| Slow queries table | `Table` from shadcn | — | `font-mono text-xs` query col with `HoverCard` for full SQL |
| Open query button | `Button variant="ghost" size="sm"` | — | Drills to BUILD → Data → SQL |
| Largest tables bars | `<SortedBars />` from Cost | `BarChart` layout="vertical" | **Bar `h-3` minimum** (Cost critique) |
| Vacuum status table | `Table` | — | `Badge` for "ok" / "needs vacuum" — semantic color |
| Missing extension banner | `Alert` variant="default" | — | When `pg_stat_statements` not loaded |

### Empty-state per sub-section (Cost critique applied)

**Connections** — when DB unavailable: render structural bar at zero with
"Database unavailable" overlay. Never blank.

**Cache hit donut** — at zero traffic:
```
   ┌──────────────┐
   │              │
   │      —       │     ← em-dash center, not "0%"
   │   no cache   │
   │   traffic    │
   │              │
   └──────────────┘
```

**Slow queries** — when no rows yet OR `pg_stat_statements` unavailable:
```
┌────────────────────────────────────────────────────────────────┐
│ Query                              Calls   Mean    Total       │
├────────────────────────────────────────────────────────────────┤
│  First period of data — slow queries appear after some traffic. │
│  Requires pg_stat_statements (loaded ✓).                        │
└────────────────────────────────────────────────────────────────┘
```

Table header always visible. Body shows the absence-explanation row.

**Largest tables** — at fresh fork (mostly empty tables):
```
suite_cost_events    ░░░░░░░░░░░░░░░  16 KB    (seeded)
suite_runs           ░░░░░░░░░░░░     12 KB    (seeded)
suite_logs           ░░░░░░          8 KB     (seeded)
```

Use muted bar color (`bg-muted-foreground/30`) for sub-MB tables;
saturated color kicks in above 100 MB.

**Vacuum** — when no tables need vacuum: affirmative empty card:
```
┌──────────────────────────────────────────────┐
│ ✓ All tables vacuumed recently               │
│ Last sweep across N tables 2h ago            │
└──────────────────────────────────────────────┘
```

### Largest tables bar height (Cost critique — Cost-by-Model bars were too thin)

```tsx
<BarChart layout="vertical" data={tables}>
  <Bar
    dataKey="size_bytes"
    fill="var(--chart-1)"
    className="!h-3"           // minimum 12px bar
    radius={[0, 4, 4, 0]}
  />
</BarChart>
```

Don't let Recharts auto-thin bars below `h-3`.

### Friendly size formatting

- `< 1KB` → "—" (don't render bar)
- `1KB - 1MB` → "X KB" (muted color)
- `1MB - 1GB` → "X MB" (full color)
- `> 1GB` → "X.Y GB" (full color)

### Live-tick

- Cache hit % updates every 60s with 200ms ease tween
- Slow queries table refreshes on tab focus only — these are expensive
- Connection count rolls subtly on change

### Slow query row with `HoverCard`

```tsx
<TableRow>
  <TableCell>
    <HoverCard>
      <HoverCardTrigger>
        <code className="font-mono text-xs truncate">
          select * from suite_co...
        </code>
      </HoverCardTrigger>
      <HoverCardContent className="w-[600px]">
        <pre className="text-xs whitespace-pre-wrap">
          {fullQuery}
        </pre>
      </HoverCardContent>
    </HoverCard>
  </TableCell>
  <TableCell className="tabular-nums">{calls}</TableCell>
  ...
</TableRow>
```

---

## Zone D — Runtime Self

**Purpose**: "What runtime am I running?" Verify version, uptime, mem,
goroutines.

### Component layout

```
┌──────────────────────────────────────────────────────────────────────┐
│  RUNTIME                                                             │
│  ──────────────────────────────────────────────────────────────────  │
│  ┌─ VERSION ────────┐ ┌─ UPTIME ──────┐ ┌─ TOP ROUTES ───────────┐  │
│  │ v1.2.0           │ │ 3d 14h 22m    │ │ POST /v1/llm/chat  42k │  │
│  │ build sha 8af3c2 │ │ since 6/14    │ │ GET  /v1/runs      18k │  │
│  └──────────────────┘ └───────────────┘ │ POST /v1/agents/...12k │  │
│  ┌─ MEMORY ─────────┐ ┌─ GOROUTINES ──┐ │ ...                    │  │
│  │ 128 MB allocated │ │ 247           │ └────────────────────────┘  │
│  └──────────────────┘ └───────────────┘                              │
│                                                                      │
│  HTTP                                                                │
│  Total requests today  1.4M  ·  p95 latency  82ms  ·  Errors  0.2% │
└──────────────────────────────────────────────────────────────────────┘
```

### Component map

| Element | shadcn | Notes |
|---|---|---|
| Outer container | `Card` (page-level) | |
| Sub-tile | `Card` (nested, subtle border) | label uppercase + value `font-mono tabular-nums` |
| Top routes table | `Table` (sortable by traffic) | from `/metrics/summary` `ByRoute` |
| HTTP stats row | flex row with separators | inline `font-mono tabular-nums` |
| HTTP stat dividers | `Separator orientation="vertical"` | between metric/value pairs |

### Empty-state (Cost critique applied)

When `total_requests = 0` (fresh fork, no traffic yet):

**Sub-tiles** render with structure:
```
┌─ VERSION ──────┐ ┌─ UPTIME ──┐ ┌─ MEMORY ───┐ ┌─ GOROUTINES ──┐
│ v1.2.0          │ │ 2m         │ │ 48 MB       │ │ 23             │
│ build 8af3c2    │ │ since 14:21│ │ allocated   │ │                │
└────────────────┘ └────────────┘ └─────────────┘ └────────────────┘
```

Real data even on fresh fork (version, uptime, mem, goroutines all exist
immediately).

**Top routes table** at zero traffic:
```
┌────────────────────────────────────────────────────────────┐
│ Route                                  Requests   Avg ms   │
├────────────────────────────────────────────────────────────┤
│ No traffic yet — try the API explorer or send a chat.     │
└────────────────────────────────────────────────────────────┘
```

Header always visible. Empty body row uses muted text + CTA link to API
explorer.

**HTTP stats row** at zero:
```
Total requests today  —  ·  p95 latency  —  ·  Errors  —
```

Em-dashes, not "0". Don't fake data, but don't hide the structure either.

### Live-tick

- Goroutines count rolls subtly on change (~5-10% changes per second)
- Heap allocated updates every 5s (don't show every GC tick — too noisy)
- HTTP stats row tick when WebSocket pushes
- Uptime "2m" → "3m" → ... rolls in place; never re-renders the tile

---

## 19. PRIMITIVES INTRODUCED (additions to library)

Adding to shared library (alongside Cost's 11):

| Primitive | Built from | Reused on |
|---|---|---|
| `<ProviderHealthCard />` | `Card` + `<Sparkline />` + `<DeltaIndicator />` | Health Zone A; future Provider detail page |
| `<ServiceHealthRow />` | `Card` (subtle) + status dot + `Button` | Health Zone B; PLATFORM → Adapters page |
| Status dot (small) | custom span + tailwind | Throughout: anchor, Zone A, Zone B, list rows |
| `<IncidentBanner />` | `Alert` variant=destructive + action `Button` row | Health Zone 0; future Errors page incident banner |

Reuses from Cost: `<Sparkline />`, `<DeltaIndicator />`, `<SortedBars />`,
`<MetricDonut />`.

---

## 20. OPEN QUESTIONS & TODOs

### Deferred to v0.2

- **Workers section** — needs River-pkg status surface (Gap 21)
- **TLS / cert expiry** — needs Caddy admin endpoint (Gap 22)
- **Status transition log** ("degraded since 14:32") — needs persistence (Gap 20)
- **Smart recovery suggestions** — link to PLATFORM page v1, intelligent
  recommendations v0.2 (Gap 23)
- **Cross-service correlation** — "DB slow queries correlate with provider
  latency spike" — research v0.2+

### Open for v1 design

1. **Group-by for backing services** — by kind / by status / alphabetical.
   Recommendation: by kind (default), with quick switcher.

2. **Zone A grid columns at different breakpoints.** 3-2-1 (desktop / tablet
   / mobile)?

3. **Refresh button granularity** — refresh all services at once, or
   per-card? Recommendation: per-card icon only + global "Refresh all"
   at top.

4. **Cache hit donut animation on update.** Tween via fill animation when
   value changes >0.5%.

5. **Slow query rendering** — first 60 chars truncated; full via HoverCard.
   Confirm width.

6. **What's the "okay vacuum" threshold?** Tied to dead_tuples ratio. Need
   a default heuristic. v1 hardcode at `dead_tuples > rows * 0.2`.

7. **Should Zone D show top routes table?** Yes — useful for "which routes
   are getting hammered" intuition. Sortable.

### Backend gaps (in `required-backend-gaps.md`)

- Gap 20 — Service status transition log (Inefficient; v0.2)
- Gap 21 — Worker / River status (Blocking for Workers; v0.2)
- Gap 22 — TLS / cert expiry (Inefficient; v0.2)
- Gap 23 — Suggested recovery actions (Inefficient; v0.2)
- Gap 24 — DB previous-period cache hit ratio (Cosmetic; v0.2)

---

## v1 SHIPS / v0.2 DEFERS — at a glance

```
SHIPS in v1:
  ✓ Anchor dot in top bar (green / yellow / red)
  ✓ Zone 0: Incident banner when degraded
  ✓ Zone A: Provider availability cards with sparklines + Switch fallback
  ✓ Zone B: Backing services grid grouped by kind
  ✓ Zone C: DB connections, cache, slow queries (if extension), tables, vacuum
  ✓ Zone D: Runtime version, uptime, mem, goroutines, top routes
  ✓ Click-out to every OSS admin UI (the central hub)
  ✓ Window picker (24h / 7d / 30d)
  ✓ Live updates via WebSocket

DEFERS to v0.2:
  ✗ Workers section (Gap 21)
  ✗ TLS / cert expiry section (Gap 22)
  ✗ "Degraded since X" transition deltas (Gap 20)
  ✗ Smart recovery action suggestions (Gap 23)
  ✗ DB cache hit ratio delta (Gap 24)
  ✗ Cross-service correlation
```

---

## Grooming checklist

```
☑ Purpose is one sentence
☑ Pillar is named (Anchor + page)
☑ Primary read identified (Incident banner + Zone A)
☑ Data sources mapped + verified to runtime/server
☑ Each data source WRAP / REFLECT / LINK declared
☑ OSS links surfaced (this page IS the central hub per Block 0)
☑ Three data states (empty / missing / degraded) specified
☑ Live data behavior explicit
☑ Mutations listed (Switch fallback, Open in X, Refresh, Acknowledge)
☑ Drill paths called out
☑ URL state declared (window, tab, service drawer, query)
☑ Mobile story declared (responsive read-only)
☑ Adapter pill placement: this PAGE is the modularity surface
☑ Reused primitives listed (Sparkline, DeltaIndicator, SortedBars, MetricDonut)
☑ New primitives justified (ProviderHealthCard, ServiceHealthRow, IncidentBanner)
☑ Library choices locked (shadcn + Recharts + lucide-react)
☑ Open questions documented
☑ Backend gaps surfaced (20-24) and severity flagged
```

All boxes checked.

---

---

# v0.1 → v1 Corrections (from implementation review)

After v0.1 ship + screenshot review, the following corrections must land
for v1. Each maps to a brief section above and is grouped by severity.

## A. Critical data-accuracy fixes (must ship in v1)

### A1. Zone A shows UPSTREAM providers, not adapter

**v0.1**: card labeled "litellm" with 91.8% uptime, 55s p95.

**Problem**: LiteLLM is the **gateway adapter** that routes to many
upstream providers. Zone A must show the upstream providers
(`openrouter`, `anthropic`, `openai`, `google`, etc.) — not the adapter.
The adapter "LiteLLM" lives in Zone B (Connected services).

**Fix path**:
- Verify `/api/v1/admin/llm/provider-health` is grouping by upstream
  provider, not by adapter. If currently returning per-adapter rows,
  this is a backend bug.
- The `health_poller.go` must poll EACH configured upstream
  (`OPENROUTER_API_KEY`, `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, etc.)
  and record per-provider rows in `suite_provider_health_log`.

### A2. Status threshold logic (semantic correctness)

**v0.1**: status pill says "healthy" while p95 latency is red 55s and
uptime is 91.8%. Contradictory.

**Fix — explicit threshold table** (apply both server-side and client-side):

```ts
function deriveStatus(uptime: number, p95_ms: number): Status {
  if (uptime >= 0.99 && p95_ms <= 1000)  return "healthy"
  if (uptime >= 0.90 || p95_ms <= 5000)  return "degraded"
  return "down"
}
```

- `healthy` requires BOTH high uptime AND low latency
- `degraded` is OR — either signal slipping is enough
- `down` is both signals failed

Status pill color MUST match (no green pill when latency is red).

### A3. AgentField belongs in INTELLIGENCE group, not OTHER

**v0.1**: AgentField appears under "OTHER" group at the bottom.

**Fix**: in `services.go` `kindBySlot` map, ensure `"reasoning"` slot
returns `kind: "intelligence"`. So Zone B groups LiteLLM + AgentField
both under INTELLIGENCE.

Also revisit OTHER bucket — Billing and Notifications should land under
DELIVERY (not OTHER), per the kind taxonomy in framework.

### A4. Recovery actions on degraded provider cards

**v0.1**: provider card with red 55s p95 has NO action buttons.

**Fix — restore `<ProviderHealthCard />` actions per §18 Zone A spec**:

```tsx
{status !== "healthy" && (
  <Button size="sm" variant="default" onClick={onSwitchFallback}>
    Switch fallback
  </Button>
)}
{adminUrl && (
  <Button size="sm" variant="outline" asChild>
    <a href={adminUrl} target="_blank">
      Open LiteLLM ↗
    </a>
  </Button>
)}
```

Critical for Journey 8 — operator must have inline recovery path.

### A5. Connected services rows missing [Open ↗] buttons

**v0.1**: rows like LiteLLM, AgentField have `admin_url` set but no
visible click-out button. The whole point of Block 0 decision #4 (Health
is the central OSS-link hub) requires these.

**Fix — every `<ServiceHealthRow />` must render**:

```tsx
{service.admin_url && (
  <Button size="sm" variant="outline" asChild className="ml-auto">
    <a href={service.admin_url} target="_blank">
      Open ↗
    </a>
  </Button>
)}
```

Right-aligned. Visible for any service with `admin_url`.

---

## B. Layout fixes

### B1. Provider card layout at N=1

**v0.1**: split UPTIME/P95 columns with vertical separator + sparkline
spanning full width below. Confusing visual structure when only one
provider configured.

**Fix — at N=1, render as full-width single card**:

```
┌──────────────────────────────────────────────────────────────────────┐
│ ● openrouter                                          healthy        │
│                                                                      │
│  Uptime    100%         p95 latency  420ms                           │
│  ▁▁▂▁▁▁▂▁▁▁▂▁▁▁▂▁▁▁▂▁▁▁▂▁▁▁▂▁  (latency over window, 24h)         │
│                                                                      │
│  3,412 samples · last check 37s ago        [Open LiteLLM ↗]         │
└──────────────────────────────────────────────────────────────────────┘
```

3-col grid only kicks in at N≥2. At N=1, full-width with clearer
labeling.

### B2. Section header weight differentiation (recurring from Cost)

**v0.1**: "LLM providers", "Connected services", "Database", "Runtime"
all roughly same weight. Sub-sections "Connections", "Cache", "Slow
queries", "Largest tables", "Vacuum" also same weight as each other.

**Fix — apply §17.5 three-tier hierarchy strictly**:

```tsx
// PRIMARY (zone heading)
<CardTitle className="text-lg font-semibold">
  Connected services
</CardTitle>

// SECONDARY (sub-section inside zone)
<CardTitle className="text-sm font-medium">
  Slow queries
</CardTitle>

// TERTIARY (group label inside row container)
<Label className="text-xs uppercase tracking-wider text-muted-foreground">
  RUNTIME
</Label>
```

Visual gradient: BIG → medium → small. Three distinct sizes. Don't put
all section headers in tertiary tier.

### B3. Database sub-sections in nested sub-Cards

**v0.1**: Connections / Cache / Slow queries / Largest tables / Vacuum
piled in one big Card with thin separators.

**Fix — each sub-section in its own `<Card>` with subtle border**:

```tsx
<Card>  {/* zone */}
  <CardHeader><CardTitle>Database</CardTitle></CardHeader>
  <CardContent className="space-y-4">

    <Card className="border-muted">  {/* sub-section */}
      <CardHeader className="py-3">
        <CardTitle className="text-sm font-medium">Connections</CardTitle>
      </CardHeader>
      <CardContent>...</CardContent>
    </Card>

    <Card className="border-muted">
      <CardHeader className="py-3">
        <CardTitle className="text-sm font-medium">Cache</CardTitle>
      </CardHeader>
      <CardContent>...</CardContent>
    </Card>

    {/* etc */}
  </CardContent>
</Card>
```

Visible gap between sub-Cards. No more piling.

---

## C. Component-level refinements

### C1. Connections widget — stacked segment bar (not single thin)

**v0.1**: single thin bar showing "9% of pool in use" + three numbers
above.

**Fix — show all three counts as stacked segments**:

```
ACTIVE 1   IDLE 8   FREE 91                            MAX 100
[█][████████]░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                                                                  
9 in use · 91 capacity
```

Three segments stacked horizontally on a single `h-3` bar:
- `bg-primary` for active (load-bearing connections)
- `bg-primary/40` for idle (warm pool)
- `bg-muted` for free (capacity remaining)

Width per segment proportional to count. Operator sees pool saturation +
warm/cold ratio at a glance.

### C2. Cache donut — sample threshold + caveat

**v0.1**: 100% hit ratio donut at ~6 samples — visually screams "perfect"
but statistically meaningless.

**Fix — gate the donut on sample count**:

```tsx
{sampleCount < 100 ? (
  <div className="text-center text-muted-foreground">
    <span className="text-2xl font-mono">—</span>
    <p className="text-xs mt-2">
      {sampleCount} samples · need ≥100 for meaningful ratio
    </p>
  </div>
) : (
  <MetricDonut hitPct={hitRatioPct} sampleCount={sampleCount} />
)}
```

Em-dash with sample count when sparse. Real donut only with statistical
confidence.

### C3. Slow query truncation + HoverCard

**v0.1**: query column allows full SQL to flow horizontally; rows include
one-shot migration DDL (REINDEX, CREATE TABLE, GRANT, TRUNCATE).

**Fix**:
- Truncate to **60 chars** with ellipsis
- `HoverCard` reveals full SQL on hover
- **Filter out one-shot DDL** by default (REINDEX, CREATE, GRANT,
  TRUNCATE, DROP, ALTER, DELETE FROM where rowcount low) — these are
  setup noise
- Add toggle: `[ ] Include DDL` checkbox above table for power users

Without DDL filtering, every fresh fork's slow queries table is just
migration noise.

### C4. Status indicator consistency

**v0.1**: provider card has status text on right ("healthy"); service
rows have just a dot (no text). Inconsistent.

**Fix — uniform pattern across all status displays**:

- **Status dot** ALWAYS left-of-name (8px circle, theme tokens)
- **Status text** when needed: right-aligned, color-keyed
  - healthy → `text-muted-foreground` (quiet — good news is no news)
  - degraded → `text-warning font-medium`
  - down → `text-destructive font-medium`

When everything is healthy, status text reads as quiet metadata. When
something's wrong, it grabs attention.

### C5. Timestamp convention

**v0.1**: provider card shows "37s ago"; service rows show "now". Same
data, two formats.

**Fix — single convention everywhere**:

```ts
function formatAge(secs: number): string {
  if (secs < 1)   return "now"
  if (secs < 60)  return `${Math.floor(secs)}s ago`
  if (secs < 3600) return `${Math.floor(secs/60)}m ago`
  return `${Math.floor(secs/3600)}h ago`
}
```

Use `formatAge()` for every "last check" / "last update" display on the
page. "now" reserved for genuinely <1s ago.

### C6. HEALTH anchor — quiet when healthy

**v0.1**: top-bar shows `● HEALTH  healthy` with green dot + word.

**Fix — hide the word "healthy"**; show status text only when
problematic:

```tsx
<HoverCardTrigger>
  <span className={cn("h-2 w-2 rounded-full", dotTone[status])} />
  <span>HEALTH</span>
  {status !== "healthy" && (
    <span className={cn("text-xs ml-1", textTone[status])}>
      {status === "down" ? `${downCount} services down` : "degraded"}
    </span>
  )}
</HoverCardTrigger>
```

Per framework principle "color is signal, not decoration" — the green
dot already says "healthy"; the word is redundant. Reserve text for when
something's wrong.

---

## D. Sidebar / cross-cutting concerns (out-of-page scope)

These aren't Health page issues but were surfaced during this review:

### D1. v0.2 badge overuse

Sidebar marks ~16 items as v0.2 even though their backend endpoints
exist (Runs, Errors, Logs, Queue, Webhook flow, Cache, Notifications,
Tenants, Users, API keys, Audit log, Billing, etc.).

**Recommended**: either
- Drop the v0.2 badge from items with shipped endpoints, OR
- Replace with "v1 lite" or "thin v1" with HoverCard explaining the
  rich version is v0.2

Current state reads as "feature not ready" which discourages exploration.

### D2. PLATFORM group not visible

Sidebar cuts off below People. PLATFORM (Adapters, Auth, LLM providers,
Webhook subscribers, Notifications channels, Secrets, Observability,
Billing adapter, Deploy targets) must be visible or scroll-reachable.

PLATFORM is essential — that's where adapter swaps live.

---

## Critique-fix summary table

| # | Severity | Surface | Fix |
|---|---|---|---|
| A1 | **Critical** | Zone A | Show upstream providers, not adapter |
| A2 | **Critical** | Anywhere status renders | Apply threshold table; red latency ≠ green pill |
| A3 | **Critical** | Zone B groups | AgentField → INTELLIGENCE; Billing/Notif → DELIVERY |
| A4 | **Critical** | Zone A | Restore Switch fallback + Open LiteLLM actions |
| A5 | **Critical** | Zone B | Add [Open ↗] to every service with `admin_url` |
| B1 | Layout | Zone A | Single-column card at N=1, 3-col at N≥2 |
| B2 | Layout | All zones | Three-tier section header weight (lg / sm / xs) |
| B3 | Layout | Zone C | Nested sub-Cards, not piled |
| C1 | Component | Zone C Connections | Stacked segment bar (active/idle/free) |
| C2 | Component | Zone C Cache | Sample threshold ≥100 before donut renders |
| C3 | Component | Zone C Slow queries | 60-char truncate + HoverCard + DDL filter |
| C4 | Component | Zone A + Zone B | Consistent status dot + text pattern |
| C5 | Component | All zones | `formatAge()` everywhere; "now" only <1s |
| C6 | Anchor | Top bar | Hide "healthy" word; show text only when problem |
| D1 | Sidebar | Cross-cut | Drop / clarify v0.2 badge |
| D2 | Sidebar | Cross-cut | PLATFORM group visible / scroll-reachable |

All checked corrections must land before Health page is considered v1.

---

_Last updated: 2026-06-17. Next: Drawer primitive design._

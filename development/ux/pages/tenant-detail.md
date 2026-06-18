# Page Brief — Tenant Detail

> The customer-care anchor. Multi-tab page composed over many primitives.
> Reached from PEOPLE → Tenants → click row.
> Groomed against `development/ux/page-design-framework.md`.

---

## 1. PURPOSE

Show everything about one of the operator's customers, in one navigable
surface — so Journey 5 (customer issue triage), Journey 7 drill from
Cost, and Journey 6 budget alert all resolve here.

Confidence questions answered:
- *"Find this customer, see their state."* (Journey 5 entry)
- *"What did this tenant do?"* (drill from Cost or Inbox)
- *"Is this tenant at risk?"* (proactive scan)
- *"Suspend / extend / refund — act safely."* (mutations)

---

## 2. PILLAR

PEOPLE group. The customer-care anchor.

Reached from:
- PEOPLE → Tenants (list) → click row
- Cost cell group-by-tenant → click tenant name
- Inbox approval / budget item → "Open tenant" action
- Cmd+K → type tenant name/email
- Activity feed event → tenant link

---

## 3. JOURNEYS SERVED

| # | Journey | Role |
|---|---|---|
| 5 | Customer-reported issue triage | **Direct — primary** |
| 6 | Push budget alert | Drill destination from Inbox alert |
| 7 | Cost-spike investigation | Drill from Cost when one tenant dominates |
| 8 | Provider outage | Sometimes — to check if specific tenants affected |

---

## 4. TIME BUDGET

- **30 sec** — quick state check ("are they healthy?")
- **3-5 min** — Journey 5 typical triage flow
- **15 min** — incident-scoped tenant investigation

---

## 5. DENSITY TARGET

**HIGH but tabbed.** Header is dense (KPIs + status + actions). Each tab
is dense in its own context. Operator navigates by tab, not by scrolling
one mega-page.

---

## 6. PRIMARY READ

**The sticky header.** Tenant name + status badge + KPI strip with
at-risk callouts give the answer in 3 seconds:
- Is this tenant active?
- Are they over budget?
- Are they generating errors?
- Are they unhealthy in any way?

Tabs are reached only when operator needs detail.

---

## 7. STRATEGIC SHAPE

A composition page over:
- **Drilldown endpoint** (`/api/v1/admin/tenants/{id}/drilldown`) for
  header + Overview tab
- **EntityListTable** primitive (from Runs brief) for Runs / Errors /
  Webhook flow tabs filtered to tenant
- **Cost zones** (from Cost brief) for Cost tab scoped to tenant
- **Custom tab content** for Members / Keys / Audit / Settings

Most of the page is reuse. New brief specs: header design + tab IA + the
4 tenant-specific tabs (Members / Keys / Audit / Settings).

---

## 8. DATA SOURCES

> **Audit status**: tenant drilldown endpoint exists and is RICH
> (`admin.go:700`). Most of the page renders from one fetch.
> Tab-specific endpoints verified for filtered Runs / Cost / Errors /
> Webhooks.

### 8.1 Primary endpoint (verified)

`GET /api/v1/admin/tenants/{id}/drilldown` returns:

```ts
{
  tenant: {
    id, name, slug, status, created_at, metadata
  },
  members: [{
    user: { id, email, name, ... },
    role: "owner" | "admin" | "member",
    last_active_at: string | null,
  }],
  api_keys: [{
    id, alias, prefix, status, last_used_at,
    rate_limit_rpm, rate_limit_tpm, monthly_cap_usd
  }],
  usage: {
    requests_30d: number,
    cost_usd_30d: number,
    storage_bytes: number,
    secrets_count: number,
    cost_sparkline: number[],       // 24 buckets
    request_sparkline: number[],    // 24 buckets
  },
  recent_runs: [{
    id, agent, status, started_at, duration_ms, cost_usd
  }],
  recent_webhooks: [{
    id, direction, event_type, status, created_at
  }],
  billing: {                         // nullable
    plan, subscription_status, current_period_end, trial_ends_at
  } | null
}
```

**One fetch fuels: header + Overview tab + Members tab + Keys tab + most
of Billing tab.**

### 8.2 Additional endpoints (per-tab)

| Tab | Endpoint | Status |
|---|---|---|
| Runs | `GET /api/v1/runs?tenant=<id>` | ✅ (Runs brief) |
| Cost | `GET /api/v1/cost?tenant=<id>` | ✅ (Cost brief) |
| Errors | `GET /api/v1/logs?level=error,fatal&tenant=<id>` | ✅ |
| Webhook flow | `GET /api/v1/webhooks/deliveries?tenant=<id>` | ✅ (verify filter support) |
| Audit | `GET /api/v1/admin/audit?tenant=<id>` | ✅ |
| Budget | `GET /api/v1/admin/budgets/{tenantId}` + `PUT` | ✅ (Cost brief) |
| Billing detail | `GET /api/v1/billing/customers/{tenantId}` | ✅ |

### 8.3 Mutations

| Mutation | Endpoint | Auditable | Mobile |
|---|---|---|---|
| Edit tenant name / metadata | `PATCH /api/v1/admin/tenants/{id}` | ✅ | n/a |
| Suspend tenant | `PATCH /api/v1/admin/tenants/{id}` with `status: "suspended"` | ✅ | Yes |
| Delete tenant | `DELETE /api/v1/admin/tenants/{id}` | ✅ | No (destructive) |
| Invite member | `POST /api/v1/admin/memberships` | ✅ | No |
| Remove member | `DELETE /api/v1/admin/memberships/{tenantId}/{userId}` | ✅ | No |
| Issue key | `POST /api/v1/admin/keys` | ✅ | Yes |
| Rotate key | `POST /api/v1/admin/keys/{id}/rotate` | ✅ | No |
| Revoke key | `DELETE /api/v1/admin/keys/{id}` | ✅ | No |
| Set budget | `PUT /api/v1/admin/budgets` | ✅ | Yes |

### 8.4 Backend gaps

| Gap | Severity | Notes |
|---|---|---|
| 33. Tenant health composite | Inefficient | Client computes "at risk" signal from drilldown + recent activity; v0.2 server-side |
| 34. Operator notes (per-tenant) | **Blocking** for Notes tab | Need new `suite_tenant_notes` table |
| 35. Tenant activity timeline merge | Inefficient | Client merges runs + audit + activity v1; v0.2 unified endpoint |

---

## 9. WRAP / REFLECT / LINK

| Surface | Pattern |
|---|---|
| Tenant data + members + keys + audit | WRAP |
| Runs / Cost / Errors tabs (filtered) | WRAP (reuse upstream pages' composition) |
| Billing detail | REFLECT — summary inline; "Open in Stripe ↗" for deep view |
| AgentField | Drawer carries the link via drill into a run |

---

## 10. THREE DATA STATES

### EMPTY (tenant just created, no activity)

Header shows the tenant with zero KPIs but **structure intact** —
sparklines render flat, member/key counts = 1 (the creator). At-risk
section absent (nothing at risk). Each tab's content:

- Overview: KPI structure, "Make first call to see activity"
- Runs: `<EntityListTable>` empty state from Runs brief
- Cost: $0 zone 1, hierarchy hidden
- Errors / Webhook flow: empty list
- Members: list of 1
- Keys: list of N (auto-provisioned at signup)
- Audit: list of N (tenant.created + key.issued)
- Settings: editable

### MISSING (tenant not found / 404)

```
┌──────────────────────────────────────────┐
│         Tenant `abc123` not found        │
│                                          │
│  May have been deleted or never existed. │
│           [Back to tenants]              │
└──────────────────────────────────────────┘
```

### DEGRADED (drilldown endpoint failing)

Header banner: "Some tenant data is stale — last successful fetch 14m
ago." Tabs render last-known data with stale chip. Mutations disabled
until refresh.

---

## 11. LIVE DATA

### WebSocket subscriptions

- New runs for this tenant → tick header KPI + show on Overview activity
- Cost ticks → header MTD value + Overview sparkline
- Budget threshold crossed → Inbox-style banner on header

### Polled

- Drilldown endpoint refetched on tab focus + 30s background
- Per-tab data fetched on tab activation

### Animation

- KPI value change: subtle roll (200ms)
- Sparkline appends + slides
- At-risk callout slides in from top when newly applicable
- Tab switch: cross-fade content (100ms)

---

## 12. MUTATIONS — concentrated in Settings tab

| Mutation | Surface | Audit | Undo | Mobile |
|---|---|---|---|---|
| Edit name | Settings tab → form Dialog | ✅ | Yes (set back) | n/a |
| Suspend tenant | Header dropdown OR Settings | ✅ | Resume | Yes |
| Delete tenant | Settings tab → confirm Dialog with type-to-confirm | ✅ | No | No |
| Set budget | Cost tab → "Edit budget" → Dialog | ✅ | Yes | Yes |
| Cap budget at $X | Header at-risk callout → quick action | ✅ | Yes | Yes |
| Invite member | Members tab → "Invite" → Dialog | ✅ | Remove | No |
| Remove member | Members tab row action | ✅ | Re-invite | No |
| Issue key | Keys tab → "Issue" → Drawer | ✅ | Revoke | Yes |
| Rotate key | Keys tab row action | ✅ | Show prev key (limited) | No |
| Revoke key | Keys tab row action | ✅ | Re-issue | No |
| Refund (when billing wired) | Billing tab → row action | ✅ | No (refund is permanent) | Yes |
| Extend budget +N% | Inbox alert action OR header callout | ✅ | Yes | Yes |
| Add operator note | Notes tab (when shipped) | ✅ | Edit / delete | No |

Mutation density is high. Header carries the **frequent** actions
(Suspend, Set budget); rare actions live in their tabs.

---

## 13. DRILL PATHS

### From Tenant detail outward

- "View in global Runs" link in Overview Recent Runs section → ACTIVITY → Runs filtered to tenant
- "View in global Cost" → MONEY → Cost scoped to tenant
- Member row → Member detail (when shipped) or audit filtered to user
- Key row → Keys detail Drawer
- Audit row → entity (run / config change / etc.)

### Drills IN

- Already enumerated in §2 Pillar.

---

## 14. URL STATE

```
/people/tenants/<tenant_id>
  ?tab=overview|runs|cost|errors|webhooks|members|keys|audit|billing|settings
  &subTab=<per-tab sub-state>
  &drawer=<entity-type>/<id>          (when a child drawer is open)
```

Tab state + drawer state composed cleanly. Browser back works between
tabs (history push on tab change).

---

## 15. MOBILE STORY

**Read + light-mutate.** Mobile users land here from Inbox alert detail
to extend budget or suspend. The page renders as:

- Sticky header (compact form — tenant name + status pill + 2 KPIs)
- Tabs collapse to horizontal scroll
- Each tab is single-column
- Drawer drills (e.g., into a run from Runs tab) take over full screen

Allowed mobile mutations: suspend, set budget, refund (the high-stakes,
frequently-mobile ones). Other mutations require desktop.

---

## 16. MODULARITY SURFACE

No adapter pill in page header — the page is operator-owned, not
adapter-backed at top level. Per-tab pills:
- Billing tab footer: `via Stripe ▾` (when billing adapter is Stripe)
- Cost tab footer: `via LiteLLM ▾` (from Cost brief)

---

# 17. ZONE-BY-ZONE DESIGN

Stack: **shadcn/ui** + **Recharts** + **lucide-react** + **tailwindcss**.

---

## 17.1 Header (sticky)

The tenant identity card. Always visible during scroll.

```
┌──────────────────────────────────────────────────────────────────────┐
│ ┌─ TENANT ─────────────────────────────────────────────────────────┐ │
│ │  acme corporation                              [active ●]        │ │
│ │  acme-corp · 9af3c2 [📋]  · created 2026-03-14                   │ │
│ │                                                                  │ │
│ │  ⚠ At risk · budget 87% consumed                                │ │
│ │    [Extend +50%]  [Cap now]                                     │ │
│ │                                                                  │ │
│ │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌────────┐ │ │
│ │  │ COST MTD │ │ REQ 30D  │ │ MEMBERS  │ │ KEYS     │ │ STATUS │ │ │
│ │  │ $87.14   │ │ 12.4k    │ │ 4        │ │ 6 active │ │ active │ │ │
│ │  │ ▴42%     │ │ ▴12%     │ │          │ │ 2 revkd  │ │        │ │ │
│ │  │ ▁▂▃▅█▆█ │ │ ▁▂▂▃▄▆█ │ │          │ │          │ │        │ │ │
│ │  └──────────┘ └──────────┘ └──────────┘ └──────────┘ └────────┘ │ │
│ │                                                                  │ │
│ │  [Issue key]  [Set budget]  [Suspend]  [More ▾]                 │ │
│ └──────────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────────┘
```

### Component map

| Element | shadcn | Recharts | Notes |
|---|---|---|---|
| Outer container | `Card` w/ `sticky top-12 z-10 bg-background/95 backdrop-blur` | — | Sticky during scroll |
| Tenant name | `CardTitle text-2xl font-semibold` | — | Primary tier |
| Slug + id | `font-mono text-xs text-muted-foreground` | — | `<CopyButton />` on id |
| Created at | `text-xs text-muted-foreground` | — | `formatAge()` + HoverCard ISO |
| Status badge | `Badge` (semantic tone) | — | active / suspended / over-budget |
| At-risk callout | `Alert` variant w/ left border | — | Shown only when applicable |
| KPI tile | `<KeyValueTile />` from Drawer brief | mini `LineChart` | Reused primitive |
| Quick-action buttons | `Button` row | — | Suspend / Set budget / Issue key / More |

### "At risk" callout — composite signal

Shown when ANY of:
- Budget >80% consumed
- Error rate last 24h >5%
- Account expiring soon (trial / payment)
- 1+ key approaching rate limit ceiling
- Suspended

Each cause becomes a separate slim Alert. Multiple stack.

```tsx
{atRiskSignals.map(signal => (
  <Alert key={signal.id} variant="warning" className="border-l-4 py-2">
    <AlertTriangle className="h-4 w-4" />
    <AlertDescription className="ml-2 flex items-center gap-2">
      {signal.message}
      {signal.actions.map(a => (
        <Button key={a.label} size="sm" variant="outline" onClick={a.onClick}>
          {a.label}
        </Button>
      ))}
    </AlertDescription>
  </Alert>
))}
```

### KPI tile reuses Drawer's `<KeyValueTile />`

But here with the Recharts mini-`Sparkline` from Cost brief. Composition
strikes again — no new primitive.

---

## 17.2 Tabs

shadcn `<Tabs>` with URL state. Default tab: Overview.

```tsx
<Tabs value={activeTab} onValueChange={onTabChange}>
  <TabsList className="grid w-full grid-cols-8 lg:w-auto lg:inline-flex">
    <TabsTrigger value="overview">Overview</TabsTrigger>
    <TabsTrigger value="runs">Runs <Badge>{usage.requests_30d}</Badge></TabsTrigger>
    <TabsTrigger value="cost">Cost</TabsTrigger>
    <TabsTrigger value="errors">Errors {errorCount > 0 && <Badge variant="destructive">{errorCount}</Badge>}</TabsTrigger>
    <TabsTrigger value="members">Members <Badge>{members.length}</Badge></TabsTrigger>
    <TabsTrigger value="keys">Keys <Badge>{api_keys.length}</Badge></TabsTrigger>
    <TabsTrigger value="audit">Audit</TabsTrigger>
    <TabsTrigger value="settings">Settings</TabsTrigger>
  </TabsList>
  <TabsContent value="overview">...</TabsContent>
  ...
</Tabs>
```

Each tab carries a **count badge** where it helps (Runs / Errors /
Members / Keys). Operator can scan tab row without entering each.

---

## 17.3 Overview tab (default)

Composed of three vertical zones — light versions of the per-domain
pages, scoped to tenant.

```
┌──────────────────────────────────────────────────────────────────────┐
│  RECENT ACTIVITY                              [View global activity →] │
│  ──────────────────                                                  │
│  Activity feed (last 20 events for this tenant)                      │
│   • 2m ago  run completed  supportdesk.reply_plan                    │
│   • 14m ago key.rotated  acme-prod-server                            │
│   • 1h ago  budget threshold crossed (80%)                           │
│   ...                                                                │
├──────────────────────────────────────────────────────────────────────┤
│  COST                                          [Open Cost tab →]      │
│   ┌─────────────────────────────┐  ┌──────────────────────────────┐  │
│   │ Spend trend 24h sparkline   │  │ Top agents                   │  │
│   │ ▁▂▃▅█▆█▅▆█▅▆▅▆█▅▆▅█▅█▆█  │  │ supportdesk    $42.18   84%  │  │
│   │ $87.14 MTD                  │  │ refund          $5.96   12%  │  │
│   └─────────────────────────────┘  └──────────────────────────────┘  │
├──────────────────────────────────────────────────────────────────────┤
│  RECENT RUNS                                  [View runs tab →]       │
│  Last 10 runs in a mini table — same row shape as Runs page          │
└──────────────────────────────────────────────────────────────────────┘
```

### Component map

| Element | shadcn | Recharts | Notes |
|---|---|---|---|
| Section container | `Card` | — | Three Cards stacked |
| Activity feed | `<ActivityFeed>` (composition of `<ActivityEvent>` items) | — | Same primitive as Home |
| Spend sparkline | `<Sparkline>` | `LineChart` | From Cost brief |
| Top agents bars | `<SortedBars>` | `BarChart` | From Cost brief, scoped to tenant |
| Mini runs table | `<EntityListTable>` (compact mode) | — | From Runs brief, no filters |

### "View global X →" links

Each section has a link to the full page filtered to this tenant. The
tabs themselves keep the scoped data; the link takes them to global view
filtered.

---

## 17.4 Runs tab

**Composition over `<EntityListTable>`** from Runs brief, with the
tenant filter locked.

```tsx
<RunsPage
  scopedTenantId={tenantId}
  hideTenantColumn       // tenant is implicit on this page
  hideTenantFilter       // can't change it
/>
```

Same FilterBar (minus tenant chip), same table, same Drawer drill. Just
filtered.

URL state: `?tab=runs&status=failed&from=...&drawer=run/abc123`

---

## 17.5 Cost tab

**Composition over Cost zones** from Cost brief, scoped to tenant.

- Zone 1 anomaly strip (scoped)
- Zone 2 hierarchy (root = this tenant; auto-expands to Agents)
- Zone 3 slice over time (group-by defaults to agent here)
- Zone 4 budget section is prominent (edit budget inline)

URL state: `?tab=cost&range=30d&groupBy=agent`

---

## 17.6 Errors tab

Filtered Errors list (when Errors page brief drops, reuse same
composition).

For v1, a simple list reading from `/api/v1/logs?level=error,fatal&tenant=X`
with `<EntityListTable>`.

---

## 17.7 Members tab

```
┌──────────────────────────────────────────────────────────────────────┐
│  MEMBERS  4 active                              [+ Invite member]    │
│  ────────                                                            │
│  ┌──────────────────────────────────────────────────────────────┐    │
│  │ Name              Email             Role     Last active     │    │
│  │ Alice Chen        alice@acme.com    owner    2m ago    [⋮]   │    │
│  │ Bob Smith         bob@acme.com      admin    1h ago    [⋮]   │    │
│  │ ...                                                         │    │
│  └──────────────────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────────────┘
```

| Element | shadcn | Notes |
|---|---|---|
| Container | `Card` | |
| Header w/ count | `CardTitle` + Badge | |
| Invite button | `Button variant="default"` opening `Dialog` w/ form | |
| Members table | `Table` from shadcn | |
| Role pill | `Badge` (owner=primary, admin=secondary, member=outline) | |
| Last active | `formatAge()` | HoverCard ISO |
| Row kebab | `DropdownMenu` | Change role · Remove |

---

## 17.8 Keys tab

```
┌──────────────────────────────────────────────────────────────────────┐
│  API KEYS  6 active · 2 revoked                       [+ Issue key]  │
│  ──────────────                                                       │
│  ┌──────────────────────────────────────────────────────────────┐    │
│  │ ● acme-prod-server  bk_live_••••••••••8a7c  active           │    │
│  │   $87 / $100 mo · 120 rpm · last used 2s ago      [Rotate]   │    │
│  │   ████████████████████████████████████░░░  87% used         │    │
│  │                                                              │    │
│  │ ● acme-staging      bk_live_••••••••••9f3b  active           │    │
│  │   $12 / $50 mo · 60 rpm · last used 12h ago       [Rotate]   │    │
│  │   ███████████░░░░░░░░░░░░░░░░░░░░░░░░░░  24% used          │    │
│  │                                                              │    │
│  │ ○ acme-dev          bk_live_••••••••••2a4f  revoked          │    │
│  │   revoked 14d ago                                            │    │
│  └──────────────────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────────────┘
```

Each key is a Card row with:
- Status dot (active/revoked)
- Alias + masked prefix + revealable suffix (`<CopyButton />`)
- Rate limits + budget bar (`<GaugeBarRow />` from Cost brief)
- Last used timestamp
- Row actions: Rotate / Revoke / View spend

Reuses `<GaugeBarRow />` primitive. Composition strikes again.

### Issue key flow

`[+ Issue key]` → Sheet (not modal — operator may scroll to verify):

```
Alias (required)        [my-server-prod]
Monthly budget cap       [$100]
Rate limit (rpm)         [120]
Rate limit (tpm)         [50,000]
Expires                  [never ▾]
Scopes                   [☑ llm.invoke ☑ runs.read ...]

[Cancel]              [Issue]
```

On success: secret revealed ONCE in modal with explicit "Copy & I've saved it" gate.

---

## 17.9 Audit tab

Operator actions for this tenant, chronologically.

| Element | shadcn | Notes |
|---|---|---|
| Filter bar | `Select` for action type, time range | |
| Audit table | `Table` from shadcn | |
| Actor cell | text + role pill | |
| Action cell | semantic phrasing ("issued key acme-prod-server") | |
| Diff button on row | `Button variant="ghost"` opens drawer with before/after JSON | |

Composes over `<DrillDrawer type="audit" />` (extending Drawer's entity types).

---

## 17.10 Settings tab

Tenant-level configuration + destructive actions.

```
┌──────────────────────────────────────────────────────────────────────┐
│  IDENTITY                                                            │
│   Name      [acme corporation]                            [Save]     │
│   Slug      acme-corp  (read-only)                                   │
│   Metadata  [{"plan_tier": "enterprise", "ref_url": ...}]            │
├──────────────────────────────────────────────────────────────────────┤
│  TENANT STATE                                                        │
│   [Suspend tenant]   Pauses all API calls; reversible                │
│   [Reactivate]                                                       │
│   (mutually exclusive)                                                │
├──────────────────────────────────────────────────────────────────────┤
│  DANGER ZONE                              [v0.2 polished]            │
│   [Delete tenant]    Permanent. Type "acme corporation" to confirm.  │
└──────────────────────────────────────────────────────────────────────┘
```

| Element | shadcn | Notes |
|---|---|---|
| Section grouping | `Card` per group | |
| Form | `Form` + `Input` + `Button` | |
| Suspend / Reactivate | `Button` w/ confirm `Dialog` | |
| Delete | `Button variant="destructive"` w/ type-to-confirm `Dialog` | "DangerZone" pattern |

---

## 18. PRIMITIVES — mostly reuse

| Primitive | Built from / where defined | Used here |
|---|---|---|
| `<KeyValueTile />` | Drawer brief | KPI tiles in header |
| `<Sparkline />` | Cost brief | KPI tiles + Overview cost |
| `<DeltaIndicator />` | Cost brief | KPI tile deltas |
| `<AnomalyCard />` | Cost brief | At-risk callouts in header (re-skinned to Alert) |
| `<SortedBars />` | Cost brief | Top agents in Overview |
| `<GaugeBarRow />` | Cost brief | Key budget bars in Keys tab |
| `<EntityListTable />` | Runs brief | Runs / Errors tabs (filtered) |
| `<EntityRow />` | Runs brief | mini Recent Runs in Overview |
| `<DrillDrawer />` | Drawer primitive | All row drills (runs, errors, keys, audit) |
| `<ActivityEvent />` | Home brief | Activity feed |
| `<ActivityFeed />` | Home composition | Overview feed |
| `formatAge()` util | Health brief | Everywhere |

### New primitives this brief introduces

| Primitive | Justification | Reused elsewhere? |
|---|---|---|
| `<TenantHeader />` | Sticky page header w/ identity + KPIs + at-risk + actions | Only here |
| `<AtRiskAlert />` | Slim Alert variant for composite-signal warnings | Yes — could be reused on User detail (v0.2), Agent detail health warnings |
| `<DangerZoneSection />` | Card with destructive actions + type-to-confirm | Yes — Settings page in other Detail views |
| `<KeyCard />` | API key Card with status / mask / budget / rotate row | Yes — when Keys global page ships |

4 new. ~12 reused. **Highest reuse ratio of any page so far.**

---

## 19. v0.1 lessons pre-applied

These corrections (from Cost / Health / Runs reviews) bake in here:

- Friendly tenant slug, copy-on-id
- Status pill quiet when active; loud when degraded
- Header section weight: `text-2xl` name → `text-lg` section header → `text-sm` tab → `text-xs` metadata
- Empty states UI-shaped (KPI tiles render structure even at zero)
- Cost numbers in `font-mono tabular-nums`
- `formatAge()` for every timestamp
- Sparklines reuse Cost's primitive
- Mutations show toast + audit ref + undo where reversible
- Type-to-confirm on destructive (delete)
- Mobile = sticky header + tabs scroll horizontally + drawer fullscreen takeover
- No adapter pill in header (composition page, not adapter-backed)

---

## 20. OPEN QUESTIONS & TODOs

### Deferred to v0.2

- **Notes tab** — requires `suite_tenant_notes` table (Gap 34)
- **Tenant health score** — composite metric server-side (Gap 33)
- **Compare two tenants side-by-side**
- **Cohort placement** (segment / signup batch)
- **Member detail page** (v1 just shows in list)
- **Refund history** (when Billing wired richer)

### Open for v1 design

1. **Tab badge counts** — only show on Runs / Errors / Members / Keys?
   Don't show on Overview / Cost / Audit / Settings. Recommend yes.

2. **Members table density** — compact rows or expanded with avatar +
   email? Recommend expanded.

3. **Header always sticky** — or release after scrolling past
   threshold? Recommend always sticky during scroll.

4. **At-risk callout positioning** — between header tiles and tab row,
   OR above the KPI strip? Recommend between (so KPIs always visible).

5. **Issue key flow — Sheet vs Dialog?** Sheet is wider (good for token
   reveal). Recommend Sheet.

6. **Delete tenant — what's the cascade summary shown?** Show count of
   keys / members / runs that will be affected before confirm.

### Backend gaps (in `required-backend-gaps.md`)

- Gap 33 — Tenant health composite (Inefficient; client v1)
- Gap 34 — Operator notes table (Blocking for Notes tab; defer)
- Gap 35 — Tenant activity timeline unified (Inefficient; client merge)

---

## v1 SHIPS / v0.2 DEFERS

```
SHIPS in v1:
  ✓ Sticky header with KPI strip + at-risk callouts + actions
  ✓ 8 tabs: Overview / Runs / Cost / Errors / Members / Keys / Audit / Settings
  ✓ Drilldown endpoint powers header + Overview + Members + Keys
  ✓ Runs / Errors / Cost tabs reuse upstream page composition
  ✓ Members tab: list + Invite / Remove
  ✓ Keys tab: list as Cards with rate-limit gauges + Issue / Rotate / Revoke
  ✓ Audit tab: chronological list + diff drawer
  ✓ Settings tab: identity + state + danger zone
  ✓ All mutations audited
  ✓ Mobile: header sticky + scrollable tabs + fullscreen drawers

DEFERS to v0.2:
  ✗ Notes tab (Gap 34)
  ✗ Tenant health composite (Gap 33 — v1 computes client-side)
  ✗ Tenant activity unified timeline (Gap 35 — v1 client merges)
  ✗ Compare two tenants
  ✗ Cohort placement
  ✗ Member detail page
  ✗ Refund history table
```

---

## Grooming checklist

```
☑ Purpose is one sentence
☑ Pillar named (PEOPLE)
☑ Primary read identified (sticky header)
☑ Data sources mapped + verified — drilldown is rich; tabs reuse upstream
☑ Each data source declared WRAP / REFLECT / LINK
☑ OSS links surfaced (Stripe in Billing tab; LiteLLM in Cost tab — inherited)
☑ Three data states specified
☑ Live data behavior explicit
☑ Mutations listed with audit + undo + mobile flag
☑ Drill paths in + out documented
☑ URL state declared with composed tab + drawer
☑ Mobile story declared
☑ Modularity surface decided (no header pill; tab-specific in tabs)
☑ Primitives reused: KeyValueTile, Sparkline, DeltaIndicator, AnomalyCard, SortedBars, GaugeBarRow, EntityListTable, EntityRow, DrillDrawer, ActivityEvent, ActivityFeed, formatAge — composition over previous briefs
☑ New primitives justified (TenantHeader, AtRiskAlert, DangerZoneSection, KeyCard)
☑ v0.1 critique lessons pre-applied
☑ Open questions documented
☑ Backend gaps surfaced (33, 34, 35)
☑ v1/v0.2 split called out
```

All checked.

---

_Last updated: 2026-06-17. Next: Errors (Drawer + EntityListTable composition)._

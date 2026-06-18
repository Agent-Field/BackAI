# Primitive Brief — Drawer (DrillDrawer)

> The universal "drill into a list row" primitive. Reused across 7+ surfaces.
> One shape, many entity types.
> Groomed against `development/ux/page-design-framework.md`.

This is a **primitive brief**, not a page brief. The Drawer is reused by
multiple pages. Designing it once unblocks Runs / Errors / Webhook flow /
Sandbox runs / Queue / Approvals / Inbound webhooks / Notifications.

---

## 1. PURPOSE

When the operator clicks a row in any list, slide in a right-side panel
showing that row's detail — without losing the list context behind.

Confidence questions answered:
- *"What does this specific entity look like?"* — every drill
- *"Was this the bad run?"* — Journeys 5, 7
- *"Should I approve this?"* — Journey 6 (mobile drawer)
- *"What happened during this incident?"* — Journey 8

---

## 2. WHERE USED

| Entity type | Page | Endpoint(s) |
|---|---|---|
| **Run** | Operate → Runs | `GET /api/v1/executions/{id}` + `/api/v1/runs/{id}/events` + `/api/v1/runs/{id}/agentfield` |
| **Error** | Operate → Errors | `GET /api/v1/logs?correlation_id=X` filtered |
| **Webhook delivery** | Operate → Webhook flow | `GET /api/v1/webhooks/deliveries/{id}` |
| **Sandbox run** | Operate → Sandbox runs | `GET /api/v1/sandbox/runs/{id}` + `/logs` (live tail) |
| **Job** | Operate → Queue | `GET /api/v1/jobs/{id}` |
| **Approval** | Inbox + Tenant detail | `GET /api/v1/approvals/{id}` + `POST /decide` |
| **Notification** | Operate → Notifications | `GET /api/v1/notifications/{id}` |
| **Inbound webhook** | Operate → Inbound webhooks (v0.2) | ❌ no endpoint v1 — see Gap 25 |

8 entity types. All but Inbound use existing endpoints.

---

## 3. UNIVERSAL SHAPE

Every drawer has these sections in this order:

```
┌────────────────────────────────────────────────────────────┐
│ HEADER                                                  ✕  │
│   <type pill>  <short id>  <copy id>                       │
├────────────────────────────────────────────────────────────┤
│ SUMMARY LINE                                                │
│   <human-readable title>                                    │
│   ● <status>  ·  <duration>  ·  <cost>  ·  <when>          │
├────────────────────────────────────────────────────────────┤
│ QUICK FACTS  (4-6 key-value tiles in grid)                  │
│   ┌──────────────┐ ┌──────────────┐ ┌──────────────┐       │
│   │ TENANT       │ │ MODEL        │ │ TOKENS       │       │
│   │ acme         │ │ claude-sonn… │ │ 1.2k · 412   │       │
│   └──────────────┘ └──────────────┘ └──────────────┘       │
├────────────────────────────────────────────────────────────┤
│ TABS  (entity-specific)                                      │
│   [Input] [Output] [Steps] [Tools] [Errors] [Trace] [Audit]│
├────────────────────────────────────────────────────────────┤
│ TAB CONTENT  (scrollable)                                    │
│   (JSON viewer, table, log stream, decision form, etc.)     │
│                                                              │
├────────────────────────────────────────────────────────────┤
│ RELATED                                                      │
│   Tenant: acme →                                            │
│   Agent: supportdesk →                                       │
│   Parent run: xyz789 →                                       │
├────────────────────────────────────────────────────────────┤
│ ACTIONS FOOTER                                               │
│   [Primary action]  [Secondary]  [Open external ↗]          │
└────────────────────────────────────────────────────────────┘
```

Six universal sections. Per-entity-type config decides the contents of
each.

---

## 4. PER-ENTITY CONFIG TABLE

Each entity type fills the universal sections with its own contents:

### Run drawer (Operate → Runs)

| Section | Content |
|---|---|
| Header | `RUN` pill + short id (last 8 chars) + copy-full-id |
| Summary | `agent.reasoner` title · status dot · duration · cost · "Nm ago" |
| Quick facts | TENANT · MODEL · TOKENS in/out · CACHE hit/miss · STARTED · TRIGGER |
| Tabs | Input · Output · Steps · Tools · Errors · Audit |
| Related | Tenant → Agent → Parent run (if sub-run) → Trace in AgentField |
| Actions | `[Re-run]` (copies input) · `[Cancel]` (if running) · `[Open in AgentField ↗]` |

### Error drawer

| Section | Content |
|---|---|
| Header | `ERROR` pill + short error id + copy |
| Summary | Error message (truncated 80) · severity · count if recurring · "Nm ago" |
| Quick facts | SOURCE (runs/jobs/handlers/webhooks) · TENANT · AGENT · LAST SEEN · COUNT |
| Tabs | Stack · Sample run · Pattern matches · Audit |
| Related | Run → Tenant → Suggested fix (if pattern recognized) |
| Actions | `[Mute]` · `[Resolve]` · `[Open run ↗]` |

### Webhook delivery drawer

| Section | Content |
|---|---|
| Header | `WEBHOOK` pill + msg id + copy |
| Summary | Event type · endpoint · status · attempts · "Nm ago" |
| Quick facts | EVENT TYPE · ENDPOINT · TENANT · ATTEMPTS · LAST RESPONSE · NEXT RETRY |
| Tabs | Payload · Headers · Response · Attempts history · Audit |
| Related | Tenant → Event source (run/job) → Subscriber config |
| Actions | `[Replay]` · `[Open in Svix ↗]` |

### Sandbox run drawer

| Section | Content |
|---|---|
| Header | `SANDBOX` pill + run id + copy |
| Summary | Image · command (truncated) · status · duration · cost · "Nm ago" |
| Quick facts | IMAGE · TENANT · TRIGGERED BY · EXIT CODE · CPU-SEC · COST |
| Tabs | Stdout · Stderr · Command · Env · Audit |
| Related | Parent agent run (if any) → Tenant |
| Actions | `[Cancel]` (if running) · `[Re-run with these inputs]` (jumps to BUILD → Sandboxes → Playground) |

Special: live-tail stdout/stderr while running. WebSocket subscribed.

### Job drawer

| Section | Content |
|---|---|
| Header | `JOB` pill + job id + copy |
| Summary | Kind · status · attempts · queued at · "Nm ago" |
| Quick facts | KIND · TENANT · ATTEMPTS · QUEUED · STARTED · DURATION |
| Tabs | Payload · Last error · Attempts history · Audit |
| Related | Related run / handler → Tenant |
| Actions | `[Retry]` · `[Send to DLQ]` · `[Cancel]` (if queued) |

### Approval drawer

| Section | Content |
|---|---|
| Header | `APPROVAL` pill + id + copy |
| Summary | Kind · status · requested by · "Nm ago" |
| Quick facts | KIND · TENANT · REQUESTED BY · STATUS · BLOCKED ENTITY · AWAITING SINCE |
| Tabs | Payload · Decision form · History of similar · Audit |
| Related | Blocked run/job → Tenant → Requesting user |
| Actions | `[Approve]` · `[Deny]` · `[Cancel]` · `[Delegate]` |

Special: Decision form is its own tab, not just an action. Includes
optional note field.

### Notification drawer

| Section | Content |
|---|---|
| Header | `NOTIFY` pill + id + copy |
| Summary | Channel · category · status · "Nm ago" |
| Quick facts | CHANNEL · RECIPIENT · CATEGORY · STATUS · ATTEMPTS · LATENCY |
| Tabs | Subject + body · Template · Provider response · Audit |
| Related | Source event (run/budget/etc.) → Tenant |
| Actions | `[Resend]` · `[Open recipient profile ↗]` |

### Inbound webhook drawer (v0.2 — Gap 25)

Deferred. v1 surfaces inbound via Logs filter.

---

## 5. DATA SOURCES — verified per entity type

> **Audit status**: 7 of 8 entity types have endpoints. Inbound webhook
> deferred. Field-level shapes vary; brief documents each.

### 5.1 Endpoint mapping

| Entity | Detail endpoint | Live channel | Notes |
|---|---|---|---|
| Run | `GET /api/v1/executions/{id}` + `GET /api/v1/runs/{id}/events` | `WS /api/v1/realtime/runs?run_id=X` | Live tail of streaming runs |
| Error | `GET /api/v1/logs?correlation_id=X` | — | Composite — error is a derived view of logs |
| Webhook delivery | `GET /api/v1/webhooks/deliveries/{id}` | — | |
| Sandbox run | `GET /api/v1/sandbox/runs/{id}` + `GET /sandbox/runs/{id}/logs` | log endpoint streams | Live tail while running |
| Job | `GET /api/v1/jobs/{id}` | — | |
| Approval | `GET /api/v1/approvals/{id}` + `POST /api/v1/approvals/{id}/decide` | — | Decision is mutation |
| Notification | `GET /api/v1/notifications/{id}` | — | |
| Inbound webhook | ❌ no GET-by-id endpoint | — | **Gap 25** — defer or expose |

### 5.2 Backend gaps

| Gap | Severity | Notes |
|---|---|---|
| 25. Inbound webhook detail GET | **Blocking** for inbound drawer | Defer v1; Logs filter substitute |
| 26. Error drawer composite endpoint | Inefficient | Currently client-side merge of logs + run id; could be `/admin/errors/{id}` |

---

## 6. THREE DATA STATES

### LOADING

While fetching:
```
HEADER       [skeleton]
SUMMARY      [skeleton — 2 lines]
QUICK FACTS  [skeleton grid — 4 tiles]
TABS         [skeleton — 4 tab placeholders]
TAB CONTENT  [skeleton — 8 line blocks]
```

Use shadcn `<Skeleton>` everywhere. Skeleton tiles match real layout shape
so no jump on load.

### NOT FOUND

```
┌────────────────────────────────────────────┐
│ ✕                                           │
│                                             │
│         Run abc123 not found                │
│                                             │
│   It may have been purged or never existed. │
│                                             │
│         [Back to list]                      │
└────────────────────────────────────────────┘
```

### FETCH ERROR

```
┌────────────────────────────────────────────┐
│ ✕                                           │
│                                             │
│  Couldn't load this run                    │
│  <error message>                            │
│                                             │
│  [Retry]  [Copy diagnostic info]            │
└────────────────────────────────────────────┘
```

---

## 7. URL STATE

Drawer is URL-addressable so deep links + reload + browser back work.

### URL convention

```
/operate/runs?drawer=run/abc123
/operate/errors?drawer=error/xyz789
/operate/webhook-flow?drawer=webhook_delivery/...
```

Pattern: `?drawer=<type>/<id>` — single query param holds both type and
id so multiple list pages can share the same parser.

Why one param: avoid `?run=abc123&job=xyz789` ambiguity. The drawer is
mutually exclusive.

### Browser back

- Open drawer → push history state
- Close drawer (ESC / ✕ / backdrop) → pop history state
- Browser back arrow returns to list state, drawer closed

### Reload

`/operate/runs?drawer=run/abc123` reloads with drawer open at that run.
No flash of empty drawer first.

---

## 8. KEYBOARD

Drawer is heavy keyboard-driven. Operators triage lists fast.

### Universal shortcuts

| Key | Action |
|---|---|
| `Esc` | Close drawer |
| `←` / `→` or `j` / `k` | Prev / next item in underlying list (drawer updates to new entity) |
| `Cmd+C` (when inside JSON viewer) | Copy current selection |
| `Cmd+L` | Copy entity URL |
| `Cmd+\` | Toggle drawer width (default ↔ wide) |
| `?` | Show keyboard hints overlay |

Arrow navigation is the killer DX feature for triage. Operator scans 50
failed runs by holding `→`.

### Type-specific shortcuts

| Entity | Key | Action |
|---|---|---|
| Run | `R` | Re-run |
| Sandbox | `X` | Cancel |
| Approval | `Y` / `A` | Approve |
| Approval | `N` / `D` | Deny |
| Error | `M` | Mute |
| Error | `R` | Mark resolved |
| Webhook delivery | `P` | Replay |
| Job | `R` | Retry |

Show hints at bottom of drawer footer (small, muted).

---

## 9. MOBILE BEHAVIOR

Drawer on mobile = **full-screen takeover**.

- Right-side slide-in becomes bottom-up slide
- Width = full viewport
- Close via swipe-down OR ✕ button
- Tabs stay (horizontal scroll if many)
- Action footer pinned at bottom

For Approval drawer specifically: mobile approval IS the
`/inbox/<alert_id>` page from Journey 6. Same primitive renders. Single
purpose, decision-shaped.

---

## 10. ANIMATION

| Event | Animation |
|---|---|
| Open | Slide-in from right, 250ms ease-out; backdrop fade-in 200ms |
| Close | Slide-out to right, 200ms ease-in; backdrop fade-out 150ms |
| Next via arrow key | Slight cross-fade content (150ms) without re-sliding the panel |
| Tab change | Tab content cross-fade (100ms) |
| Status change live | Status dot pulses; summary line value rolls subtly |
| New event in live tab (Steps/Stdout) | New row slides in at top with brief highlight |
| Action loading | Button shows spinner inline; action footer dims |

---

## 11. SUB-PRIMITIVES INTRODUCED

Building blocks inside the Drawer (each is its own composable):

| Sub-primitive | Built from | Reused beyond drawer? |
|---|---|---|
| `<DrillDrawer />` | shadcn `Sheet` + composition | The wrapper; only here |
| `<DrawerHeader />` | `Badge` (type) + mono short id + `Button` ghost (close) | Only drawer |
| `<DrawerSummaryLine />` | Status dot + title + meta separators | Only drawer |
| `<DrawerQuickFacts />` | Grid of `<KeyValueTile />` | grid pattern reusable on tenant detail header |
| `<KeyValueTile />` | Card subtle + label + value | reusable globally |
| `<DrawerTabs />` | shadcn `Tabs` | wrapped with URL state |
| `<DrawerJSONViewer />` | custom (collapsible JSON tree with copy + search) | reusable on API explorer |
| `<DrawerLiveTail />` | scrollable mono pre + WebSocket | reusable on Logs page tail mode |
| `<DrawerRelated />` | List of Link rows | only drawer |
| `<DrawerActions />` | sticky footer with `Button` row | only drawer |
| `<DrawerKeyboardHints />` | small dismissable bar with `kbd` chips | reusable on other keyboard surfaces |

11 sub-primitives. Most are drawer-only; `<KeyValueTile />` and
`<DrawerJSONViewer />` reuse elsewhere.

---

## 12. COMPONENT SPEC

### Top-level shell

```tsx
<DrillDrawer
  type="run"
  id="abc123"
  open={isOpen}
  onClose={() => router.back()}
  onPrev={() => navigateTo(prevItem)}
  onNext={() => navigateTo(nextItem)}
  hasPrev={hasPrev}
  hasNext={hasNext}
/>
```

### Internal structure

```tsx
<Sheet open={open} onOpenChange={onClose}>
  <SheetContent side="right" className="w-[640px] sm:max-w-[640px] p-0">
    <DrawerHeader type={type} id={id} onClose={onClose} />
    <DrawerSummaryLine entity={entity} />
    <DrawerQuickFacts facts={facts} />
    <DrawerTabs tabs={tabs} defaultTab={defaultTab} urlState />
    <DrawerRelated links={related} />
    <DrawerActions actions={actions} />
    <DrawerKeyboardHints hints={typeHints} />
  </SheetContent>
</Sheet>
```

### Type-discriminated content

```tsx
const drawerConfig: Record<EntityType, DrawerConfig<any>> = {
  run: {
    fetcher: (id) => api.executions.get(id),
    pillLabel: "RUN",
    summaryLine: (e) => `${e.agent}.${e.reasoner}`,
    quickFacts: (e) => [
      { label: "TENANT",  value: e.tenant_name },
      { label: "MODEL",   value: formatModelName(e.model) },
      { label: "TOKENS",  value: `${e.tokens_in} · ${e.tokens_out}` },
      { label: "CACHE",   value: e.cache_hit ? "hit" : "miss" },
      { label: "STARTED", value: formatAge(e.started_at) },
      { label: "TRIGGER", value: e.trigger_source },
    ],
    tabs: [
      { id: "input",  label: "Input",  render: (e) => <JSONViewer data={e.input} /> },
      { id: "output", label: "Output", render: (e) => <JSONViewer data={e.output} /> },
      { id: "steps",  label: "Steps",  render: (e) => <ReasonerPath path={e.reasoner_path} /> },
      { id: "tools",  label: "Tools",  render: (e) => <ToolCallList calls={e.tool_calls} /> },
      { id: "errors", label: "Errors", render: (e) => <ErrorBlock error={e.error} />, hideWhen: (e) => !e.error },
      { id: "audit",  label: "Audit",  render: (e) => <AuditList entries={e.audit_entries} /> },
    ],
    related: (e) => [
      { label: "Tenant",      href: `/people/tenants/${e.tenant_id}`,   text: e.tenant_name },
      { label: "Agent",       href: `/build/agents/${e.agent}`,         text: e.agent },
      { label: "Parent run",  href: `/operate/runs?drawer=run/${e.parent_run_id}`, text: e.parent_run_id, hideWhen: !e.parent_run_id },
      { label: "Trace in AgentField", href: `http://agentfield:8081/runs/${e.trace_id}`, external: true },
    ],
    actions: (e) => [
      { label: "Re-run", primary: true, onClick: () => rerun(e), shortcut: "R" },
      { label: "Cancel", onClick: () => cancel(e), shortcut: "X", hideWhen: e.status !== "running" },
      { label: "Open in AgentField", external: true, href: `http://agentfield:8081/runs/${e.trace_id}` },
    ],
    keyboardHints: [
      { key: "R", action: "Re-run" },
      { key: "X", action: "Cancel" },
    ],
  },
  error: { /* ... */ },
  webhook_delivery: { /* ... */ },
  sandbox_run: { /* ... */ },
  job: { /* ... */ },
  approval: { /* ... */ },
  notification: { /* ... */ },
}
```

One config map. Adding a new entity type = adding one entry.

### KeyValueTile primitive

```tsx
<KeyValueTile
  label="TENANT"
  value="acme"
  href={`/people/tenants/${tenantId}`}      // optional — makes value clickable
  mono                                       // optional — monospace value
  hideWhenEmpty                              // optional — collapse if no value
/>
```

Renders:
```
┌──────────────────────┐
│ TENANT               │   ← text-xs uppercase tracking-wider text-muted-foreground
│ acme                 │   ← text-sm font-medium (font-mono if mono)
└──────────────────────┘
```

Used in drawer + tenant detail header + future entity pages.

### JSON viewer

```tsx
<DrawerJSONViewer
  data={obj}
  defaultExpanded={1}     // first level open
  collapsedNodes={["metadata"]}  // optionally collapse known noisy keys
  searchable                       // ⌘F triggers in-viewer search
  copyable                         // Cmd+C copies node
/>
```

Built from custom recursive component. Each node has chevron, key, value,
optional copy button. Strings, numbers, booleans, null styled by type
(monospace, color-coded subtly).

For large JSON (>1MB), use virtualization. Most cost-event payloads are small.

### Live tail

```tsx
<DrawerLiveTail
  source={`/api/v1/sandbox/runs/${id}/logs`}
  initialLines={lastN}
  autoScroll
  onPause={() => ...}
  filterRegex={regex}
/>
```

WebSocket connects, streams lines. Pre-formatted mono. Auto-scrolls
unless user scrolls up (then auto-pauses with "New lines below ↓" chip).

### DrawerKeyboardHints

```tsx
<DrawerKeyboardHints
  hints={[
    { key: "R", action: "Re-run" },
    { key: "←/→", action: "Navigate" },
    { key: "Esc", action: "Close" },
  ]}
  dismissible
/>
```

Small bar at bottom of drawer with `<kbd>` chips. Dismissible per-session
(localStorage). Visible by default to help operators learn keys.

---

## 13. WIDTH + STACKING

### Default width

640px on desktop. 50% of viewport when viewport > 1280px (gives operator
room for big JSON).

### Wide mode

`Cmd+\` toggles to 880px or 60% of viewport. For inspecting large
payloads / long stdout. Persists per session.

### Stacking (nested drawers)

When drawer is open and operator clicks a Related link that's also a
drawer-eligible entity (e.g., parent run from a run drawer):
- v1: replace current drawer content with new entity (back arrow returns)
- v0.2: nested stack (new drawer slides over current, back arrow pops)

Recommendation: **v1 replaces**. Stack adds complexity without much
gain at v1 scale.

---

## 14. CROSS-CUTTING CONCERNS

### Audit on drawer interactions

- Drawer open → no audit (read)
- Action triggered → audited per the action's normal endpoint
- Toast confirms with audit ref + undo affordance per framework

### Tenant scope respect

If top-bar tenant switcher is set, drawer drill respects it. Opening a
run drawer when tenant scope is "acme" only works if the run belongs to
acme; otherwise 404 with helpful message.

### Time format

`formatAge(seconds)` from Health Zone D rules apply: "now" / "Ns ago" /
"Nm ago" / "Nh ago" / "Mar 14". HoverCard reveals full ISO timestamp.

### Status indicators

Status dots + text follow framework §17.6 rules — color tokens, quiet
when healthy, loud when problem.

### Copy ID button

Every header has `<CopyButton value={fullId} />` next to the short id.
Single click copies, toast confirms "Copied".

---

## 15. OPEN QUESTIONS

1. **JSON viewer size threshold** — at what size do we virtualize? My
   gut: >100 nodes or >50KB. Confirm with realistic payload sizes.

2. **Live-tail buffer size** — how many lines kept in memory before
   trimming? Recommendation: 5000 lines circular buffer; older fades.

3. **Stacked drawers in v1 or v0.2?** — recommended v0.2 (simpler v1)

4. **Keyboard hints default visibility** — on by default, dismissable.
   First-time operators benefit; power users dismiss.

5. **Mobile drawer animation** — slide from bottom or fade in? Recommend
   slide-up (matches native iOS / Android conventions).

6. **Re-fetching on tab change** — should each tab refetch its content,
   or fetch everything on open? Recommendation: fetch everything on open
   (drawer payloads are usually small); refetch on tab focus only for
   live tabs (Steps, Stdout).

7. **Drawer minimum height on mobile** — full screen always, or expand
   from bottom-sheet? Recommendation: full screen on mobile for max
   readability.

---

## 16. BACKEND GAPS

| Gap | Severity | Notes |
|---|---|---|
| 25. Inbound webhook detail GET endpoint | Blocking for inbound drawer | Defer to v0.2; v1 uses Logs filter |
| 26. Error drawer composite endpoint | Inefficient | Client-side merge of logs + run id v1; `/admin/errors/{id}` v0.2 |

---

## 17. v1 SHIPS / v0.2 DEFERS

```
SHIPS in v1:
  ✓ Universal shell (Sheet wrapper + 6 sections)
  ✓ 6 entity types: run, webhook_delivery, sandbox_run, job, approval, notification
  ✓ URL state (?drawer=type/id)
  ✓ Keyboard navigation (Esc, arrows, type-specific keys)
  ✓ Sub-primitives: header, summary, quick facts grid, tabs, related, actions
  ✓ KeyValueTile (also used outside drawer)
  ✓ JSON viewer (recursive, copyable, searchable)
  ✓ Live tail (WebSocket) for Sandbox stdout/stderr
  ✓ Mobile full-screen
  ✓ Skeleton loading + not-found + error states
  ✓ Animation: slide-in, content cross-fade between siblings

DEFERS to v0.2:
  ✗ Inbound webhook drawer (Gap 25)
  ✗ Error drawer composite endpoint (Gap 26)
  ✗ Stacked / nested drawers
  ✗ Drawer-pinned mini-view (open multiple drawers side-by-side)
  ✗ Virtualized JSON viewer for very large payloads
```

---

## 18. UNBLOCKS DOWNSTREAM PAGES

With Drawer designed, these page briefs become composition exercises
rather than primitive-design exercises:

- Runs (Operate) — list + drawer
- Errors (Operate) — list + drawer
- Webhook flow (Operate) — list + drawer
- Sandbox runs (Operate) — list + drawer
- Queue (Operate) — list + drawer for jobs
- Notifications (Operate) — list + drawer
- Approvals embedded in Inbox + Tenant detail — drawer reuses
- Logs (Operate) — drawer for individual log entry with related entity drilldown

Each page brief should reference this Drawer primitive rather than
re-spec the shell.

---

## Grooming checklist

```
☑ Purpose is one sentence
☑ Where used: 8 entity types enumerated
☑ Universal shape locked (6 sections)
☑ Per-entity config table (header / summary / facts / tabs / actions per type)
☑ Data sources audited per entity type
☑ Backend gaps surfaced (25, 26)
☑ Three states (loading / not found / error) specified
☑ URL state convention locked
☑ Keyboard shortcuts documented (universal + type-specific)
☑ Mobile behavior declared (full-screen takeover)
☑ Animation conventions explicit
☑ 11 sub-primitives identified, justified, library mapping
☑ Component spec with TypeScript types
☑ Cross-cutting concerns (audit, tenant scope, time format, status, copy)
☑ Open questions documented
☑ v1 / v0.2 split called out
```

All checked.

---

_Last updated: 2026-06-17. Next: Runs page (composition over Drawer)._

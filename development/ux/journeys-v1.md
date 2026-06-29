# BackAI Admin — User Journey Grooming (v1)

> Fresh first-principles grooming. Not anchored to the current admin spec.
> Output: concrete UX/UI organization decisions for the admin dashboard.
> Audience: product, design, frontend.

---

## What this doc does

Walks 8 canonical operator journeys through a fixed template, then synthesizes
**recurring surfaces, recurring primitives, and IA refinements** from them.
The output is the design brief for re-shaping the admin around what operators
actually do — not what the backend happens to expose.

Companion docs (still authoritative for product spec / implementation):

- `development/ui-plan-v1.md` — current admin product spec
- `development/admin-design-patterns-v1.md` — visual + interaction contract
- `development/execution-blocks-v1.md` — backend roadmap (Blocks 0-9)
- `docs/dashboard/spec-v1.md` — implementation spec

This doc precedes them — it's the journey-down design source.

---

## Operator profile (locked)

- **Solo or 2-3 person team.** Wears every hat.
- **AI-augmented coder.** Claude Code or Cursor open during dev. Operator is
  more reviewer / orchestrator than writer.
- **Ships products, not platforms.** Goal: AI SaaS live, not platform-engineer.
- **Terminal + browser fluent.** CLI is comfort. Skims docs, runs snippets.
- **Watches dashboards reactively.** Opens admin when something pings.
- **Vocal.** Tweets wins and losses both.
- **Trust hungry.** Self-hosts because they want VISIBILITY.

Deprioritized in v1: multi-operator (teams + roles), "see customer-app as
tenant" iframe, full guided first-run demo (designed separately).

---

## Locked design principles

1. **Admin = confidence loop.** Two metrics dominate: time-to-assess-confidence
   (target <3 sec) and time-to-restore-confidence (target <60 sec for common
   mutations).
2. **Volume is a credibility signal.** Pages stay dense. Empty states look
   alive. Sidebar fully expanded. Cmd+K is the universal de-clutterer.
3. **Programmable equivalence with Claude Code.** Every admin action has a
   CLI / API twin. The operator's AI partner can drive admin too.
4. **MVP-honest.** Show only what real endpoints can serve. Where data isn't
   there, the OSS service has its own UI we link to.
5. **Modularity quiet.** Adapter pill is the only visible signal. No OSS
   ribbons. No marketplace UI.
6. **Operator dignity.** Assume competence. Don't gate behind tutorials. Be
   terse.

---

## Journey grooming template

Every journey below is walked through this fixed shape so we can extract
patterns:

```
Journey name
  Persona snapshot       (who, when, what they're doing)
  Trigger                (what made them open admin)
  Time budget            (how long this should take)
  Mental state           (what they're thinking + fearing)
  Path                   (the surfaces they traverse)
  Mutations              (what they might do)
  Confidence signal      (what makes them satisfied)
  Exit                   (left feeling X)
  UX implications        (what this requires of interactions)
  UI organization        (what this requires of structure)
```

---

## The 8 canonical journeys

Each journey is a distinct mode the admin must serve. Together they cover
~95% of operator-admin time. Listed roughly in operator-life order.

| # | Journey | Trigger | Mode | Time budget |
|---|---|---|---|---|
| 1 | First open of fresh fork | Cold open Day 0 | First-run | 60-90 sec |
| 2 | Daily 3-sec health glance | Cold open any day | Status check | 3-10 sec |
| 3 | Verify a Claude-Code change | After AI edit | Dev | 30-90 sec |
| 4 | Playground iteration | "Test my agent" | Dev | 5-15 min |
| 5 | Customer-reported issue triage | Email/Slack from customer | Customer-care | 3-5 min |
| 6 | Push alert: budget cap looming | Mobile push notif | Alert-response | 30-60 sec |
| 7 | Cost-spike investigation | Spotted on Cost page | Detective | 5-15 min |
| 8 | Provider outage / incident | Multi-alert burst | Incident | 15-30 min |

---

### Journey 1 — First open of fresh fork

**Persona snapshot**: Sara just ran `npx create-backai legal-saas && docker compose up`. She's never seen the admin before. Tab is open at localhost.

**Trigger**: Cold open Day 0, minute 0.

**Time budget**: 60-90 seconds to "I trust this."

**Mental state**: *"Is this real? Did it install right? What does it DO? Is this screenshot-worthy?"*
**Fears**: empty / broken / half-built / toy.

**Path**:
- Lands on Home
- Eyes go to KPI strip — *number* of tiles registers, even if values are zero
- Activity feed shows realistic seeded events (since demo mode is on by default)
- Sees Your-Stack widget with adapters named
- Reveals dev tenant key
- Copies try-it snippet
- Clicks 2-3 sidebar items just to look — every page shows UI-shaped content
- Hits Cmd+K out of curiosity — sees breadth of jump targets

**Mutations**: key reveal only.

**Confidence signal**: *"This thing has way more in it than I expected."*

**Exit**: tweets a screenshot. Comes back to actually use it.

**UX implications**:
- KPI tiles render their structure (label, unit, sparkline frame) even at zero
- Activity feed seeded by `demo_mode` on install — turn-off-able in Setup
- Every page renders UI-shaped empty state, not "no data yet" text
- Cmd+K shows ~30 jump targets to communicate scope
- Welcome block is prominent BUT compresses on first dismissal

**UI organization**:
- **`demo_mode` is a first-class platform feature**, default-on for fresh forks
- Empty states are designed as UI-shaped frames, not text blocks
- Sidebar is fully expanded by default (no progressive disclosure)
- Welcome block has a compact mode for post-first-visit
- Cmd+K trigger is visible (not just keyboard-only) on Home

---

### Journey 2 — Daily 3-sec health glance

**Persona snapshot**: Sara, day 14, has 20 customers. Mid-morning. Opens admin tab.

**Trigger**: Cold open, habitual midday glance.

**Time budget**: 3-10 sec.

**Mental state**: *"Anything fucked?"*
**Fears**: missed an outage, someone's burning money I haven't priced.

**Path**:
- Hit Home
- KPI strip scan — anything red? any trend wrong direction?
- Activity feed scan — anything notable?
- Backing services row — all green?
- Leave if all green
- Drill if anything yellow / red

**Mutations**: none usually.

**Confidence signal**: *"All green. I'm good."*

**Exit**: closes tab, back to building.

**UX implications**:
- Home must be readable in 3 sec
- Color = signal: red = act now, yellow = watch, green = chill, gray = no data
- KPIs need "delta vs yesterday" inline so trend is visible without click
- Activity feed should bubble severity to top (not strict chronological)
- Backing services strip persistent (always-visible health badge per OSS)

**UI organization**:
- Home is the anchor; rest of admin orbits it
- KPI tiles have built-in delta + sparkline (not separate views)
- Activity feed has severity-aware ordering (not pure time)
- Backing services strip is visible site-wide (footer or persistent rail), not just on Home

---

### Journey 3 — Verify a Claude-Code change

**Persona snapshot**: Sara, day 5, just told Claude "add a refund evidence reasoner to the QA agent." Claude wrote code, container restarted.

**Trigger**: Tab switch from editor → admin.

**Time budget**: 30-90 sec to confirm change is real.

**Mental state**: *"Did Claude actually do it? Is it real at runtime?"*
**Fears**: Claude hallucinated, code lints but doesn't load, change doesn't match intent.

**Path**:
- Cmd+K → "qa agent" → jumps to Build → Agents → qa → Detail
- Looks at reasoner list — sees new `refund_evidence` reasoner
- Clicks it → schema is right
- Notices "loaded at 14:32" badge (live indicator that THIS version is live)
- Opens Playground tab
- Runs a test input that exercises refund_evidence
- Sees reasoner path includes the new step
- Sees cost / latency / output
- Closes admin, back to Claude to keep iterating

**Mutations**: playground run.

**Confidence signal**: *"New reasoner is there, schema is right, test passed."*

**Exit**: back to Claude with confidence the change took.

**UX implications**:
- "Loaded at" or "version SHA" badge on agent so operator knows runtime is fresh
- Reasoner list updates live without manual refresh after container restart
- Playground accepts input fast, streams output
- Per-step cost visible during stream (operator is cost-aware during dev)

**UI organization**:
- Build → Agents → Detail is the verify-what-I-built surface
- Playground is a real tab on Agent detail (not modal, not sub-route)
- "Last reload" / version indicator everywhere agent definitions are rendered
- Activity feed surfaces "agent X reloaded at T" event for cross-check
- Diff-aware UX: "you had 9 reasoners last load, now 10" callout

---

### Journey 4 — Playground iteration

**Persona snapshot**: Sara, day 5, iterating on a prompt + tools. Running 8-15 tests across 20 min.

**Trigger**: "Test my agent" mode after Claude wrote v1.

**Time budget**: 5-15 min per cycle, often multiple cycles per session.

**Mental state**: *"Is the prompt good? Is cost OK? Why did it pick that tool?"*
**Fears**: cost balloons during dev, output is subtly wrong, comparing runs is manual.

**Path**:
- Build → Agents → qa → Playground
- Picks (or pastes) input scenario — sometimes from a saved test scenario
- Runs it; watches streaming output, reasoner path, tool calls
- Sees per-step cost + latency + tokens
- Tweaks something (maybe via Claude in editor) → re-run
- Sometimes overrides model for testing (`gpt-4o` vs `claude-sonnet`)
- Compares two recent runs side-by-side
- Decides ship or iterate

**Mutations**: playground runs (cost-bearing).

**Confidence signal**: *"Output is right. Cost is in budget. Reasoner path matches intent."*

**Exit**: tells Claude what to change next, or commits.

**UX implications**:
- Playground state survives reload — don't lose test inputs / outputs
- "Re-run with same input" is one click
- Saved scenarios — build a test bench over time (named, persistent)
- Compare two runs visually (side-by-side or diff)
- Model picker overrides agent's default for one test
- Live cost ticker during the run

**UI organization**:
- Playground is a substantial surface (not a side panel)
- Recent runs in playground are sticky / saveable
- Compare mode is built-in (multi-select runs → "diff")
- Per-step cost / latency / tokens always visible
- Override controls (model, temperature, tool toggles) flanking the input area

---

### Journey 5 — Customer-reported issue triage

**Persona snapshot**: Sara, day 30, gets an email "your bot gave me a weird answer this morning, here's the screenshot."

**Trigger**: Email / Slack from customer with timestamp + screenshot.

**Time budget**: 3-5 min to understand + decide response.

**Mental state**: *"Find the customer, find the run, see what they saw, decide what to tell them."*
**Fears**: can't find their data; issue is invisible (no run logged); customer churns over slow response.

**Path**:
- Cmd+K → types customer email → jumps to Customers → Tenants → that tenant
- Tenant detail → runs tab filtered to that tenant
- Sorts by timestamp matching email screenshot
- Opens the suspect run drawer
- Sees input, reasoner path, tool calls, output, cost, errors
- Identifies the issue (wrong tool called, model returned bad citation, etc.)
- Decides response: refund / extend / explain
- Issues refund (if applicable) via mutation → audit entry
- Adds note to audit ("Ticket #X, customer Y, resolved")

**Mutations**: refund + budget extend + audit note.

**Confidence signal**: *"I see what they saw. I know what to tell them."*

**Exit**: replies to email with specifics + confidence.

**UX implications**:
- Cmd+K matches across tenant email, name, id, even partial
- Tenant detail is a complete lens — cost, runs, errors, audit, contact info, all tabs
- Run drawer must be COMPLETE — operator sees exactly what customer saw, including the agent's input + every tool call + final output
- Mutations (refund, extend) are inline on tenant page, one-click, audit-stamped
- Optional: link from run drawer back to "all runs for this tenant in last 24h" for cluster check

**UI organization**:
- Tenant detail is a substantial multi-tab page (Overview / Runs / Cost / Errors / Members / Keys / Audit / Settings)
- Run drawer is universal across pages — same shape on Operate→Runs and Tenant→Runs tab
- Mutation toast on tenant page with audit reference + undo
- Notes are first-class on tenants ("operator note" log separate from system audit)

---

### Journey 6 — Push alert: budget cap looming

**Persona snapshot**: Sara, day 30, at dinner. Phone pings: "Tenant `acme` at 90% of monthly budget."

**Trigger**: Push notification from BackAI's own alert system.

**Time budget**: 30-60 sec to decide and act.

**Mental state**: *"Is acme a real customer worth extending? Or anomalous spike?"*
**Fears**: can't fully assess from phone; wrong decision either way damages business.

**Path**:
- Tap notification → opens mobile-optimized "Alert Detail" page
- Sees: tenant name, current spend, monthly cap, % consumed, last 7d trend
- Sees: top 3 recent runs (cost, what they did)
- Sees: tenant's plan, signup date, MRR
- Decides: extend cap +50%, let it cap, suspend
- One tap → confirmation
- Audit entry written; push back to dinner

**Mutations**: budget extend OR let cap OR suspend.

**Confidence signal**: *"Decision made, system handled it, audit logged."*

**Exit**: back to dinner.

**UX implications**:
- Mobile-optimized "Alert Detail" view exists for hot mutations
- Push notification deep-links to that specific alert's detail
- Single-action mutation (not multi-page wizard)
- Recent context (last 7d trend + top runs + plan) shown inline
- Don't make operator hunt — pre-decide what they need to see

**UI organization**:
- **`/alerts/{alert_id}` mobile-optimized routes** — new pattern not in current spec
- Push channel preferences in Setup → Notifications (operator-side, not customer-side)
- Pulse / Inbox surface showing operator's own alert history
- Mutations on mobile route mirror full-admin mutations (programmable equivalence)

---

### Journey 7 — Cost-spike investigation

**Persona snapshot**: Sara, day 45, daily glance shows today's spend up 3x. Stops to investigate.

**Trigger**: Anomaly spotted on Home or Cost.

**Time budget**: 5-15 min to find root cause.

**Mental state**: *"What changed? Tenant? Model? Agent? Bug? Provider price?"*
**Fears**: bug burning money, abuse, provider change, my own regression.

**Path**:
- Operate → Cost
- Group by tenant — one tenant dominates? Or many?
- Group by agent — one agent dominates?
- Group by model — model mix changed?
- Group by reasoner — one step blew up?
- "Top expensive runs today" table — drills into specific runs
- Identifies cause (e.g., one tenant uploaded 200-page doc, or prompt regression)
- Decides: set per-tenant budget, fix prompt via Claude, suspend if abuse

**Mutations**: budget cap, possibly suspend, possibly Claude-driven prompt fix.

**Confidence signal**: *"I found it. I capped it."*

**Exit**: closes admin knowing spend trajectory is controlled.

**UX implications**:
- Cost page supports rapid pivot — single-click group-by switches
- "Anomaly" callouts inline (e.g., "Tenant `acme` is 5x its 7d avg")
- Drill from any cell (tenant, agent, model, reasoner) → filtered runs view
- Forecasting visible (current trend × days remaining = month-end estimate)
- Compare to past period built-in ("vs last week")

**UI organization**:
- Cost page is rich and dense, designed for pivot
- Anomaly detection is a feature (not just a chart) — surfaced at top
- Drill drawer is universal across pages
- "Set budget" inline action on any tenant row that pops up an anomaly
- Cost-per-reasoner requires reasoner-tagged LLM calls (backend prerequisite)

---

### Journey 8 — Provider outage / incident

**Persona snapshot**: Sara, day 60, multiple alerts in 2 min. Cascading errors across agents.

**Trigger**: Multi-alert burst (mobile + email + Slack).

**Time budget**: 15-30 min to diagnose + mitigate + recover.

**Mental state**: *"Everything's on fire. Scope? Cause? Mitigation?"*
**Fears**: customers churning right now, blame falls on me, can't see what's broken.

**Path**:
- Hits Errors page first — what's the error pattern? messages, models, tenants
- Group by model — all errors are Anthropic models? confirmed
- Operate → Health → provider status row → Anthropic shows degraded
- Cross-check on LiteLLM admin UI (link out) — confirms upstream
- Setup → LLM providers → swaps default chat model to OpenRouter Claude
- OR: enables fallback chain so future calls auto-fall-back
- Returns to Errors page
- Watches error rate drop in real time (live tick)
- After 5 min of clean: incident over
- Posts retrospective tweet "BackAI auto-fellback during Anthropic outage"

**Mutations**: fallback config edit, possibly model default swap.

**Confidence signal**: *"Error rate dropping. System recovered. Audit logged."*

**Exit**: writes a postmortem (or tweets the win).

**UX implications**:
- Errors page must pattern-cluster (group, dedup, similar errors collapsed)
- Health page is the incident landing — provider statuses front and center
- "Swap fallback" must be 2 clicks max (incident is not time for menu hunts)
- Real-time recovery indicator — see error rate drop after mutation
- Recovery actions inline on observability pages ("provider X degraded — switch fallback?")

**UI organization**:
- Errors page is observability + remediation — has "suggested action" hints when patterns match known shapes
- Health page is the incident landing — provider grid + adapter status front-and-center
- Setup → LLM providers must support mid-incident edits with low friction
- Live ticks during incident: error rate, success rate, current adapter
- Operator-side "incident timeline" surface (auto-built from events during high-alert periods)

---

## Synthesis: what falls out across all 8 journeys

### Recurring surfaces (appear in 2+ journeys)

| Surface | Journeys served | Implication |
|---|---|---|
| **Home** | 1, 2 + entry for 3, 5, 7, 8 | Highest-traffic surface. Optimize first. Density + scannability. |
| **Cmd+K** | 3, 5, 6, 7, 8 | Operator's superpower. Not nice-to-have — load-bearing. |
| **Tenant detail** | 5, 6, 7 | Substantial multi-tab page. Customer-care anchor. |
| **Run drawer** | 3, 4, 5, 7 | Universal across Runs / Errors / Sandbox / Webhooks. Build once. |
| **Cost page** | 4, 7 (entry) + influences 2 | Dense, pivot-heavy, anomaly-aware. |
| **Errors page** | 5, 8 | Pattern-clustering + suggested-action hints. |
| **Health page** | 8 | Incident landing. Provider grid + adapter status. |
| **Playground** | 3, 4 | Substantial dev surface (not a sub-panel). |
| **Activity feed** | 1, 2, 3 (verify reload), 8 (incident timeline) | Typed, severity-ordered, persistent. |
| **Mobile alert detail** | 6 | NEW — not in current spec. |

### Recurring primitives (need to be designed once, used everywhere)

| Primitive | Used by | Notes |
|---|---|---|
| **KPI tile** (label, value, delta, sparkline) | Home, Cost, Health, Tenant detail | Built-in delta and trend, not separate views. |
| **Drawer** (universal drill: header / overview / details / actions) | Runs, Errors, Deliveries, Sandbox runs, Jobs, Approvals, Webhooks | One shape, many entities. |
| **Group-by pivot control** | Cost, Errors, Runs, Activity | Single-click switches between dimensions. |
| **Mutation toast** (audit ref + undo) | Every mutation, everywhere | Reversible where possible. |
| **Tenant scope switcher** | Whole admin | Top-left. Re-scopes most pages. |
| **Adapter pill** ("via X") | Cost, Runs, Storage, Queue, Webhook deliveries, Sandbox, Traces, Logs, Metrics, Errors | Quiet modularity signal. |
| **Empty state, UI-shaped** | Every page | Frames + structure even at zero data. |
| **Activity event** (typed, severity, drill link) | Activity feed, audit, alerts | One shape across surfaces. |
| **Live-tick indicator** | Home KPIs, Cost during incident, Playground during run | Real-time WebSocket-backed. |
| **Filter chip set with URL state** | Every list view | Shareable views via URL. |

### IA refinements the journeys force

These are decisions where journey-thinking diverges from feature-thinking.

1. **Home is THE anchor — design it first, design it deepest.** It serves journeys 1 + 2 directly, and is the entry point for 3, 5, 7, 8. Should optimize density, scannability, delta-by-default, and live ticks.

2. **Cmd+K is load-bearing, not decoration.** Journeys 3, 5, 6, 7, 8 all use it as the fastest path. Treat it as a primary interaction, not a power-user feature. Visible trigger on Home; hint to first-time operators; ~30+ jump targets to convey scope.

3. **The Drawer is a universal primitive.** Runs, Errors, Deliveries, Sandbox runs, Jobs, Approvals, Webhooks all surface a list → detail flow. Design ONE drawer with the shape: header (id, status, when, owner) / overview tile / full detail (collapsible) / actions footer. Reuse everywhere.

4. **Mobile/alert surface is a real gap.** Journey 6 cannot be served by the current desktop-only admin. Need `/alerts/{alert_id}` mobile-optimized routes + push channel preferences + one-tap mutations.

5. **Demo-data mode is first-class.** Journey 1 fails without it. Make `BACKAI_DEMO_MODE=on` (default for fresh fork) seed realistic activity, runs, costs, errors so admin looks ALIVE on first open. Operator turns it off when they have real traffic.

6. **Group-by + filter persistence are critical.** Journeys 5, 7, 8 all rely on rapid pivoting. URL-encode every filter + grouping so views are shareable and survive reload.

7. **Welcome block is one-time, not a daily surface.** After first dismissal, "Your stack" + "Your key" collapse to a thin always-visible card. Don't take prime Home real estate forever.

8. **Playground deserves substantial space, not a sub-panel.** Journey 4 spends 15 min there. Saved scenarios, compare-two-runs, model override, live cost ticker.

9. **Live data matters at dev + incident moments.** WebSocket-backed ticks on KPIs, error rate, current run cost. The difference between "I trust this" and "I'll wait and see."

10. **Recovery actions on observability pages.** Errors page → "suggested mitigation" hints when patterns match. Cost page → "set budget" inline on anomalous tenant rows. Health page → "swap fallback" 2-click. Don't make incident response a menu hunt.

11. **"Loaded at" / version indicators wherever the operator might suspect drift.** Agents, modules, brand. Journey 3 confidence depends on knowing what version is running RIGHT NOW.

12. **Operator notes are a first-class log alongside system audit.** Journey 5 ends with adding context ("ticket #X, resolved with refund"). Distinct from audit (which logs what happened) — notes log WHY.

### Gaps the journeys reveal vs. current spec

| Gap | Severity | Journey that revealed it |
|---|---|---|
| Mobile/alert detail surface missing | High | 6 |
| Demo-data mode not first-class | High | 1 |
| Playground "saved scenarios" + "compare two runs" missing | Medium | 4 |
| Anomaly callouts on Cost page | Medium | 7 |
| Suggested-action / mitigation hints on Errors page | Medium | 8 |
| Operator alert inbox + push preferences | High | 6 |
| "Loaded at" / version indicators on live entities | Medium | 3 |
| Operator notes (distinct from system audit) | Low | 5 |
| Live-tick during recovery | Medium | 8 |
| Pulse/Inbox surface for operator's alert history | Medium | 6 |
| Universal drawer not yet a single primitive in design | Medium | 3, 5, 7 |

---

## What this implies for UX/UI organization

### Page-level requirements (per surface)

#### Home

- KPI strip (~8 tiles): every tile shows label, value, delta vs yesterday,
  sparkline, click → drill. Always-visible even at zero.
- Activity feed (right rail, ~20 entries): typed, severity-ordered, persistent.
- Backing services row: persistent health pills across the bottom (or footer
  band visible on all pages).
- Welcome block (top, compresses on dismissal): "Your stack" + dev tenant
  key + try-it snippet, compact card thereafter.
- Quick actions strip: 4-6 common mutations one click away.
- Cmd+K trigger visible (button or chip), not just keyboard.
- Live-tick on KPIs via WebSocket.

#### Cost

- Group-by control (model / agent / tenant / reasoner / day) as primary
  navigation, not a sub-filter.
- Anomaly callout band ("Tenant `acme` is 5x its 7d avg [Set budget]").
- Forecast line on chart (extrapolation to month end + budget delta).
- Top spenders table with inline "Set budget" mutation.
- Drill from any cell → filtered runs.
- Cache savings widget.

#### Errors

- Pattern-clustered list (similar errors collapsed; count + sample).
- Group-by + filter (source / tenant / model / pattern).
- Suggested-action hints when patterns match known shapes ("Anthropic 429 —
  switch fallback?").
- Live-tick during incidents.
- Drill drawer with full stack + related run.

#### Health

- Backing services grid (each OSS: name, version, status, latency, last-check).
- LLM provider availability row (per upstream provider: status, fallback
  activation count).
- Recent incidents timeline (auto-built from alert events).
- Recovery actions inline ("Swap default fallback to X").
- Link out to native OSS UIs from each row.

#### Tenant detail (Customers → Tenants → tenant)

- Multi-tab: Overview, Runs, Cost, Errors, Members, Keys, Audit, Settings.
- Overview shows: cost trend, run rate, error rate, budget consumption,
  recent activity, contact info.
- Inline mutations: extend budget, suspend, refund, add note.
- Compose-friendly: notes, audit, mutations all chronologically visible.

#### Agent detail

- "Loaded at" / version SHA badge.
- Reasoner list with diff indicator ("9 → 10 reasoners since last load").
- Playground tab (substantial, not nested).
- Recent runs filtered to this agent.
- Cost trend.

#### Playground

- Substantial surface (full page, not modal).
- Input pane with named scenario picker (saved tests).
- Streaming output pane with reasoner path + per-step cost.
- Compare mode (select 2+ recent runs → side-by-side diff).
- Model / temperature / tool overrides flanking input.
- Live cost ticker during run.

#### Mobile alert detail (NEW)

- Single-purpose page per alert type.
- Tenant or entity context up top.
- Recent trend + key metrics.
- 1-3 mutation buttons (decision-shaped, not menu-shaped).
- Confirmation modal with single tap.

### Cross-page primitives (build once, use everywhere)

1. **Drawer** — header / overview / details / actions footer
2. **KPI tile** — label / value / delta / sparkline / drill
3. **Group-by control** — pill row that re-shapes the chart + table
4. **Filter chip set** — URL-persistent, shareable
5. **Mutation toast** — confirmation + audit ref + undo where reversible
6. **Tenant scope switcher** — top-left, persistent
7. **Adapter pill** — small "via X" badge, dropdown reveals admin link
8. **Empty state frame** — UI-shaped, not text
9. **Activity event** — timestamp / icon / actor / message / drill
10. **Live-tick** — value with subtle pulse animation when updated

### Sidebar IA — rebuilt from operator concerns, not feature categories

The previous admin sidebar (Overview / Operate / Build / Customers / Setup /
Brand) is **feature-organized, not journey-organized**. It groups by what the
backend exposes, not by what the operator is asking. Killed in this grooming.

The 8 journeys surface **six operator concerns**:

| Concern | Operator question | Journeys |
|---|---|---|
| PULSE | "Am I OK? What needs me right now?" | 1, 2, 6, 8 |
| ACTIVITY | "What just happened on my platform?" | 2, 3, 5, 8 |
| MONEY | "Where's my spend going?" | 2, 6, 7 |
| PEOPLE | "Who's using my product?" | 5, 6, 7 |
| BUILD | "My code at runtime" | 3, 4, 8 |
| PLATFORM | "What's plugged in? Configure foundation" | 8, one-time |

These map cleanly to **4 pinned anchors + 4 concern groups**:

```
TOP — PINNED ANCHORS  (always visible, carry live values)

  ⌂  Home                        ← the assessment anchor
  📥 Inbox                [N]    ← alerts + approvals + decisions queue
  💰 Cost                $X.XX   ← today's spend + delta
  ☁  Health               ●     ← infra + provider status dot

GROUPS  (collapsible; ACTIVITY + BUILD default-open)

  ACTIVITY              ← "What's happening?"
    Runs
    Errors
    Logs
    Traces
    Webhook flow                 ← in + out delivery observability
    Cache

  PEOPLE                ← "Who's using it?"
    Tenants
    Sessions
    Keys
    Budgets
    Billing
    Audit
    Notes

  BUILD                 ← "My code at runtime"
    Agents (+ Playground)
    Reasoners
    Tools (MCP + native)
    Sandboxes (+ Playground)
    Crons
    Modules
    Data (Tables / SQL / Memory / Storage / Search)
    Harnesses
    API explorer
    Feature flags

  PLATFORM              ← "What's plugged in"
    Adapters (all swappable slots)
    LLM providers
    Auth
    Webhook subscribers
    Notifications channels
    Secrets
    Observability config
    Brand
    Deploy targets
```

**The pinned anchors are the design unlock.** They aren't just nav — each
carries a live value. Result: ~80% of Journey 2 (the 3-sec glance) resolves
in the sidebar itself without clicking. Open admin → see four colors +
numbers → close tab.

For Journey 8 (incident), the Health dot going yellow → red is the first
indicator. The nav itself raises the alarm.

**Explicit kills**:
- "Overview" as a group (Home promoted to anchor)
- "Setup" as a name (renamed PLATFORM — operators think about what's plugged in, not "settings")
- "Customers" as a name (renamed PEOPLE)
- Brand as pinned top-level (demoted into PLATFORM — one-time concern, doesn't earn prime real estate)
- "Operate" as a name (split into PULSE-anchors + ACTIVITY group; old "Operate" was 7 unrelated things)

Cmd+K remains the primary nav for experienced operators after week 1. The
sidebar is the discovery + first-impression surface and the live-status
surface.

---

## What changes in the spec from this grooming

Based on the journey synthesis, here are concrete changes the existing
`development/ui-plan-v1.md` should absorb:

1. **Add `BACKAI_DEMO_MODE` as a runtime feature** — default-on for fresh
   fork, seeds realistic activity / runs / costs.
2. **Add `/alerts/{alert_id}` mobile-optimized routes** — Journey 6.
3. **Promote Drawer to a documented universal primitive** with a single shape
   across all list→detail flows.
4. **Add anomaly callout band to Cost page spec**.
5. **Add suggested-action hint pattern to Errors page spec**.
6. **Add "Loaded at" / version indicator to Agent detail, Modules, Brand**.
7. **Add "Compare two runs" + "Saved scenarios" to Playground spec**.
8. **Add "Operator notes" as a first-class artifact distinct from system audit**.
9. **Promote Cmd+K from secondary to primary nav** in spec language.
10. **Add Operator Alert Inbox / Pulse surface** for the operator's own alert
    history (distinct from customer-facing notifications).

Each change is small. Cumulatively they shift the admin from "feature catalog"
to "journey-driven workspace."

---

## Next steps

1. Review this grooming with product owner — confirm journey set + principles.
2. Pick 1-2 journeys to deeply prototype the new patterns:
   - Recommended: Journey 1 (first-run wow) + Journey 8 (incident recovery)
   - These are the highest-stakes, most distinctive surfaces
3. Update `development/ui-plan-v1.md` with the 10 concrete spec changes above.
4. Build the universal Drawer primitive — biggest reuse win.
5. Spec the mobile alert routes — biggest user-experience gap.

---

_Last updated: 2026-06-16. Grounded in operator profile + 8 canonical
journeys, not feature catalog. See `development/ui-plan-v1.md` for current
admin product spec to be updated against this grooming._

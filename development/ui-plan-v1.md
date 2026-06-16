# BackAI Dashboard — UI Plan (v1, product perspective)

> Fresh planning doc. Grounded in (a) the backend APIs the runtime actually exposes today and (b) the product strategy from our prior session.
> Companion to (not a replacement for) the architectural spec at `development/dashboard-spec-v1.md`.
> Updates as backend evolves (watchdog tracks routes / protocols / adapters in `/tmp/backai-ui-watchdog.log`).
> Audience: the UI/UX designer + frontend engineer. No component-level decisions here — only what data each page must show.

---

## What this doc is and is not

| Is | Is not |
|---|---|
| A product-level inventory of pages the operator needs | A component or layout spec |
| A mapping of pages → data → backing API endpoints | A frontend implementation guide |
| Honest about which pages have backing APIs today and which don't | A wishlist independent of backend reality |
| A living doc updated as the backend agent ships new endpoints | A frozen design snapshot |

---

## Product principles (carried from prior session)

1. **Dashboard = operations on YOUR fork.** Pure reference (SDK docs, recipes, tutorials) lives on a separate docs site, not in here.
2. **MVP-honest.** Only spec UI for data we can actually serve from existing endpoints. Where there's no endpoint today, the page either thins or the section links out to the underlying OSS service.
3. **Console reads, code writes.** Observation is the default mode. Mutations are deliberate, drawer-based, and audited. Structural changes happen in code, not the console.
4. **Quiet modularity.** The adapter architecture is visible in one place (Setup → Adapters) and via a small `via <slot>` indicator on adapter-backed pages. No prominent OSS branding.
5. **Tenant scope is orthogonal to nav.** A tenant switcher up top re-scopes pages that support it. The sidebar structure never changes.
6. **Empty / missing / degraded are different states.** Empty = "no data yet, here's how to make some." Missing = "current adapter doesn't expose this." Degraded = "adapter unhealthy; last-known data shown."
7. **Drill from observability to source.** Any list cell → detail → "open in adapter admin." One drill away from the underlying truth, always.

---

## Status legend (used per page below)

| Symbol | Meaning |
|---|---|
| ✅ | Backing API exists today; page is fully serveable |
| 🟡 | Partial — primary data exists; some derived/aggregated fields require additional aggregation or new endpoints |
| ❌ | Page concept depends on backend work the runtime doesn't have yet; page is either deferred or thinned to nothing |

---

## Sidebar nav (product-perspective draft)

Six working groups + one pinned page. Sidebar groups collapse / expand; defaults below.

```
OVERVIEW
  Home                         ✅ — dev welcome + KPI strip + activity + service status

OPERATE  (default open)
  Runs                         ✅ — agent + handler execution stream
  Cost                         🟡 — primary data ✅; forecast / cache-savings are client-side compute
  Errors                       🟡 — via /logs filtered; dedicated /errors endpoint not present
  Traces                       🟡 — basic spans from OTel; deep trace explorer would link out
  Queue                        ✅ — job queue observability
  Cache                        ✅ — LLM cache effectiveness
  Sandbox runs                 ✅ — sandbox execution log (often missed surface)
  Webhook deliveries           ✅ — outbound deliveries
  Notifications (deliveries)   ✅ — alert delivery audit
  Approvals (HITL queue)       ✅ — human-decision queue
  Activity (customer-side)     ✅ — what the operator's customers did
  Health                       🟡 — thin: service status pills only; deeper checks link out
  Logs                         ✅ — raw log viewer

BUILD  (default open)
  Agents                       ✅ — registry + per-agent detail + playground
  Reasoners                    🟡 — flat listing derived from /agents; cross-agent analytics deferred
  Tools                        🟡 — listing + invoke from /tools/native + /tools/adapters + /mcp/tools
  Skills (MCP)                 ✅ — MCP servers + tools + attachments
  Harnesses                    ✅ — Claude Code / Codex / Gemini / opencode probe + status
  Crons                        ✅ — scheduled jobs
  Sandboxes (playground)       ✅ — pool status + run-a-command surface
  Modules                      ✅ — workload modules + enable/disable
  Data
    Tables                     ✅
    SQL                        ✅
    Memory                     ✅
    Storage                    ✅
    Search                     ✅
  Feature flags                ✅
  API explorer                 ✅ — Scalar embedded against /openapi.json
  Shipwright                   ✅ — if shipping in v1 (coding-agent factory)

CUSTOMERS  (default collapsed)
  Tenants                      ✅ — list + detail/drilldown
  API keys                     ✅
  Members                      ✅
  Sessions                     🟡 — via better-auth DB + /logs filtered for auth events
  Budgets                      ✅
  Audit log                    ✅
  OAuth connections            ✅
  Billing summary              ✅ — Stripe/Lago deep link out

SETUP  (default collapsed)
  Adapters                     ✅ — runtime adapter inventory and capability caveats
  Auth providers               ✅ — display from better-auth config
  LLM providers                🟡 — thin from /llm/models; rich data lives in LiteLLM admin
  Sandbox adapter              ✅ — from /sandbox/pool
  Webhook subscribers          ✅
  Notifications (channels)     🟡 — display from env; full CRUD UI may be deferred
  Secrets                      ✅
  Observability                ✅ — display read-only; metrics endpoint exists
  Billing adapter              ✅ — Stripe or Lago selection + status
  Deploy targets               ✅ — informational (no provisioning UI v1)

BRAND  (pinned, top-level)
  Brand                        🟡 — read-only display of brand.yaml v1; editing is a file edit
```

---

## Cross-cutting elements (apply to every page)

These behave the same everywhere; treat them as the dashboard chrome.

| Element | Behavior | Backed by |
|---|---|---|
| **Top bar** | Logo · tenant switcher · breadcrumbs · search/command palette trigger · theme toggle · profile · notification bell | Operator session + tenant context |
| **Tenant scope switcher** | "Platform" or "Tenant: <name>". Persistent. Re-scopes pages that support tenant filtering. Pages that don't (Develop-style, Setup, Brand) ignore it. | List from `GET /api/v1/admin/tenants` |
| **Command palette (⌘K)** | Jump to any page · search tenants/agents/runs/keys · trigger common actions · link out to backing OSS admin UIs | Aggregates `agents`, `admin/tenants`, `admin/keys`, recent runs, nav |
| **Mutation pattern** | Create/edit flows open as right-side drawers, not modals. Every mutation produces an audit entry. Toast confirms with audit reference and offers undo where reversible. Every form should expose a "Show as code" toggle. | `audit` log endpoint + per-resource POST/PUT/DELETE |
| **Adapter pill** | Subtle "via <slot>" indicator (e.g., "via LLM gateway") on pages backed by a slot. Click → docs · open native admin in new tab · "swap slot" link. Modularity stays quiet. | Adapter introspection endpoint (TBD; see Setup → Adapters) |
| **Live data** | Real-time pages (Home, Cost, Runs, Errors, Queue) update via WebSocket. Numbers tick subtly; no flashing. | `GET /api/v1/realtime`, `GET /api/v1/realtime/runs` |
| **Empty / missing / degraded** | Three distinct visual states. Empty = "no data yet" with snippet to generate some. Missing = "current adapter doesn't expose this" with link to adapter docs. Degraded = "stale/unhealthy" with last-known data shown. | Capability declaration on adapter; health endpoint per service |

---

## Pages — purpose, data, APIs

Each entry: **purpose** · **data to show** (product terms) · **primary API(s)** · **actions** · **status**.

### Overview › Home `/`

**Purpose**: First page on landing. Two jobs in one: welcome the developer (for first-run / new operators) and answer "is anything broken right now" (for established operators).

**Data**:
- Welcome block (collapsible after first dismissal): operator's runtime URL · their dev tenant API key with reveal · "your stack" summary (which adapter per slot) · one try-it-now snippet pre-filled with their URL+key · 3-5 "next step" links
- KPI strip (live tiles): requests/min · error rate % · cost today · cost MTD · queue depth · active runs · failed runs (24h) · % of total budget consumed
- Recent activity feed: last ~20 platform events (runs · deploys · tenant changes · budget thresholds · errors) with drill links
- Stack status row: per backing service (postgres · litellm · agentfield · river · svix · minio · redis) — pill with status dot · version · last-checked timestamp
- Quick action cards: "Issue API key" · "Add tenant" · "Test an agent" · "Open API explorer"

**APIs**:
- `GET /api/v1/home/overview` ✅
- `GET /api/v1/metrics/summary` ✅
- `GET /api/v1/activity?limit=20` ✅
- `GET /health` `GET /ready` ✅
- `GET /api/v1/realtime` (WebSocket for live KPI ticks) ✅

**Actions**: reveal key · copy snippet · dismiss welcome · drill from any KPI / activity / service pill

**Status**: ✅

---

### Operate › Runs `/operate/runs`

**Purpose**: Inspect every agent or handler execution. The first place an operator goes to debug.

**Data**:
- Filter bar: time range · agent (multi-select) · reasoner · tenant · status (running / succeeded / failed / timeout) · cost range · search by run id
- Run list (table): timestamp · agent.reasoner · tenant · status · duration · cost USD · token count · trigger source
- Detail drawer per run: run id · model used · status · duration · cost · tokens in/out · cache hit · input payload · output payload · reasoner path · tool/sub-agent calls with timing+cost · errors if failed · audit reference · "Open full DAG in AgentField ↗"

**APIs**:
- `GET /api/v1/runs` ✅
- `GET /api/v1/runs/{id}/events` ✅ (event stream / lifecycle)
- `GET /api/v1/runs/{id}/agentfield` ✅ (deep DAG link)
- `GET /api/v1/executions/{id}` ✅ (handler executions)
- `POST /api/v1/runs/{id}/cancel` `POST /api/v1/runs/{id}/pause` `POST /api/v1/runs/{id}/resume` ✅
- `POST /api/v1/runs/{id}/request-approval` ✅ (creates an Approvals entry)

**Actions**: filter / search / sort · re-run (copy input to playground) · cancel / pause / resume · request approval

**Status**: ✅

---

### Operate › Cost `/operate/cost`

**Purpose**: Spend awareness, budget control, forecast, value-of-cache. The page that proves BackAI is a real backend not a wrapper.

**Data**:
- Scope controls: tenant filter · time range (24h / 7d / 30d / 90d) · group-by (model / agent / reasoner / tenant / day)
- Spend chart: spend over time, stacked by chosen group-by
- KPI tiles: spent today · spent MTD · forecast EOM at current rate (with budget delta) · cache savings USD + hit rate · avg request cost (with p99)
- Top spenders table (column configurable: tenants / agents / models / reasoners) — name · today · period · sparkline · drill link
- Top expensive runs: run id · agent.reasoner · cost · tokens · when · drill to Runs
- Budgets snapshot: tenant · cap · used USD+% · alert threshold · status (ok / near / over)
- Cache effectiveness widget: hit rate · top cached prompts · estimated savings
- Per-model unit economics card: avg cost per call by model

**APIs**:
- `GET /api/v1/cost` ✅ (summary)
- `GET /api/v1/cost/events` ✅
- `GET /api/v1/admin/budgets` `GET /api/v1/admin/budgets/{tenantId}` `PUT /api/v1/admin/budgets` ✅
- `GET /api/v1/llm/cache/stats` ✅
- LiteLLM admin `/spend/keys`, `/spend/tags`, `/spend/logs`, `/global/spend` ✅ (link out for deeper)

**Compute client-side**: forecast (linear regression on time-series) · cache savings (hit count × avg cost) · cost-per-reasoner (LiteLLM spend grouped by `reasoner` tag when tagged)

**Actions**: change filters · set budget (drawer) · drill from any expensive row · "Open LiteLLM admin ↗"

**Status**: 🟡 (rich UI possible from existing endpoints; forecast / cache-savings are client compute)

---

### Operate › Errors `/operate/errors`

**Purpose**: Triage failures across runs, jobs, handlers, webhooks. The active firefight page.

**Data**:
- Filter bar: source (runs / jobs / handlers / outbound webhooks / inbound webhooks) · severity · tenant · agent · time range · status (active / muted / resolved)
- Error list: timestamp · source · summary · count (if recurring) · tenant · agent or job kind · last seen
- Detail drawer per error: full stack/payload · sample run or job id with drill · suggested fix link if recognized · audit references · mute / resolve

**APIs**:
- `GET /api/v1/logs?level=error,fatal` ✅
- Filter / dedup logic client-side; pattern grouping client-side
- (Future) dedicated `GET /api/v1/admin/errors` endpoint with backend grouping — currently logs do the job

**Actions**: mute (with reason + expiry) · resolve · bulk mute · drill to source

**Status**: 🟡 (works via logs filter; pattern grouping is client-side; dedicated errors endpoint not present)

---

### Operate › Traces `/operate/traces`

**Purpose**: Span tree for a request. Performance debugging.

**Data**:
- Filter bar: trace id · agent · time range
- Trace list: trace id · root span name · duration · span count · status
- Trace detail view: hierarchical span tree · per-span name/duration/attributes/status · critical path highlighted

**APIs**:
- Runtime OTel exporter is configured but no in-product trace browser endpoint — link out to external collector (Tempo/Honeycomb/Langfuse) if configured
- (Future) light in-product trace browser would call a `GET /api/v1/traces` endpoint

**Actions**: search by trace id · drill into spans · copy span attributes · "Open in external trace explorer ↗"

**Status**: 🟡 (page can render minimally; deep exploration requires linking out)

---

### Operate › Queue `/operate/queue`

**Purpose**: Status of the async job queue.

**Data**:
- Counts by status: pending · running · succeeded (today) · failed (today) · retrying · dead-lettered
- Latency tiles: p50 / p95 / p99 pickup time · p50 / p95 / p99 duration
- Job kind breakdown: per kind — count · avg duration · error rate
- Job list: id · kind · status · attempts · queued-at · last error · tenant
- Detail drawer per job: full payload · attempts history · last error · related run/handler

**APIs**:
- `GET /api/v1/queues/summary` ✅
- `GET /api/v1/jobs` `GET /api/v1/jobs/{id}` `GET /api/v1/jobs/definitions` ✅
- `POST /api/v1/jobs/{id}/retry` ✅
- `POST /api/v1/jobs` (enqueue from UI) ✅

**Actions**: retry failed job · send to dead-letter · filter · "Open River UI ↗" if exposed

**Status**: ✅

---

### Operate › Cache `/operate/cache`

**Purpose**: LLM cache effectiveness — proves the platform saves money.

**Data**:
- Overall hit rate (today / 7d / 30d) and savings USD
- Top cached prompts by hit count
- Top misses by cost
- Cache size · expiry policy summary
- Per-tenant hit-rate breakdown

**APIs**:
- `GET /api/v1/llm/cache/stats` ✅

**Actions**: flush all · flush by tenant · flush by prompt hash (if backend supports)

**Status**: ✅

---

### Operate › Sandbox runs `/operate/sandbox-runs`

**Purpose**: Observability for every sandbox execution. Often missed surface — we have the data; surface it.

**Data**:
- Filter bar: tenant · adapter · status (running / succeeded / failed / timeout / cancelled) · time range · image · exit code range · free-text command search
- Sandbox-runs list: started at · tenant · image · command preview · status · duration · exit code · CPU-seconds · cost USD · triggered by (agent name / operator / API)
- Detail drawer per run: full command/env/mounts/limits · live stdout/stderr stream · exit code · duration · cost · peak CPU/memory · triggered-by drill · cancel button if running

**APIs**:
- `GET /api/v1/sandbox/runs` ✅
- `GET /api/v1/sandbox/runs/{id}` ✅
- `GET /api/v1/sandbox/runs/{id}/logs` ✅ (live tail / SSE)
- `DELETE /api/v1/sandbox/runs/{id}` ✅ (cancel)

**Actions**: cancel · drill to parent agent run · "Re-run with these inputs" → Build → Sandboxes playground

**Status**: ✅

---

### Operate › Webhook deliveries `/operate/webhooks`

**Purpose**: Observability for outbound webhooks (what fired, what succeeded, what failed).

**Data**:
- Filter bar: event type · endpoint · tenant · status (delivered / failed / retrying) · time range
- Deliveries list: timestamp · event type · endpoint · tenant · status · attempts · last response code
- Detail drawer: full event payload · outbound request headers · response body+headers · retry schedule

**APIs**:
- `GET /api/v1/webhooks/deliveries` ✅
- `GET /api/v1/webhooks/deliveries/{id}` ✅
- `POST /api/v1/webhooks/deliveries/{id}/retry` ✅
- Svix dashboard (link out) for delivery archive, replay, signing history

**Actions**: manually replay · filter / search · "Open Svix admin ↗"

**Status**: ✅

---

### Operate › Notifications (deliveries) `/operate/notifications`

**Purpose**: Delivery audit for alerts the platform sent (email / Slack / SMS / log). Distinct from Setup → Notifications which is channel configuration.

**Data**:
- Filter bar: channel · status (delivered / failed / queued / muted) · category (budget-alert / error-alert / approval-needed / etc.) · tenant · time range
- Stats tiles: sent today · delivery rate % · failure rate % · avg latency
- Notifications list: timestamp · channel · recipient · category · status · retry count · latency · link to triggering event
- Detail drawer: full payload (subject, body, template used) · provider response · retry history

**APIs**:
- `GET /api/v1/notifications` ✅
- `GET /api/v1/notifications/{id}` ✅
- `GET /api/v1/notifications/stats` ✅
- `POST /api/v1/notifications` (resend) ✅

**Actions**: manually resend · filter · mute future notifications of same kind

**Status**: ✅

---

### Operate › Approvals (HITL queue) `/operate/approvals`

**Purpose**: Queue of pending human decisions gating workflow execution. AI agents waiting on approval before proceeding.

**Data**:
- Filter bar: status (pending / approved / denied / cancelled) · kind · tenant · time range · requested by · decided by
- Stats tiles: pending count · avg decision time · approval rate · escalation rate
- Approvals list: created at · kind · tenant · requested by · status · decision time · decided by · decision note preview
- Detail drawer: full payload (structured request) · related run/job that's blocked · decision form (approve / deny / cancel + note) · audit references · history of similar decisions for context

**APIs**:
- `GET /api/v1/approvals` ✅
- `GET /api/v1/approvals/{id}` ✅
- `POST /api/v1/approvals` ✅
- `POST /api/v1/approvals/{id}/decide` ✅

**Actions**: approve / deny / cancel with note · bulk decide on filtered set · drill into blocked run

**Status**: ✅. Pending count should also surface on Home KPI strip.

---

### Operate › Activity (customer-side) `/operate/activity`

**Purpose**: Feed of actions the operator's customers took inside the customer-app. Distinct from Audit (which is operator-side mutations).

**Data**:
- Filter bar: actor type (user / API key / system / anonymous) · action verb · resource type · tenant · user · time range
- Activity list: timestamp · actor · action · resource type+id · tenant · IP · user-agent
- Detail panel: full metadata · related runs / costs ("this action triggered run X which cost Y")

**APIs**:
- `GET /api/v1/activity` ✅
- `POST /api/v1/activity` ✅ (record from external sources if needed)

**Actions**: filter / search · export to CSV · drill to related entities

**Status**: ✅

---

### Operate › Health `/operate/health`

**Purpose**: At-a-glance "is everything up?" for the fork's backing services. Thin in v1.

**Data**:
- Backing service status grid: per service (postgres · litellm · agentfield · river · svix · minio · redis) — name · status dot · version · last-checked · link to native admin UI
- Runtime self-status: own /health · /ready · uptime · build version
- LLM provider availability (one row per upstream provider configured; LiteLLM `/health` per-model link-out)
- Optional: Prometheus link out for metrics graphs

**APIs**:
- `GET /health` ✅
- `GET /ready` ✅
- `GET /metrics` ✅ (Prometheus exposition)
- Each OSS service's own `/health` polled

**Link out** for everything else: DB stats / cert expiry / time-series / worker counts → Build → Data → SQL against `pg_stat_*`, or OSS admin

**Actions**: click any service → opens its admin in new tab · manual refresh

**Status**: 🟡 (thin in v1; deeper needs PG-stats endpoint that doesn't exist yet)

---

### Operate › Logs `/operate/logs`

**Purpose**: Raw log viewer with filters. Drop into the logs when Errors / Traces aren't enough.

**Data**:
- Filter bar: log level · service · tenant · time range · free-text search
- Log stream (virtualized): timestamp · level · service · message · structured field highlights (run_id, tenant_id, etc.)
- Tail mode toggle: live-streaming new logs
- Structured field expand per entry

**APIs**:
- `GET /api/v1/logs` ✅
- `GET /slow` ✅ (slow queries)
- WebSocket tail (if implemented)

**Actions**: filter · tail · export filtered logs (CSV / JSONL) · save filter as named view

**Status**: ✅

---

### Build › Agents `/build/agents`

**Purpose**: Registry of all agents the runtime knows about. Detail per agent. Test playground.

**Data (list)**:
- Per agent row: name · reasoner count · calls today/7d · cost today · error rate · container status · version

**Data (detail per agent)**:
- Header: name · container image · version · status pill
- Reasoners table: name · kind · schema preview · tool list · avg cost · error rate · calls today
- Recent runs filtered to this agent
- Cost trend for this agent
- MCP servers configured for this agent
- Capabilities declared (harnesses available, models used)

**Data (playground sub-view)**:
- Input form generated from entry reasoner's schema
- Live streaming output
- Per-step cost+timing
- Reasoner trace expanded inline
- Run id link to Runs detail

**APIs**:
- `GET /api/v1/agents` ✅
- `POST /api/v1/agents/{call}` ✅ (sync invoke)
- `POST /api/v1/agents/async/{call}` ✅ (async — streams via realtime)
- `GET /api/v1/realtime/runs?run_id=<id>` ✅ (streaming output)

**Actions**: test in playground · search · no CRUD (agents are code-defined)

**Status**: ✅

---

### Build › Reasoners `/build/reasoners`

**Purpose**: Flat cross-agent listing of all reasoners. "What reasoning steps exist on my fork."

**Data**:
- Reasoner list: agent.reasoner id · kind (entry / parallel / nested / synthesis) · parent agent · schema (input / output) preview · declared tool list

**APIs**:
- Derived from `GET /api/v1/agents` (each agent entry contains its reasoners) ✅

**Actions**: filter by agent · search · click reasoner → parent agent detail

**Link out**: cost-per-reasoner is on Operate → Cost (group-by reasoner) · source DAG → AgentField at `:8081`

**Status**: 🟡 (listing works today; cross-agent cost/latency analytics deferred)

---

### Build › Tools `/build/tools`

**Purpose**: Inventory of every tool the agents can call. Native built-ins + MCP. Test-invoke supported. Usage analytics deferred.

**Data**:
- Tabs: Native tools / MCP tools
- Native tools (`/tools/native` + `/tools/adapters`): tool id · description · enabled toggle · schema
- MCP tools (`/mcp/tools` + `/mcp/servers`): server name · tool name · transport · schema · server status

**APIs**:
- `GET /api/v1/tools/native` ✅
- `GET /api/v1/tools/adapters` ✅
- `POST /api/v1/tools/call` ✅
- `POST /api/v1/tools/native/{tool}/invoke` ✅
- `POST /api/v1/tools/native/{tool}/enable` ✅
- `PUT /api/v1/tools/adapters/{id}/enabled` ✅
- `GET /api/v1/mcp/tools` ✅
- `POST /api/v1/mcp/call` ✅

**Actions**: toggle enable / disable · test invoke (form from tool schema) · drill into MCP server

**Status**: 🟡 (listing + invoke works; usage analytics deferred)

---

### Build › Skills (MCP) `/build/skills`

**Purpose**: MCP servers configured per agent. Manage installed skills.

**Data**:
- Skill list: name · attached agents · tools exposed · server status
- Per server: name · transport (stdio / SSE / HTTP) · tools list (with schemas) · reachable status

**APIs**:
- `GET /api/v1/skills` ✅
- `POST /api/v1/skills` ✅
- `POST /api/v1/skills/attach` ✅
- `DELETE /api/v1/skills/{id}` ✅
- `GET /api/v1/mcp/servers` ✅
- `GET /api/v1/mcp/servers/{name}` ✅
- `POST /api/v1/mcp/servers` ✅
- `DELETE /api/v1/mcp/servers/{name}` ✅
- `PUT /api/v1/mcp/servers/{name}/enabled` ✅

**Actions**: install skill · attach to agent · enable / disable server · test tool call

**Status**: ✅

---

### Build › Harnesses `/build/harnesses`

**Purpose**: Coding-agent harnesses (Claude Code / Codex / Gemini / opencode). Registry + probe status.

**Data**:
- Harnesses list: provider · version · agents using it · models available · last probe time · last probe status
- Per-harness detail: capability matrix (file edits, multi-file, tool use, planning, etc.) · available models · recent invocations · probe history

**APIs**:
- `GET /api/v1/harnesses` ✅
- `GET /api/v1/harnesses/{provider}` ✅
- `POST /api/v1/harnesses/{provider}/probe` ✅

**Actions**: probe now · view probe logs · disable per agent

**Status**: ✅

---

### Build › Crons `/build/crons`

**Purpose**: Scheduled (cron-style) jobs. Different from the one-shot queue.

**Data**:
- Cron list: name · schedule (cron expr) · kind (job / agent.call / handler / custom) · tenant scope · active/paused · next run · last run status · last run timestamp · avg duration
- Per-cron detail: schedule editor with human-readable preview · target action · last N runs · pause/edit history

**APIs**:
- `GET /api/v1/crons` ✅
- `GET /api/v1/crons/{id}` ✅
- `POST /api/v1/crons` ✅
- `PUT /api/v1/crons/{id}/active` ✅
- `DELETE /api/v1/crons/{id}` ✅

**Actions**: create cron (drawer) · edit schedule · pause / resume · trigger manually · delete

**Status**: ✅

---

### Build › Sandboxes (playground) `/build/sandboxes`

**Purpose**: Dev surface for the platform's code-execution sandboxes — test config, inspect pool, run ad-hoc commands. Distinct from Operate → Sandbox runs.

**Data**:
- Tab 1 — Playground: image picker · command field · optional env / cwd / mounts / timeout / CPU+memory caps · tenant scope · Run button · live output stream · on-completion exit code / duration / cost / peak CPU+memory
- Tab 2 — Pool: current adapter (Docker / gVisor / Firecracker / e2b / Modal) · pool stats (warm / active / queued / idle) · per-tenant pool usage · adapter-specific health

**APIs**:
- `POST /api/v1/sandbox/run` ✅
- `GET /api/v1/sandbox/pool` ✅
- `GET /api/v1/sandbox/runs/{id}/logs` ✅ (live tail)
- `DELETE /api/v1/sandbox/runs/{id}` ✅

**Actions**: run command · cancel running sandbox · inspect any past run (drills to Operate → Sandbox runs)

**Status**: ✅

---

### Build › Modules `/build/modules`

**Purpose**: Workload modules — domain backend code mounted under `/workload/<id>/`. Enable / disable.

**Data**:
- Installed modules: id · name · version · status (enabled / disabled) · mounted routes · migrations status · source path
- Per-module detail: manifest contents · declared routes / crons / jobs · env vars used

**APIs**:
- `GET /api/v1/modules` ✅

**Actions**: enable / disable (writes config, may require restart) · open source path

**Status**: ✅ (read; toggle UI is light)

---

### Build › Data › Tables `/build/data/tables`

**Purpose**: Browse Postgres tables in the platform schemas (suite + agentfield).

**Data**:
- Left: schemas list → tables with row counts
- Right (selected table): structure (columns, types, indexes, constraints) · data preview (paged rows with filtering)
- Tabs per table: Data · Structure · Policies (RLS) · Indexes

**APIs**:
- `GET /api/v1/db/tables` ✅
- `GET /api/v1/db/tables/{schema}/{name}` ✅
- `GET /api/v1/db/rows` ✅

**Actions**: filter by column · sort · paged scroll · read-only

**Status**: ✅

---

### Build › Data › SQL `/build/data/sql`

**Purpose**: Ad-hoc read-only SQL workbench.

**Data**:
- SQL editor
- Saved snippets / query history
- Results table below editor
- Execution timing

**APIs**:
- `POST /api/v1/db/sql` ✅ (read-only enforced server-side)

**Actions**: run query · save snippet · export results (CSV / JSON)

**Status**: ✅

---

### Build › Data › Memory `/build/data/memory`

**Purpose**: Inspect / debug the per-scope vector memory store used by agents.

**Data**:
- Scope picker: tenant / agent / session / global
- Entries list: key · kind · created at · size · sample value
- Detail panel: full value · embedding vector summary (model used, dim count)
- Semantic search test: enter query → top-k results with cosine scores

**APIs**:
- `GET /api/v1/memory` ✅
- `GET /api/v1/memory/get` ✅
- `POST /api/v1/memory/search` ✅
- `PUT /api/v1/memory` ✅
- `DELETE /api/v1/memory` ✅

**Actions**: delete entry · run semantic search test · export

**Status**: ✅

---

### Build › Data › Storage `/build/data/storage`

**Purpose**: Browse objects per tenant / bucket.

**Data**:
- Bucket list: name · file count · total size · per-tenant usage breakdown
- Bucket contents: file list (key · size · content type · uploaded by · uploaded at)
- File detail: metadata · presigned URL preview · presigned URL generator

**APIs**:
- `GET /api/v1/storage` ✅
- `GET /api/v1/storage/{key...}` ✅
- `GET /api/v1/storage/signed-url` ✅
- `POST /api/v1/storage/upload` ✅
- `DELETE /api/v1/storage/{key...}` ✅
- MinIO admin (link out) for advanced bucket-level ops
- **Storage adapter protocol** `docs/adapters/protocols/storage-v1.md` exists and is being audited. Capability fields: `max_object_size_bytes`, `single_put_max_bytes`, `presign_ttl_max_seconds`, `supports_range_requests`, `supports_metadata_headers`, `supports_signed_uploads`, `max_keys_per_list`, `bucket_required`.

**Actions**: delete file · generate presigned URL · "Open MinIO console ↗"

**UI impact from storage adapter capabilities**: signed-URL "Generate" button hidden when `supports_signed_uploads=false`; TTL slider clamps to `presign_ttl_max_seconds`; upload pre-validation against `max_object_size_bytes` / `single_put_max_bytes`; pagination size aligned with `max_keys_per_list`. When non-MinIO adapter is active, "Open MinIO console" link replaced with that adapter's admin link or hidden.

**Status**: ✅ (works today; will become capability-aware as adapter introspection endpoint lands)

---

### Build › Data › Search `/build/data/search`

**Purpose**: Inspect Postgres FTS indexes used for in-app search.

**Data**:
- Index list with source table+column
- Sample queries
- Performance stats per index (if available)

**APIs**:
- `POST /api/v1/search` ✅
- `PUT /api/v1/search/documents` ✅
- `DELETE /api/v1/search/documents/{namespace}/{key}` ✅

**Actions**: test query against an index

**Status**: ✅

---

### Build › Feature flags `/build/feature-flags`

**Purpose**: Runtime feature flags.

**Data**:
- Flag list: key · description · value · rollout % (if variant) · tenant overrides count · last changed
- Per-flag detail: history of changes · per-tenant overrides

**APIs**:
- `GET /api/v1/config/flags` ✅
- `PUT /api/v1/config/flags/{key}` ✅

**Actions**: toggle · set rollout % · add tenant override · audit on every change

**Status**: ✅

---

### Build › API explorer `/build/api-explorer`

**Purpose**: Try every endpoint of YOUR running fork without writing client code. The only "dev tool" inside the dashboard because it uses YOUR fork's auth / endpoints / schema. Pure docs / reference live on the docs site.

**Data**:
- Embedded API explorer (e.g., Scalar) against the runtime's OpenAPI spec
- Auth selector top of page: "Operator session" (default) or "Tenant: <picker>" (uses tenant API key)
- Endpoint groups by tag
- Top-right: "Download schema ▾" (OpenAPI JSON / YAML / TypeScript types / Python Pydantic / Go structs)

**APIs**:
- `GET /openapi.json` ✅ (the schema source)
- All `/api/v1/*` endpoints invokable from try-it

**Actions**: try-it on any endpoint · switch auth context · download schema · copy as curl / TS / Py

**Status**: ✅

---

### Build › Shipwright `/build/shipwright` (conditional — only if shipping in v1)

**Purpose**: Coding-agent factory tasks. Long-running agentic coding tasks (clone repo, run harness in sandbox, return PR).

**Data**:
- Tasks list: id · title · repo · status · harness used · model used · sandbox adapter · cost · duration · output (PR / artifact link)
- Per-task detail: input prompt · sandbox run reference · harness invocation logs · file diffs · cost breakdown

**APIs**:
- `GET /api/v1/shipwright/tasks` ✅
- `GET /api/v1/shipwright/tasks/{id}` ✅
- `POST /api/v1/shipwright/tasks` ✅
- `POST /api/v1/shipwright/tasks/{id}/complete` ✅

**Actions**: create task (drawer) · cancel running task · re-run failed · open sandbox run detail

**Status**: ✅ (only ship the page if Shipwright is part of v1 product scope; otherwise omit)

---

### Customers › Tenants `/customers/tenants`

**Purpose**: List and manage tenants (the operator's customers).

**Data (list)**:
- Per tenant row: name · id · created at · members count · API keys count · cost today · cost MTD · budget consumed % · status (active / suspended)
- Filters / search

**Data (detail)**:
- Header: name · id · status
- Tabs: Overview · Members · API keys · Usage · Audit · Settings
- Overview: cost summary · recent runs · recent errors · budget status
- Members: user list with role
- API keys: list with alias · status · rpm/tpm/budget · last used
- Usage: requests over time · top agents/models
- Audit: filtered audit log
- Settings: name · metadata · suspend toggle · delete

**APIs**:
- `GET /api/v1/admin/tenants` ✅
- `GET /api/v1/admin/tenants/{id}` ✅
- `GET /api/v1/admin/tenants/{id}/drilldown` ✅
- `POST /api/v1/admin/tenants` ✅
- `PATCH /api/v1/admin/tenants/{id}` ✅
- `DELETE /api/v1/admin/tenants/{id}` ✅

**Actions**: create · suspend / resume · delete (cascade summary)

**Status**: ✅

---

### Customers › API keys `/customers/api-keys`

**Purpose**: Issue, rotate, revoke API keys across all tenants.

**Data**:
- Key list: alias · tenant · masked id · status (active / revoked / expired) · rpm/tpm/budget cap · used budget % · last used · created at
- Issue drawer: tenant select · alias · budget cap · rate limits · expiration · scopes · "Show as code" toggle · one-time secret reveal after creation

**APIs**:
- `GET /api/v1/admin/keys` ✅
- `POST /api/v1/admin/keys` ✅
- `GET /api/v1/admin/keys/{id}/spend` ✅
- `DELETE /api/v1/admin/keys/{id}` ✅
- LiteLLM virtual key mirror (auto-managed)

**Actions**: issue · rotate · revoke · filter / search

**Status**: ✅

---

### Customers › Members `/customers/members`

**Purpose**: Users across tenants.

**Data**:
- Member list: name · email · tenant(s) · role(s) · last login · MFA status · provider
- Per-member detail: tenant memberships · audit · recent sessions

**APIs**:
- `GET /api/v1/admin/users` ✅
- `GET /api/v1/admin/users/{id}/export` ✅
- `POST /api/v1/admin/users/{id}/erase` ✅
- `GET /api/v1/admin/memberships` ✅
- `POST /api/v1/admin/memberships` ✅
- `DELETE /api/v1/admin/memberships/{tenantId}/{userId}` ✅
- Better-auth user table (via dashboard Drizzle) for richer profile

**Actions**: invite · remove from tenant · disable account · export user data · erase user data (GDPR)

**Status**: ✅ (note: GDPR export/erase endpoints exist; full Compliance page deferred but actions inline here)

---

### Customers › Sessions `/customers/sessions`

**Purpose**: Active customer sessions and auth events. Security operations.

**Data**:
- Tabs: Active sessions / Auth events
- Active sessions: user · tenant · started at · last active · IP · user agent · MFA used · expires at
- Auth events: timestamp · event type (login / logout / login-failed / password-reset / MFA-* / OAuth-grant / token-refresh) · user · tenant · IP · user-agent · success/failure · reason if failed
- Stats: active session count · sign-ups today · login failures today · suspicious activity flags

**APIs**:
- Better-auth `session` table via dashboard Drizzle queries ✅ — **today**
- `GET /api/v1/logs?event=auth.*` ✅ for auth event stream
- **Auth adapter caveat (in progress):** the v1 protocol does NOT define a session-listing endpoint. When auth becomes adapter-pluggable, the "active sessions" table is only populated if the adapter exposes session enumeration (capability flag TBD). Auth-events tab via `/logs` continues to work for any adapter.

**Actions**: force logout a session · lock / unlock account · filter / search

**UI impact when auth adapter lands**: active-sessions tab shows "Session enumeration not supported by current auth adapter" when capability is absent; auth-events tab works unchanged.

**Status**: 🟡 (works for better-auth today; adapter-aware behavior pending)

---

### Customers › Budgets `/customers/budgets`

**Purpose**: Per-tenant budget caps + alert thresholds.

**Data**:
- Budget list: tenant · monthly cap · period start · used USD+% · alert threshold · status · last alert sent
- Per-budget edit drawer: cap · threshold · alert recipient

**APIs**:
- `GET /api/v1/admin/budgets` ✅
- `GET /api/v1/admin/budgets/{tenantId}` ✅
- `PUT /api/v1/admin/budgets` ✅
- LiteLLM budget mirror (auto)

**Actions**: set / update / delete budget · test alert delivery

**Status**: ✅

---

### Customers › Audit log `/customers/audit`

**Purpose**: Full provenance feed of every mutation.

**Data**:
- Filter bar: actor (user / system / API key) · entity type · action · tenant · time range
- Entries: timestamp · actor · action · entity type · entity id · before/after diff summary · IP · user-agent
- Detail panel: full diff JSON

**APIs**:
- `GET /api/v1/admin/audit` ✅

**Actions**: filter / search · export CSV

**Status**: ✅

---

### Customers › OAuth connections `/customers/oauth`

**Purpose**: Per-tenant external OAuth grants (Google / Slack / GitHub / etc.). Used when backend agents act on behalf of users.

**Data**:
- Connections list: tenant · provider · connected user · scopes granted · token expiry · status (active / expired / revoked / refresh-failed)
- Stats: total connections · expiring this week · revoked this week
- Per-connection detail: all scopes · refresh history · recent agent calls that used this connection

**APIs**:
- `GET /api/v1/oauth/connections` ✅
- `GET /api/v1/oauth/providers` ✅
- `POST /api/v1/oauth/{provider}/authorize` ✅
- `DELETE /api/v1/oauth/{provider}` ✅
- `POST /api/v1/oauth/token` ✅
- `GET /oauth/callback/{provider}` ✅ (browser flow)

**Actions**: revoke connection · trigger refresh · filter / search

**Status**: ✅

---

### Customers › Billing summary `/customers/billing`

**Purpose**: Lightweight per-tenant billing snapshot. Deep billing lives in the configured billing adapter (Stripe / Lago).

**Data**:
- Per tenant: current plan · MRR · last invoice status · payment method status
- Aggregate: total MRR · churn signals · recent plan changes

**APIs**:
- `GET /api/v1/billing/customers` ✅
- `GET /api/v1/billing/customers/{tenantId}` ✅
- `GET /api/v1/billing/meters` ✅
- `POST /api/v1/billing/customers/{tenantId}/portal` ✅

**Actions**: "Open in Stripe ↗" per tenant · upgrade plan triggers adapter's checkout flow

**Status**: ✅

---

### Setup › Adapters `/setup/adapters`

**Purpose**: Read-only inventory of every adapter slot and which adapter the fork is currently running. The one place modularity is visible (quietly).

**Data**:
- Slot list, each row: slot name (LLM gateway · Object storage · Sandbox · Billing · Notifications · Secrets · ...) · current adapter name + version · status pill · link to native admin UI when one exists
- For Tier-1 / Tier-3 slots (truly pluggable): show capability declaration (chat / embed / spend / etc.)
- For Tier-2 slots (Postgres etc.): show "swap by changing connection string"; vendor + version detected
- For Tier-4 slots (foundational): show "core; not designed for swap"

**APIs**:
- `GET /api/v1/plugins` ✅ (plugin registry — partial)
- `GET /api/v1/admin/adapters` ✅ — tier-aware adapter introspection endpoint owned by the runtime

**Actions**: open active adapter's admin UI in new tab

**Status**: ✅ for inventory; 🟡 for slot-specific typed capability accessors that still synthesize `contract_pending`

---

### Setup › Auth providers `/setup/auth-providers`

**Purpose**: Configure better-auth.

**Data**:
- Configured providers: email/password · OAuth providers (Google / GitHub / etc.) · magic links
- Trusted origins list
- Session config: lifetime / refresh / secure cookie flags
- OAuth provider keys status (present / missing — values in Secrets)

**APIs**:
- Better-auth config via dashboard (no runtime endpoint; Drizzle / config files) — **today**
- **In progress (backend agent):** auth adapter protocol `docs/adapters/protocols/auth-v1.md` landed; runtime interface at `services/runtime/internal/auth/provider.go`. When this ships, the page reads adapter capabilities (`supports_oauth_providers`, `supports_mfa`, `supports_sso`, `supports_passwordless`, `supports_magic_links`) and shows/hides sections accordingly.

**Actions**: add provider · toggle · edit trusted origins · open adapter docs

**UI impact when auth adapter lands**: page becomes adapter-aware — provider list is dynamic from `Capabilities.SupportsOAuthProviders`, MFA/SSO/magic-link rows hide when adapter declares them unsupported. Tier moves from "better-auth specific" toward "any adapter".

**Status**: ✅ for current (better-auth) · 🟡 for capability-aware version (in progress)

---

### Setup › LLM providers `/setup/llm-providers`

**Purpose**: Show what models the gateway has configured + provider key health. Thin in v1; richer data lives in LiteLLM admin.

**Data**:
- Models list (from `/api/v1/llm/models`): model id · display name · provider · cost per 1M tokens (prompt) · cost per 1M tokens (completion) · supports_streaming · supports_tools
- Provider keys status (per provider: present / missing)
- Default model

**APIs**:
- `GET /api/v1/llm/models` ✅ (thin — what we have today)
- LiteLLM admin `/model/info` `/model_group/info` `/health` (link out for context window, capabilities matrix, fallback chains, TTFT, provider health)
- **Protocol landed:** `docs/adapters/protocols/llm-chat-v1.md` ✅. Capability fields confirmed: `supports_chat`, `supports_embeddings`, `supports_streaming`, `supports_tools`, `supports_vision`, `supports_json_mode`, `supports_logprobs`, `supports_fallback_chains`, `supports_per_tenant_attribution`, `model_prefixes[]`, `max_completion_tokens`, `max_context_window`, `rate_limit_per_minute`, `default_model`, `fallback_chain_default[]`, `tokeniser_available`. Adapter extraction from `internal/llmgateway/gateway.go` is the remaining step.

**Actions**: filter / search models · click any model → API explorer pre-filled with `/api/v1/llm/chat/completions` + that model id · "Open LiteLLM admin ↗" prominent

**UI impact when adapter extraction completes**: title becomes "LLM gateway · <adapter name>"; the capability fields above drive which sub-rows render (e.g., hide fallback-chain section if `supports_fallback_chains=false`; hide tools-call test if `supports_tools=false`); model list filters by `model_prefixes[]`; per-tenant attribution toggle gated on `supports_per_tenant_attribution`. Cost page's "via X" pill source updates accordingly.

**Status**: 🟡 (protocol done; capability-aware UI pending interface extraction)

---

### Setup › Sandbox adapter `/setup/sandbox`

**Purpose**: Show current sandbox adapter + pool status. Adapter switching is env-config in v1.

**Data**:
- Current adapter (Docker / gVisor / Firecracker / e2b / Modal) — from `/sandbox/pool`
- Pool stats: warm / active / queued / idle
- Per-adapter capability matrix (cold start / GPU / isolation strength / cost)

**APIs**:
- `GET /api/v1/sandbox/pool` ✅
- Adapter selection via env (no runtime PUT in v1)
- **Sandbox adapter protocol** `sandbox-v1.md` defined. Caps: `max_timeout_s`, `supports_gpu`, `supports_network`, `supports_mounts`, `supports_streaming`, `cold_start_ms`, `image_pull_required`, `max_cpu`, `max_memory_gb`, `network_modes[]`, `allow_egress_supported`, `artifacts_upload`.

**Actions**: open adapter docs · operator changes env + restarts

**UI impact from sandbox adapter capabilities** (applies on this page AND on Build → Sandboxes playground): timeout input clamps to `max_timeout_s`; GPU toggle hidden when `supports_gpu=false`; network toggle hidden when `supports_network=false`; mount UI hidden when `supports_mounts=false`; CPU/memory inputs clamp to `max_cpu` / `max_memory_gb`; network mode dropdown driven by `network_modes[]`; "stream logs live" toggle hidden when `supports_streaming=false`; cold-start expectation pill shows `cold_start_ms`.

**Status**: ✅

---

### Setup › Webhook subscribers `/setup/webhook-subscribers`

**Purpose**: Configure outbound webhook subscribers (which events go to which endpoints).

**Data**:
- Subscriber list: event types · endpoint URL · signing key status · tenant scope (all / specific) · active/paused
- Available event types catalog
- Per-subscriber detail: signing key status · tenant scope · history

**APIs**:
- `GET /api/v1/webhooks/endpoints` ✅
- `POST /api/v1/webhooks/endpoints` ✅
- `DELETE /api/v1/webhooks/endpoints/{id}` ✅
- `POST /api/v1/webhooks/send` ✅
- Svix `/api/v1/event-type/` (link out for full catalog)

**Actions**: add subscriber · rotate signing key · pause / resume · "Open Svix dashboard ↗"

**Status**: ✅

---

### Setup › Notifications (channels) `/setup/notifications`

**Purpose**: Channel configuration (log / Resend / Postmark / Slack). Delivery audit lives in Operate → Notifications.

**Data**:
- Channels list: name · type · keys status · last test send
- Per-channel: API keys / webhook URLs · test send button

**APIs**:
- (Configuration endpoint partial — display from env config in v1)
- Test send via existing `POST /api/v1/notifications`
- **Notifications adapter protocol** `notifications-v1.md` audited 08:42. Caps: `channels[]`, `supports_html`, `supports_templates`, `supports_cc_bcc`, `tracks_delivery_status`, `supports_retry`, `max_recipients_per_message`, `max_body_size_bytes`, `rate_limit_per_minute`, `supports_metadata_passthrough`.

**Actions**: add / edit / remove channel · send test

**UI impact from notifications adapter capabilities**: channel picker constrained to `channels[]` from the adapter (email-only adapter hides SMS/push options); HTML body editor hidden when `supports_html=false`; template picker hidden when `supports_templates=false`; CC/BCC fields hidden when `supports_cc_bcc=false`; "Retry" action hidden when `supports_retry=false`; delivery-status column in Operate → Notifications hidden when `tracks_delivery_status=false`. Recipient list validates against `max_recipients_per_message`; body validates against `max_body_size_bytes`; rate-limit pill shows `rate_limit_per_minute`.

**Status**: 🟡 (display from env in v1; full CRUD UI deferred; capability gating ready when adapter introspection lands)

---

### Setup › Secrets `/setup/secrets`

**Purpose**: Secrets vault contents (names only — values never shown).

**Data**:
- Secret list: name · type · last rotated at · used-by (adapter references) · encryption envelope info

**APIs**:
- `GET /api/v1/secrets` ✅
- `GET /api/v1/secrets/{key}` ✅
- `PUT /api/v1/secrets/{key}` ✅
- `POST /api/v1/secrets/{key}/reveal` ✅
- `POST /api/v1/secrets/{key}/rotate` ✅
- `DELETE /api/v1/secrets/{key}` ✅
- **Secrets adapter protocol** `secrets-v1.md` exists; audited 08:42. Caps: `supports_versioning`, `supports_rotation`, `supports_rotation_generate`, `supports_reveal`, `supports_metadata`, `kms_backend` (label), `max_value_bytes`, `version_retention_count`, `audit_log_revealed`.

**Actions**: rotate · delete (with confirm) · reveal once (audited)

**UI impact from secrets adapter capabilities**: "Reveal" button hidden unless BOTH `supports_reveal=true` AND `audit_log_revealed=true` (the runtime requires the audit promise). "Rotate" button hidden when `supports_rotation=false`. Version history tab hidden when `supports_versioning=false` or `version_retention_count=0`. Header pill shows `kms_backend` label for operator clarity. Value input field validates against `max_value_bytes`.

**Status**: ✅

---

### Setup › Observability `/setup/observability`

**Purpose**: Configure traces / metrics / logs destinations.

**Data**:
- OTel exporter endpoint config
- Metrics scrape URL (Prometheus exposition)
- Log level
- Optional Langfuse profile toggle

**APIs**:
- Config display from env (no runtime endpoint v1)
- `GET /metrics` ✅ (Prometheus exposition)

**Actions**: edit endpoint (env-only in v1) · open Prometheus / Langfuse if shipped

**Status**: ✅ (display + link out; no in-product config UI v1)

---

### Setup › Billing adapter `/setup/billing-adapter`

**Purpose**: Pick + configure the billing provider (Stripe / Lago / none).

**Data**:
- Adapter selection · API keys status · webhook endpoint · default plan map
- Plan map: BackAI internal plan ids → provider price ids

**APIs**:
- `GET /api/v1/billing/customers` ✅ (wraps the adapter)
- Adapter selection via env
- **Billing adapter protocol** `billing-v1.md` audited 08:42. Caps: `supports_customers`, `supports_subscriptions`, `supports_metered_billing`, `supports_customer_portal`, `supports_usage_reporting`, `supports_webhook_verification`, `default_currency`, `is_stub`, `admin_dashboard_url`.

**Actions**: switch adapter (env) · edit plan map · "Open Stripe / Lago dashboard ↗"

**UI impact from billing adapter capabilities**: "Open admin ↗" link target driven by `admin_dashboard_url` (Stripe URL, Lago URL, etc.). Sections hide when their cap is false: subscriptions block (`supports_subscriptions`), metered-usage tile (`supports_metered_billing`), portal-link button (`supports_customer_portal`), usage reporting (`supports_usage_reporting`). When `is_stub=true`, display a clear "stub / dev mode" banner so operators don't think billing is wired to a real provider.

**Status**: ✅

---

### Setup › Deploy targets `/setup/deploy-targets`

**Purpose**: Informational view of deploy targets (Railway / Fly / Render / Helm / Compose). v1 has no in-product provisioning.

**Data**:
- Available targets · per-target status (configured / not) · last deploy at · last deploy status · per-target env / secrets

**APIs**:
- Provider CLIs invoked from operator's local machine — no runtime admin endpoint
- Status display from env config

**Actions**: open provider's own console (link out) · operator runs deploy CLI locally

**Status**: ✅ (informational)

---

### Brand `/brand` (pinned)

**Purpose**: Show the operator what's currently in `brand.yaml`. Edit happens in the file directly; reload picks it up.

**Data**:
- Brand name · primary + accent colors (with swatches) · logo preview · favicon preview · custom domain (if configured)

**APIs**:
- Parse of `brand.yaml` from fork root (no runtime endpoint v1; could be a thin `GET /api/v1/admin/brand` later)

**Actions**: "Edit brand.yaml" (copies file path for the operator to open in editor) · "Reload from file" if hot-reload is wired

**Status**: 🟡 (read-only display in v1; in-product editor deferred)

---

## What v1 explicitly does NOT include (and where to go)

| Concept | v1 status | Where instead |
|---|---|---|
| Inline AI chat on every page | Deferred | — |
| Plugin marketplace / browse / install UI | Deferred | Plugin registry exists; UI is hidden in v1 |
| Adapter swap wizard | Deferred | Env var + restart in v1; later UI |
| Widget-level plugin injection into existing pages | Deferred | Plugin tabs only in v1 |
| Page-replacement plugins | Deferred | — |
| Voice / WebRTC module surfaces | Deferred | — |
| Multi-region orchestration UI | Out of scope v1 | — |
| In-product code editor / IDE | Out of scope | Operator uses their IDE on the fork |
| Visual workflow / drag-drop builder | Out of scope | Code-first identity |
| Dedicated GDPR Compliance page | Deferred | Export / erase actions inline in Customers → Members |
| Rate limits dedicated page | Deferred | LiteLLM admin link-out + Logs filter for 429s |
| Inbound webhooks observability page | Deferred | Logs filter; svix dashboard for outbound |
| Guardrails admin UI | Deferred | Service active; config via env (regex default, optional Presidio sidecar) |
| Reasoner / tool cross-agent analytics | Deferred | Listing only in v1; cost-per-reasoner via Operate → Cost group-by |
| Models page (separate from LLM providers) | Cut | Folded into Setup → LLM providers + Build → API explorer |

---

## How this doc stays current (watchdog protocol)

> **API contract audit in progress.** Backend agent is reviewing all API contracts. Expect renames, response-shape edits, deprecations across the next several passes. The watchdog now tracks edits (not just additions) so contract drift surfaces immediately. Pages with status ✅ today may demote to 🟡 if their backing endpoint reshapes.

A background watchdog is monitoring at `/tmp/backai-ui-watchdog.log`:

| What | Watched at | What triggers an update to this doc |
|---|---|---|
| Runtime HTTP routes (add / remove / rename) | `services/runtime/internal/**/*.go` route grep every 45s | New `/api/v1/*` → promote 🟡 → ✅; removed route → demote ✅ → 🟡 or cut page |
| Handler file hashes | `services/runtime/internal/server/*.go` shasum | Response-shape edit → re-verify the page's "Data" inventory against new shape |
| OpenAPI generator hashes | `services/runtime/internal/openapi/*.go` shasum | Schema drift → check whether API explorer / SDK download metadata still aligns |
| Protocol doc filenames | `docs/adapters/protocols/*.md` listing | New protocol per slot → adds an adapter slot to Setup → Adapters |
| Protocol doc body hashes | shasum per `.md` in `docs/adapters/` | Edit to an existing protocol → capability fields may change; re-check affected pages |
| Adapter directories | `find services/runtime/internal -type d -name adapters` | New adapter dir or `interface.go` → impacts what shows as available in Setup → Adapters |

When the watchdog log records a change, the next pass should:
1. Re-pull routes via `cat /tmp/backai-routes-snapshot.txt`
2. Diff against this doc's API references
3. Update the affected page's status / APIs / data inventory
4. Note new pages if a whole subsystem appeared

The watchdog does **not** touch the dashboard codebase or backend code. It's read-only.

---

## Open questions / things to confirm with backend agent (parking lot)

These are deliberately left unresolved so I can sync with backend progress instead of speculating.

### In progress — all eight slots have protocol contracts (audit pass ongoing)

| Slot | Protocol file | Last edit | Capability surface | UI surfaces affected |
|---|---|---|---|---|
| **Auth** | `auth-v1.md` | 08:43 (audited 2×) | `supports_oauth_providers[]`, `supports_magic_links`, `supports_passwordless`, `supports_mfa`, `supports_sso`, `session_lifetime_seconds`, `supports_token_introspection` | Setup → Auth providers · Customers → Sessions (active-sessions tab capability-gated — no session-list endpoint in protocol) · Customers → Members (routes through adapter, not direct Drizzle) |
| **LLM chat gateway** | `llm-chat-v1.md` | 08:43 (audited 2×) | `supports_chat / embeddings / streaming / tools / vision / json_mode / logprobs / fallback_chains / per_tenant_attribution`, `model_prefixes[]`, `max_completion_tokens`, `max_context_window`, `rate_limit_per_minute`, `default_model`, `fallback_chain_default[]`, `tokeniser_available` | Setup → LLM providers · Operate → Cost ("via X" pill source) · Operate → Cache · API explorer auth-context selector |
| **Storage** | `storage-v1.md` | 08:41 (audited 2×) | `max_object_size_bytes`, `single_put_max_bytes`, `presign_ttl_max_seconds`, `supports_range_requests`, `supports_metadata_headers`, `supports_signed_uploads`, `max_keys_per_list`, `bucket_required` | Build → Data → Storage |
| **Billing** | `billing-v1.md` | 08:43 (audited 2×) | `supports_customers / subscriptions / metered_billing / customer_portal / usage_reporting / webhook_verification`, `default_currency`, `is_stub`, `admin_dashboard_url` | Setup → Billing adapter · Customers → Billing summary |
| **Notifications** | `notifications-v1.md` | 08:42 | `channels[]`, `supports_html / templates / cc_bcc / retry / metadata_passthrough`, `tracks_delivery_status`, `max_recipients_per_message`, `max_body_size_bytes`, `rate_limit_per_minute` | Setup → Notifications · Operate → Notifications (delivery-status column gate) |
| **Secrets** | `secrets-v1.md` | 08:42 | `supports_versioning / rotation / rotation_generate / reveal / metadata`, `kms_backend`, `max_value_bytes`, `version_retention_count`, `audit_log_revealed` | Setup → Secrets (Reveal button requires `supports_reveal && audit_log_revealed`) |
| **Sandbox** | `sandbox-v1.md` | 07:21 (stable) | `max_timeout_s`, `supports_gpu / network / mounts / streaming`, `cold_start_ms`, `image_pull_required`, `max_cpu`, `max_memory_gb`, `network_modes[]`, `allow_egress_supported`, `artifacts_upload` | Setup → Sandbox adapter · Build → Sandboxes (input clamping, toggle visibility per cap) · Operate → Sandbox runs |
| **Multimodal** | `multimodal-v1.md` | 08:43 (audit) | `supports_tts / stt / image_generation / image_edit / image_variation`, `model_prefixes[]`, **`supports_streaming_tts` (advisory-only in v1 — must be `false`; reserved for v2)**, `default_voice`, `max_input_chars`, `audio_formats[]` | API explorer (verb-by-verb endpoint visibility) · future multimodal playground if added |

### Still open

1. **Capability declaration object — universal shape across slots** — auth defines `Capabilities` struct cleanly. Confirm same pattern across LLM gateway / sandbox / storage / billing / notifications / secrets so the dashboard reads them uniformly.
2. **Trace browser endpoint** — is a `GET /api/v1/traces` planned, or do operators always link out to an external trace store?
3. **Errors aggregation** — will there be a `GET /api/v1/admin/errors` for dedup/grouping or do we stay client-side on `/logs`?
4. **Brand admin endpoint** — is `GET / PUT /api/v1/admin/brand` planned, or stays as file edit?
5. **Plugin manifest schema** — what does the dashboard plugin manifest look like, for the "plugin tabs inject into parent group" behavior?
6. **Auth session-listing capability** — does any concrete auth adapter expose session enumeration? Determines whether Customers → Sessions "active sessions" tab ever has live data outside better-auth.

Items move from "Still open" → "In progress" → into page specs above as the backend agent answers them via shipped endpoints.

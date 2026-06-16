# BackAI Dashboard — Information Architecture & Page Content Spec (v1)

## What this doc is

A content/data brief for the UI/UX designer of the v1 BackAI operator dashboard.

It tells you:
- The sidebar hierarchy and why each group exists
- Every page that ships in v1
- What data each page must show
- What actions each page must support
- How pages drill into each other
- Edge / empty-state notes

It does **not** prescribe layout, components, typography, color, visual hierarchy, density, or interactions. Use your judgment for design — this doc is the substance, you bring the form.

---

## Audience for the dashboard itself

- **Primary user**: developers operating their own fork of BackAI
- **Mental model reference**: Supabase Studio — control center for a forkable backend
- **Tolerance**: developer-grade information density is fine, even desirable
- **Theme**: dark mode is the primary theme

---

## Terminology (read before reading the rest)

BackAI has three different "people" concepts. The dashboard is designed for one of them and shows the others as data. This is the most important distinction to internalize.

| Term | Who they are | Where they live | Do they see this dashboard? |
|---|---|---|---|
| **Operator** | The developer who forked BackAI and runs the platform | Logged into this dashboard | **Yes — this dashboard is built for them** |
| **Tenant** | One of the operator's customers (an account / workspace / organization in the operator's SaaS) | Created when someone signs up to the customer-app | No — they see the customer-app, not this dashboard |
| **Member** | A human user inside a tenant. In solo mode 1 tenant = 1 member. In team mode 1 tenant can have many members. | Created when a user signs up; can be invited into existing tenants | No |

### Concretely:

- A customer signs up via the **customer-app** (`apps/customer-app/`) at `localhost:34000`. That signup atomically creates a `suite_tenants` row, a `suite_users` row, and a `suite_memberships` row joining them. The user is now a "member" of a new "tenant."
- The customer-app **never shows the word "tenant"** to its end users. To them, the tenant is "my account" or "my workspace."
- The **operator** opens this dashboard at `localhost:33000` and sees their customers in `CUSTOMERS / Tenants`. To the operator, every signed-up customer = one tenant.
- The **tenant switcher** at the top of the dashboard lets the operator filter the whole dashboard to one specific customer ("show me only acme's runs and costs").
- **Multi-tenancy** as a product feature means: every tenant is fully isolated — its own API keys, Postgres RLS, budget, cost ledger, audit, memory scope, storage prefix.

### Why this matters for the design

- "Tenant" appears throughout the dashboard. Treat it as a first-class noun in the operator's vocabulary. Don't soften it to "customer" or "user" — those words mean different things.
- The dashboard is **single-operator, multi-tenant**: the operator is unique (one fork = one operator's deployment); their tenants are many.
- The tenant switcher is the most important context control in the entire UI. Whatever it's set to should always be visually obvious.

---

---

## Product philosophy that shapes the IA

These principles are why pages are grouped the way they are. Internalize them; they answer most "should this go here?" questions.

0. **MVP-honest. Show only what we can actually serve from real endpoints.** Every page in this spec is grounded in an existing route in `services/runtime/internal/server/` or a documented OSS admin endpoint. Where the data doesn't exist today, the page is either thinned to what does or cut entirely, with a link out to the OSS service's own dashboard. We don't ship UI for data we can't get.
1. **Dashboard = operations on YOUR fork. Docs = reference for anyone.** If a page's value is the same whether the operator is signed in or not, it's docs and belongs on `docs.backai.dev`, not in the dashboard. The dashboard only hosts surfaces that need the operator's session, key, endpoints, data, or running state.
2. **Group by the operator's job, not by feature category.** Each sidebar group answers a question the dev is actively asking.
3. **Daily > monthly.** Observability surfaces sit higher than one-time configuration.
4. **One-time setup ≠ daily observability.** Even when they're about the same subject. Webhook delivery (daily) and webhook subscriber config (one-time) live in different sections.
5. **Modularity is the architecture, communicated quietly.** Every layer is an adapter; the dashboard reflects this through a Setup → Adapters page and subtle "via <X>" pills on adapter-backed pages. No prominent OSS branding.
6. **Console reads, code writes.** The dashboard is overwhelmingly an observability and light-config surface. Structural changes (new agents, schema, modules) happen in code, not in the console.
7. **Tenant scope is orthogonal to navigation.** The tenant switcher re-scopes content; the sidebar structure does not change.
8. **Drill from observability to source.** Every metric → list → detail → "open in adapter" should be one path away from the underlying truth.

---

## Cross-cutting elements (present on all pages)

These behave consistently across every page. Designer should treat them as the chrome.

### Top bar
- Product wordmark (left)
- Tenant scope switcher (next to wordmark)
- Breadcrumbs reflecting current location
- Universal search / command-palette trigger
- Theme toggle
- Profile menu
- Alerts/notifications indicator

### Tenant scope switcher
- Two modes: **"Platform"** (cross-tenant view) and **"Tenant: <name>"** (scoped to one tenant)
- Persistent control; placement should reinforce that scope affects what's shown
- A clear visual indicator when in tenant scope, with one-click revert to platform-wide
- Pages that **respect scope**: most of Operate, most of Customers, some Build views (Agents, Data)
- Pages that **ignore scope**: Develop, Setup, Brand

### Command palette
- Global trigger (keyboard shortcut + clickable from top bar)
- Categories to surface:
  - Jump to: any page, tenant, agent, run, key
  - Actions: common mutations (issue key, set budget, test agent, etc.)
  - Docs: search the docs site
  - External: link out to adapter admin UIs (LiteLLM admin, MinIO console, Svix dashboard, etc.)

### Mutation pattern
- Create/edit flows open as right-side drawers, not modals (preserves list context behind)
- Every mutation produces an audit log entry
- Toast confirmation references the audit entry; offers undo where reversible
- Every form should expose a **"Show as code"** toggle revealing the equivalent SDK/CLI call. Teaches the SDK; lets devs skip the UI next time.

### Adapter pill
- Subtle indicator on pages backed by an adapter slot (Cost, Runs, Storage, Webhook deliveries, Queue, etc.)
- Format: "via <adapter>" (e.g., "via LiteLLM")
- Click reveals: view adapter docs, open adapter admin in new tab, "change adapter" link into Setup → Adapters
- This is how modularity is communicated. Quietly.

### Live data
- Real-time pages (Home, Cost, Runs, Errors, Queue) pull live updates via WebSocket subscriptions
- Update animations should be subtle — values tick up; no flashing

### Empty states
- Every empty state should contain:
  - 1-line explanation
  - "What to do" code snippet (curl + SDK)
  - Optional "seed demo data" button
  - Link to relevant docs

---

## Sidebar hierarchy

The primary navigation. Groups are collapsible. Default-open: **Operate, Build**. Default-collapsed: **Customers, Setup**. **Brand** pinned at the top level (not in a group).

```
OVERVIEW
  Home                  (developer welcome + KPIs + activity)

OPERATE
  Runs                  (agent + handler runs)
  Cost
  Errors
  Traces
  Queue
  Cache
  Sandbox runs          (we have observability endpoints — surface them)
  Webhook deliveries
  Notifications
  Approvals
  Activity
  Health                (thin in v1 — service status pills only)
  Logs

BUILD
  Agents
  Reasoners             (thin in v1 — per-agent listing, no cross-agent analytics)
  Tools                 (thin in v1 — listing + test invoke, no analytics)
  Skills (MCP)
  Harnesses
  Crons
  Sandboxes             (playground + pool status; observability lives in Operate)
  Modules
  Data
    Tables
    SQL
    Memory
    Storage
    Search
  Feature flags
  API explorer          (the only "dev tool" in the dashboard, because it
                         uses YOUR fork's auth, endpoints, and schema)
  Shipwright            (only if shipping in v1)

CUSTOMERS
  Tenants
  API keys
  Members
  Sessions
  Budgets
  Audit log
  Activity log
  OAuth connections
  Billing summary

SETUP
  Adapters              (thin in v1 — read-only inventory from env config)
  Auth providers
  LLM providers         (thin in v1 — uses GET /api/v1/llm/models only)
  Sandbox adapter
  Webhook subscribers
  Notifications
  Secrets
  Observability
  Billing adapter
  Deploy targets

BRAND                   (thin in v1 — read-only display of brand.yaml)

PLUGINS  (hidden in v1 unless plugins are installed; plugin items inject into
          their declared parent group rather than living in this silo)
```

### What v1 dropped (and where to go instead)

| Was in spec | Status in v1 | Where to go for that capability |
|---|---|---|
| Operate → Inbound webhooks (observability) | Cut | Filter Operate → Logs by source slug, or open the recipient handler's logs |
| Setup → Inbound webhooks (config) | Cut | Slugs configured via env / workload module YAML in v1; UI later |
| Operate → Rate limits (observability) | Cut | Filter Operate → Logs for 429s; open LiteLLM admin for per-key headroom |
| Setup → Guardrails | Cut | Service is active (regex default); config via env vars in v1 (`AF_STACK_GUARDRAILS_ENABLED`, `AF_STACK_PII_PROVIDER`, Presidio URLs). Rule editing UI later. |
| Build → Models | Cut | Replaced by enriched Setup → LLM providers; deeper testing via Build → API explorer |
| Customers → Compliance | Cut | GDPR / CCPA out of scope for v1 |

The principle for these cuts: **the data either requires backend work we're not doing in v1, or the OSS service already has a dashboard we can link to**.

### What does NOT live in the dashboard

Per principle #1 (dashboard = operations on your fork), pure reference material (SDK docs, CLI reference, API annotations, integration recipes, adapter/module/plugin authoring guides) does not live in the dashboard. The dashboard hosts only surfaces that need the operator's session, key, endpoints, data, or running state. Reference material lives elsewhere and the dashboard does not link to it.

### Why each group exists

| Group | Operator's question | Why this position |
|---|---|---|
| Overview | "Is my fork healthy AND how do I make my first call?" | First thing they look at on landing. Absorbs the dev welcome experience. |
| Operate | "What's happening in my running fork right now?" | Daily firefight. Top working group because operators check it constantly. |
| Build | "Where's my code surface?" | Their agents, data, modules, plus the API explorer for testing endpoints. |
| Customers | "How are my tenants doing?" | Multi-tenancy management. Weekly, not daily. |
| Setup | "How is the platform itself configured?" | One-time configuration. Collapsed because operators touch it rarely. |
| Brand | "Make it mine." | Pinned at top level because it's the operator's identity moment. |

---

## Page specifications

For each page below: **purpose** (why it exists), **data displayed** (what to show), **actions** (mutations available), and **notes** (edge cases, empty states, drill paths).

---

### OVERVIEW › Home

**Purpose**: The first page operators open. Two jobs: (a) for new operators, welcome them as developers and get them to their first successful call. (b) for established operators, the 5-second health and activity check.

**Data displayed**:

#### Welcome block (prominent for new operators; collapsible to a thin card after first dismissal)

- Greeting using the operator's name + the fork's brand name
- **Your stack** read-only summary
  - One row per adapter slot: LLM gateway, agent runtime, database, object storage, job queue, webhooks out, auth, billing, sandbox, notifications
  - Each row shows: slot name and currently configured adapter (e.g., "LLM gateway · LiteLLM v1.40")
  - Tiny footnote: "All swappable — see Setup → Adapters"
- **Your dev tenant key**
  - Alias, masked key with reveal toggle, rpm / tpm / monthly budget caps
  - Link: "Issue another →" (opens API keys drawer)
- **Try it now** — one snippet with a language toggle (curl / TypeScript / Python / Go)
  - Snippet is pre-filled with this fork's runtime URL and the operator's dev tenant key — runs as-is, no substitution needed
  - Copy button
  - Suggested default snippet: chat through the LLM gateway. Alternates available via a small inline selector: "Call an agent", "Embed the chat widget", "Run a sandbox", "Upload to storage"
- **Next steps** link list
  - "Add an agent" → docs link
  - "Set a budget" → opens Customers → Budgets
  - "Open API explorer" → Build → API explorer
  - "Read the SDK guide" → docs link
- Dismiss button — collapses this block into a compact "Your key + your stack" card that stays visible thereafter

#### Status block (always visible)

- **KPI strip** (live tiles)
  - Requests per minute (last 5 min average)
  - Error rate % (last 1h)
  - Cost today (USD)
  - Cost month-to-date (USD)
  - Queue depth
  - Currently running runs
  - Failed runs (last 24h)
  - % of budget consumed across all tenants
- **Recent activity feed** (last ~20 entries across the platform)
  - Each entry has: timestamp, event type icon, summary text, optional drill link
  - Event types include: run completed / failed, deploy, tenant created, budget threshold crossed, configuration changed, error
- **Stack status row**
  - Health pill per backing service (postgres, litellm, agentfield, river, svix, minio, etc.)
  - Each pill shows: name, version, status dot, last-checked timestamp

#### Quick actions row (always visible)

- "Issue an API key"
- "Add a tenant"
- "Test an agent"
- "Open API explorer"

**Actions**:
- Reveal key, copy snippet, switch snippet language, dismiss welcome block
- Click any KPI tile → drill into the appropriate Operate sub-page filtered to that metric
- Click activity entry → opens related entity in a drawer or page
- Click quick action card → opens its flow (typically a drawer)
- Click any stack-status pill → opens that service's status detail

**Notes**:
- The dismiss state of the welcome block is the only piece of UI state the dashboard remembers per-operator. After dismissal, the compact key + stack card stays visible at the side or as a thin section, but the prominent first-call experience is gone.
- Live: KPIs and the activity feed update in real-time via WebSocket
- **Empty data state** (no calls yet): KPIs show zeroes with a subtle hint pointing at the "Try it now" snippet above.

---

### OPERATE › Runs

**Purpose**: Inspect every run that's happened on the platform. Debug agent and handler executions.

**Data displayed**:
- **Filter bar**: time range, agent (multi-select), reasoner, tenant (multi-select), status (running / succeeded / failed / timeout), cost range, search by run id
- **Table columns**: timestamp, agent.reasoner, tenant, status, duration, cost (USD), token count, trigger source (HTTP / webhook / job / cron)
- **Default sort**: timestamp descending
- **Live**: new runs appear at the top in real-time

**Detail drawer** (opens when a row is selected):
- Run id
- Agent, reasoner, tenant, model used
- Status, duration, cost, tokens in/out, cache hit/miss
- Input payload (collapsible JSON)
- Output payload (collapsible JSON)
- Reasoner path (sequence of reasoners that fired)
- Tool calls / sub-agent calls list (each with timing and cost)
- Errors / stack trace (if failed)
- Audit entry reference
- Adapter pill linking to "open full DAG in AgentField"

**Actions**:
- Filter, search, sort
- Re-run (copies input to a new playground run)
- Cancel (if still running)
- Drill into related entities (tenant, agent, error)

**Notes**:
- **Empty state**: "No runs yet — try the quickstart" with snippet.
- URL encodes filters so views are shareable.

---

### OPERATE › Cost

**Purpose**: Critical USP page. Spend awareness, budget control, forecast, and the value of caching. The page that proves BackAI is a real backend, not a wrapper.

**Data displayed**:
- **Scope controls**: tenant filter (or "All"), time range (24h / 7d / 30d / 90d / custom), group-by (model / agent / reasoner / tenant / day)
- **Main spend chart**: spend over time, stacked by the chosen group-by dimension
- **KPI tiles**
  - Spent today
  - Spent this month
  - Forecast end-of-month at current rate (with delta vs. budget)
  - Cache savings (USD + hit-rate %)
  - Average request cost (with p99)
- **Top spenders table** (configurable column — tenants, agents, models, reasoners)
  - Name, today total, period total, trend sparkline, drill link
- **Top expensive runs table**
  - Run id, agent.reasoner, cost, tokens, when, drill to Runs detail
- **Budgets snapshot**
  - Per-tenant: cap, used (USD + %), alert threshold, status (ok / near / over)
  - Drills into Budgets editor
- **Cache effectiveness widget**
  - Hit rate
  - Top cached prompts
  - Estimated savings
- **Per-model unit economics card**
  - Average cost per call by model

**Actions**:
- Change filter / group-by / time range
- Set a budget (opens drawer)
- Drill from any expensive row to Runs
- Adapter pill links to LiteLLM admin

**Notes**:
- Live. Values tick up during real-time activity.
- **Empty state**: demo data with banner "send your first chat to see real cost flow."
- This page is the dashboard's most polished surface. Designer should treat it as the marquee.

---

### OPERATE › Errors

**Purpose**: Triage failures across runs, jobs, handlers, and webhooks. Active firefight surface.

**Data displayed**:
- **Filter bar**: source (runs / jobs / handlers / outbound webhooks / inbound webhooks), severity, tenant, agent, time range, status (active / muted / resolved)
- **Error list**: timestamp, source, summary message, count (if recurring), tenant, agent/job kind, last seen
- **Detail drawer per error**
  - Full stack / payload
  - Sample run or job id with drill
  - Suggested fix link (if the platform recognizes the pattern)
  - Audit references
  - Mute / resolve buttons

**Actions**:
- Mute (with reason + expiry)
- Mark resolved
- Bulk mute
- Drill to source entity (Runs, Queue, Webhook deliveries)

---

### OPERATE › Traces

**Purpose**: Span tree for any request. Performance debugging.

**Data displayed**:
- **Filter bar**: trace id, agent, time range
- **Trace list**: trace id, root span name, duration, span count, status
- **Trace detail view**
  - Hierarchical span tree
  - Per span: name, duration, attributes (model, tokens, tenant, etc.), status
  - Critical path highlighted
- Link to "Open in Langfuse" when the observability profile is enabled

**Actions**:
- Search by trace id
- Drill into spans
- Copy span attributes

**Notes**:
- v1 ships a minimal own trace viewer. Deep trace exploration is a link-out.

---

### OPERATE › Queue

**Purpose**: Status of the async job queue.

**Data displayed**:
- **Counts by status**: pending, running, succeeded (today), failed (today), retrying, dead-lettered
- **Latency tiles**: p50 / p95 / p99 job pickup time; p50 / p95 / p99 job duration
- **Job kind breakdown**: per kind — count, average duration, error rate
- **Job list**: id, kind, status, attempts, queued-at, last error (if any), tenant
- **Detail drawer per job**
  - Full payload
  - Attempts history with timestamps and errors
  - Last error stack
  - Related run or handler entity

**Actions**:
- Retry failed job
- Send to dead-letter
- Filter, search
- Adapter pill links to River UI for deeper queue introspection

---

### OPERATE › Webhook deliveries

**Purpose**: Observability for outbound webhooks — what fired, what succeeded, what failed.

**Data displayed**:
- **Filter bar**: event type, endpoint, tenant, status (delivered / failed / retrying), time range
- **Deliveries list**: timestamp, event type, endpoint, tenant, status, attempts, last response code
- **Detail drawer per delivery**
  - Full event payload
  - Outbound request headers
  - Response body and headers
  - Retry schedule (next attempt + remaining)

**Actions**:
- Manually replay a delivery
- Filter, search
- Adapter pill links to Svix dashboard for replay archive

**Notes**:
- Subscriber configuration lives in Setup → Webhook subscribers, **not** here. This page is purely observability.

---

### OPERATE › Cache

**Purpose**: LLM response cache effectiveness.

**Data displayed**:
- Overall hit rate (today, 7d, 30d) and savings (USD)
- Top cached prompts by hit count
- Top misses by cost
- Cache size, expiry policy summary
- Per-tenant hit-rate breakdown

**Actions**:
- Flush all cache (confirm modal)
- Flush by tenant
- Flush by prompt hash

---

### BUILD › Agents (registry)

**Purpose**: List every agent the runtime knows about. Entry point to agent detail and playground.

**Data displayed**:
- **Per agent row**
  - Name
  - Reasoner count
  - Calls today
  - Calls last 7d
  - Cost today
  - Error rate
  - Container status (healthy / unhealthy / starting)
  - Version
- Filter, search

**Actions**:
- Click agent → opens detail page
- Search

**Notes**:
- No CRUD in v1 — agents are defined in code, not the console.

---

### BUILD › Agents › Detail (page per agent)

**Purpose**: Inspect a specific agent's reasoners, runs, cost, and configuration.

**Data displayed**:
- **Header**: agent name, container image, version, status pill
- **Reasoners table**
  - Name
  - Kind (entry / parallel / nested / synthesis)
  - Schema (input, output)
  - Tool list
  - Cost average per call
  - Error rate
  - Calls today
- **Recent runs table** (filtered to this agent)
- **Cost trend chart** for this agent over time
- **MCP servers configured for this agent**
- **Capabilities declared** (available harnesses, models used, etc.)
- Adapter pill linking to "open in AgentField" for the source definition

**Actions**:
- Click reasoner → drilldown to per-reasoner view (runs filtered, schema visible)
- Test in playground (jump to Playground sub-view)
- Click run → opens Run detail drawer

---

### BUILD › Agents › Playground (sub-view per agent)

**Purpose**: Test an agent without writing code.

**Data displayed**:
- Input form generated from the entry reasoner's schema
- Live streaming output
- Per-step cost and timing
- Reasoner trace expanded inline as the agent runs
- Run id link to Runs detail once complete

**Actions**:
- Submit run
- Cancel mid-run
- Save run id, copy result, re-run with modifications

---

### BUILD › Data › Tables

**Purpose**: Browse every Postgres table (afstack + agentfield schemas).

**Data displayed**:
- **Left**: schemas list → tables list with row counts
- **Right** (selected table)
  - Structure: columns, types, indexes, constraints
  - Data preview: paged rows with filtering
  - Tabs: Data, Structure, Policies (RLS), Indexes

**Actions**:
- Filter by column
- Sort
- Paged scroll
- Read-only (no inline mutations)

---

### BUILD › Data › SQL

**Purpose**: Ad-hoc read-only SQL queries.

**Data displayed**:
- SQL editor (code-mirror)
- Saved snippets / query history
- Results table below the editor
- Execution timing

**Actions**:
- Run query (read-only enforced server-side)
- Save snippet
- Export results (CSV, JSON)

---

### BUILD › Data › Memory

**Purpose**: Inspect and debug the per-scope vector memory store used by agents.

**Data displayed**:
- **Scope picker**: tenant / agent / session / global
- **Entries list**: key, kind, created at, size, sample value
- **Detail panel**: full value, embedding vector summary (model used, dim count)
- **Semantic search test**: enter query → top-k results with cosine scores

**Actions**:
- Delete entry
- Run semantic search test
- Export

---

### BUILD › Data › Storage

**Purpose**: Browse objects per tenant / bucket.

**Data displayed**:
- **Bucket list**: name, file count, total size, per-tenant usage breakdown
- **Bucket contents**: file list (key, size, content type, uploaded by, uploaded at)
- **File detail**: metadata, presigned URL preview, presigned URL generator

**Actions**:
- Delete file
- Generate presigned URL
- Adapter pill links to MinIO console for advanced ops

---

### BUILD › Data › Search

**Purpose**: Inspect Postgres FTS indexes used for in-app search.

**Data displayed**:
- Index list with source table and column(s)
- Sample queries
- Performance stats per index

**Actions**:
- Test query against an index

---

### BUILD › Modules

**Purpose**: Workload modules — domain backend code mounted under `/workload/<id>/`. Enable / disable installed modules.

**Data displayed**:
- **Installed modules list**
  - Id
  - Name
  - Version
  - Status (enabled / disabled)
  - Mounted routes
  - Migrations status
  - Source path
- **Per-module detail**
  - Manifest contents
  - Declared routes
  - Declared crons
  - Declared jobs
  - Env vars used

**Actions**:
- Enable / disable (writes config; may require runtime restart)
- Open source path in the fork

**Notes**:
- No browseable marketplace in v1.
- No author-your-own scaffolding promoted in v1.

---

### BUILD › Skills (MCP)

**Purpose**: MCP servers configured per agent.

**Data displayed**:
- **Per agent**: list of configured MCP servers — name, transport (stdio / SSE / HTTP), tools exposed
- **Per server**: tool list with schemas, reachability status

**Actions**:
- Test a tool call (form generated from tool schema)
- Open server's own UI if its manifest declares a URL

---

### BUILD › Feature flags

**Purpose**: Runtime feature flags.

**Data displayed**:
- **Flag list**: key, description, value (boolean / number / string / variant), rollout % (if variant), tenant overrides count, last changed
- **Per-flag detail**
  - History of changes
  - Per-tenant overrides

**Actions**:
- Toggle
- Set rollout %
- Add tenant override
- Audit entry on every change

---

### BUILD › API explorer

**Purpose**: Try every endpoint of YOUR running fork without writing client code. This is the only "dev tool" page in the dashboard because it uses YOUR fork's auth, endpoints, schema, and data — operational, not reference. Full annotated API reference lives at `docs.backai.dev/api`.

**Data displayed**:
- Embedded Scalar UI rendering the live OpenAPI 3.1 spec served by the runtime
- Auth selector at the top of the page
  - "Operator session" (default — uses the dashboard login)
  - "Tenant: <picker>" (uses a tenant API key for tenant-scoped endpoints)
- Endpoint groups by tag (agents, llm, runs, tenants, keys, cost, sandbox, storage, memory, audit, etc.)
- Top-right action: **"Download schema ▾"** menu with options
  - OpenAPI 3.1 JSON
  - OpenAPI 3.1 YAML
  - TypeScript types (`.d.ts`)
  - Python Pydantic models (`.py`)
  - Go structs (`.go`)
- Top-right link: "Full API reference → docs.backai.dev/api"

**Actions**:
- Try-it on any endpoint
- Switch auth context (operator session ↔ tenant key)
- Download schema or generated types
- Copy any generated request as curl / TS / Python

**Notes**:
- The Scalar embed must feel native — matching theme, no double scrollbars, consistent fonts.
- "Try it" requests should flow through the same audit log as production traffic, tagged as having originated from the API explorer.

---

### CUSTOMERS › Tenants

**Purpose**: List and manage tenants.

**List data**:
- **Per tenant row**: name, id, created at, members count, API keys count, cost today, cost month-to-date, budget consumed %, status (active / suspended)
- Filter, search

**Detail page** (also reachable from the tenant switcher):
- **Header**: name, id, status
- **Tabs**
  - Overview: cost summary, recent runs, recent errors, budget status
  - Members: user list with role
  - API keys: list with alias, status, rpm/tpm/budget, last used
  - Usage: requests over time, top agents/models
  - Audit: filtered audit log
  - Settings: name, metadata, suspend toggle, delete

**Actions**:
- Create tenant (drawer)
- Suspend / resume
- Delete (with cascade summary)

---

### CUSTOMERS › API keys

**Purpose**: Issue, rotate, revoke API keys across all tenants.

**Data displayed**:
- **Key list**: alias, tenant, masked id, status (active / revoked / expired), rpm/tpm/budget cap, used budget %, last used at, created at
- Filter by tenant, status

**Issue drawer**:
- Tenant selector
- Alias
- Budget cap (monthly USD)
- Rate limits (rpm, tpm)
- Expiration (optional)
- Scopes (optional)
- "Show as code" toggle reveals SDK call
- One-time secret reveal after creation

**Actions**:
- Issue
- Rotate
- Revoke
- Filter, search

---

### CUSTOMERS › Members

**Purpose**: Users across tenants.

**Data displayed**:
- **Member list**: name, email, tenant(s), role(s), last login, MFA status, provider (email / OAuth)
- **Per member detail**: tenant memberships, audit, recent sessions

**Actions**:
- Invite user
- Remove from tenant
- Disable account

---

### CUSTOMERS › Budgets

**Purpose**: Per-tenant budget caps and alert thresholds.

**Data displayed**:
- **Budget list**: tenant, monthly cap (USD), period start, used (USD + %), alert threshold %, status (ok / near / over), last alert sent
- **Per-budget edit drawer**: cap, threshold, alert recipient

**Actions**:
- Set / update / delete budget
- Test alert delivery

---

### CUSTOMERS › Audit log

**Purpose**: Full provenance feed of every mutation across the platform.

**Data displayed**:
- **Filter bar**: actor (user / system / API key), entity type, action, tenant, time range
- **Entries list**: timestamp, actor, action, entity type, entity id, before/after diff summary, IP, user-agent
- **Detail panel**: full diff JSON

**Actions**:
- Filter, search
- Export to CSV

---

### CUSTOMERS › Billing summary

**Purpose**: Lightweight per-tenant billing snapshot. Deep billing lives in the configured billing adapter (Stripe, Lago).

**Data displayed**:
- **Per tenant**: current plan, MRR, last invoice status, payment method status
- **Aggregate**: total MRR, churn signals, recent plan changes

**Actions**:
- "Open in Stripe" link out per tenant
- Plan upgrade triggers the adapter's checkout flow

---

### SETUP › Adapters

**Purpose**: Read-only inventory of every adapter slot and which adapter the fork is currently running. Thin in v1 — display only, no swap UI.

**Data displayed** (sourced from runtime config introspection — env vars + module manifests):
- **Slot list**, each row with:
  - Slot name (e.g., "LLM gateway", "Object storage", "Job queue")
  - Current adapter name + version when known
  - Status pill (derived from the corresponding service's `/health` if exposed; otherwise "unknown")
  - Admin link → opens that OSS's own UI in a new tab when one exists
- **Slots covered in v1** (limited to what env config exposes):
  - LLM gateway (currently always LiteLLM)
  - Object storage (MinIO in dev; S3 / R2 / GCS / Azure via `AF_STACK_S3_ADAPTER`)
  - Sandbox (Docker / gVisor / Firecracker / e2b / Modal via `AF_STACK_SANDBOX_ADAPTER`)
  - Billing (Stripe / Lago / none via `AF_STACK_BILLING_ADAPTER`)
  - Notifications (log / Resend / Postmark)
  - Secrets (envelope local; KMS providers via env)

**Actions**:
- Open the active adapter's own admin UI in a new tab when one exists

**Notes**:
- v1 has no swap UI — adapter switching is a config change + restart. The page exists as a reference so operators can see "what's powering each layer of my fork."
- For slots without a current adapter introspection endpoint, the page may show static values from env vars without runtime confirmation.

---

### SETUP › Auth providers

**Purpose**: Configure better-auth (or its successor adapter).

**Data displayed**:
- Configured providers: email/password, OAuth providers (Google, GitHub, etc.), magic links
- Trusted origins list
- Session config: lifetime, refresh, secure cookie flags
- OAuth provider keys status (present / missing — values managed via Secrets)

**Actions**:
- Add provider
- Toggle provider
- Edit trusted origins
- Open adapter docs

---

### SETUP › LLM providers

**Purpose**: Show what models the gateway has configured + provider key health. This is the "what LLMs do I have" landing. For routing rules, fallback chains, virtual-key admin, or per-model deep configuration, the page links out to the LiteLLM admin UI.

**Data displayed** (sourced from `GET /api/v1/llm/models` — what we actually have today):

**Models list**
- Model id (the string clients pass — e.g., `openai/gpt-4o`)
- Display name
- Upstream provider (OpenAI / Anthropic / Google / Mistral / etc.)
- Cost per 1M tokens (prompt)
- Cost per 1M tokens (completion)
- Supports streaming (boolean)
- Supports tool calls (boolean)

That's the full set of fields the runtime returns today. Render them in a sortable, filterable table.

**Provider keys (status only)**
- Per provider, whether the upstream key is configured (present / missing). Values are managed via Setup → Secrets.

**Actions**:
- Filter / search the models table
- Click any model → jumps to Build → API explorer pre-filled with `POST /api/v1/llm/chat/completions` and that model id

**Link out for everything else**:
- A prominent **"Open LiteLLM admin ↗"** button at the top right of the page links to `:4000/ui` for:
  - Per-model context window, max tokens, mode (chat/embed/image/audio), vision/function-calling capability
  - Fallback chain configuration
  - Per-virtual-key budget and rate-limit admin
  - Provider routing rules and retry policy
  - Live provider health check (LiteLLM's `/health` endpoint)
  - Spend logs at the raw level

**Notes**:
- This page is intentionally thin in v1. Capability matrix, TTFT, throughput, fallback chain visibility, error rate, and live provider health all exist in LiteLLM's admin UI. We don't duplicate them.
- The runtime's static catalog at `internal/llmgateway/models.go` is what feeds this page. Operators get their richer view by clicking through to LiteLLM admin.
- If future runtime work proxies LiteLLM's `/model/info` and `/health` into our `/api/v1/llm/models` response, this page can absorb those fields without changing layout.

---

### SETUP › Sandbox adapter

**Purpose**: Pick and configure the sandbox runtime.

**Data displayed**:
- Available adapters: Docker, gVisor, Firecracker, e2b, Modal
- Current selection
- Per-adapter config: image registry, default limits (CPU, memory, timeout), credentials status

**Actions**:
- Switch adapter
- Edit limits
- Open adapter docs

---

### SETUP › Webhook subscribers

**Purpose**: Configure which events go to which endpoints. Distinct from Operate / Webhook deliveries (which is observability).

**Data displayed**:
- **Subscriber list**: event types subscribed, endpoint URL, signing key status, tenant scope (all / specific), status (active / paused)
- **Event types catalog**: list of all available platform events that can be subscribed to

**Actions**:
- Add subscriber
- Rotate signing key
- Pause / resume
- Open Svix dashboard for delivery archive

---

### SETUP › Notifications

**Purpose**: Configure notification channels.

**Data displayed**:
- **Channels list**: log (default), Resend (email), Postmark (email), Slack (webhook), etc.
- **Per-channel config**: API keys / webhook URLs
- Test send button per channel

**Actions**:
- Add / edit / remove channel
- Send test notification

---

### SETUP › Secrets

**Purpose**: Secrets vault contents (names only; values never displayed).

**Data displayed**:
- **Secret list**: name, type, last rotated at, used-by references (e.g., "litellm/openai", "stripe/secret_key")
- Encryption envelope info

**Actions**:
- Rotate
- Delete (with confirmation)
- Read-only "used by" inventory showing which adapters reference each secret

---

### SETUP › Observability

**Purpose**: Configure traces, metrics, and log destinations.

**Data displayed**:
- OTel exporter endpoint config
- Metrics scrape URL (Prometheus exposition)
- Log level
- Langfuse opt-in toggle (if profile is available)

**Actions**:
- Edit endpoint
- Toggle Langfuse profile (writes a compose hint; may require restart)
- Test exporter

---

### SETUP › Billing adapter

**Purpose**: Pick and configure the billing provider.

**Data displayed**:
- Adapter selection: Stripe / Lago / none
- Active adapter config: API keys status, webhook endpoint, default plan map
- Plan map: BackAI-internal plan ids → Stripe/Lago price ids

**Actions**:
- Switch adapter
- Edit plan map
- Open the adapter's dashboard

---

### SETUP › Deploy targets

**Purpose**: Configure where the fork deploys.

**Data displayed**:
- Available targets: Railway, Fly.io, Render, Helm / Kubernetes, Nomad, Docker Compose (default)
- Per-target status: configured / not, last deploy at, last deploy status
- Per-target secrets and env vars

**Actions**:
- Trigger deploy (calls the provider's CLI)
- Edit env
- Open the target's console

---

### BRAND

**Purpose**: Show the operator what's currently in `brand.yaml` so they can confirm their fork's identity. Thin in v1 — read-only display; editing happens in the file directly and a restart picks it up.

**Data displayed** (parsed from `brand.yaml` at the fork root):
- Brand name
- Primary color + accent color (with color swatches)
- Logo (preview)
- Favicon (preview)
- Custom domain (if configured)

**Actions**:
- "Edit brand.yaml" — link / button that copies the file path so the operator can open it in their editor
- "Reload from file" — re-parses brand.yaml without a full restart (if runtime supports hot-reload)

**Notes**:
- v1 has no in-product editor. Brand changes are a file edit + customer-app rebuild.
- The customer-facing app inherits the brand. The operator dashboard does **not** rebrand — it stays "BackAI Studio."

---

---

## Page specifications — additions (round 2 audit)

The pages below were missed in the first pass. They cover backend subsystems that exist today (verified against `services/runtime/internal/`) and operational concerns that a serious AI-SaaS builder cares about. Grouped by sidebar section.

---

### OPERATE › Notifications

**Purpose**: Delivery audit for alerts and notifications the platform sent out (email / Slack / SMS / log). Different from Setup → Notifications which is channel configuration.

**Data displayed**:
- **Filter bar**: channel (email / Slack / SMS / log), status (delivered / failed / queued / muted), category (budget-alert / error-alert / approval-needed / etc.), tenant, time range
- **Stats tiles**: sent today, delivery rate %, failure rate %, average latency
- **Notifications list**: timestamp, channel, recipient, category, status, retry count, latency, link to the source event that triggered it
- **Detail drawer per notification**
  - Full payload (subject, body, template used)
  - Provider response
  - Retry history

**Actions**:
- Manually resend
- Filter, search
- Mute future notifications of the same kind

---

### OPERATE › Approvals (HITL — human-in-the-loop)

**Purpose**: Queue of pending human decisions gating workflow execution. AI agents that need approval before proceeding (e.g., "approve this refund", "confirm before sending external email") wait here.

**Data displayed**:
- **Filter bar**: status (pending / approved / denied / cancelled), kind (refund / external-action / spend-limit / custom), tenant, time range, requested by, decided by
- **Stats tiles**: pending count, average decision time, approval rate %, escalation rate
- **Approvals list**: created at, kind, tenant, requested by, status, decision time (if decided), decided by, decision note preview
- **Detail drawer per approval**
  - Full payload (the structured request)
  - Related run / job that's blocked waiting
  - Decision form (approve / deny / cancel with optional note)
  - Audit references
  - History of similar decisions for context

**Actions**:
- Approve / Deny / Cancel with note
- Bulk decide on filtered subset
- Delegate to another operator
- Drill into the blocked run

**Notes**:
- This page should pulse / highlight when new approvals arrive — there's a human latency cost.
- Pending count is also surfaced on Home KPI strip.

---

### OPERATE › Activity (customer activity)

**Purpose**: Feed of actions the operator's **customers** took inside the customer-app. Distinct from Audit log (which records what the **operator** did).

**Data displayed**:
- **Filter bar**: actor type (user / API key / system / anonymous), action verb, resource type, tenant, user, time range
- **Activity list**: timestamp, actor (user or API key), action, resource type + id, tenant, IP, user-agent
- **Detail panel per entry**
  - Full metadata
  - Related runs / costs (e.g., "this action triggered run X which cost Y")

**Actions**:
- Filter, search
- Export to CSV
- Drill into related entities

**Notes**:
- Powers downstream surfaces like Customers → Tenant detail "recent activity" tab.
- Activity is data your customers generated. Audit is data the operator generated.

---

### OPERATE › Health

**Purpose**: At-a-glance "is everything up?" for the fork's backing services. Thin in v1 — service-level health pills only. For deep inspection of any service, click through to that OSS's own dashboard.

**Data displayed** (sourced from `GET /health` + `GET /ready` per service):

**Backing service status grid**
- Row per backing service: name, status dot (green/yellow/red), version, last-checked timestamp, link to native admin UI
- Services to include: postgres, litellm, agentfield, river (via runtime healthcheck), svix, minio, redis (svix-private)
- Runtime self-status: own `/health`, `/ready`, current uptime, build version

**LLM provider availability** (via LiteLLM `GET /health` — link out if unhealthy)
- One row per upstream provider configured (OpenAI / Anthropic / etc.): status pill, link to "View in LiteLLM admin ↗"

**Actions**:
- Click any service → opens its admin UI in a new tab
- Refresh checks on demand

**Link out for everything else**:
- Database stats (slow queries, table sizes, connection pool, vacuum) → no in-product surface in v1; operators access via Build → Data → SQL queries against `pg_stat_*` views
- Worker count / restart rate → no in-product surface in v1; covered by `GET /metrics` (Prometheus exposition) and Operate → Queue
- TLS / cert expiry → no in-product surface in v1; check Caddy logs / `:2019/pki/ca/local/certificates` if admin API enabled
- Provider latency / error rate time-series → LiteLLM admin

**Notes**:
- v1 Health is a status board, not an observability stack. Deeper analytics get added as the underlying endpoints are built.
- If the operator wants metrics graphs, Prometheus is exposed at `:9090` (link out from this page).

---

### OPERATE › Logs

**Purpose**: Raw log viewer with filters. The "drop into the logs" surface that complements Errors and Traces.

**Data displayed**:
- **Filter bar**: log level (debug / info / warn / error / fatal), service (runtime / agentfield / litellm / agents / handlers), tenant, time range, free-text search
- **Log stream**: timestamp, level, service, message, key fields (run_id, tenant_id, error code, etc.)
- **Tail mode toggle**: live-streaming new logs as they arrive
- **Structured field view** per log entry: expand JSON of all attached attributes

**Actions**:
- Filter, search, tail
- Export filtered logs (CSV / JSONL)
- Save a filter as a named view

---

### BUILD › Reasoners (cross-agent listing)

**Purpose**: A flat listing of every reasoner across every agent so operators can see "what reasoning steps exist on my fork" at a glance. Thin in v1 — listing only; cross-agent cost/latency/error analytics require aggregation endpoints that aren't built yet.

**Data displayed** (sourced from `GET /api/v1/agents` — each agent entry includes its reasoners):
- **Reasoner list**: `agent.reasoner` id, kind (entry / parallel / nested / synthesis), parent agent (clickable to agent detail), schema (input / output preview), declared tool list

**Actions**:
- Filter by agent
- Search by reasoner name
- Click any reasoner → opens the parent agent's detail page (Build → Agents → Detail)

**Link out for everything else**:
- Reasoner-level cost / latency / error analytics → not available in v1; operators get cost-by-reasoner via Operate → Cost when group-by-reasoner is selected (uses LiteLLM `tags` if reasoner is tagged on the LLM call)
- Source / DAG of a reasoner → "Open in AgentField ↗" link goes to AgentField's UI at `:8081`

**Notes**:
- This page is a flat overview. Per-agent reasoner detail (with tools, recent runs, sub-reasoners) lives in Build → Agents → Detail.

---

### BUILD › Tools

**Purpose**: Inventory of every tool the agents can call — native built-in tools plus MCP-exposed tools — with the ability to test-invoke. Thin in v1: listing + invoke; usage analytics (call frequency, error rate, ROI) require aggregation endpoints not built yet.

**Data displayed**:

**Tabs**: Native tools / MCP tools

**Native tools** (sourced from `GET /api/v1/tools/native` and `GET /api/v1/tools/adapters`)
- Tool id, description, enabled toggle (writes via `POST /api/v1/tools/native/{tool}/enable` or `PUT /api/v1/tools/adapters/{id}/enabled`), schema (input / output)

**MCP tools** (sourced from `GET /api/v1/mcp/tools` and `GET /api/v1/mcp/servers`)
- Server name, tool name, transport (stdio / SSE / HTTP), schema, server status (reachable / not)

**Actions**:
- Toggle enable / disable for native tools
- Test invoke any tool — form generated from the tool's schema; calls `POST /api/v1/tools/call` or `POST /api/v1/tools/native/{tool}/invoke` or `POST /api/v1/mcp/call`
- Drill into MCP server detail (Build → Skills → server)

**Link out for everything else**:
- Tool usage analytics (call frequency per tool, error rates, ROI) → not in v1; check Operate → Logs filtered by tool call events for raw data
- MCP server's own UI (if the server exposes one) → link from the per-tool row when manifest declares a URL

---

### BUILD › Sandboxes

**Purpose**: The dev surface for the platform's code-execution sandboxes. Operators test sandbox configuration here, inspect the pool, and trigger ad-hoc commands. Distinct from Operate → Sandbox runs (which is the observability feed of every sandbox execution).

**Data displayed**:

**Tab 1 — Playground**
- Image picker (or free-text image field) — e.g., `python:3.12-slim`, `node:20-alpine`
- Command field (textarea, supports shell syntax)
- Optional: env vars, working directory, mount paths, timeout, CPU / memory caps
- Tenant scope (operator or selected tenant key)
- "Run" button → calls `POST /api/v1/sandbox/run`
- Live output stream (stdout + stderr) as the sandbox runs
- On completion: exit code, duration, cost (USD), peak CPU / memory
- "Save as preset" — bookmark common combos

**Tab 2 — Pool**
- Current adapter (Docker / gVisor / Firecracker / e2b / Modal) — from `GET /api/v1/sandbox/pool`
- Pool stats: warm pods, active runs, queued, idle
- Per-tenant pool usage (if isolated)
- Adapter-specific health signals

**Actions**:
- Run a sandbox command (Tab 1)
- Cancel a running sandbox (Tab 1, while live)
- Inspect any past run — links into Operate → Sandbox runs filtered

**Notes**:
- Tab 1 is the "scratch terminal" for the operator. The Playground intentionally feels like a small IDE.
- Adapter selection is configured in Setup → Sandbox adapter; this page reflects the current choice but doesn't switch it.

---

### OPERATE › Sandbox runs

**Purpose**: Observability for every sandbox execution that's happened on the platform. Paired with Build → Sandboxes (which is the dev / test surface).

**Data displayed** (sourced from `GET /api/v1/sandbox/runs` + `GET /api/v1/sandbox/runs/{id}`):
- **Filter bar**: tenant, adapter, status (running / succeeded / failed / timeout / cancelled), time range, image, exit code range, free-text command search
- **Table columns**: started at, tenant, image, command preview, status, duration, exit code, CPU-seconds, cost (USD), triggered by (agent name / operator / API)
- **Detail drawer per run** (`GET /api/v1/sandbox/runs/{id}`)
  - Full command, env, mount paths, limits
  - Full stdout / stderr (live tail if still running — `GET /api/v1/sandbox/runs/{id}/logs`)
  - Exit code, duration, cost, peak CPU / memory
  - Triggered by: agent name + reasoner if applicable, with drill to the parent run
  - Cancel button (`DELETE /api/v1/sandbox/runs/{id}`) if still running

**Actions**:
- Filter, search, sort
- Cancel a running sandbox
- Drill to parent agent run if applicable
- "Re-run with these inputs" — copies command into Build → Sandboxes → Playground

**Notes**:
- Sandbox runs are different entities from agent runs. They share the "drawer with stream + drill" pattern but are listed separately because operators debug them differently (shell output, exit codes, CPU-seconds — vs. agent runs' reasoner paths, tokens, model usage).

---

### BUILD › Crons (scheduled jobs)

**Purpose**: Definitions and observability for scheduled (cron-style) jobs. Different from the queue (which is one-shot async work).

**Data displayed**:
- **Cron list**: name, schedule (cron expr), kind (job / agent.call / handler / custom), tenant scope (all / specific), active / paused, next run time, last run status, last run timestamp, average duration
- **Per-cron detail**
  - Schedule editor (cron expression with human-readable preview)
  - Target action: which job / agent / handler runs
  - Last N runs with status and duration
  - History of pauses / edits

**Actions**:
- Create cron (drawer)
- Edit schedule
- Pause / resume
- Trigger manually (run now)
- Delete

---

### BUILD › Harnesses (Claude Code / Codex / Gemini / opencode)

**Purpose**: Coding-agent harnesses are runtime components inside agent containers. This page is the registry + probe status — which harnesses are installed, which models they support, what's their health.

**Data displayed**:
- **Harnesses list**: provider (Claude Code / Codex / Gemini / opencode), version, agents using it, models available, last probe time, last probe status (success / failed)
- **Per-harness detail**
  - Capability matrix: which features this harness supports (file edits, multi-file, tool use, planning, etc.)
  - Available models
  - Recent invocations
  - Probe history

**Actions**:
- Probe now
- View probe logs
- Disable per agent

---

### BUILD › Shipwright (coding-agent factory)

**Purpose**: Surface for the Shipwright coding-agent factory tasks (if shipping in v1). Long-running agentic coding tasks: clone repo, run harness in sandbox, return PR.

**Data displayed**:
- **Tasks list**: id, task title, repo, status (pending / running / completed / failed), harness used, model used, sandbox adapter, cost, duration, output (link to PR or artifact)
- **Per-task detail**
  - Input prompt
  - Sandbox run reference
  - Harness invocation logs
  - File diffs produced
  - Cost breakdown

**Actions**:
- Create task (drawer)
- Cancel running task
- Re-run failed task
- Open sandbox run detail

**Notes**:
- Only ship this page if Shipwright is part of v1. If not, omit entirely.

---

### CUSTOMERS › Sessions (auth events)

**Purpose**: Active customer sessions and auth events. Security operations surface.

**Data displayed**:
- **Tabs**: Active sessions / Auth events
- **Active sessions table**: user, tenant, started at, last active, IP, user agent, MFA used, expires at
- **Auth events table**: timestamp, event type (login / logout / login-failed / password-reset / MFA-enrolled / MFA-failed / OAuth-grant / token-refresh), user, tenant, IP, user-agent, success / failure, reason if failed
- **Stats**: active session count, sign-ups today, login failures today, suspicious activity flags

**Actions**:
- Force logout a session
- Lock / unlock account
- Filter, search

---

### CUSTOMERS › Activity log (per-tenant)

**Purpose**: Per-tenant view of customer actions. Same data as Operate → Activity but scoped to one tenant and embedded in tenant detail.

**Data displayed**:
- Same shape as Operate → Activity but tenant-filtered
- Embedded as a tab on the Tenant detail page (not a standalone sidebar entry — listed here for spec completeness)

---

### CUSTOMERS › OAuth connections

**Purpose**: Per-tenant external OAuth grants (Google, Slack, GitHub, etc.) the platform holds on behalf of customers. Required when backend agents act on behalf of users.

**Data displayed**:
- **Connections list**: tenant, provider (Google / Slack / GitHub / Microsoft / etc.), connected user, scopes granted, token expiry, status (active / expired / revoked / refresh-failed)
- **Stats**: total connections, expiring this week, revoked this week
- **Per-connection detail**
  - All scopes
  - Refresh history
  - Recent agent calls that used this connection

**Actions**:
- Revoke a connection
- Trigger refresh
- Filter, search

---

---

## Cross-cut data this dashboard makes possible (v1 scope)

Things no single page owns. Only entries that can be computed from existing endpoints are listed here; richer cross-cut metrics are deferred until aggregation endpoints exist.

| Composite metric | Surfaces it appears on | Source |
|---|---|---|
| Cost per model | Operate → Cost (group-by model) | Runtime `/api/v1/cost` + LiteLLM `/spend/tags` |
| Cost per tenant | Operate → Cost (group-by tenant) + Customers → Tenant detail | Runtime `/api/v1/cost` + LiteLLM `/spend/keys` |
| Cost per agent | Operate → Cost (group-by agent) + Build → Agents detail | Runtime `/api/v1/cost` |
| Cost forecast (linear projection) | Operate → Cost | Client-side regression on time-series |
| Cache savings (USD) | Operate → Cache | Runtime `/api/v1/llm/cache/stats` × per-call cost |
| Approval decision latency | Operate → Approvals | Runtime `/api/v1/approvals` (computed from created_at + decided_at) |
| Token-expiring-soon (OAuth) | Customers → OAuth connections | Runtime `/api/v1/oauth/connections` |
| Reasoner cost (when reasoner tagged on LLM call) | Operate → Cost (group-by reasoner) | LiteLLM `/spend/tags` with `reasoner` tag |

Cross-cut data that does **not** make v1 (requires aggregation endpoints): TTFT and streaming throughput per model, provider availability time-series, rate-limit headroom analytics, tenant composite health score, tool ROI, worker restart rate, TLS cert expiry tracking. These are deferred and where relevant the dashboard links out to the OSS service that has the data.

---

---

## Backend connection map (per page)

For the implementation engineer / integrator. Tells you, per dashboard page, **exactly which backend endpoints to call** and **which OSS service is the source of truth**. Every page is verified against actual routes in `services/runtime/internal/server/` and / or the OSS service's documented API. Where a page combines data from multiple sources, the "compute" column tells you what's derived client-side.

Conventions:
- "Runtime" = our Go runtime at `:8080`, paths under `/api/v1/*`
- "LiteLLM" = sidecar at `:4000`, admin auth via `LITELLM_MASTER_KEY` header
- "AgentField" = control plane at `:8081` (mostly proxied through the runtime's `internal/agentfield` client)
- "Svix" = sidecar at `:8071`
- "Postgres" = direct queries to `pg_stat_*` views and our `suite_*` tables
- "MinIO" = admin at `:9000`, console at `:9001`
- "River" = job queue, queried via the `river-pkg` Go library (no HTTP)
- "Caddy" = admin API at `:2019` if exposed

### Overview

| Page | Primary endpoints | Secondary / cross-source | Compute / derive client-side |
|---|---|---|---|
| Home | Runtime `GET /api/v1/home/overview`, `GET /api/v1/metrics/summary`, `GET /api/v1/activity?limit=20`, `GET /health`, `GET /ready` | Realtime: WebSocket `GET /api/v1/realtime` for KPI ticks | Welcome-block dismissed flag is a client preference (localStorage). "Your stack" reads from runtime config endpoint. |

### Operate

| Page | Primary endpoints | Secondary / cross-source | Compute / derive client-side |
|---|---|---|---|
| Runs | Runtime `GET /api/v1/runs` (filters), `GET /api/v1/runs/{id}/events`, `GET /api/v1/executions/{id}` | AgentField `GET /api/v1/runs/{id}/agentfield` for full DAG (link out) | — |
| Cost | Runtime `GET /api/v1/cost`, `GET /api/v1/cost/events`, `GET /api/v1/admin/budgets` | LiteLLM `GET /spend/keys`, `GET /spend/tags`, `GET /spend/logs`, `GET /global/spend` | Forecast = linear regression on time-series; cache savings = `cache_hit_count × avg_cost_per_call`; cost-per-reasoner = LiteLLM spend grouped by `reasoner` tag |
| Errors | Runtime `GET /api/v1/logs?level=error,fatal` filtered by source | — | Dedup / pattern grouping computed client-side |
| Traces | Runtime traces endpoint (OTel exporter) | Optional Langfuse `GET /traces/{id}` if profile enabled | — |
| Queue | Runtime `GET /api/v1/queues/summary`, `GET /api/v1/jobs`, `GET /api/v1/jobs/{id}` | River library queries server-side | — |
| Cache | Runtime `GET /api/v1/llm/cache/stats` | — | Savings forecast |
| Sandbox runs | Runtime `GET /api/v1/sandbox/runs`, `GET /api/v1/sandbox/runs/{id}`, `GET /api/v1/sandbox/runs/{id}/logs`, `DELETE /api/v1/sandbox/runs/{id}` | — | — |
| Webhook deliveries (outbound) | Runtime `GET /api/v1/webhooks/deliveries`, `GET /api/v1/webhooks/deliveries/{id}`, `POST /api/v1/webhooks/deliveries/{id}/retry` | Svix dashboard at `:8071` (link out) for delivery archive, replay, signing key history | — |
| Notifications | Runtime `GET /api/v1/notifications`, `GET /api/v1/notifications/{id}`, `GET /api/v1/notifications/stats`, `POST /api/v1/notifications` (resend) | — | — |
| Approvals | Runtime `GET /api/v1/approvals`, `GET /api/v1/approvals/{id}`, `POST /api/v1/approvals/{id}/decide` | — | Average decision time |
| Activity (customer) | Runtime `GET /api/v1/activity` (filters) | — | — |
| Health (v1 thin) | Runtime `GET /health`, `GET /ready`. Each OSS service's own `/health` polled and surfaced as a status pill. | LiteLLM `GET /health` for provider availability (link out for deeper). MinIO admin metrics. Prometheus at `:9090` (link out). | — |
| Logs | Runtime `GET /api/v1/logs` (filters), `GET /slow` for slow queries, WebSocket tail for live | — | — |

### Build

| Page | Primary endpoints | Secondary / cross-source | Compute / derive client-side |
|---|---|---|---|
| Agents (registry) | Runtime `GET /api/v1/agents` | AgentField capabilities response via runtime client | Container status from `harnesses.probe` |
| Agents (detail per agent) | Runtime `GET /api/v1/agents` filtered; `GET /api/v1/runs?agent=<name>` for recent runs; `GET /api/v1/cost?agent=<name>` for cost trend; `GET /api/v1/mcp/servers?agent=<name>` | AgentField `GET /api/v1/runs/{id}/agentfield` for definition source (link out) | — |
| Agents → Playground (per agent) | Runtime `POST /api/v1/agents/{call}` (sync) or `POST /api/v1/agents/async/{call}` (async, streams via realtime); `GET /api/v1/realtime/runs?run_id=<id>` for streaming | — | — |
| Reasoners (flat listing) | Derived from `GET /api/v1/agents` (each agent entry contains its reasoners) | LiteLLM `/spend/tags` for reasoner-tagged spend when group-by-reasoner is used on Cost | — |
| Tools (listing + invoke) | Runtime `GET /api/v1/tools/native`, `GET /api/v1/tools/adapters`, `GET /api/v1/mcp/tools`; `POST /api/v1/tools/call`, `POST /api/v1/tools/native/{tool}/invoke` for testing | — | — |
| Sandboxes (playground + pool) | Runtime `POST /api/v1/sandbox/run`, `GET /api/v1/sandbox/pool`, `GET /api/v1/sandbox/runs/{id}/logs` (live tail) | — | — |
| Skills (MCP) | Runtime `GET /api/v1/mcp/servers`, `GET /api/v1/mcp/servers/{name}`, `GET /api/v1/mcp/tools`, `POST /api/v1/mcp/call`; `POST /api/v1/skills`, `GET /api/v1/skills`, `POST /api/v1/skills/attach` | — | — |
| Harnesses | Runtime `GET /api/v1/harnesses`, `GET /api/v1/harnesses/{provider}`, `POST /api/v1/harnesses/{provider}/probe` | — | — |
| Crons | Runtime `GET /api/v1/crons`, `GET /api/v1/crons/{id}`, `POST /api/v1/crons`, `PUT /api/v1/crons/{id}/active`, `DELETE /api/v1/crons/{id}` | — | Human-readable schedule preview |
| Modules | Runtime `GET /api/v1/modules` | — | — |
| Data → Tables | Runtime `GET /api/v1/db/tables`, `GET /api/v1/db/tables/{schema}/{name}`, `GET /api/v1/db/rows` | — | — |
| Data → SQL | Runtime `POST /api/v1/db/sql` (read-only enforced) | — | — |
| Data → Memory | Runtime `GET /api/v1/memory`, `GET /api/v1/memory/get`, `POST /api/v1/memory/search`, `PUT /api/v1/memory`, `DELETE /api/v1/memory` | — | — |
| Data → Storage | Runtime `GET /api/v1/storage`, `GET /api/v1/storage/{key...}`, `GET /api/v1/storage/signed-url`, `POST /api/v1/storage/upload`, `DELETE /api/v1/storage/{key...}` | MinIO admin if deeper bucket-level ops needed (link out) | — |
| Data → Search | Runtime `POST /api/v1/search`, `PUT /api/v1/search/documents`, `DELETE /api/v1/search/documents/{namespace}/{key}` | — | — |
| Feature flags | Runtime `GET /api/v1/config/flags`, `PUT /api/v1/config/flags/{key}` | — | — |
| API explorer | Embed Scalar against runtime `GET /openapi.json` | — | Auth selector uses operator session cookie or selected tenant API key |
| Shipwright (if v1) | Runtime `GET /api/v1/shipwright/tasks`, `GET /api/v1/shipwright/tasks/{id}`, `POST /api/v1/shipwright/tasks`, `POST /api/v1/shipwright/tasks/{id}/complete` | — | — |

### Customers

| Page | Primary endpoints | Secondary / cross-source | Compute / derive client-side |
|---|---|---|---|
| Tenants (list) | Runtime `GET /api/v1/admin/tenants`, `POST /api/v1/admin/tenants` | — | Health-score composite (see below) |
| Tenant (detail) | Runtime `GET /api/v1/admin/tenants/{id}`, `GET /api/v1/admin/tenants/{id}/drilldown`, `PATCH /api/v1/admin/tenants/{id}`, `DELETE /api/v1/admin/tenants/{id}` | LiteLLM virtual key info; AgentField run stats filtered to tenant; activity log filtered | — |
| API keys | Runtime `GET /api/v1/admin/keys`, `POST /api/v1/admin/keys`, `GET /api/v1/admin/keys/{id}/spend`, `DELETE /api/v1/admin/keys/{id}` | LiteLLM `GET /key/info` for live rpm/tpm headroom; `POST /key/generate` mirrored by runtime on issue | — |
| Members | Runtime `GET /api/v1/admin/memberships`, `POST /api/v1/admin/memberships`, `DELETE /api/v1/admin/memberships/{tenantId}/{userId}` | Better-auth user table via Drizzle queries in dashboard for richer profile data | — |
| Sessions | Better-auth sessions table via dashboard Drizzle queries | Runtime `GET /api/v1/logs?event=auth.*` for auth events | — |
| Budgets | Runtime `GET /api/v1/admin/budgets`, `GET /api/v1/admin/budgets/{tenantId}`, `PUT /api/v1/admin/budgets` | LiteLLM budget endpoints mirrored automatically | — |
| Audit log | Runtime `GET /api/v1/admin/audit` (filters) | — | — |
| Activity log (per-tenant) | Runtime `GET /api/v1/activity?tenant=<id>` | — | — |
| OAuth connections | Runtime `GET /api/v1/oauth/connections`, `GET /api/v1/oauth/providers`, `DELETE /api/v1/oauth/{provider}`, `POST /api/v1/oauth/{provider}/authorize`, `POST /api/v1/oauth/token`, callback at `GET /oauth/callback/{provider}` | — | Token-expiring-soon warning |
| Billing summary | Runtime `GET /api/v1/billing/customers`, `GET /api/v1/billing/customers/{tenantId}`, `GET /api/v1/billing/meters`, `POST /api/v1/billing/customers/{tenantId}/portal` | Stripe / Lago dashboard for deep invoice / payment-method detail (link out) | MRR aggregation |

### Setup

| Page | Primary endpoints | Secondary / cross-source | Compute / derive client-side |
|---|---|---|---|
| Adapters | Read-only env-config display in v1; richer introspection (`GET /api/v1/admin/adapters`) deferred. | Each adapter's own admin UI (link out) | — |
| Auth providers | Better-auth config via dashboard (no runtime endpoint) | — | — |
| LLM providers | Runtime `GET /api/v1/llm/models` — returns id, display_name, provider, cost in/out, supports_streaming, supports_tools. That's the v1 surface. | LiteLLM admin at `:4000/ui` (link out) for context window, capabilities matrix, fallback chains, TTFT, provider health, virtual key admin, spend logs. | — |
| Sandbox adapter | Runtime `GET /api/v1/sandbox/pool` for current adapter status; config via env (no PUT endpoint v1) | — | — |
| Webhook subscribers (outbound config) | Runtime `GET /api/v1/webhooks/endpoints`, `POST /api/v1/webhooks/endpoints`, `DELETE /api/v1/webhooks/endpoints/{id}`, `POST /api/v1/webhooks/send` | Svix `GET /api/v1/app/{app_id}/endpoint/`, `POST /api/v1/app/{app_id}/endpoint/`, `GET /api/v1/event-type/` for event types catalog | — |
| Notifications (channels) | Read-only display from env config in v1; full config UI requires `GET/POST/PUT /api/v1/admin/notifications/channels` later. Delivery side at `/api/v1/notifications` is fully wired. | — | — |
| Secrets | Runtime `GET /api/v1/secrets`, `GET /api/v1/secrets/{key}`, `PUT /api/v1/secrets/{key}`, `DELETE /api/v1/secrets/{key}`, `POST /api/v1/secrets/{key}/reveal`, `POST /api/v1/secrets/{key}/rotate` | — | — |
| Observability | Config-only (env vars); no runtime endpoint v1 | Prometheus scrape at `/metrics`; OTel exporter target via env | — |
| Billing adapter | Runtime `GET /api/v1/billing/*` (already wraps adapter); selection via env | Stripe dashboard or Lago UI (link out) | — |
| Deploy targets | Provider CLI invocations from operator's local machine; runtime exposes no admin endpoint here | Railway / Fly / Render APIs invoked by their CLIs | — |

### Brand

| Page | Primary endpoints | Secondary / cross-source | Compute / derive client-side |
|---|---|---|---|
| Brand | Read-only parse of `brand.yaml` from the fork root in v1. Editing is a file edit + restart. | — | Swatches rendered from parsed color values |

### Endpoints v1 explicitly does NOT need

Per the MVP cuts above, none of the following are required for the v1 dashboard to ship. They are deferred and the corresponding pages have either been removed or link out instead:

- `GET /api/v1/admin/db/health` (was for Health PG stats — link out to Prometheus / direct SQL via Build → Data instead)
- `GET /api/v1/admin/rate-limits` (was for Rate limits page — page cut; use LiteLLM admin + Logs filter)
- `GET /api/v1/admin/webhooks/inbound` + slug CRUD (Inbound webhooks pages cut; configure via env / workload module)
- `GET /api/v1/admin/guardrails/rules` + CRUD (Guardrails page cut; configure via env)
- `GET /api/v1/admin/notifications/channels` CRUD (read-only env display in v1)
- `GET /api/v1/admin/reasoners/summary` (Reasoners page is flat listing only in v1)
- `GET /api/v1/admin/tools/summary` (Tools page is listing + invoke only in v1)
- `GET /api/v1/admin/adapters` (Adapters page is read-only env display in v1)
- `GET /api/v1/admin/brand` / `PUT /api/v1/admin/brand` (Brand page is read-only parse in v1)

These can be added in later versions to lift the corresponding pages from "thin" to "rich."

### Direct OSS service endpoints called from the dashboard (with auth)

For reference, the dashboard talks to these OSS services directly (not just through our runtime). Auth and base URLs:

| Service | Base URL (default) | Auth | Endpoints we use |
|---|---|---|---|
| LiteLLM | `http://litellm:4000` | Header `Authorization: Bearer ${LITELLM_MASTER_KEY}` | `/spend/keys`, `/spend/tags`, `/spend/logs`, `/global/spend`, `/key/info`, `/key/generate`, `/model/info`, `/model_group/info`, `/health`, `/health/readiness`, `/budget/info` |
| Svix | `http://svix-server:8071` | Header `Authorization: Bearer ${SVIX_AUTH_TOKEN}` | `/api/v1/app/{app_id}/endpoint/`, `/api/v1/app/{app_id}/msg/`, `/api/v1/app/{app_id}/msg/{msg_id}/attempt/`, `/api/v1/app/{app_id}/msg/{msg_id}/replay`, `/api/v1/event-type/`, `/api/v1/app/{app_id}/stats/` |
| MinIO | `http://minio:9000` (admin), `:9001` (console) | S3 SigV4 with `MINIO_ROOT_USER` / `MINIO_ROOT_PASSWORD` | `/minio/v2/metrics/cluster`, S3 admin API for bucket-level ops |
| Postgres | `postgresql://postgres:5432/afstack` | DB credentials | `pg_stat_statements`, `pg_stat_user_tables`, `pg_stat_database`, `pg_stat_activity`, `pg_size_pretty(pg_total_relation_size(...))`, `pg_locks`, `pg_indexes`. Note: `pg_stat_statements` requires the extension to be enabled (`CREATE EXTENSION pg_stat_statements`). |
| AgentField | `http://agentfield:8081` | DID-based auth via runtime's AgentField client | `/v1/capabilities`, `/v1/executions/{id}`, `/v1/runs/{id}` |
| Caddy | `http://caddy:2019` (if admin enabled) | None (localhost-only by default) | `/config/`, `/pki/ca/local/certificates` for cert expiry |
| Better-auth | Same Postgres DB | Session cookie from dashboard | Direct Drizzle queries against `user`, `session`, `account`, `verification` tables |
| Presidio (if enabled) | `${AF_STACK_PRESIDIO_ANALYZER_URL}`, `${AF_STACK_PRESIDIO_ANONYMIZER_URL}` | None (internal network) | `/analyze`, `/anonymize` |
| Prometheus | `http://prometheus:9090` | None (local) | `/api/v1/query`, `/api/v1/query_range` |

The runtime proxies most of these where it can to avoid the dashboard holding admin secrets. Direct dashboard-to-OSS only happens for read-only display (e.g., the dashboard's "link out" buttons that open the OSS's own UI in a new tab).

---

## Plugin injection (v1 behavior)

- Plugin tabs may inject into any sidebar group based on the plugin's manifest
- Manifest declares: parent group, sidebar item label / icon / order, page route + component
- Plugin items appear visually identical to first-party items (no special badge in v1)
- The PLUGINS sidebar section is **hidden when no plugins are installed**
- v1 does **not** ship: a marketplace, browse experience, in-product installer, or author-your-own scaffolding promotion

---

## What v1 explicitly does NOT include

These are intentionally excluded so the v1 surface stays focused:

- Inline AI chat on every page (deferred)
- Plugin marketplace / browse / install UI (deferred)
- Adapter swap wizard / form-based switching (docs-link only in v1)
- Widget-level plugin injection into existing pages (deferred)
- Page-replacement plugins (deferred)
- Voice / WebRTC module surfaces (deferred)
- Multi-region orchestration UI
- In-product code editor / IDE
- Visual workflow / drag-drop builder
- Author-your-own-plugin scaffolding promoted in UI

---

## Notes for the designer

- Mental model reference: **Supabase Studio**.
- Tolerate developer-grade information density.
- Modularity is communicated through the Adapters page and subtle "via <X>" adapter pills — not through prominent OSS branding.
- Tone: precise, fast, clean, helpful — like a great developer tool.
- Default mode of every page is **observation**. Mutation is always a drawer.
- Cmd+K is the universal jump. Design with the assumption that experienced operators rarely click the sidebar after the first day.
- Dark mode is the primary theme.

Everything visual is your judgment. This document is the substance; you bring the form.

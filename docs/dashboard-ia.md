# Dashboard Information Architecture

The dashboard is the operator console for an BackAI deployment — what a
dev or platform team uses to build and operate an AI-native backend.

## Principle

The dashboard has two mental modes, one audience-scoped view, and one
low-level plumbing section:

1. **Build** — configuring the product you're building
2. **Operate** — observing the product as it runs
3. **Customers** — your end-users (when multi-tenancy is enabled)
4. **Infrastructure** — database, storage, and secret primitives

Group nav by mental mode, not by feature category. A dev wiring up
webhooks and a dev watching webhook deliveries are doing different jobs;
they live in different groups.

## Conventions

- **Generic naming throughout.** "Agents," "runs," "tools" — never product-
  or vendor-specific terms. This is an independent open-source backend
  system. The agent runtime is one implementation detail; the dashboard
  doesn't tie its vocabulary to it.
- **Don't rebuild what others do better.** Where a deep view exists in the
  runtime UI, link out. Example: a "View full trace →" button on each run
  detail page opens the runtime's existing DAG view in a new tab. We don't
  recreate the graph.
- **No aspirational tabs.** Each tab pays for itself or doesn't exist.
- **No domain-specific surfaces.** No "eval" tab, no "prompt management"
  tab. Those mean different things per workload (chatbot eval ≠ code-agent
  eval ≠ RAG eval). Users build what's right for them using our primitives
  (storage, database, runs).

## Top-level navigation

```
[Home]   Build   Operate   Customers   Infrastructure          ⌘K   [user]
```

The "Customers" group is **always visible** but shows an empty state
when multi-tenancy is disabled — with a clear "Enable multi-tenancy to
use this section" CTA pointing at the docs.

## Groups and tabs

### Home (standalone)

Overview dashboard. The first thing anyone sees.

| Surface | Shows |
|---|---|
| Home | Requests/min, errors, cost today, queue depth, alerts, recent runs, recent webhook deliveries |

**Hero tab.** Polished to Linear/Vercel grade.

### Build — configuring your product

What you wire up when adding capabilities. Daily-touched during dev/staging.

| Tab | What you do here |
|---|---|
| **Agents** | Catalog of registered agents, schemas, sample inputs, attached tools |
| **Integrations** | MCP servers, skills, harnesses — anything that gives agents new capabilities. Per-tenant install when MT on. |
| **Webhooks** | Incoming endpoint definitions (Stripe → you, GitHub → you) and outgoing destinations |
| **Auth** | Identity providers (Google, GitHub, magic link, MFA), session config |
| **Billing** | The billing integration *your customers* see (Stripe / Lago): plans, metered metrics, webhook handlers |
| **MCP** | Model Context Protocol servers and tools |
| **Skills** | Installed AF skillkit bundles — install, list, attach |
| **Modules** | Read-only view of `config.yaml`: which suite modules are enabled, adapter choices. Editing happens in the file (git-tracked). |
| **Dashboard Plugins** | Read-only list of operator-console tabs discovered from `apps/dashboard/plugins/` at build time. |

### Operate — observing what's running

Live data. Where you go when something's happening or broken.

| Tab | What it shows |
|---|---|
| **Runs** | Agent executions list with filters (tenant, agent, status, cost, time). Click → summary card with logs inline. "View full trace →" links to the runtime's existing trace UI for the deep graph. |
| **Shipwright** | Coding-agent tasks with status, repo, AgentField execution link, and patch / PR pointer. |
| **Approvals** | Human approval requests with payload JSON, status filters, and approve / deny / cancel actions. |
| **Logs** | Live tail across all services. Search by tenant, agent, request ID. |
| **Queues** | Live job queue state — pending/running/failed counts, recent jobs, retry button. Distinct from "Jobs" in Build (definitions vs runtime). |
| **Cost** | Spend by model, agent, tenant, day. Budget alerts. Forecast. |
| **Sandbox Activity** | Live pool status, recent runs, per-run logs + artifacts + cost (when sandbox module enabled). |
| **Webhook Activity** | Recent incoming + outgoing deliveries with status, failures, retries. |
| **Notifications** | Outbox, recent delivery attempts, and notification adapter state. |
| **Crons** | Scheduled jobs that fire on a cron expression. |
| **Metrics** | Runtime KPIs and top routes. |

**Hero tab: Cost.** This is the differentiator vs "Supabase + Helicone separately." Polish to Linear/Vercel grade.

### Customers — your end users (always visible; gated by MT)

When multi-tenancy is **disabled**: each tab in this group shows an
empty state with "Enable multi-tenancy to use this section. See
`docs/multi-tenancy.md`."

When MT is **enabled**:

| Tab | What it shows |
|---|---|
| **Tenants** | List of your customer orgs. Click → drilldown: usage, cost, members, secrets, API keys, audit log. |
| **Users** | All end-users across tenants (your customers' team members) |
| **API Keys** | Keys issued to customers, scopes, rotation, last-used timestamps |
| **Customer Billing** | Per-tenant billing status (Stripe subscription, invoices, usage records sent) |
| **Audit** | Who did what when across tenants. Queryable + exportable for compliance. |

### Infrastructure — low-level backend plumbing

Backend primitives that support every product built on the stack. These
are intentionally separate from Build so product configuration stays
focused on what the fork ships.

| Tab | What it shows |
|---|---|
| **Adapters** | Active and available config-level swaps for storage, sandbox, notifications, and billing |
| **Database** | Tables you define, SQL runner, RLS policy view, vector collections, memory keys |
| **Storage** | Buckets you create, signed URL config, lifecycle rules |
| **Secrets** | Per-tenant or global secrets vault (LLM keys, GitHub tokens, Stripe keys, etc.) |

### Settings (standalone)

Suite-level operator settings (not your product config — that lives in Build).

| Surface | What you do here |
|---|---|
| Settings | Operator account, dashboard theme, feature flags, suite-level (not customer) API keys |

## Hero tabs (polish budget)

Two:

1. **Home** — first screen, viral screenshot
2. **Operate → Cost** — differentiator vs every non-AI backend platform

Every other tab uses standard shadcn components and ships
"shadcn-clean" but not hero polish.

## What's NOT in the dashboard

| Cut | Why |
|---|---|
| Eval / regression UI | Means radically different things across workloads (chatbot, code agent, RAG). Users build their own. |
| Prompt management / template library | Devs version prompts in git. UI for it is feature creep. |
| Marketplace / template gallery | Too early. The `examples/` folder is enough. |
| Custom domain manager | v2 feature. CLI for v1. |
| Multi-region admin | v2 feature. |
| Workflow designer / visual builder | Code is the interface. Not a no-code tool. |
| Deploy UI | CLI + git. |
| Schema migration UI | CLI: `af-stack db migrate`. |
| Adapter swap UI | `config.yaml` edit (git-tracked). |

## What links out instead of being rebuilt

| Where we link out | Why |
|---|---|
| Deep execution traces / DAG | The runtime already renders this well. We link from per-run detail page. |
| Detailed APM / metric dashboards | We emit OTel; users point at Grafana/SigNoz/Honeycomb. We don't recreate. |
| Stripe billing portal | Embed link, don't rebuild. |
| OAuth provider configuration | Link to provider consoles. |

## Per-tab depth rule

Each tab is **one screen** at depth 1 (list, dashboard, or tree). Depth 2
only for detail pages (per-run, per-tenant, per-customer). No deeper. If
something needs more than 2 clicks, the IA is wrong.

## Reference inspiration

| Product | What we steal |
|---|---|
| **Vercel** | Home page shape: deploys/traffic graph/recent activity at a glance |
| **Supabase Studio** | Database tab (table editor, SQL runner, RLS view) — embedded components |
| **Stripe Dashboard** | Per-customer detail page; "Developers" section pattern (API Keys / Webhooks visible, not buried) |
| **Linear** | Speed, keyboard-first nav, ⌘K, thoughtful empty states, group-then-tab nav structure |
| **Convex** | Inline logs on run detail (no graph required for most debugging) |
| **Helicone / Langfuse** | Cost dashboard cut (model × tenant × day) — but bundled, not standalone |
| **Inngest / Trigger.dev** | Run timeline with steps + retries visualized |
| **Sidekiq Web** | Queue monitor doing one thing well |

## What we don't steal from each

| Anti-pattern | Why we resist |
|---|---|
| Vercel's deeply nested Settings | Hard to find anything. Our Settings stays shallow. |
| Supabase Edge Functions tab | Most users never use it. Don't make a tab for everything. |
| Helicone's eval features | Means different things. Don't pick a definition. |
| PostHog's feature creep | They admit it. Stay focused. |

## End-user (customer-facing) dashboard

Not part of v1. The "scaffold for your customers' dashboard" lives in
the examples and workload-module path when we ship the Notable / Shipwright
templates. The v1 dashboard is operator-only.

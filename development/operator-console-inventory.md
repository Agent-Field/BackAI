# Operator Console — Navbar Audit

What every sidebar item is supposed to do, what's built today, and how
a developer who forks this configures it. The framing matters: **AF
Stack is a forkable template, not a SaaS**. The dashboard is the
operator's window into the running system; the *source of truth* for
configuration is your fork of the code + your `.env` file, not the UI.

This audit reads like a checklist so you can use it as a v1.1 backlog.

## Configuration model

Three tiers, in order of how a developer changes them:

| Tier | Where to change | Examples |
|---|---|---|
| **Code** | Edit a `.ts` / `.go` / `.py` file in your fork | Auth providers, custom dashboard plugins, new agents, workload modules, custom workload routes |
| **Env / config.yaml** | Edit `.env` and restart | Module on/off, LLM provider keys, sandbox adapter choice, storage backend |
| **Runtime data** | Use the dashboard or REST | Tenants, API keys, secrets, MCP servers, skills, crons, webhook endpoints, budgets |

The dashboard touches **tier 3** only. Tiers 1 + 2 are intentionally
code-first — this is what makes AF Stack forkable rather than a managed
SaaS. The dashboard helps you *see* tier 1 + 2 state but never write to
it.

---

## Home — the top-of-funnel page

| | |
|---|---|
| **Built?** | ✅ Real |
| **What it shows** | Last 24h cost sparkline, requests/min, error rate, queue depth, recent runs (10), recent webhook deliveries (5), open alerts |
| **Source** | `GET /api/v1/home/overview` aggregates from suite_cost_events + suite_jobs + suite_runs + suite_webhook_deliveries |
| **DX gap** | None major. Could add a "first thing I should look at today" callout. |

---

## Build group — "what does this fork ship?"

Every Build tab answers a single question for the developer: *"What
product configuration does my fork ship?"* These are code, env, and
runtime configuration surfaces for agents, modules, integrations,
webhook endpoints, auth, and billing. Low-level plumbing now lives in
Infrastructure.

### Agents

| | |
|---|---|
| **Built?** | ✅ Real |
| **What it shows** | All AF agents registered with the AgentField control plane at this runtime's startup, plus their reasoners and tags |
| **Source** | `GET /api/v1/agents` |
| **How developer configures** | **Code**: drop a Python file under `apps/backend/agents/<name>/main.py` with `Agent(node_id=…)` + `@app.reasoner`. The agent registers itself at startup. |
| **DX gap** | Empty by default in the bundled compose (only sample is there). The example: paste the code from `apps/backend/agents/sample/main.py`, your reasoners show up here. |

### Integrations

| | |
|---|---|
| **Built?** | ✅ Real (directory page, not data) |
| **What it shows** | 5-card directory linking to MCP, Skills, Agents, Webhooks, Sandboxes — with live counts |
| **DX gap** | None. This is navigation, by design. |

### Webhooks

| | |
|---|---|
| **Built?** | ✅ Real (inbound endpoint config) |
| **What it shows** | Table of inbound endpoints with HMAC algorithm + signature header + forward target + active toggle. |
| **Source** | `/api/v1/webhooks/endpoints` |
| **How developer configures** | **UI/REST** for adding endpoints. Code edit not needed. |
| **DX gap** | "Add endpoint" Dialog could include common-provider presets (GitHub, Stripe, Shopify) so you don't need to remember the signature header per vendor. |

### Auth

| | |
|---|---|
| **Built?** | ✅ Real (observation only) |
| **What it shows** | Which providers are enabled, which env vars they need, session storage layout |
| **Source** | `process.env` on the dashboard side |
| **How developer configures** | **Env first** for shipped providers: `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` for Google, or `AF_STACK_SSO_*` for enterprise SSO through an OIDC broker. Additional providers are code-owned edits in `apps/dashboard/src/lib/auth.ts`. |
| **DX gap** | The page is read-only. It reports the active auth env contract and callback URL, but it does not mutate auth provider config at runtime. |
| **Current OAuth / SSO** | Email+password and magic links are wired. Google OAuth is enabled when keys are present. Enterprise SSO/SAML is wired through better-auth generic OAuth: Authentik or WorkOS handles SAML and exposes OIDC to AF Stack. |

### Billing

| | |
|---|---|
| **Built?** | ✅ Real (observation + meter list) |
| **What it shows** | Stripe adapter mode (live vs stub), customer count, active meters, supported meter names |
| **Source** | `/api/v1/billing/customers` + `/api/v1/billing/meters` |
| **How developer configures** | **Env**: `STRIPE_SECRET_KEY=sk_test_…` or leave unset for stub. The adapter switches itself. |
| **DX gap** | "Open Stripe Portal" link goes to `example.com` in stub mode — confusing without context. The page should hide the button in stub mode (or make it explicit). |

### MCP

| | |
|---|---|
| **Built?** | ✅ Real (CRUD + call-tool) |
| **What it shows** | Configured servers with status + tools browser + ad-hoc call sheet |
| **Source** | `/api/v1/mcp/servers`, `/api/v1/mcp/tools`, `/api/v1/mcp/call` |
| **How developer configures** | **UI/REST**: add servers with transport (stdio/sse), command, env (with `secret:<key>` to pull from the vault). Or via `af-stack mcp add` CLI. |
| **DX gap** | All 4 seeded servers showed "errored" because `uvx` isn't in the runtime container — that's the architecture issue you flagged. Refactor coming. |

### Skills

| | |
|---|---|
| **Built?** | ✅ Real (install/attach/uninstall) |
| **What it shows** | Installed skill bundles with name + version + harnesses + tags + tenant scope + attach button |
| **Source** | `/api/v1/skills` + `/api/v1/skills/attach` |
| **How developer configures** | **UI/REST**: install by source string (`af-skill://vendor/name@version`, `/abs/path` to a `skill.toml`, or `embedded` for built-ins). |
| **DX gap** | The `af-skill://` source format is a stub registry — just URL parsing today. Local path works fully. |

### Harnesses (folded into Agents)

| | |
|---|---|
| **Built?** | ✅ Probe-only, surfaced on Build → Agents |
| **What it shows** | The 4 supported CLI providers (Claude Code, Codex, Gemini, OpenCode) with status badge (ready / needs_auth / missing / errored) |
| **Source** | `/api/v1/harnesses` |
| **How developer configures** | **Agent container**: install the CLI binary in `apps/backend/agents/<name>/Dockerfile`; the agent declares capabilities through AgentField. |
| **DX gap** | The dedicated tab is gone. Next pass should make the Agents page show harness readiness per registered agent once the API exposes per-agent ownership. |

### Modules

| | |
|---|---|
| **Built?** | ✅ Real |
| **What it shows** | 13 built-in modules + workload modules with enabled/disabled badge and per-module descriptions |
| **Source** | `/api/v1/modules` |
| **How developer configures** | **Env or config.yaml**: `AF_STACK_MODULE_<UPPER_SNAKE>=true|false` or edit `apps/backend/config.yaml`. Restart. |
| **DX gap** | None major. The page is honest about being read-only with restart-to-change semantics. |

---

## Operate group — "what's happening RIGHT NOW?"

Every Operate tab is a live monitoring surface.

### Runs

| | |
|---|---|
| **Built?** | ✅ Real |
| **What it shows** | Agent execution history with status, agent name, tenant, duration, cost |
| **Source** | `/api/v1/runs` |
| **DX gap** | Drill-down sheet exists; could add a "rerun with same input" button. |

### Shipwright

| | |
|---|---|
| **Built?** | ✅ Real |
| **What it shows** | Coding-agent task queue with status, repo, AgentField execution link, and final patch / PR pointer |
| **Source** | `/api/v1/shipwright/tasks` |
| **DX gap** | The page links out to AgentField for live DAG / step detail rather than rebuilding that inspector in AF Stack, by design. Production hardening remains around deeper git workload primitives. |

### Approvals

| | |
|---|---|
| **Built?** | ✅ Real |
| **What it shows** | Tenant-scoped human approval requests with payload JSON, status filter, and approve / deny / cancel actions |
| **Source** | `/api/v1/approvals` |
| **DX gap** | No blocking wait helper yet in the SDK; callers poll `suite.approvals.get()` or list pending requests. |

### Logs

| | |
|---|---|
| **Built?** | ✅ Real (5s polling) |
| **What it shows** | Live runtime log stream from the in-process ring buffer with level + free-text search |
| **Source** | `/api/v1/logs` |
| **DX gap** | No tail follow. Logs only — no trace integration in the UI yet. |

### Queues

| | |
|---|---|
| **Built?** | ✅ Real |
| **What it shows** | Job state counts + recent jobs + per-kind stats |
| **Source** | `/api/v1/queues/summary` |
| **DX gap** | No "drain queue" admin button (which you'd want for production incidents). |

### Cost

| | |
|---|---|
| **Built?** | ✅ Real |
| **What it shows** | 24-bucket sparkline + period total vs previous period + breakdown by model / agent / tenant / day + live cost event stream |
| **Source** | `/api/v1/cost`, `/api/v1/cost/events` |
| **DX gap** | None major. This page is the strongest tab in the dashboard. |

### Sandbox Activity

| | |
|---|---|
| **Built?** | ✅ Real |
| **What it shows** | Pool KPIs (warm / active / queued) + adapter card + recent runs table with status badges + drilldown sheet with stdout/stderr download links |
| **Source** | `/api/v1/sandbox/pool`, `/api/v1/sandbox/runs` |
| **DX gap** | No in-flight stream of stdout yet (Stream method exists on the adapter interface but isn't wired to the UI). |

### Notifications

| | |
|---|---|
| **Built?** | ✅ Real |
| **What it shows** | Delivery table with status badges, KPI strip, "Send notification" Dialog |
| **Source** | `/api/v1/notifications`, `/api/v1/notifications/stats` |
| **DX gap** | When adapter is `log`, the "Send notification" button still works but the message goes to the log buffer — could explain that more clearly in the Dialog. |

### Webhook Activity

| | |
|---|---|
| **Built?** | ✅ Real |
| **What it shows** | Unified inbound + outbound delivery feed with direction-arrow icons, status badges, response codes, attempts |
| **Source** | `/api/v1/webhooks/deliveries` |
| **DX gap** | No "replay this delivery" button on the row, which is a common ops need. |

### Crons

| | |
|---|---|
| **Built?** | ✅ Real (CRUD) |
| **What it shows** | Schedule table with name + crontab + job + next run + last run + active toggle + Create dialog |
| **Source** | `/api/v1/crons` |
| **DX gap** | None major. |

### Metrics

| | |
|---|---|
| **Built?** | ✅ Real |
| **What it shows** | Runtime KPIs (requests, p95, goroutines, heap, uptime) + top-10 routes by request count |
| **Source** | `/api/v1/metrics/summary` |
| **DX gap** | No graph over time — just the current snapshot. For trends, use external Prometheus scrape of `/metrics`. |

### Cost Explorer (plugin)

| | |
|---|---|
| **Built?** | ✅ Real (first-party plugin) |
| **What it shows** | Top spenders by model + by tenant, period total + delta, MoM delta bar |
| **Why it's a plugin** | Reference implementation of the plugin pattern so external developers can see how to add their own tabs |
| **DX gap** | None. This is a teaching example. |

### Dashboard Plugins

| | |
|---|---|
| **Built?** | ✅ Real |
| **What it shows** | Read-only list of dashboard plugins bundled into the current build, including source folder, sidebar group, and route. |
| **Source** | `apps/dashboard/plugins/*` via build-time scanner |
| **DX gap** | None for v1. Plugins are fork code, not marketplace packages. |

---

## Customers group — only renders when multi-tenancy is on

These tabs hide entirely when MT is off (single-tenant deploy).

### Tenants

| | |
|---|---|
| **Built?** | ✅ Real (list + drilldown) |
| **What it shows** | Tenant table with slug + name + plan + member count + 30d cost. Click → drilldown with usage sparklines + members + keys + recent runs + recent webhooks + billing card |
| **Source** | `/api/v1/admin/tenants`, `/api/v1/admin/tenants/{id}/drilldown` |
| **DX gap** | "Create tenant" Dialog exists, but no "invite member by email" flow yet. |

### Users

| | |
|---|---|
| **Built?** | ✅ Real (read-only list) |
| **What it shows** | User table with email + last_active_at + membership count |
| **Source** | `/api/v1/admin/users` |
| **DX gap** | No "force password reset" or "deactivate" actions yet. |

### API Keys

| | |
|---|---|
| **Built?** | ✅ Real |
| **What it shows** | All keys across tenants with prefix + tenant + scopes + last_used + status, plus "Issue key" Dialog |
| **Source** | `/api/v1/admin/keys` |
| **DX gap** | None major. |

### Customer Billing

| | |
|---|---|
| **Built?** | ✅ Real shape, stubbed Stripe data without key |
| **What it shows** | Per-tenant Stripe customer status + usage meters + Stripe Portal button |
| **Source** | `/api/v1/billing/customers`, `/api/v1/billing/meters` |
| **DX gap** | In stub mode the Portal button goes to example.com — should hide or label clearly. |
| **Visibility** | Hidden from sidebar when `AF_STACK_BILLING_ADAPTER=none`; direct visits show an "Enable billing" empty state. |

### Audit

| | |
|---|---|
| **Built?** | ✅ Real (now — was empty until we wired the audit hooks an hour ago) |
| **What it shows** | Admin action log with timestamp, action badge, tenant, actor, resource, metadata |
| **Source** | `/api/v1/admin/audit` |
| **DX gap** | No CSV export. |

### ~~Notable~~ — REMOVED

This was an example plugin leaking through. Fixed in this commit.
Examples that want their own dashboard surfaces should ship a plugin
under `examples/<id>/dashboard-plugin/` and the operator copies it into
`apps/dashboard/plugins/<id>/` only when running that example.

---

## Infrastructure group — "what low-level plumbing backs the stack?"

Infrastructure tabs are the operator's read/write surfaces for storage,
database, and secret primitives. They are intentionally separate from
Build so product configuration does not mix with low-level backend
plumbing.

### Adapters

| | |
|---|---|
| **Built?** | ✅ Real (read-only) |
| **What it shows** | Active adapter plus available and planned choices for Storage, Sandbox, Notifications, and Billing |
| **Source** | Dashboard env (`AF_STACK_S3_ADAPTER`, `AF_STACK_SANDBOX_ADAPTER`, `AF_STACK_NOTIFICATIONS_ADAPTER`, `AF_STACK_BILLING_ADAPTER`, `STRIPE_SECRET_KEY`) |
| **How developer configures** | **Env/config**: edit the relevant env var or deploy target, then restart. |
| **DX gap** | No in-UI swap by design. Config stays in the fork/deploy target. |

### Database

| | |
|---|---|
| **Built?** | ✅ Real |
| **What it shows** | Schema browser, row viewer (paginated), structure tab, RLS policies tab, SQL runner, memory browser |
| **Source** | `GET /api/v1/db/tables`, `/api/v1/db/rows`, `/api/v1/memory` |
| **How developer configures** | **Code/migrations**: schema lives in `services/runtime/internal/db/migrations/`. Add a workload module with its own migrations under `workload-modules/<id>/migrations/`. |
| **DX gap** | The SQL runner is RLS-aware (binds the operator's session) so it can't shoot tenants in the foot. Could surface this more clearly in UI copy. |

### Storage

| | |
|---|---|
| **Built?** | ✅ Real |
| **What it shows** | Object browser scoped to the runtime's bucket. Upload, download via signed URL, delete. |
| **Source** | `GET /api/v1/storage/objects`, `POST /api/v1/storage/objects` |
| **How developer configures** | **Env**: `AF_STACK_S3_ADAPTER=minio` (default) or `s3`. Then `AF_STACK_S3_ENDPOINT`, `AF_STACK_S3_BUCKET`, `AF_STACK_S3_ACCESS_KEY`, `AF_STACK_S3_SECRET_KEY`. |
| **DX gap** | The browser shows sandbox stdout/stderr blobs by default; could add tenant filter. |

### Secrets

| | |
|---|---|
| **Built?** | ✅ Real (CRUD + reveal with audit) |
| **What it shows** | Vault entries, masked. Reveal pops a Dialog and writes a `secret.reveal` audit row. |
| **Source** | `/api/v1/secrets` |
| **How developer configures** | **UI/REST** for per-tenant secrets. **Env** for the KMS key that encrypts them: `AF_STACK_KMS_KEY` (32-byte hex). |
| **DX gap** | None. This one is right. |

---

## System group

### Hello (placeholder)

The `hello` plugin was a scaffolding artifact. Removed.

### Settings

| | |
|---|---|
| **Built?** | ⚠️ Stub |
| **What it shows** | A "Settings page lands later" message |
| **What it should show** | Operator profile, dashboard theme override, telemetry opt-in/out, factory reset |
| **DX gap** | Lowest priority — the profile dropdown handles sign-out which is the only critical thing. |

---

## How OAuth actually works today

To answer your specific question:

1. **Email + password is wired and works.** Sign-up at the customer
   app, sign-in at the operator dashboard.

2. **Google OAuth code path exists but you need to provide keys.**
   Edit your `.env`:
   ```
   GOOGLE_CLIENT_ID=<your-id>
   GOOGLE_CLIENT_SECRET=<your-secret>
   ```
   Restart the dashboard. The better-auth Google provider is then active.

3. **Enterprise SSO/SAML is wired through OIDC.** Set
   `AF_STACK_SSO_ISSUER`, `AF_STACK_SSO_CLIENT_ID`, and
   `AF_STACK_SSO_CLIENT_SECRET`. Use Authentik for the self-hosted
   SAML-to-OIDC bridge, or WorkOS for the managed broker. The callback is
   `<BETTER_AUTH_URL>/api/auth/oauth2/callback/enterprise-sso`.

4. **Magic link** is wired via the `magicLink` plugin but the
   `sendMagicLink` callback currently logs the link to stdout instead
   of emailing it. To make it real: forward to the notifications
   module instead of `console.log`.

5. **The `/build/auth` page** today shows what's *configured* — but
   you're right that it should also show the **code snippet** to add a
   provider, because that's how you actually add one. Adding that to
   the page is a 10-minute edit.

---

## Where the dashboard is the WRONG place to configure

These are intentionally code-only — the dashboard observes them but
won't write to them, because a forkable template invites code edits:

- **Adding an OAuth provider beyond Google / enterprise SSO** → edit `apps/dashboard/src/lib/auth.ts`
- **Changing the default LLM model** → edit `pricing.go` catalog
- **Changing rate-limit defaults** → edit `ratelimit/config.go`
- **Adding a new module** → write a Go package + register in main.go
- **Adding a sandbox adapter** → implement the interface + register

These are intentionally dashboard-driven:

- Tenants, members, API keys, secrets, MCP servers, skills, crons,
  webhook endpoints, budgets, dashboard plugins (file-drop discovery).

Anything in the second bullet that you'd expect to be in the first
bullet (or vice versa) is a DX bug worth reporting.

---

## The "forkable template" framing

The repos this is inspired by:

| Tool | What we steal | Where we diverge |
|---|---|---|
| **Cal.com** | Self-hostable SaaS with auth + tenants + billing all in one repo | They're a single product; we're a backend you ship many products on |
| **Plane** | Operator console pattern + plugin extensibility | They target project management; we target AI products |
| **Supabase** (self-hosted) | Backend-in-a-box with managed PG | They don't ship AI primitives; we add cost ledger + sandboxes + MCP |
| **Mastra** | Agent framework | They don't ship the multi-tenancy + billing + dashboard plumbing; we host theirs (AgentField) as one of the layers |
| **Better-T-Stack** / **Create-T3-App** | Curated stack template | They scaffold a starter; we ship the running services + dashboard too |

Pick a tab in the operator console. If the experience is "I expect to
edit code" — that's a tier-1 surface. If it's "I expect to click" —
that's a tier-3 surface. The mix is what makes this forkable rather
than a SaaS we charge for.

> **Archived 2026-06-07.** This document covers Phase 0-16 (now shipped).
> Kept for historical context. For current state, see
> [`STRATEGY.md`](../../development/strategy.md) and [`STACK.md`](../stack.md).

# AF Stack: Product Requirements Document

**Codename**: `af-stack` (see `BRAND.yaml`)
**Owner**: TBD
**Status**: Draft, ready to start coding
**Last updated**: 2026-06-06

## 1. Summary

AF Stack is the open backend platform for the AI era. It bundles AgentField
with everything else needed to ship a real product — identity, multi-tenancy,
sandboxes, storage, queues, public APIs, billing, dashboard — in a single
fork-friendly monorepo.

The thing developers clone, customize, and deploy. Not a hosted service. Not
a closed SDK. Not a framework. A complete, self-hostable backend platform
where AI is a native compute primitive.

## 2. Why this exists

### Problem

Building an AI-native product today requires assembling 10+ disconnected
services: auth (Clerk/WorkOS), database (Supabase/RDS), object storage
(S3/R2), queue (Inngest/Hatchet), LLM gateway (Helicone/Portkey), agent
runtime (AgentField/Mastra/CrewAI), sandboxes (e2b/Modal), webhooks (Svix),
billing (Stripe/Lago), observability (Langfuse/Datadog), and the glue between
them. Each integration costs weeks of work. Each vendor introduces lock-in.

Existing "backend-in-a-box" products (Supabase, Convex, Appwrite) don't
include AI primitives. Existing AI platforms (Dify, Langflow, Mastra) don't
include backend primitives. Builders rebuild the same plumbing for every
project.

### Opportunity

A single self-hostable monorepo that includes both halves (general backend
+ AI primitives), opinionated where opinionation accelerates, modular where
modularity matters. Distributed under a permissive license. Forkable.
Customizable.

### Strategic goal

Drive AgentField adoption by being the canonical "everything around AF"
package. Every AF Stack deployment is an AF deployment. Every AF user who
needs production plumbing finds AF Stack first.

## 3. Target users

### Primary: Solo developers and indie hackers

- Building AI-native side projects or solo SaaS
- Want `docker compose up` to just work
- Don't want to operate 10 separate services
- Will fork the repo and customize

### Primary: Startup engineering teams

- 2-10 person teams building production AI products
- Need auth, multi-tenancy, billing, isolation
- Need production-grade sandboxes for code agents
- Will run on their own infrastructure
- Want to own the code (vendor lock-in is a non-starter)

### Secondary: Enterprise platform teams

- Internal AI platforms for multiple business units
- Need SSO, RBAC, audit, sovereign deployment
- Will fork and customize heavily
- Want OSS as baseline, will add proprietary integrations on top

### Non-users (explicitly out of scope)

- No-code platform builders (Dify, Flowise are better)
- Pure RAG-only use cases (Llamaindex is enough)
- Teams unwilling to self-host (we don't ship hosted)
- Teams that don't use AI (this isn't a general backend competitor)

## 4. Success metrics

### Adoption (v1 launch + 6 months)

- 5,000+ GitHub stars within 6 months of v1 launch
- 500+ public forks
- 50+ deployments visible in public examples / case studies
- 10+ community-contributed adapters
- 100+ Discord/Slack community members

### AgentField virality (the real goal)

- 50%+ of new AF deployments come via AF Stack
- 30%+ of AF Stack deployments use 5+ AF agents
- LLM gateway endpoint adoption: 10k+ unique deployments using
  OpenAI-compatible endpoint

### Product quality

- 60-second quickstart from `git clone` to working dashboard with sample agent
- < 5 minutes to first custom agent deployed
- < 30 minutes to multi-tenant SaaS with auth + billing wired
- < 1 day to fork, rebrand, and ship a derivative product

### Operational

- All five hero dashboard tabs at Linear/Vercel-grade polish
- Functional CRUD for remaining tabs (no glaring gaps)
- API surface: every documented method works
- OpenAPI spec generated and accurate for every endpoint

## 5. Product principles

### 5.1 Fork-friendly is a feature, not a side effect

Everything inspectable. Everything deletable. Config in files, git-tracked.
Dashboard is editable code, not a closed binary.

### 5.2 Opinionated core, pluggable edges

Postgres + AgentField + Next.js dashboard are locked. Sandbox adapter, email
provider, billing engine, vector store, queue at scale: swappable adapters
with clear interfaces.

### 5.3 Every LLM call goes through AgentField

No `suite.llm.chat()` bypass. OpenAI-compatible endpoint is a shim that
routes through AF. Identity, cost, traces, audit are preserved invariantly.

### 5.4 Two SDKs, clear boundary

`app.*` (AgentField SDK) defines agents. `suite.*` (Suite SDK) calls them
and runs everything else. Inside an agent, both work. Outside, only suite.

### 5.5 Small operational SDK, separate admin SDK

Main SDK = ~22 methods devs use daily. Admin SDK = ~30 methods for
governance/management, separate package, separate credentials. Dashboard
+ CLI handle everything else.

### 5.6 Semantic verbs over generic operations

`suite.jobs.enqueue()` not `suite.send_to_queue()`. `suite.agents.call()`
not `suite.execute_function()`. Names express intent.

### 5.7 60-second quickstart is sacred

`git clone` → `docker compose up` → working dashboard with a sample agent
running in 60 seconds. If a feature breaks this, it doesn't ship in v1.

### 5.8 Honest defaults, clear extension paths

Document every opinionated choice and how to replace it. No surprises.

## 6. Scope: what's in v1

### 6.1 Compute primitives

All six compute shapes are first-class:

1. **HTTP handlers** — sync REST endpoints (suite gateway + app code)
2. **Async agents** — AF's durable execution, hours/days OK
3. **Background jobs** — River-backed PG queue for non-AI work
4. **Scheduled tasks** — River cron for recurring work
5. **Event triggers** — webhooks in, queue subscribers, change streams (workload module)
6. **Streams** — SSE/WS for live output
7. **Sandboxed execution** — Docker / gVisor / Firecracker / e2b adapters

### 6.2 Modules (16 total)

**Core (always on)**:
- AgentField runtime
- Postgres (state, vector, FTS, queue)
- Single-binary Go runtime (suite gateway, jobs, scheduler)
- Next.js dashboard
- Module + adapter + hook system

**Always available**:
- identity (better-auth)
- public-gateway (with OpenAPI auto-gen)
- LLM gateway shim (OpenAI-compatible, routes through AF)
- jobs (River)
- crons
- secrets-vault (PG + KMS envelope)
- storage (MinIO/S3 adapter)
- notifications (log-stub + Resend default)
- webhooks-in (Svix)
- billing (Stripe direct + Lago documented)
- observability (OpenTelemetry emit)
- MCP client (Anthropic SDK bundled)
- skills (wraps AF skillkit)

**Optional (off by default)**:
- multi-tenancy (PG RLS, one-flag enable)
- sandbox (4 adapters at launch)

**Workload modules (importable)**:
- git-workload
- multimodal-storage
- change-stream-listener

### 6.3 Dashboard

Single Next.js app, **operator-only** in v1. Customer-facing scaffold
lands in Phase 13.

See `docs/dashboard-ia.md` for the complete information architecture.

**Top-level navigation** (mental-mode groups, not feature categories):

```
[Home]   Build   Operate   Customers   [Settings]
```

- **Home** — overview dashboard (requests/min, errors, cost today, queue
  depth, recent runs, alerts)
- **Build** — configuring your product: Agents · Integrations · Database
  · Storage · Secrets · Webhooks · Auth · Billing · Jobs · Sandboxes ·
  Modules (read-only)
- **Operate** — observing what runs: Runs · Logs · Queues · Cost ·
  Sandbox Activity · Webhook Activity
- **Customers** — your end users: Tenants · Users · API Keys · Customer
  Billing · Audit. Always visible; each tab shows an empty state with
  "Enable multi-tenancy" CTA when MT is off.
- **Settings** — suite-level operator settings (account, theme, plugins,
  flags). Not your product config.

**Two hero tabs** at Linear/Vercel-grade polish:

1. **Home** — first screen, viral screenshot
2. **Operate → Cost** — differentiator vs Supabase + Helicone separately

Every other tab uses standard shadcn components, ships shadcn-clean,
no extra polish budget.

**Generic vocabulary throughout** — "agents," "runs," "tools" — never
vendor-specific. The runtime is an implementation detail.

**Link out, don't rebuild** — the per-run page shows a summary card with
"View full trace →" linking to the runtime's existing DAG view. We do
not duplicate the graph.

**Not in the dashboard**: eval/regression UI, prompt management,
workflow designer, deploy UI. See `docs/dashboard-ia.md`.

### 6.4 SDKs

**Main Suite SDK** (`suite.*`, ~22 methods):
- `ctx.tenant_id`, `ctx.user_id`, `ctx.request_id`
- `suite.agents.call/call_async/stream/status/cancel/approve/deny/pending_approvals`
- `suite.jobs.enqueue`
- `suite.secrets.get`
- `suite.storage.upload/signed_url/download`
- `suite.notifications.email`
- `suite.billing.meter/has_budget`
- `suite.memory.get/set/search`
- `suite.sandbox.run`
- `suite.webhooks.send`
- `suite.pubsub.publish/subscribe`
- `suite.tools.list_mcp_tools/call_mcp`

**Admin SDK** (`suite.admin.*`, separate package, ~30 methods): agents
versioning/deploy, policy, MCP install, secrets write, tenants/users/keys
management, audit, skills install.

**Languages**: Python first, TypeScript parity, Go after.

**REST + OpenAPI** for everything not in an SDK.

### 6.5 Deploy targets

- `docker-compose.yml` (default for local + small VPS)
- Helm chart (k8s)
- Fly.io / Railway / Render one-click buttons
- Nomad job spec
- Documented swap paths for cloud-managed components

### 6.6 Examples (6 ship in v1)

| Example | Shape | Validates |
|---|---|---|
| 01-notable | Notion-with-AI (pure app) | Suite as general backend |
| 02-shipwright | SWE-AF SaaS (heavy AI) | Sandbox + MT + heavy agents |
| 03-llm-gateway-only | Just the OpenAI shim | Lowest-commitment viral wedge |
| 04-podcast-creator | Multimodal generator | Multimodal workload module |
| 05-reactive-enrichment | Event-driven pipeline | Change-stream module |
| 06-deep-research | Long-running fan-out | Massive AF agent counts |

## 7. Out of scope for v1

Listed honestly so we don't scope-creep:

- SSO/SAML (BoxyHQ/Zitadel) — adapter documented, build in v2
- Polished LLM observability UI (Langfuse-grade) — basic in v1, deep in v2
- External vector store adapters (Pinecone, Qdrant) — pgvector only in v1
- Serverless sandbox adapters (Lambda, Modal, Cloud Run) — 4 adapters in v1
- Custom domain / multi-region
- Backup/restore tooling
- Module registry / marketplace UI
- Rust / Ruby / Java SDKs
- Eval module
- A2A / AGNTCY protocol support
- "Suite as MCP server" gateway (v2 hero candidate)
- Mobile app for the dashboard

## 8. User stories

### 8.1 Solo dev, weekend project

> As a solo developer, I want to clone the repo, run `docker compose up`,
> see a working dashboard, and have my first custom agent deployed in
> under 30 minutes.

Acceptance:
- README quickstart is < 10 lines of commands
- Sample agent ships and works without configuration changes
- Hot reload on agent code changes
- One env var (`OPENROUTER_API_KEY` or similar) is the only required config

### 8.2 Startup CTO, evaluating

> As a CTO evaluating backends for our AI product, I want to see whether
> the suite handles auth, multi-tenancy, billing, and sandboxes in a way
> we wouldn't have to replace within 6 months.

Acceptance:
- Multi-tenancy can be enabled via config flag + migration
- Better-auth supports the auth flows we need (OAuth, magic link, MFA)
- Stripe integration works out of the box
- Sandbox adapter swap from Docker (dev) to Firecracker (prod) is a config change
- Honest documentation of every limitation

### 8.3 SaaS builder, productizing

> As a builder taking my agent demo to production, I want to add identity,
> billing, sandboxes, and a customer-facing dashboard without rebuilding
> the core agent code.

Acceptance:
- AF agent code from prototype works unchanged in suite
- Enable modules via config: `multi-tenancy`, `billing`, `sandbox`
- End-user dashboard scaffold provides login/billing/usage immediately
- Customer onboarding flow can be scripted via admin SDK

### 8.4 Enterprise platform team, internal tool

> As a platform team, I want to host the suite on our k8s, integrate with
> our SSO, route LLM calls through our gateway, and ship internal tools
> our business units consume.

Acceptance:
- Helm chart deploys to k8s
- SSO adapter contract is documented; we can implement BoxyHQ ourselves
- LLM gateway adapter can route to our internal proxy
- Multi-tenancy supports per-business-unit isolation
- Audit log is queryable and exportable
- All admin operations work via admin SDK (scriptable)

### 8.5 Open source contributor

> As an OSS contributor, I want to add a new sandbox adapter or dashboard
> plugin without forking the upstream repo permanently.

Acceptance:
- Adapter contract is a Go interface < 100 lines
- Existing adapters (docker, gvisor, e2b) are reference implementations
- Plugin manifest format is documented
- Adapters and plugins ship as separate repos installable via CLI

## 9. Functional requirements

### 9.1 Runtime

- **R-RT-1**: Single Go binary (`af-stack`) embeds: HTTP gateway, jobs
  runner, scheduler, hook engine, module loader, OpenAPI generator
- **R-RT-2**: Stateless when configured with Postgres
  (`AGENTFIELD_STORAGE_MODE=postgresql`, memory-fallback off)
- **R-RT-3**: Horizontal scaling via load balancer + replica count
- **R-RT-4**: Hot reload of agent code via file watcher in dev mode
- **R-RT-5**: Single `docker compose up` brings up all services in < 60s
  on a modern dev machine
- **R-RT-6**: Helm chart supports N-replica deployments with shared PG

### 9.2 AgentField integration

- **R-AF-1**: AF control plane runs in PG mode, memory-fallback disabled
- **R-AF-2**: Suite invokes AF via REST + WebSocket (existing AF surface)
- **R-AF-3**: AF executions auto-tagged with tenant context when MT enabled
- **R-AF-4**: AF DAGs visible in suite dashboard (proxied/embedded)
- **R-AF-5**: AF verifiable credentials accessible via `suite.admin.audit.verify_credential`
- **R-AF-6**: AF skillkit operations exposed via `suite.admin.skills.*`

### 9.3 LLM gateway (OpenAI-compatible shim)

- **R-LLM-1**: Endpoint `POST /api/v1/llm/chat/completions` accepts
  OpenAI-format requests
- **R-LLM-2**: Endpoint `POST /api/v1/llm/embeddings` and `images/generations`
  for completeness
- **R-LLM-3**: All requests routed through AF (no provider direct path)
- **R-LLM-4**: Per-tenant API key resolution from Authorization header
- **R-LLM-5**: Cost tracked per tenant per model
- **R-LLM-6**: Streaming SSE supported in OpenAI format
- **R-LLM-7**: Existing OpenAI SDK clients work without code changes by
  pointing `base_url` at the suite

### 9.4 Identity

- **R-ID-1**: better-auth integrated with PG storage
- **R-ID-2**: Email + password, OAuth (Google, GitHub, MS), magic link supported
- **R-ID-3**: Session cookies and bearer tokens both accepted
- **R-ID-4**: MFA supported (TOTP at minimum)
- **R-ID-5**: Admin SDK can create users, list, disable, reset passwords

### 9.5 Multi-tenancy

- **R-MT-1**: Off by default. Enable via `config.yaml` + migration
- **R-MT-2**: Every domain table gets `tenant_id` (uuid)
- **R-MT-3**: PG row-level security enforces tenant isolation
- **R-MT-4**: Middleware sets `app.tenant_id` session variable per request
- **R-MT-5**: Object storage paths prefixed `s3://bucket/tenants/{tenant_id}/`
- **R-MT-6**: Secrets scoped to tenant + KMS key per tenant
- **R-MT-7**: Sandbox workspaces isolated per tenant
- **R-MT-8**: AF agents tagged with `tenant:{tenant_id}` for policy enforcement

### 9.6 Public API gateway

- **R-GW-1**: API key authentication with scoped permissions
- **R-GW-2**: Per-tenant per-endpoint rate limiting
- **R-GW-3**: Schema validation at the edge (request and response)
- **R-GW-4**: OpenAPI 3.1 spec auto-generated at `/openapi.json`
- **R-GW-5**: Async endpoints expose AF's submit/poll/webhook/SSE pattern
- **R-GW-6**: Request audit log keyed on tenant + API key + endpoint

### 9.7 Jobs (App-level)

- **R-JB-1**: River-backed PG queue (separate tables from AF)
- **R-JB-2**: `suite.jobs.enqueue(name, args)` queues a job
- **R-JB-3**: Job handlers declared in `apps/backend/jobs/` discovered at startup
- **R-JB-4**: Retries with exponential backoff
- **R-JB-5**: Dead-letter queue for permanently failed jobs
- **R-JB-6**: Per-tenant job concurrency limits
- **R-JB-7**: Dashboard tab shows queued/running/failed counts + tail

### 9.8 Crons

- **R-CR-1**: River cron extension
- **R-CR-2**: Crons declared in `apps/backend/crons/` with cron expression
- **R-CR-3**: Dashboard shows schedule + last run + next run
- **R-CR-4**: Manual trigger via dashboard or admin SDK

### 9.9 Sandbox

- **R-SB-1**: Adapter interface (Go) with Run/Stream/Stop/Capabilities methods
- **R-SB-2**: Four adapters shipped: docker, gvisor, firecracker, e2b
- **R-SB-3**: `suite.sandbox.run()` returns exit code, stdout, stderr, artifacts
- **R-SB-4**: Network restriction modes: `open`, `restricted`, `isolated`
- **R-SB-5**: Per-tenant workspace isolation when MT enabled
- **R-SB-6**: Capabilities published per adapter (timeout, GPU support, etc.)
- **R-SB-7**: Cost tracked in cpu-seconds and reported to billing meter

### 9.10 Storage

- **R-ST-1**: MinIO bundled as default
- **R-ST-2**: S3 adapter (AWS, R2, GCS via S3 API) configurable
- **R-ST-3**: `suite.storage.upload(bytes, key)` and `signed_url(key, ttl)`
- **R-ST-4**: Per-tenant prefix isolation when MT enabled
- **R-ST-5**: Dashboard browser for buckets

### 9.11 Secrets vault

- **R-SC-1**: PG storage with KMS envelope encryption
- **R-SC-2**: Per-tenant KMS key when MT enabled
- **R-SC-3**: `suite.secrets.get(key)` returns plaintext (decrypted in-process)
- **R-SC-4**: Admin SDK: set, list, delete, rotate
- **R-SC-5**: Dashboard UI shows keys (redacted), supports rotation
- **R-SC-6**: Adapter documented for Vault, Doppler, Infisical

### 9.12 Notifications

- **R-NT-1**: Outbox pattern in PG
- **R-NT-2**: Default email adapter: log-stub (writes to console)
- **R-NT-3**: Resend adapter (one env var)
- **R-NT-4**: `suite.notifications.email(to, template, data)` enqueues
- **R-NT-5**: Templates in `apps/backend/templates/` (MJML or React Email)
- **R-NT-6**: Failed sends retried with exponential backoff

### 9.13 Webhooks (in + out)

- **R-WI-1**: Svix bundled for incoming webhooks
- **R-WI-2**: `gateway.yaml` declares incoming webhook endpoints with verifier
  (hmac_sha256, etc.) and target AF agent
- **R-WI-3**: Outgoing webhooks via PG outbox + retry worker
- **R-WI-4**: `suite.webhooks.send(url, payload, secret)` enqueues

### 9.14 Billing

- **R-BL-1**: Per-tenant usage counters in PG (llm_tokens, sandbox_seconds,
  storage_gb, api_calls, jobs)
- **R-BL-2**: `suite.billing.meter(metric, value, tags)` records usage
- **R-BL-3**: Stripe adapter: customer creation, subscription, invoicing
- **R-BL-4**: Lago adapter documented (advanced usage-based billing)
- **R-BL-5**: `suite.billing.has_budget(amount_usd)` for pre-flight checks
- **R-BL-6**: Dashboard shows per-tenant cost breakdown

### 9.15 Observability

- **R-OB-1**: OpenTelemetry SDK in suite runtime, AF, agent SDKs
- **R-OB-2**: Bundled OTel collector forwards to user-configured backend
- **R-OB-3**: Prometheus metrics at `/metrics`
- **R-OB-4**: Structured JSON logs to stdout
- **R-OB-5**: Dashboard "Observe" group has log search + metrics overview
- **R-OB-6**: Adapters documented: SigNoz, Grafana stack, Honeycomb, Datadog

### 9.16 MCP client

- **R-MC-1**: Anthropic MCP Python SDK bundled in `af_stack` package
- **R-MC-2**: Anthropic MCP TS SDK bundled in `@af-stack/sdk`
- **R-MC-3**: Suite runtime can host MCP server connections (stdio + SSE)
- **R-MC-4**: `config.yaml` block declares MCP servers per tenant or globally
- **R-MC-5**: `suite.tools.list_mcp_tools()` returns tools available to caller
- **R-MC-6**: `suite.tools.call_mcp(server, tool, args)` invokes a tool
- **R-MC-7**: Dashboard tab shows installed MCP servers + tools per server
- **R-MC-8**: Per-tenant MCP server isolation when MT enabled

### 9.17 Module + adapter + hook system

- **R-MD-1**: Modules in `modules/` discovered at startup via manifest
- **R-MD-2**: Adapters in `modules/<mod>/adapters/<name>/` selectable in config
- **R-MD-3**: Hooks registered via decorator/annotation in user code
- **R-MD-4**: Hook points: `gateway.pre_auth`, `af.pre_execute`,
  `llm.pre_call`, `sandbox.pre_run`, `storage.pre_upload`, plus all
  `post_*` variants and `tenant.created`, `billing.pre_charge`
- **R-MD-5**: Workload modules in `workload-modules/` importable via CLI

### 9.18 Dashboard

- **R-DB-1**: Next.js 15+ App Router, **shadcn/ui only** for components
  (no custom UI primitives), Tremor for charts, lucide-react for icons,
  React Flow for any graph viz, TanStack Table for tables >50 rows
- **R-DB-2**: Three top-level groups (Build, Operate, Customers) + Home +
  Settings; ⌘K command palette navigation
- **R-DB-3**: Two hero tabs at production polish (Home, Operate → Cost);
  every other tab shadcn-clean
- **R-DB-4**: Dark mode default with light mode toggle
- **R-DB-5**: Auth via better-auth, operator role required for all admin routes
- **R-DB-6**: `Customers` group always visible; tabs show "Enable
  multi-tenancy" empty state when MT module disabled
- **R-DB-7**: `Build → Modules` is read-only (config edits go through
  `config.yaml`, git-tracked); UI offers a reload button only
- **R-DB-8**: Plugin system: drop files in `apps/dashboard/plugins/<name>/`
  to add tabs without forking
- **R-DB-9**: Per-run detail page provides a "View full trace →" link to
  the runtime's existing DAG view (we don't recreate the graph)
- **R-DB-10**: Customer-facing dashboard scaffold is deferred to Phase 13
  (not part of v1 dashboard)

### 9.19 CLI

- **R-CL-1**: `af-stack` CLI installable via Homebrew, scoop, install script
- **R-CL-2**: Commands:
  - `init <project>` — scaffold a new project
  - `dev` — run docker-compose + hot reload
  - `module enable/disable <name>`
  - `mcp add/remove/list <url>`
  - `harness install <name>` — installs Claude Code, Codex, Gemini CLIs
  - `sandbox install <name>` — installs gVisor or others
  - `skill install/list <name>`
  - `tenant create/list/update <args>`
  - `secrets set/get/list/delete`
  - `db migrate/rollback`
  - `deploy --target=fly|railway|render|helm`
  - `logs tail [--service]`
  - `import-module <github-url>` — pull a workload module

### 9.20 Examples

Each example ships with:
- README explaining what it demonstrates
- Working docker-compose
- Sample data / seed migrations
- Walk-through documentation
- Deploy button(s) for one-click trial

## 10. Non-functional requirements

### 10.1 Performance

- **NF-PF-1**: Dashboard initial load < 2s on broadband
- **NF-PF-2**: API request overhead (gateway → AF → response) < 50ms p50
- **NF-PF-3**: `suite.jobs.enqueue` returns in < 10ms p50
- **NF-PF-4**: Sandbox cold start: docker < 2s, gVisor < 3s, Firecracker
  < 5s, e2b < 2s warm
- **NF-PF-5**: Suite runtime supports 1k req/s per replica on 2-core node

### 10.2 Reliability

- **NF-RL-1**: No data loss on graceful shutdown (drain mode)
- **NF-RL-2**: Crash-safe job and agent execution (River + AF guarantees)
- **NF-RL-3**: HITL pauses survive control plane restart
- **NF-RL-4**: Outbox patterns for outgoing webhooks and notifications

### 10.3 Security

- **NF-SC-1**: All secrets encrypted at rest (KMS envelope)
- **NF-SC-2**: All cross-tenant access denied by default (RLS)
- **NF-SC-3**: Sandbox network isolation enforced at adapter level
- **NF-SC-4**: API key prefix visible, secret hashed (bcrypt or argon2id)
- **NF-SC-5**: Webhook signatures verified before any processing
- **NF-SC-6**: No secret material in logs or traces
- **NF-SC-7**: Apache 2.0 / MIT license for all bundled OSS

### 10.4 Observability

- **NF-OB-1**: OTel emission for every primitive (jobs, gateway, sandbox, etc.)
- **NF-OB-2**: Structured logs in JSON with consistent fields
- **NF-OB-3**: Request IDs propagated through all components
- **NF-OB-4**: Correlation IDs link gateway requests to AF executions

### 10.5 Developer experience

- **NF-DX-1**: Hot reload for agent code in dev mode
- **NF-DX-2**: Hot reload for dashboard code in dev mode
- **NF-DX-3**: Type-safe SDK in Python (mypy strict) and TS (strict tsconfig)
- **NF-DX-4**: IDE autocomplete for `config.yaml` via JSON Schema
- **NF-DX-5**: Error messages are actionable (point at the fix)
- **NF-DX-6**: Every README example is copy-paste runnable

### 10.6 Documentation

- **NF-DC-1**: Quickstart docs work end to end (validated by CI)
- **NF-DC-2**: Every module has a README in its folder
- **NF-DC-3**: Every adapter has a README
- **NF-DC-4**: Every example has a walkthrough
- **NF-DC-5**: API reference auto-generated from OpenAPI
- **NF-DC-6**: Architecture diagram in repo root

### 10.7 License

- **NF-LC-1**: Apache 2.0 for the suite
- **NF-LC-2**: All bundled dependencies compatible
- **NF-LC-3**: Trademark policy documented for forks

## 11. Risks and mitigations

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| AF API surface changes mid-build | Medium | High | Pin to a specific AF release; coordinate with AF team |
| Bundled OSS license change | Low | Medium | All bundled deps are Apache 2.0 / MIT; alternates documented |
| Sandbox security incident | Medium | High | Adapter isolation defaults (Firecracker for MT), security review before launch |
| Dashboard polish underdelivers | High | Medium | Tight scope: 5 hero tabs only; functional CRUD for rest |
| Single-binary scaling claim fails | Medium | High | Validate AF stateless mode (already done); load-test before launch |
| Multi-tenancy edge cases (RLS bypass) | Medium | Critical | Comprehensive test suite, security audit, deny-by-default |
| Scope creep into v2 features | High | Medium | This PRD is the gate; v2 list is locked |
| Naming dispute / brand conflict | Medium | Low | Use placeholder until trademark search complete |

## 12. Open questions to resolve before shipping v1

| # | Question | Owner | Deadline |
|---|---|---|---|
| Q1 | Final product name and brand | Product | Before code freeze |
| Q2 | Hosted documentation domain | Product | Pre-launch |
| Q3 | Identity/DID surface in app code (read-only? sign?) | Eng + Product | Mid-build |
| Q4 | VC generation from app code: needed in v1? | Product | Mid-build |
| Q5 | Cost attribution rules for cross-agent calls | Eng | Mid-build |
| Q6 | Cross-tenant agent invocation policy | Eng + Product | Mid-build |
| Q7 | License: Apache 2.0 confirmed? | Legal | Pre-launch |
| Q8 | Contribution license agreement (CLA)? | Legal | Pre-launch |

## 13. Launch criteria (v1 ready to ship)

- [ ] All R-* functional requirements implemented and tested
- [ ] All NF-* non-functional requirements measured and met
- [ ] All 6 examples run end to end on fresh clone
- [ ] 60-second quickstart validated by 5+ external testers
- [ ] Five hero dashboard tabs at production polish
- [ ] OpenAPI spec generated, accurate, all endpoints documented
- [ ] Security audit completed for sandbox and multi-tenancy
- [ ] Helm chart deployed and validated on minikube + one cloud k8s
- [ ] Docker Compose validated on macOS + Linux + Windows WSL
- [ ] All deploy buttons (Fly, Railway, Render) work end to end
- [ ] License confirmed and applied
- [ ] Documentation site live at chosen domain
- [ ] Discord/community channels live
- [ ] Launch blog post drafted
- [ ] Demo video recorded

## 14. References

- `PLAN.md` — full architectural plan
- `TECH-SPEC.md` — implementation specifications
- `ROADMAP.md` — phased build plan
- `docs/af-stateless-validation.md` — AF single-binary analysis
- `docs/sdk-strategy.md` — SDK shape and naming
- `docs/example-notable-walkthrough.md` — pure-app workload validation
- `docs/example-shipwright-walkthrough.md` — heavy-AI workload validation

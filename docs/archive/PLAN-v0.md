> **Archived 2026-06-07.** This document covers Phase 0-16 (now shipped).
> Kept for historical context. For current state, see
> [`STRATEGY.md`](../../development/strategy.md) and [`STACK.md`](../stack.md).

# AF Stack: Architectural Plan

This document captures the full architectural plan for the suite that wraps
AgentField. Working name `AF Stack` (see `BRAND.yaml` to rename).

For a single read-through this is everything we decided during ideation.

## Position

> The open backend platform for the AI era. Self-host AgentField with
> everything else needed to ship a real product: identity, multi-tenancy,
> sandboxes, storage, queues, public APIs, billing, dashboard. One repo.
> Fork-friendly. Your code.

Critically: this is **a complete general-purpose backend**, not "AI bolted on
the side." The dev building a Notion clone uses it for everything (auth, db,
storage, jobs, billing) AND gets first-class AI primitives (agents, gateway,
sandboxes, memory). They don't run two backends.

## Three viral loops (all converge on AgentField adoption)

1. **Backend devs adopt the suite for their full backend.** They came for
   auth/db/storage/jobs. AI is native. They add an LLM feature, discover AF,
   build agents.
2. **AI-tool users hit the suite.** They use Helicone (gateway), Inngest
   (jobs), Supabase (db) separately. Suite bundles all three plus agents.
   Consolidation = AF deployment.
3. **AF users productize.** They hit "I need plumbing" walls and the suite
   is the canonical answer.

The LLM gateway exposed standalone (OpenAI-compatible) is the
lowest-commitment entry point. Devs adopt the gateway alone, then discover
the rest.

## Two reference workloads (validation)

Both walked end-to-end in `docs/example-*-walkthrough.md`.

| Workload | Type | Modules on | What it proves |
|---|---|---|---|
| **Notable** (Notion-with-AI) | Pure-app | 13/16 | Suite stands as a general backend; AI is one feature among many |
| **Shipwright** (SWE-AF SaaS) | Heavy AI | 15/16 | Sandbox + git workload + heavy AF usage all hold together |

12/16 modules shared between them. Differences are the workload-specific
opt-ins. Same suite, same control plane, same dashboard.

## Module catalog

Every module is deletable (or disabled). Every adapter is swappable.

### Core (always on, can't be removed)

| Component | Choice | Why |
|---|---|---|
| AgentField | Built, own | The differentiator |
| Postgres | Adopt | Hosts AF tables, app tables, multi-tenancy, vector (pgvector), FTS, queue, pub/sub |
| Single-binary runtime | Built (Go) | The orchestrator; stateless, scales horizontally |
| Next.js dashboard | Built shell + adopted components | Themable; fork-friendly |
| Module loader | Built | Adapter / hook / module system; suite-specific glue |

### Compute and execution

| Module | v1 default | Other adapters documented |
|---|---|---|
| Agent runtime | AF native | n/a (AF is the only agent runtime) |
| App job queue | River (Go, PG-backed) | Hatchet, BullMQ adapter |
| App cron | River cron | n/a |
| App pub/sub | PG LISTEN/NOTIFY | NATS, Redis |
| Sandbox | Docker (dev) / gVisor / Firecracker / e2b | Lambda, Modal, Cloud Run, k8s-pod, Daytona |
| Compute scheduler | docker-compose / helm / Nomad / Fly | ECS, Cloud Run, Hetzner Cloud |

### State and storage

| Module | v1 default | Other adapters documented |
|---|---|---|
| Database admin UI | Supabase Studio components (embed) | Drizzle Studio, NocoDB |
| Object storage | MinIO → S3 | R2, GCS, Azure Blob, Backblaze |
| Vector store | AF native (pgvector) | Pinecone, Qdrant, Weaviate |
| Search | Postgres FTS | Meilisearch, Typesense |
| KV / cache | AF native (PG) | Redis |

### Identity and access

| Module | v1 default | Other adapters documented |
|---|---|---|
| Auth library | better-auth | Lucia, Clerk, WorkOS, Auth0 |
| Identity provider (advanced) | Zitadel adapter doc | Hanko, Keycloak, Ory Kratos |
| SSO/SAML | BoxyHQ adapter doc | WorkOS SSO |
| Authz / policies | PG RLS + AF tag policies | Cerbos, Permify, OpenFGA |
| Agent identity | AF native (DIDs) | n/a (AF owns this) |
| Multi-tenancy | Built (PG RLS) | Schema-per-tenant doc |
| Secrets vault | Built (PG + KMS envelope) | Vault, Doppler, Infisical |

### Edge and connectivity

| Module | v1 default | Other adapters documented |
|---|---|---|
| HTTP server | Go (chi or echo) | n/a |
| Reverse proxy / TLS | Caddy | Traefik, Nginx |
| Public API gateway | Built (Go) | n/a (this is suite-specific) |
| Incoming webhooks | Svix (bundled) | Hookdeck |
| Outgoing webhooks | Built thin (PG outbox + Go retry) | n/a |
| Notifications outbox | Built thin + log-stub default | Novu, Knock |
| Email | Stub default | Resend, Postmark, SendGrid, SES, Mailgun |
| SMS / push | Stub default | Twilio, OneSignal, Knock |

### LLM and observability

| Module | v1 default | Other adapters documented |
|---|---|---|
| LLM gateway | AF native (LiteLLM under the hood) | OpenAI-compatible shim at `/api/v1/llm/*` routes through AF (no bypass path) |
| LLM observability | Built (hero views in dashboard) | Langfuse adapter for deep traces |
| App observability | OpenTelemetry emit | SigNoz, Grafana stack, Honeycomb, Datadog |
| Logs storage | OTel emit | Quickwit, Loki |
| Metrics | Prometheus | n/a |
| Traces | OTel | n/a |

### Billing and metering

| Module | v1 default | Other adapters documented |
|---|---|---|
| Usage metering | Built (PG counters per tenant) | n/a |
| Billing engine | Stripe direct | Lago, Paddle, Polar, Orb |
| Invoicing | Stripe | Lago |

### Workload-specific (optional, importable)

| Module | When to enable |
|---|---|
| Git workload | SWE-AF, codeaf, any code-agent product |
| Multimodal storage (ffmpeg + Whisper + Vision) | Podcast-af, reel-af, ad-ai |
| Change-stream listener (PG + Mongo + Kafka) | Reactive-atlas, enrichment pipelines |
| MCP registry | AF native (AF handles) |

## Extensibility surface (six layers)

Higher layers are rarer changes and more invasive.

| Layer | What you change | Effort |
|---|---|---|
| 1. Adapters | Swap one impl for another (Docker → Lambda) | ~200 lines, 1 day |
| 2. Hooks | Cross-cutting logic at named hook points | ~30 lines, 30 min |
| 3. New modules | Add primitives we never shipped | 1-2 days |
| 4. Dashboard plugins | Add tabs without forking | 1-2 hours |
| 5. Schema extension | Add columns or tables | 5 min |
| 6. AF agents | Your agent logic | AF's normal flow |

### Hook points

Named interception points where users register functions without changing module code:

- `gateway.pre_auth`, `gateway.post_auth`
- `af.pre_execute`, `af.post_execute`
- `llm.pre_call`, `llm.post_call`
- `sandbox.pre_run`, `sandbox.post_run`
- `storage.pre_upload`
- `notifications.pre_send`
- `billing.pre_charge`
- `tenant.created`

Use cases: PII redaction, custom rate limits, SIEM audit logging, virus
scanning on uploads, custom discount logic.

### Adapter contract template

Every module ships an interface like:

```go
type Sandbox interface {
    Run(ctx context.Context, spec RunSpec) (*RunResult, error)
    Stream(ctx context.Context, spec RunSpec) (<-chan LogLine, *RunResult, error)
    Stop(ctx context.Context, jobID string) error
    Capabilities() Capabilities
}
```

`Capabilities()` lets the suite reject workloads that need features the chosen
adapter doesn't support (e.g., Lambda's 15-min timeout).

## Dashboard

Single Next.js app, operator-only in v1 (customer-facing scaffold lands in
Phase 13). Three mental-mode groups + Home + Settings.

See [`docs/dashboard-ia.md`](../dashboard-ia.md) for the full IA.

### Top-level navigation

```
[Home]   Build   Operate   Customers              ⌘K   [user]
```

- **Home** (standalone) — overview dashboard, viral hero
- **Build** — configuring your product (Agents, Integrations, Database,
  Storage, Secrets, Webhooks, Auth, Billing, Jobs, Sandboxes, Modules)
- **Operate** — observing what runs (Runs, Logs, Queues, Cost,
  Sandbox Activity, Webhook Activity)
- **Customers** — your end users (Tenants, Users, API Keys,
  Customer Billing, Audit). Always visible; gated by multi-tenancy: each
  tab shows an empty state with an "Enable multi-tenancy" CTA when MT is off.
- **Settings** (standalone) — suite-level operator settings, not product config

### Hero tabs (polish budget)

Two only:

1. **Home** — first screen, the viral screenshot
2. **Operate → Cost** — differentiator vs every non-AI backend platform

Every other tab ships shadcn-clean but not hero polish.

### Generic naming

The dashboard uses **generic AI-backend vocabulary** throughout: "agents,"
"runs," "tools." No vendor-specific terminology. The runtime is an
implementation detail.

### Don't rebuild what's already excellent

Where the agent runtime ships a polished view (e.g., the execution DAG),
the dashboard provides a summary card with a "View full trace →" link
that opens the runtime UI. No duplicated graph code.

### What's NOT in the dashboard

- Eval / regression UI (means different things per workload)
- Prompt management (devs version in git)
- Workflow designer / visual builder (code is the interface)
- Deploy, migration, adapter-swap UIs (CLI + config files)
- Multi-region admin (v2)

### Action coverage promise

Every dashboard action = a CLI command = an SDK call. Operators script things,
CI/CD needs it, agents can do it.

## SDK strategy

See `docs/sdk-strategy.md` for the full doc. Summary:

**The invariant**: all model calls go through AgentField. No `suite.llm.*`
bypass. The OpenAI-compatible endpoint at `/api/v1/llm/*` is a shim that
routes through AF.

**The DX model**: "`app.*` defines agents. `suite.*` calls them and runs
everything else."

- **AgentField SDK** (`app.*`): for inside agent processes — `.ai()`,
  `.harness()`, `.call()`, `.memory.*`, `.pause()`, `@app.reasoner()`
- **Suite SDK** (`suite.*`, `ctx.*`): for outside agents — invokes agents via
  `suite.agents.call/call_async/stream/status/cancel`, plus HITL, discovery,
  versioning, memory, policy, MCP, harness, AND infra (jobs, secrets, storage,
  notifications, billing, sandbox, webhooks, search, pubsub)
- **Raw REST + OpenAPI**: every SDK call maps to an HTTP endpoint, auto-spec'd

Inside an AF agent, both SDKs work. Outside an agent, only the suite SDK
exists.

## AgentField stateless single-binary validation

See `docs/af-stateless-validation.md`. Summary:

AF can run as a stateless single Go binary when:
- `AGENTFIELD_STORAGE_MODE=postgresql`
- Memory-fallback flags set to `false` (production-safe)
- Helm chart drops the PVC in PG mode
- Registry storage is PG-backed (verify or one-line AF PR)
- WebSocket connections are stateless (already true — HTTP callbacks, not WS routing)

With these settings the single-binary stateless claim is honest and matches
the lineage of Caddy, Gitea, Plausible, Grafana, MinIO.

## Repo skeleton (v1 shape)

```
af-stack/                            # this folder, renamed when name decided
  README.md
  BRAND.yaml                         # name placeholder
  docker-compose.yml                 # ~8 services + N agent containers
  docker-compose.prod.yml
  deploy/
    helm/                            # k8s chart
    fly.toml
    railway.toml
    nomad.hcl
  
  apps/
    backend/                         # YOUR app code (per project)
      agents/                        # AF harnesses (yours + imported)
      handlers/                      # plain HTTP handlers
      jobs/                          # River jobs
      crons/
      streams/
      migrations/                    # YOUR SQL migrations
      config.yaml                    # module enable/disable, adapters
    dashboard/                       # Next.js admin + end-user scaffold
      app/(admin)/                   # operator views, shipped as-is
      app/(workspace)/[slug]/        # end-user, customizable
      plugins/                       # community plugins drop here
  
  packages/
    sdk-py/
    sdk-ts/
    sdk-go/
    auth/                            # better-auth integration
    db/                              # Drizzle / SQL helpers
    gateway-client/                  # LLM gateway client
  
  services/                          # platform binaries (rarely edited)
    runtime/                         # Go: gateway, jobs runner, scheduler
    sandbox-host/                    # sandbox adapter runtime
  
  modules/                           # suite primitives, deletable
    identity/
    multi-tenancy/
    sandbox/
      interface.go
      adapters/{docker,gvisor,firecracker,e2b}/
    storage/
    public-gateway/
    secrets-vault/
    jobs/
    notifications/
    webhooks-in/
    billing/
    llm-gateway/
    observability/
    dashboard-scaffold/
  
  workload-modules/                  # optional, importable
    git-workload/
    multimodal-storage/
    change-stream-listener/
  
  examples/
    01-notable/                      # Notion-with-AI (pure-app)
    02-shipwright/                   # SWE-AF SaaS (heavy AI)
    03-llm-gateway-only/             # lowest-commitment viral wedge
    04-podcast-creator/              # multimodal
    05-reactive-enrichment/          # event-driven
    06-deep-research/                # long-running fan-out
  
  docs/
    quickstart.md
    architecture.md
    modules.md
    adapters.md
    hooks.md
    customize-dashboard.md
    swap-defaults.md
    deploy.md
```

## Bill of materials: what's bundled vs what user installs

### Bundled (zero install, just `docker compose up`)

**Container images pulled:**
- AgentField control plane (`agentfield/control-plane:latest`)
- Postgres 16
- MinIO (default object storage)
- Svix (incoming webhooks)
- OTel collector
- Caddy (prod reverse proxy + TLS)

**Built from monorepo:**
- Suite runtime (Go binary: gateway, jobs, scheduler, hooks, module loader)
- Next.js dashboard
- Default agent base image with **OpenCode pre-installed** (so 60s quickstart works)
- Example agent containers

**Compiled into binaries:**
- River (PG-backed job queue) in suite runtime
- MCP SDK (Go) in suite runtime
- Anthropic MCP Python SDK in `af_stack` package
- Anthropic MCP TS SDK in `@af-stack/sdk`
- shadcn + Tremor + Supabase Studio components in dashboard

### Installed by user with one-line script (`af-stack install ...`)

- Claude Code CLI: `af-stack install harness claude-code`
- Codex CLI: `af-stack install harness codex`
- Gemini CLI: `af-stack install harness gemini`
- gVisor runtime: `af-stack install sandbox gvisor`
- MCP servers: `af-stack mcp add <url>`
- Skills (Anthropic format): `af-stack skill install <pkg>`

### Configured by user (no install, env or secrets)

- LLM API keys (OpenRouter, Anthropic, OpenAI, Google)
- GitHub token (if using git-workload module)
- Email/SMS provider keys (Resend, Postmark, Twilio)
- Stripe keys (if billing module enabled)
- e2b API key (if using e2b sandbox adapter)

### Documented, user-managed (no suite involvement)

- Firecracker + Flintlock self-host (link to upstream)
- ffmpeg / Whisper / Vision models (multimodal workload module)
- Custom MCP server builds
- k8s cluster ops

### Strategic call on harness CLIs

Bundling all four harness CLIs would inflate the default image and may have
license-redistribution issues. Bundling none kills the 60-second quickstart.

**Decision**: bundle OpenCode (truly OSS) in the default agent base image.
Other three harnesses install via one-line scripts. Quickstart example uses
`provider="opencode"` so the demo works out of box.

## v1 scope (what ships)

**Core**: single-binary Go runtime; module + adapter + hook system; Next.js
dashboard with 5 hero tabs and functional CRUD for the rest; Python + TS SDKs
(Go SDK uses AF's directly).

**Modules**: identity (better-auth), multi-tenancy (off by default),
public-gateway (with OpenAPI auto-gen), LLM OpenAI-compat shim (routes through
AF), jobs (River), crons, sandbox (docker/gvisor/firecracker/e2b), storage
(MinIO/S3), secrets-vault (PG+KMS), notifications (log-stub + Resend),
webhooks-in (Svix bundled), billing (Stripe direct, Lago documented), OTel
observability.

**Workload modules**: git, multimodal storage, change-stream listener.

**Examples**: 6 listed above.

**Deploy**: docker-compose, helm chart, fly/railway/render buttons, Nomad spec.

Estimated 3-4 months for a focused team.

## v2 (post-launch, community-driven)

- SSO/SAML module (BoxyHQ or Zitadel)
- Polished LLM observability UI (deeper, or full Langfuse embed)
- Vector adapters (Pinecone, Qdrant)
- Serverless sandbox adapters (Lambda, Modal, Cloud Run)
- Custom domain / multi-region
- Backup/restore tooling
- Module registry / marketplace
- More language SDKs (Rust, Ruby, Java)
- Eval module (if AF doesn't grow it natively)
- **MCP Gateway for orgs**: suite exposes itself AS an MCP server so client
  tools (Cursor, Claude Desktop, Continue) can point at one endpoint and get
  all org-approved MCP tools with per-user auth. Symmetric: AF agents and
  client tools share the same MCP server pool. Possible viral hero.
- **A2A / AGNTCY adapter** (in AF, not suite): if cross-platform agent
  protocols solidify, AF adds them; suite inherits.

## On protocols (MCP / A2A / ACP)

### MCP (Model Context Protocol)

**Correction from earlier analysis**: AF does NOT have MCP client capability
today. AF has:
- `skillkit/` that installs **Anthropic-style skills** into harnesses
- Harness providers (Claude Code, Codex, Gemini CLI, OpenCode) which bring
  their own MCP support
- `app.call()` for AF-agent-as-tool

But native MCP client (AF agent calling an external MCP server's tool)
**is not in AF**. Real gap.

**v1 decision**: suite adds MCP client capability natively.
- Bundle the official Anthropic MCP SDKs (Python: Apache 2.0, TS: MIT)
- Add MCP server config via `config.yaml` and `af-stack mcp add <url>`
- Expose `suite.tools.list_mcp_tools()` and `suite.tools.call_mcp(server, tool, args)`
  in main SDK
- Admin SDK: `suite.admin.mcp.install/list/remove`
- Per-tenant MCP isolation when multi-tenancy module is on
- ~500 lines of suite code plus SDK bundles

### Harness providers and skills (already native in AF)

- All four harness providers shipped: Claude Code, Codex, Gemini CLI, OpenCode
- AF's `skillkit` installs Anthropic-format skills into harnesses
- Suite admin SDK exposes: `suite.admin.skills.install/list/attach`
- No new implementation needed; suite just renders + wraps

### A2A (Google) / AGNTCY ACP

Skip for v1. Emerging cross-platform protocols. Most users build internal
systems where AF's DID + `app.call()` covers agent-to-agent. If standards
win, AF adds adapters and the suite inherits.

### OSS MCP gateways (don't bundle in v1)

For client capability we bundle official SDKs. For gateway/aggregation
(`metamcp`, `mcp-proxy`, `mcphub`) — skip. Pick when v2 "suite as MCP server"
ships.

## What's locked vs still open

**Locked from ideation:**
- Position: "complete backend for the AI era"
- AF is core; not removable
- Postgres is core
- Module + adapter + hook architecture
- Two SDKs coexist (AF + Suite); REST is first-class
- Five hero dashboard tabs
- LLM gateway exposed standalone
- Multi-tenancy as module, off by default
- Sandbox interface with four v1 adapters

**Still open:**
- Final name and brand
- LLM gateway as separately-deployable artifact (it's at least separately
  callable; question is whether it ships as its own docker image)
- Dashboard sub-brand (Hangar?) vs single brand
- Exact tagline copy
- Whether SWE-AF and other AF examples get "import-as-module" support in AF
  itself or stay as suite-side conventions
- Frontend framework strategy beyond Next.js
- Whether the suite hosts a public adapter / module registry day one

## Next concrete moves (any order)

1. Write the README hero (what someone sees in 60 seconds)
2. Spec `ctx` and `suite.*` SDK across Python + TypeScript (the DX that
   makes or breaks adoption)
3. Spec the LLM gateway as a standalone deployable (lowest-commitment viral
   wedge)
4. Validate AF stateless claims by reading registry code (or a one-line PR
   to AF if needed)
5. Build the docker-compose skeleton (the 60-second quickstart artifact)
6. Pick the final name and run the rename

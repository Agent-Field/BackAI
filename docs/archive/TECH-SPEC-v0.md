> **Archived 2026-06-07.** This document covers Phase 0-16 (now shipped).
> Kept for historical context. For current state, see
> [`STRATEGY.md`](../../development/strategy.md) and [`STACK.md`](../stack.md).

# AF Stack: Technical Specification

Implementation details for v1. Pair with `PRD.md` for product context and
`ROADMAP.md` for sequencing.

## 1. System architecture

### 1.1 High-level diagram

```
                       ┌──────────────────┐
                       │   Reverse proxy  │
                       │   (Caddy / LB)   │
                       └────────┬─────────┘
                                │
        ┌───────────────────────┼───────────────────────┐
        ▼                       ▼                       ▼
  ┌──────────┐            ┌──────────┐            ┌──────────┐
  │ Suite    │            │ Suite    │            │ Suite    │
  │ runtime  │            │ runtime  │            │ runtime  │
  │ (Go)     │            │ (Go)     │            │ (Go)     │
  └────┬─────┘            └────┬─────┘            └────┬─────┘
       │                       │                       │
       └───────────┬───────────┴───────────┬───────────┘
                   │                       │
                   ▼                       ▼
            ┌─────────────┐         ┌─────────────┐
            │ AF Control  │         │ AF Control  │   ← stateless
            │ Plane (Go)  │         │ Plane (Go)  │     replicas
            └──────┬──────┘         └──────┬──────┘
                   │                       │
                   └───────────┬───────────┘
                               │
                               ▼
                    ┌──────────────────┐
                    │   Postgres 16+   │
                    │  (state + jobs + │
                    │   vector + FTS)  │
                    └────────┬─────────┘
                             │
            ┌────────────────┼────────────────┐
            ▼                ▼                ▼
      ┌──────────┐    ┌──────────┐    ┌──────────────┐
      │  MinIO   │    │  Svix    │    │ Sandbox host │
      │ (S3 API) │    │webhooks  │    │ pool (docker │
      │          │    │          │    │ / gvisor /   │
      │          │    │          │    │ firecracker) │
      └──────────┘    └──────────┘    └──────────────┘

      ┌──────────────────────────────────────────┐
      │ User agent processes (Python / Go / TS)   │   ← register with
      │  - notable-ai  - swe-af  - my-custom      │     AF control plane
      └──────────────────────────────────────────┘
```

### 1.2 Component breakdown

| Component | Tech | Stateful? | Process |
|---|---|---|---|
| Suite runtime | Go | No | Single binary, N replicas |
| AF control plane | Go | No (PG mode) | Single binary, N replicas |
| Dashboard | Next.js | No | Served by suite runtime or separate Next process |
| Postgres | PG 16+ | Yes | Managed or self-hosted; primary state |
| MinIO | MinIO | Yes (disk) | Single node or distributed cluster |
| Svix | Svix server | Yes (PG-backed) | Service; uses same or separate PG |
| Sandbox host | Per adapter | Yes (job slots) | Separate tier |
| Agent processes | User code | No (state in AF/PG) | User-defined |

### 1.3 Process boundaries

- **Suite runtime** is one Go binary, handles: HTTP gateway, jobs runner,
  cron scheduler, hooks engine, module loader, OpenAPI server, MCP host,
  webhook outbox, notifications outbox, billing meter aggregation
- **AF control plane** is the existing AgentField binary, unchanged
- **Dashboard** is Next.js; in dev runs separately, in prod can be served by
  the suite runtime via embedded build or run as its own service
- **Agent processes** are user-written, register with AF control plane

### 1.4 Network topology

- Suite runtime: ports `8080` (HTTP), `9090` (metrics)
- AF control plane: port `8081` (internal)
- Dashboard: port `3000` (dev), embedded in 8080 (prod)
- Postgres: `5432`
- MinIO: `9000` (API), `9001` (console)
- Svix: `8071`
- Sandbox host: managed per adapter
- Agent processes: per-agent port (callback URL pattern)

## 2. Repository structure

```
af-stack/
├── README.md
├── PRD.md
├── TECH-SPEC.md                     # this file
├── ROADMAP.md
├── PLAN.md
├── BRAND.yaml
├── LICENSE
├── docker-compose.yml
├── docker-compose.prod.yml
├── docker-compose.dev.override.yml
├── go.mod
├── go.sum
├── package.json                     # workspace root for JS
├── pnpm-workspace.yaml
├── pyproject.toml                   # workspace root for Python
├── Makefile
├── .env.example
├── .gitignore
│
├── apps/
│   ├── backend/                     # USER app code; ships as scaffold
│   │   ├── agents/
│   │   │   └── .gitkeep
│   │   ├── handlers/
│   │   │   └── .gitkeep
│   │   ├── jobs/
│   │   │   └── .gitkeep
│   │   ├── crons/
│   │   │   └── .gitkeep
│   │   ├── streams/
│   │   │   └── .gitkeep
│   │   ├── migrations/
│   │   │   └── .gitkeep
│   │   ├── templates/
│   │   │   └── .gitkeep
│   │   └── config.yaml              # default config, user edits
│   │
│   └── dashboard/                   # Next.js app
│       ├── app/
│       │   ├── (auth)/
│       │   ├── (admin)/             # operator views
│       │   │   ├── db/
│       │   │   ├── agents/
│       │   │   ├── gateway/
│       │   │   ├── sandboxes/
│       │   │   ├── tenants/
│       │   │   ├── jobs/
│       │   │   ├── crons/
│       │   │   ├── webhooks/
│       │   │   ├── notifications/
│       │   │   ├── billing/
│       │   │   ├── secrets/
│       │   │   ├── modules/
│       │   │   ├── mcp/
│       │   │   ├── skills/
│       │   │   ├── auth/
│       │   │   ├── keys/
│       │   │   ├── logs/
│       │   │   ├── metrics/
│       │   │   ├── storage/
│       │   │   └── memory/
│       │   ├── (workspace)/[slug]/  # end-user views (scaffold)
│       │   │   ├── settings/
│       │   │   ├── members/
│       │   │   ├── usage/
│       │   │   └── audit/
│       │   ├── api/                 # Next.js route handlers
│       │   └── layout.tsx
│       ├── components/
│       ├── lib/
│       ├── plugins/                 # dashboard plugin drop-zone
│       └── package.json
│
├── packages/                        # shared libraries
│   ├── sdk-py/                      # Python SDK (af_stack)
│   │   ├── pyproject.toml
│   │   ├── af_stack/
│   │   │   ├── __init__.py
│   │   │   ├── ctx.py
│   │   │   ├── agents.py
│   │   │   ├── jobs.py
│   │   │   ├── secrets.py
│   │   │   ├── storage.py
│   │   │   ├── notifications.py
│   │   │   ├── billing.py
│   │   │   ├── memory.py
│   │   │   ├── sandbox.py
│   │   │   ├── webhooks.py
│   │   │   ├── pubsub.py
│   │   │   ├── tools.py             # MCP tools
│   │   │   ├── admin/               # admin SDK as sub-package
│   │   │   │   ├── __init__.py
│   │   │   │   ├── agents.py
│   │   │   │   ├── policy.py
│   │   │   │   ├── mcp.py
│   │   │   │   ├── secrets.py
│   │   │   │   ├── tenants.py
│   │   │   │   ├── users.py
│   │   │   │   ├── keys.py
│   │   │   │   ├── audit.py
│   │   │   │   ├── skills.py
│   │   │   │   └── harness.py
│   │   │   └── _http.py             # internal HTTP client
│   │   └── tests/
│   │
│   ├── sdk-ts/                      # TypeScript SDK (@af-stack/sdk)
│   │   ├── package.json
│   │   ├── src/
│   │   │   ├── index.ts
│   │   │   ├── ctx.ts
│   │   │   ├── agents.ts
│   │   │   ├── jobs.ts
│   │   │   ├── secrets.ts
│   │   │   ├── storage.ts
│   │   │   ├── notifications.ts
│   │   │   ├── billing.ts
│   │   │   ├── memory.ts
│   │   │   ├── sandbox.ts
│   │   │   ├── webhooks.ts
│   │   │   ├── pubsub.ts
│   │   │   ├── tools.ts
│   │   │   └── admin/
│   │   └── tests/
│   │
│   ├── sdk-go/                      # Go SDK
│   │   ├── go.mod
│   │   ├── suite/
│   │   └── admin/
│   │
│   ├── db/                          # shared SQL helpers, Drizzle schemas
│   ├── auth/                        # better-auth wiring
│   └── ui/                          # shared dashboard components
│
├── services/                        # platform binaries
│   └── runtime/                     # main suite Go binary
│       ├── cmd/
│       │   └── af-stack/
│       │       └── main.go
│       ├── internal/
│       │   ├── gateway/             # HTTP gateway
│       │   ├── jobs/                # River integration
│       │   ├── crons/               # River cron
│       │   ├── hooks/               # hook engine
│       │   ├── modules/             # module loader
│       │   ├── openapi/             # spec generator
│       │   ├── mcp/                 # MCP host
│       │   ├── outbox/              # webhook + notification outbox
│       │   ├── billing/             # meter aggregator
│       │   ├── observability/       # OTel setup
│       │   └── config/              # config loader
│       └── pkg/                     # exported helpers
│
├── modules/                         # suite primitive modules
│   ├── identity/
│   │   ├── manifest.yaml
│   │   ├── migrations/
│   │   ├── api/
│   │   ├── dashboard/
│   │   └── README.md
│   ├── multi-tenancy/
│   ├── public-gateway/
│   ├── llm-gateway/                 # OpenAI-compat shim
│   ├── secrets-vault/
│   ├── sandbox/
│   │   ├── manifest.yaml
│   │   ├── interface.go
│   │   ├── adapters/
│   │   │   ├── docker/
│   │   │   ├── gvisor/
│   │   │   ├── firecracker/
│   │   │   └── e2b/
│   │   └── README.md
│   ├── storage/
│   │   └── adapters/{minio,s3}/
│   ├── notifications/
│   │   └── adapters/{log,resend,postmark,ses,twilio}/
│   ├── webhooks-in/                 # Svix integration
│   ├── billing/
│   │   └── adapters/{stripe,lago}/
│   ├── observability/
│   ├── mcp-client/
│   ├── skills/
│   └── dashboard-scaffold/
│
├── workload-modules/                # optional, importable
│   ├── git-workload/
│   ├── multimodal-storage/
│   └── change-stream-listener/
│
├── examples/
│   ├── 01-notable/                  # Notion-with-AI
│   ├── 02-shipwright/               # SWE-AF SaaS
│   ├── 03-llm-gateway-only/
│   ├── 04-podcast-creator/
│   ├── 05-reactive-enrichment/
│   └── 06-deep-research/
│
├── deploy/
│   ├── helm/
│   │   └── af-stack/
│   │       ├── Chart.yaml
│   │       ├── values.yaml
│   │       ├── values-prod.yaml
│   │       └── templates/
│   ├── nomad/
│   ├── fly.toml
│   ├── railway.toml
│   ├── render.yaml
│   └── README.md
│
├── docs/
│   ├── quickstart.md
│   ├── architecture.md
│   ├── modules.md
│   ├── adapters.md
│   ├── hooks.md
│   ├── customize-dashboard.md
│   ├── swap-defaults.md
│   ├── deploy.md
│   ├── install-harnesses.md
│   ├── install-sandboxes.md
│   ├── install-mcp-servers.md
│   ├── examples/
│   ├── af-stateless-validation.md
│   ├── sdk-strategy.md
│   ├── example-notable-walkthrough.md
│   └── example-shipwright-walkthrough.md
│
└── scripts/
    ├── install.sh
    ├── seed.sh
    └── test-quickstart.sh
```

## 3. Data model

### 3.1 Core suite tables

**Suite namespace** (separate from AF's tables):

```sql
-- Tenants (multi-tenancy module)
create table suite_tenants (
  id uuid primary key default gen_random_uuid(),
  slug text unique not null,
  name text not null,
  plan text default 'free',
  settings jsonb default '{}',
  quota jsonb default '{}',
  created_at timestamptz default now(),
  deleted_at timestamptz
);

-- Users (via better-auth)
create table suite_users (
  id uuid primary key default gen_random_uuid(),
  email text unique not null,
  name text,
  avatar_url text,
  created_at timestamptz default now(),
  deleted_at timestamptz
);

-- Memberships (user-tenant relationship)
create table suite_memberships (
  tenant_id uuid references suite_tenants(id) on delete cascade,
  user_id uuid references suite_users(id) on delete cascade,
  role text not null check (role in ('owner','admin','member','viewer')),
  invited_at timestamptz default now(),
  accepted_at timestamptz,
  primary key (tenant_id, user_id)
);

-- API keys
create table suite_api_keys (
  id uuid primary key default gen_random_uuid(),
  tenant_id uuid references suite_tenants(id) on delete cascade,
  prefix text unique not null,
  hashed_secret text not null,
  name text,
  scopes text[] not null default '{}',
  created_by uuid references suite_users(id),
  created_at timestamptz default now(),
  last_used_at timestamptz,
  expires_at timestamptz,
  revoked_at timestamptz
);

-- Secrets (per-tenant when MT on)
create table suite_secrets (
  tenant_id uuid references suite_tenants(id) on delete cascade,
  key text not null,
  encrypted_value bytea not null,
  kms_key_id text not null,
  metadata jsonb default '{}',
  rotate_after timestamptz,
  created_at timestamptz default now(),
  updated_at timestamptz default now(),
  primary key (tenant_id, key)
);

-- Gateway request log
create table suite_gateway_requests (
  id uuid primary key default gen_random_uuid(),
  tenant_id uuid,
  api_key_id uuid references suite_api_keys(id),
  user_id uuid,
  endpoint text not null,
  method text not null,
  status_code int,
  duration_ms int,
  request_bytes int,
  response_bytes int,
  af_execution_id text,
  request_id text,
  ip inet,
  user_agent text,
  created_at timestamptz default now()
);
create index on suite_gateway_requests (tenant_id, created_at desc);
create index on suite_gateway_requests (api_key_id, created_at desc);
create index on suite_gateway_requests (af_execution_id);

-- Sandbox runs
create table suite_sandbox_runs (
  id uuid primary key default gen_random_uuid(),
  tenant_id uuid,
  workspace_id text,
  af_execution_id text,
  agent_node_id text,
  adapter text not null,
  image text not null,
  command jsonb not null,
  status text not null check (status in
    ('queued','running','done','failed','timeout','killed')),
  exit_code int,
  duration_s int,
  cpu_seconds numeric,
  memory_peak_mb int,
  network_bytes_in bigint,
  network_bytes_out bigint,
  started_at timestamptz,
  ended_at timestamptz,
  stdout_url text,
  stderr_url text,
  artifacts_url text,
  created_at timestamptz default now()
);
create index on suite_sandbox_runs (tenant_id, created_at desc);
create index on suite_sandbox_runs (af_execution_id);

-- Billing meter records
create table suite_meter_events (
  id uuid primary key default gen_random_uuid(),
  tenant_id uuid not null references suite_tenants(id),
  metric text not null,                 -- llm_tokens|sandbox_seconds|storage_gb|api_calls|jobs
  value numeric not null,
  tags jsonb default '{}',
  af_execution_id text,
  request_id text,
  occurred_at timestamptz default now()
);
create index on suite_meter_events (tenant_id, metric, occurred_at desc);

-- Billing customers (Stripe linkage)
create table suite_billing_customers (
  tenant_id uuid primary key references suite_tenants(id),
  stripe_customer_id text unique,
  stripe_subscription_id text,
  plan text,
  current_period_start timestamptz,
  current_period_end timestamptz,
  updated_at timestamptz default now()
);

-- Notifications outbox
create table suite_notifications_outbox (
  id uuid primary key default gen_random_uuid(),
  tenant_id uuid,
  channel text not null,                -- email|sms|slack|webhook
  recipient text not null,
  template text,
  data jsonb default '{}',
  status text not null default 'pending',
  attempts int default 0,
  last_error text,
  scheduled_at timestamptz default now(),
  sent_at timestamptz,
  created_at timestamptz default now()
);
create index on suite_notifications_outbox (status, scheduled_at)
  where status = 'pending';

-- Outgoing webhooks outbox
create table suite_webhooks_outbox (
  id uuid primary key default gen_random_uuid(),
  tenant_id uuid,
  url text not null,
  payload jsonb not null,
  secret_ref text,
  status text not null default 'pending',
  attempts int default 0,
  last_error text,
  next_attempt_at timestamptz default now(),
  created_at timestamptz default now()
);
create index on suite_webhooks_outbox (status, next_attempt_at)
  where status = 'pending';

-- MCP servers
create table suite_mcp_servers (
  id uuid primary key default gen_random_uuid(),
  tenant_id uuid,                       -- null = global
  name text not null,
  transport text not null check (transport in ('stdio','sse','http')),
  config jsonb not null,                -- url, command, env, etc.
  enabled boolean default true,
  created_at timestamptz default now(),
  updated_at timestamptz default now()
);

-- Audit log (suite-level events; AF has its own VCs)
create table suite_audit_log (
  id uuid primary key default gen_random_uuid(),
  tenant_id uuid,
  user_id uuid,
  api_key_id uuid,
  action text not null,                 -- 'tenant.created'|'secrets.rotated'|...
  resource_type text,
  resource_id text,
  metadata jsonb default '{}',
  ip inet,
  user_agent text,
  occurred_at timestamptz default now()
);
create index on suite_audit_log (tenant_id, occurred_at desc);
create index on suite_audit_log (action, occurred_at desc);
```

### 3.2 Row-level security (multi-tenancy)

When MT module enabled, every table with `tenant_id` gets RLS:

```sql
alter table suite_secrets enable row level security;
create policy tenant_isolation on suite_secrets
  using (tenant_id::text = current_setting('app.tenant_id', true));

-- repeat for each tenant-scoped table
```

Middleware sets `app.tenant_id` per request:

```go
// In gateway middleware
tx.ExecContext(ctx, "select set_config('app.tenant_id', $1, true)", tenantID)
```

### 3.3 Job + cron tables (River)

River creates its own tables (`river_job`, `river_leader`, etc.) on first
run. We don't manage these directly; River does.

### 3.4 AgentField tables

AF manages its own tables (`af_executions`, `af_workflows`, `af_memory`,
`af_credentials`, etc.). The suite does not modify these directly; it calls
AF's REST API.

## 4. Service interfaces

### 4.1 Module interface (Go)

```go
package modules

type Module interface {
    ID() string
    Manifest() Manifest
    Migrations() []Migration
    Routes() []Route                  // HTTP routes to register
    Hooks() []HookRegistration        // hooks this module subscribes to
    Init(ctx context.Context, deps Dependencies) error
    Shutdown(ctx context.Context) error
}

type Manifest struct {
    ID           string
    Name         string
    Version      string
    Description  string
    Dependencies []string              // other module IDs
    Capabilities []string              // declared
    ConfigSchema json.RawMessage       // JSON Schema
}
```

### 4.2 Adapter interfaces

**Sandbox adapter**:

```go
type Sandbox interface {
    Run(ctx context.Context, spec RunSpec) (*RunResult, error)
    Stream(ctx context.Context, spec RunSpec) (<-chan LogLine, *RunResult, error)
    Stop(ctx context.Context, jobID string) error
    Capabilities() Capabilities
}

type RunSpec struct {
    Image        string
    Command      []string
    Files        map[string][]byte      // path -> contents
    Env          map[string]string
    TimeoutS     int
    CPU          int
    MemoryGB     int
    Network      NetworkMode            // open | restricted | isolated
    AllowEgress  []string               // hosts when restricted
    WorkspaceID  string                 // for tenant isolation
}

type RunResult struct {
    ExitCode     int
    Stdout       string                 // truncated; full at StdoutURL
    Stderr       string                 // truncated
    StdoutURL    string                 // S3 URL
    StderrURL    string
    Artifacts    map[string][]byte      // path -> contents (small)
    ArtifactsURL string                 // S3 URL for large artifacts
    CPUSeconds   float64
    DurationMS   int
    MemoryPeakMB int
}

type Capabilities struct {
    MaxTimeoutS     int
    SupportsGPU     bool
    SupportsNetwork bool
    SupportsMounts  bool
    ColdStartMS     int
}
```

**Storage adapter**:

```go
type Storage interface {
    Upload(ctx context.Context, key string, r io.Reader, opts UploadOpts) error
    Download(ctx context.Context, key string) (io.ReadCloser, error)
    SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error)
    Delete(ctx context.Context, key string) error
    List(ctx context.Context, prefix string) ([]Object, error)
}
```

**Email adapter** (notifications submodule):

```go
type EmailSender interface {
    Send(ctx context.Context, msg EmailMessage) error
    Capabilities() EmailCapabilities    // templates, attachments, etc.
}
```

**Billing adapter**:

```go
type BillingProvider interface {
    CreateCustomer(ctx context.Context, tenant Tenant) (string, error)
    UpdateSubscription(ctx context.Context, customerID, planID string) error
    ReportUsage(ctx context.Context, customerID string, events []MeterEvent) error
    GetPortalURL(ctx context.Context, customerID string) (string, error)
    HandleWebhook(ctx context.Context, payload []byte, sig string) (BillingEvent, error)
}
```

### 4.3 Hook engine

```go
type HookEngine interface {
    Register(point HookPoint, handler HookHandler) error
    Fire(ctx context.Context, point HookPoint, payload any) (any, error)
}

type HookPoint string
const (
    HookGatewayPreAuth     HookPoint = "gateway.pre_auth"
    HookGatewayPostAuth    HookPoint = "gateway.post_auth"
    HookAFPreExecute       HookPoint = "af.pre_execute"
    HookAFPostExecute      HookPoint = "af.post_execute"
    HookLLMPreCall         HookPoint = "llm.pre_call"
    HookLLMPostCall        HookPoint = "llm.post_call"
    HookSandboxPreRun      HookPoint = "sandbox.pre_run"
    HookSandboxPostRun     HookPoint = "sandbox.post_run"
    HookStoragePreUpload   HookPoint = "storage.pre_upload"
    HookNotifPreSend       HookPoint = "notifications.pre_send"
    HookBillingPreCharge   HookPoint = "billing.pre_charge"
    HookTenantCreated      HookPoint = "tenant.created"
)
```

Hooks form a chain: each handler receives the payload, can modify it, and
returns. Errors short-circuit the chain.

### 4.4 REST API surface

All endpoints under `/api/v1/`. Auth via Bearer token (API key) or session
cookie. Multi-tenancy resolved from auth context.

**Agents**:
```
POST   /agents/{ns}.{func}                  # sync call
POST   /agents/async/{ns}.{func}            # async call
GET    /agents/stream/{ns}.{func}           # SSE stream
GET    /executions/{id}                     # status
DELETE /executions/{id}                     # cancel
GET    /executions/{id}/dag                 # DAG
GET    /executions/{id}/trace               # trace
POST   /executions/{id}/replay              # replay with edits

POST   /executions/{id}/approve             # HITL
POST   /executions/{id}/deny
GET    /approvals/pending                   # list pending

GET    /agents                              # discover
GET    /agents/{ns}/schema                  # schema for IDE

# Admin only:
GET    /agents/{ns}/versions
PUT    /agents/{ns}/weight                  # canary
POST   /agents/{ns}/promote
POST   /agents/{ns}/rollback
```

**LLM gateway (OpenAI-compatible)**:
```
POST   /llm/chat/completions
POST   /llm/embeddings
POST   /llm/images/generations
GET    /llm/models
```

**Memory**:
```
GET    /memory/{scope}/{key}
PUT    /memory/{scope}/{key}
POST   /memory/{scope}/search
```

**Jobs**:
```
POST   /jobs                                # enqueue
GET    /jobs                                # list
GET    /jobs/{id}
DELETE /jobs/{id}                           # cancel
```

**Storage**:
```
POST   /storage/upload                      # multipart
GET    /storage/signed-url
GET    /storage/{key}                       # download
DELETE /storage/{key}
GET    /storage                             # list with prefix
```

**Secrets**:
```
GET    /secrets/{key}
POST   /secrets/{key}                       # admin only
DELETE /secrets/{key}                       # admin only
GET    /secrets                             # admin only, list
POST   /secrets/{key}/rotate                # admin only
```

**Notifications**:
```
POST   /notifications/email
POST   /notifications/slack
GET    /notifications                       # outbox status
```

**Webhooks**:
```
POST   /webhooks/send                       # outgoing
POST   /webhooks/in/{slug}                  # incoming, Svix-managed
```

**Billing**:
```
POST   /billing/meter
GET    /billing/budget
GET    /billing/usage
GET    /billing/portal-url                  # Stripe portal redirect
```

**Sandbox**:
```
POST   /sandbox/run
GET    /sandbox/runs/{id}
GET    /sandbox/runs/{id}/logs              # streamed
DELETE /sandbox/runs/{id}
```

**MCP tools**:
```
GET    /tools/mcp                           # list available
POST   /tools/mcp/call                      # invoke
POST   /tools/mcp/servers                   # admin: install
GET    /tools/mcp/servers                   # admin: list
DELETE /tools/mcp/servers/{id}              # admin: remove
```

**Tenants / users / keys** (all admin):
```
POST   /admin/tenants
GET    /admin/tenants
GET    /admin/tenants/{id}
PATCH  /admin/tenants/{id}
DELETE /admin/tenants/{id}

POST   /admin/users
GET    /admin/users
PATCH  /admin/users/{id}
DELETE /admin/users/{id}

POST   /admin/keys
GET    /admin/keys
POST   /admin/keys/{id}/rotate
DELETE /admin/keys/{id}                     # revoke
```

**Audit**:
```
GET    /admin/audit
POST   /admin/audit/verify-credential
GET    /admin/audit/export
```

**Skills** (wraps AF skillkit):
```
POST   /admin/skills/install
GET    /admin/skills
POST   /admin/skills/attach
```

**OpenAPI**:
```
GET    /openapi.json                        # auto-generated spec
GET    /docs                                # interactive docs (Swagger UI or similar)
```

## 5. Configuration

### 5.1 `apps/backend/config.yaml` schema

```yaml
suite:
  modules:
    identity:
      adapter: better-auth
      providers:
        email_password: { enabled: true }
        oauth:
          google: { enabled: true, client_id: env.GOOGLE_CLIENT_ID }
          github: { enabled: false }
        magic_link: { enabled: true }
      mfa: { enabled: false }

    multi-tenancy:
      enabled: false                  # off by default
      strategy: pg-rls

    public-gateway:
      cors:
        origins: ['*']
      rate_limit:
        anonymous: 60/hour
        authenticated: 1000/hour

    llm-gateway:
      models:
        - id: claude-sonnet-4
          provider: anthropic
        - id: gpt-4o
          provider: openai
      cache:
        semantic: false
        ttl_hours: 24
      budgets:
        default_tenant_monthly_usd: 100

    jobs:
      adapter: river
      concurrency: 10

    crons:
      adapter: river

    secrets-vault:
      kms: env                        # env|aws-kms|vault
      rotation_default_days: 90

    storage:
      adapter: minio                  # minio|s3|r2|gcs
      bucket: af-stack
      region: us-east-1

    notifications:
      channels:
        email:
          adapter: log                # log|resend|postmark|ses|mailgun
          from: hello@example.com
        slack:
          adapter: webhook
        sms:
          adapter: log                # log|twilio

    webhooks-in:
      adapter: svix

    billing:
      adapter: stripe                 # stripe|lago
      metering:
        enabled: true
        metrics: [llm_tokens, sandbox_seconds, storage_gb, api_calls]

    observability:
      otel:
        endpoint: http://otel-collector:4317
        service_name: af-stack
      logs:
        format: json
        level: info

    mcp-client:
      enabled: true

    skills:
      enabled: true

  sandbox:
    enabled: false                    # off by default
    adapter: docker                   # docker|gvisor|firecracker|e2b
    pool:
      warm: 5
      max: 50
    defaults:
      cpu: 2
      memory_gb: 4
      timeout_s: 300
      network: restricted

  workload-modules: []                # [git-workload, multimodal-storage, change-stream-listener]
```

### 5.2 Environment variables

Core variables (read at startup, override config.yaml):

```bash
# Required
AF_STACK_DATABASE_URL=postgres://...
AGENTFIELD_STORAGE_MODE=postgresql
AGENTFIELD_STORAGE_POSTGRES_URL=postgres://...
AGENTFIELD_STORAGE_POSTGRES_ENABLE_MEMORY_FALLBACK=false
AGENTFIELD_STORAGE_POSTGRES_ENABLE_DID_FALLBACK=false
AGENTFIELD_STORAGE_POSTGRES_ENABLE_VC_FALLBACK=false

# Auth secrets
AF_STACK_AUTH_SECRET=...              # session signing
AF_STACK_KMS_KEY=...                  # secrets encryption

# LLM keys (only required if you use them)
OPENROUTER_API_KEY=...
ANTHROPIC_API_KEY=...
OPENAI_API_KEY=...
GOOGLE_API_KEY=...

# Storage
AF_STACK_S3_ENDPOINT=http://minio:9000
AF_STACK_S3_BUCKET=af-stack
AF_STACK_S3_ACCESS_KEY=...
AF_STACK_S3_SECRET_KEY=...

# Optional
RESEND_API_KEY=...
STRIPE_SECRET_KEY=...
STRIPE_WEBHOOK_SECRET=...
SVIX_AUTH_TOKEN=...
```

## 6. SDK implementation details

### 6.1 Python SDK

**Package**: `af_stack` on PyPI.

**Dependencies**: `httpx`, `pydantic >= 2.0`, `anyio`, `mcp` (official Anthropic).

**Auth resolution**:
- In agent process (`AGENT_CALLBACK_URL` set): use internal token
- In handler/job (FastAPI/Starlette context): pull from `ctx` middleware
- Outside (script): construct `Client(api_key=...)`

**Context (`ctx`)**:
```python
from af_stack import ctx

# Available wherever middleware has run
ctx.tenant_id      # uuid or None
ctx.user_id        # uuid or None
ctx.api_key_id     # uuid or None
ctx.request_id     # str

# In jobs (River sets context from job metadata):
ctx.job_id
ctx.attempt
```

**HTTP client**: shared httpx async client, auto-retry on 5xx with jitter,
trace propagation via OTel.

**Streaming**: SSE for `agents.stream()` and `pubsub.subscribe()`.

### 6.2 TypeScript SDK

**Package**: `@af-stack/sdk` on npm.

**Dependencies**: `zod` (schemas), `@modelcontextprotocol/sdk` (Anthropic MCP).

**Compatible runtimes**: Node 20+, Bun, Deno, edge (Cloudflare Workers, Vercel Edge).

**Auth resolution**:
- Server: env or `ctx` from middleware
- Edge/browser: requires API key passed in `new SuiteClient({ apiKey })`

### 6.3 Go SDK

**Module**: `github.com/agent-field/af-stack/sdk-go/suite`

Wraps the same HTTP API. Used inside Go agent code and by the suite runtime
internally.

## 7. Build, test, deploy

### 7.1 Local development

```bash
# Bring up everything
docker compose up

# Hot reload services
make dev                              # uses air for Go, next dev for dashboard

# Run tests
make test                             # all
make test-go
make test-py
make test-ts

# Lint
make lint
```

### 7.2 CI pipeline (GitHub Actions)

- `lint`: golangci-lint, ruff, eslint, prettier
- `test-unit`: per-language test suites
- `test-integration`: docker-compose up + integration suite
- `test-quickstart`: fresh clone + docker compose up + curl sample endpoints
- `build-images`: docker build per service, push to registry on tag
- `release`: goreleaser for binary, npm publish, PyPI publish on tag

### 7.3 Docker images

- `af-stack/runtime:VERSION` — suite Go binary
- `af-stack/dashboard:VERSION` — Next.js standalone build
- `af-stack/agent-base:VERSION` — Python base with AF SDK + OpenCode
- `af-stack/sandbox-host:VERSION` — sandbox controller per adapter
- `af-stack/all-in-one:VERSION` — single image with embedded Next.js (for
  simple deployments)

### 7.4 Helm chart

`deploy/helm/af-stack/` follows Helm best practices:
- `values.yaml` matches `config.yaml` structure
- Subcharts for Postgres, MinIO (or disable to use external)
- HPA on suite runtime
- PVCs for MinIO only (suite runtime stateless)
- Conditional on `multiTenancy.enabled`
- Network policies for sandbox host isolation

## 8. Testing strategy

### 8.1 Unit tests

- Go: `go test ./...` with mocked adapters
- Python: `pytest` with `httpx` mock transport
- TS: `vitest` with `msw` for HTTP mocks

### 8.2 Integration tests

- `docker compose up -d` brings full stack
- Hit each REST endpoint with realistic payloads
- Validate cross-module flows (agent → memory → billing)

### 8.3 E2E tests

- Playwright for dashboard
- For each example: clone → compose up → run scripted journey

### 8.4 Quickstart validation (CI)

A scripted "fresh user" simulation runs in CI:
1. Clone repo
2. `cp .env.example .env`, set `OPENROUTER_API_KEY` from secret
3. `docker compose up -d`
4. Wait for healthchecks
5. `curl POST /api/v1/agents/sample.echo`
6. Assert 200, valid response, trace appears in DB

### 8.5 Load tests

- `k6` or `vegeta` for gateway throughput
- Target: 1k req/s per replica sustained for 10 minutes

### 8.6 Security tests

- Static analysis (gosec, bandit, eslint-plugin-security)
- Dependency scanning (Dependabot, Snyk)
- Multi-tenancy isolation test suite (assert RLS bypass attempts fail)
- Sandbox escape test suite (attempt to break out, must fail)

## 9. Performance budgets

| Operation | Target p50 | Target p99 |
|---|---|---|
| Suite runtime cold start | 1s | 3s |
| Gateway request overhead | 20ms | 100ms |
| `suite.jobs.enqueue` | 5ms | 30ms |
| `suite.memory.get` | 5ms | 50ms |
| `suite.secrets.get` (with cache) | 1ms | 10ms |
| `suite.storage.upload` (1MB) | 100ms | 500ms |
| Dashboard initial load | 1s | 3s |
| Sandbox docker cold start | 1s | 3s |
| Sandbox firecracker warm start | 2s | 5s |

## 10. Naming conventions

### 10.1 Database tables

All suite tables prefixed `suite_`. AF tables prefixed `af_` (existing
convention). User tables in user namespace.

### 10.2 Module IDs

Lowercase kebab-case: `identity`, `multi-tenancy`, `public-gateway`,
`secrets-vault`, `llm-gateway`, etc.

### 10.3 Hook points

Dotted lowercase: `gateway.pre_auth`, `af.pre_execute`, etc.

### 10.4 Environment variables

`AF_STACK_*` for suite-specific. `AGENTFIELD_*` for AF (existing). Standard
provider keys keep their conventional names (`OPENAI_API_KEY`, etc.).

### 10.5 Config files

YAML for human-edited (`config.yaml`, `gateway.yaml`). JSON for
machine-edited (`openapi.json`).

## 11. Versioning

- Suite uses semver: `v0.1.0` → `v1.0.0` for stable
- Each AF Stack release pins a tested AF version (compatibility matrix in docs)
- API surface follows additive changes only between minor versions
- Breaking changes only at major versions

## 12. Logging conventions

Structured JSON, single line per event:

```json
{
  "ts": "2026-06-06T21:00:00Z",
  "level": "info",
  "service": "af-stack-runtime",
  "request_id": "req_abc123",
  "tenant_id": "t_xyz",
  "user_id": "u_pqr",
  "msg": "gateway.request",
  "endpoint": "POST /api/v1/agents/foo.bar",
  "status": 200,
  "duration_ms": 42,
  "af_execution_id": "exec_def456"
}
```

Common fields: `ts`, `level`, `service`, `request_id`, `tenant_id`,
`user_id`, `msg`. Module-specific fields extend.

## 13. Error model

All errors returned as:

```json
{
  "error": {
    "code": "VALIDATION_FAILED",
    "message": "field 'goal' is required",
    "request_id": "req_abc123",
    "details": { "field": "goal" }
  }
}
```

Error codes are stable enums. HTTP status codes follow conventions
(400/401/403/404/409/422/429/500/503).

## 14. Observability spec

- **Traces**: OpenTelemetry, propagated via `traceparent` header
- **Metrics**: Prometheus format at `/metrics`. Standard names:
  - `af_stack_gateway_requests_total{tenant, endpoint, status}`
  - `af_stack_jobs_enqueued_total{name, tenant}`
  - `af_stack_jobs_duration_seconds{name, tenant}`
  - `af_stack_sandbox_runs_total{adapter, status, tenant}`
  - `af_stack_sandbox_cpu_seconds_total{tenant}`
  - `af_stack_meter_events_total{metric, tenant}`
- **Logs**: structured JSON, shipped to user-configured backend

## 15. Deferred / open implementation questions

These need decisions before relevant module starts:

| # | Module | Question |
|---|---|---|
| Q1 | secrets-vault | Use external KMS by default in production, or generate local KMS key? |
| Q2 | sandbox | Default Firecracker image: distroless? alpine? language-tagged variants? |
| Q3 | multi-tenancy | Auto-create tenant on first user signup, or require admin invite? |
| Q4 | public-gateway | Default rate limits for anonymous tier — too generous? too tight? |
| Q5 | llm-gateway | When AF execution fails, return OpenAI-format error or AF-format? |
| Q6 | dashboard | Bundle Supabase Studio components as npm dep or vendor source? |
| Q7 | mcp-client | Per-tenant MCP server config storage encrypted or plain? |
| Q8 | observability | Bundle Grafana stack as opt-in compose? |

Each is small enough to resolve during the relevant phase's implementation.

## 16. References

- AgentField repo: `platform/agentfield/`
- AF stateless validation: `docs/af-stateless-validation.md`
- SDK strategy: `docs/sdk-strategy.md`
- Example walkthroughs: `docs/example-*-walkthrough.md`
- PRD: `PRD.md`
- Roadmap: `ROADMAP.md`

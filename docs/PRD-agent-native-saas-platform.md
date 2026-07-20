# PRD: Agent-Native SaaS Backend

**Status:** Proposed

**Product:** BackAI

**Audience:** Platform, runtime, CLI, SDK, and security engineers

## 1. Summary

BackAI should be the open-source backend for the AI era: a developer runs one
CLI command, gives the project to Codex or Claude Code, and asks it to build a
SaaS product. The coding agent owns domain code. BackAI owns the production
plumbing that should not be reimplemented for every product.

The repository already contains most backend primitives. The missing product
is a coherent, machine-operable path from `af-stack init` to a secure domain
backend. Today an agent can build AI features, but it cannot add an arbitrary
SaaS data model, routes, and background work without editing platform code or
creating sidecars manually.

### Users and primary job

- A founder asks Codex or Claude Code to build a SaaS product.
- An application engineer extends the generated domain code.
- A platform operator configures and verifies production without maintaining a
  custom integration stack.
- A coding agent discovers capabilities, makes safe changes, and proves them
  through deterministic commands.

Primary job: initialize a backend, describe the product to a coding agent, and
receive a secure, deployable application without rebuilding common backend
infrastructure.

## 2. Product promise

```text
af-stack init contract-review --template saas
cd contract-review
codex # or claude
```

The developer can then request:

> Build a multi-tenant contract-review SaaS with document upload, Stripe
> plans, a review agent, human approval, and completion webhooks.

The coding agent should only need to write:

- Domain schema and business rules
- Customer-facing UI
- Domain routes, jobs, and events
- Agents, prompts, and tools
- Product-specific tests

BackAI must provide and enforce:

- Authentication, tenancy, authorization, and audit
- Database, RLS, migrations, storage, and search
- Secrets, OAuth connections, and webhook verification
- Jobs, crons, retries, and idempotency
- LLM routing, agent execution, memory, cost, and budgets
- Billing primitives, observability, backups, and deployment contracts

## 3. Goals

1. Let a coding agent build an arbitrary multi-tenant SaaS backend without
   modifying the BackAI runtime.
2. Make insecure defaults difficult or impossible to ship.
3. Provide one stable CLI, API, and SDK contract that agents can discover and
   verify without human interpretation.
4. Compose proven open-source systems behind small, stable BackAI primitives.
5. Keep local development and production deployment behavior aligned.

Product principles:

- Secure by construction and fail closed
- Machine-operable before dashboard-only
- One supported golden path before more choices
- Domain-neutral core with optional profiles
- Typed, discoverable, and executable contracts
- Local and production behavior must match

## 4. Non-goals

BackAI will not implement domain products such as commerce, CRM, healthcare,
marketplace payouts, tax, inventory, or collaborative editing. It will provide
the secure primitives needed to build them.

BackAI will also not prioritize:

- Native SAML parsing; use an OIDC broker
- General VPC orchestration or a managed-only control plane
- Every cloud and deployment target
- Every model, tool, or SaaS integration as core code
- Product-specific SDK namespaces

## 5. Ownership boundary

| BackAI owns | Application owns |
| --- | --- |
| Identity, tenancy, permissions | Domain entities and workflows |
| Secure connections and secrets | Product-specific integrations selected by the developer |
| Storage, jobs, events, and delivery | Domain handlers and job logic |
| LLM and AgentField infrastructure | Prompts and reasoner topology |
| Metering, budgets, and audit | Product pricing and entitlement mapping |
| Production health and deployment contracts | Customer experience |

A feature belongs in core when it is required by at least three materially
different SaaS categories or is necessary for security, correctness, or
operations. Domain-specific behavior belongs in a module, adapter, or example.

## 6. Open-source composition policy

BackAI should integrate mature open-source systems rather than replace them:
Postgres for durable state and isolation, AgentField for agent execution,
LiteLLM for provider routing, River for jobs, better-auth for identity,
MinIO/S3 for objects, and OpenTelemetry/Prometheus for observability.

BackAI should own only the contracts between them: tenant context, permissions,
configuration, lifecycle, audit, health, SDKs, and production validation. A new
dependency is accepted only when it removes substantial custom infrastructure,
has a maintained security posture, can be self-hosted, and fits behind a stable
adapter. Multiple interchangeable implementations are added only after a real
second use case exists.

## 7. Current assessment

### Working foundation

- Go runtime behind one base URL
- Postgres, pgvector, tenant RLS, API keys, and operator RBAC
- OpenAI-compatible gateway, AgentField, cost ledger, and budgets
- Storage, secrets, jobs, crons, webhooks, notifications, and billing
- Customer app, operator dashboard, Python and TypeScript SDKs
- Docker Compose development and Helm production deployment

### Blocking gaps

| Gap | Consequence |
| --- | --- |
| Workload-module loader is not wired | Domain routes and migrations require runtime edits or manual sidecars |
| Remote Python/TypeScript jobs do not execute | Coding agents cannot implement normal application workers in their primary language |
| API-key scopes are not generally enforced per route | A tenant key is broader than its declared permissions imply |
| General idempotency is absent | Safe retries can duplicate notifications and mutations |
| SDK, OpenAPI, and runtime surfaces drift | Generated code can type-check and still fail against the live runtime |
| Secrets SDK and runtime authorization disagree | The advertised per-tenant secret primitive is not a reliable app contract |
| `init` produces a minimal consumer, not a complete SaaS project | The CLI does not yet deliver the product promise |
| Security checks are documented but not all compile-time gates | Generated code can omit RLS, authorization, or safe execution policy |

## 8. Requirements

### R1. Contract truth and fail-closed security — P0

BackAI must have one executable contract across runtime, OpenAPI, CLI, and
SDKs.

Requirements:

- SaaS mode refuses to boot when tenant authentication or RLS prerequisites
  are disabled or unsafe.
- Every authenticated route declares accepted principal types and required
  scopes.
- Tenant API-key scopes are enforced by runtime middleware.
- Every retryable mutation supports an idempotency key and response replay.
- Secrets are correctly tenant-scoped; operator access is a separate surface.
- OpenAPI describes every public route.
- Python and TypeScript SDK conformance tests run against a live runtime.
- Security-sensitive defaults fail boot rather than degrade silently.

Required principals:

| Principal | Boundary |
| --- | --- |
| End-user session | The user's allowed resources inside one tenant |
| Tenant service key | Explicit route and resource scopes inside one tenant |
| Agent identity | Declared reasoner, tool, connection, and data capabilities |
| Tenant owner/admin/member | Tenant management permissions by role |
| Platform operator/owner | Audited cross-tenant operations and break-glass actions |

SDK visibility is not authorization. The runtime must evaluate the principal,
tenant, environment, action, and resource on every protected request.

Why this is core: the product promises developers that BackAI owns security.
Documentation and SDK typing cannot provide that guarantee; enforcement must
occur at runtime and in CI.

Acceptance:

- A key with `storage:read` receives `403` from `storage:write`, LLM, secret,
  and admin routes.
- Retrying the same mutation returns the original response without repeating
  side effects.
- CI fails when an SDK method has no matching live route or response schema.
- A SaaS deployment with RLS bypass or missing auth fails readiness.

### R2. Runtime-loaded application modules — P0

`af-stack module new <id>` must create a deployable domain backend, not a
design-stage directory.

Minimum module contract:

```text
workload-modules/projects/
├── backai.module.yaml
├── migrations/
├── routes/
├── jobs/
├── crons/
├── policies.yaml
└── tests/
```

The runtime must:

- Discover enabled modules and validate their manifests
- Apply versioned migrations before serving module traffic
- Mount routes under `/workload/<id>/`
- Bind tenant, user, key, request, and trace context automatically
- Register jobs, crons, events, and OpenAPI operations
- Expose module health and version in the operator dashboard
- Refuse tenant-owned tables without `tenant_id`, RLS, and required policies

Why this is core: every SaaS needs domain data and APIs. Without a supported
module boundary, coding agents must modify trusted platform code, defeating
the security and upgrade model.

Acceptance:

- A generated notes module can migrate, create, list, update, and delete notes
  for two tenants with an isolation test proving no cross-tenant access.
- The module requires no change under `services/runtime/`.
- Removing or disabling the module removes its routes without damaging data.

### R3. Language-neutral workers — P0

BackAI must execute TypeScript and Python job handlers through a stable remote
worker protocol while retaining River as the durable queue.

The protocol must provide authenticated dispatch, tenant context, retries,
timeouts, cancellation, idempotency, heartbeats, structured logs, dead-letter
state, and graceful shutdown.

Why this is core: imports, email, document processing, webhooks, and scheduled
work exist in almost every SaaS. Restricting handlers to Go makes the primary
agent-generated application languages second-class.

Acceptance:

- Equivalent TypeScript and Python workers pass the same conformance suite.
- A killed worker does not lose a job and cannot process another tenant's job.

### R4. Agent-ready CLI and scaffold — P0

The CLI is the product front door and must be safe for non-interactive agents.

Required commands:

```text
af-stack init|dev|doctor|status|test|deploy
af-stack db diff|push|reset|generate
af-stack module new|validate
af-stack agent new|validate
af-stack job new
af-stack connection add|list|remove
af-stack secrets set|list
af-stack logs
```

Requirements:

- Every command supports stable exit codes and `--json` output.
- Destructive commands support `--dry-run` and explicit confirmation bypass
  for controlled automation.
- `init --template saas` creates a customer app, one domain module, one agent,
  tests, local configuration, and deployment configuration.
- The scaffold includes `AGENTS.md`, `CLAUDE.md`, and machine-readable platform
  capabilities and constraints.
- `af-stack test` runs isolation, migration, SDK, and production-config gates.

Why this is core: Codex and Claude Code need deterministic commands, structured
errors, and executable verification. A rich dashboard cannot replace this.

### R5. Connections, not one-off integrations — P1

BackAI must provide one secure connection contract for external services.

A connection owns:

- OAuth or API-key setup
- Encrypted storage and token refresh
- Requested and granted scopes
- Tenant/user ownership
- Health, revocation, and audit events
- Webhook verification metadata
- A typed capability descriptor

Application code receives a connection handle, not raw credentials. Initial
reference adapters should be limited to GitHub, Stripe, Google, and one
messaging service.

Why this is core: nearly every SaaS connects to external systems, but shipping
dozens of integrations in the runtime is bloat. A secure adapter contract lets
the ecosystem add connections without expanding the trusted core.

### R6. Stable SDK developer experience — P1

Python and TypeScript must expose the same supported product surface.

Target usage:

```ts
const backai = new BackAI({ baseUrl, apiKey })
await backai.agents.call("reviewer.review", input)
await backai.jobs.enqueue("index-document", payload)
```

Requirements:

- Explicit client instances; environment-configured `suite.*` remains a
  convenience singleton.
- Identical naming, error envelopes, timeouts, retries, pagination, and
  idempotency behavior in Python and TypeScript.
- Separate server/admin exports so browser bundles cannot import privileged
  operations accidentally.
- Runtime compatibility check and semantic version policy.
- Generated typed clients for module routes and AgentField reasoner schemas.
- Go remains deferred until Python and TypeScript reach live parity.

Why this is core: coding agents rely on discoverability and types. Broad but
inconsistent SDKs increase generated-code failure rates.

### R7. Production operating contract — P1

BackAI should wire production requirements instead of merely documenting
them.

Requirements:

- Docker Compose is the local golden path; Helm is the production golden path.
- Production validation covers restricted DB roles, RLS, TLS, CORS, KMS,
  object-storage isolation, sandbox policy, and network policy.
- Backup and restore tests run automatically on a schedule.
- Database migrations declare rollback compatibility and deployment safety.
- Shared quotas remain correct with multiple runtime replicas.
- Default alert rules cover readiness, provider failures, queue age, budget
  enforcement, delivery failures, database saturation, and backup health.
- Every customer action can be correlated across request, agent graph, model
  calls, tools, cost, and delivery events.

Why this is core: production safety is the value BackAI promises to remove
from application developers. Additional PaaS templates are not a substitute
for one verified production path.

### R8. Minimum complete SaaS lifecycle — P1

The scaffold must include the backend lifecycle shared by normal SaaS apps:

- Tenant creation and deletion
- Invitation, acceptance, removal, and ownership transfer
- Owner, admin, member, billing, and viewer roles
- Service accounts and expiring scoped keys
- Email verification, recovery, and optional MFA policy
- Stripe subscription reconciliation, trials, cancellation, failed-payment
  state, entitlements, and idempotent usage ingestion
- Customer-visible session, key, connection, and audit management

Why this is core: these are recurring security and commercial workflows, not
domain behavior. Tax, refunds, marketplace payouts, and custom role builders
remain provider or application concerns.

## 9. Core versus optional

### Keep in the trusted core

- Postgres, pgvector, RLS, auth, scoped keys, and audit
- AgentField, LLM gateway, metering, and budgets
- Storage, secrets, jobs, crons, webhooks, and connections
- CLI, OpenAPI, Python/TypeScript SDKs, and operator evidence
- Docker Compose and Helm golden paths

### Make optional modules or profiles

- Sandboxes and coding harnesses
- MCP hosting and native agent tools
- Audio, image, and future video APIs
- Database Studio and dashboard plugins
- Provider-specific connection adapters

### Keep only as examples

- SupportDesk
- Shipwright
- Deep Research
- Notable

### Defer

- Nomad and additional PaaS targets without maintained acceptance tests
- Lago until the Stripe lifecycle is complete
- VPC peering, PrivateLink, BYOC, and multi-region orchestration
- Native mobile and Go SDKs

This is not removal for minimalism. It reduces the trusted and documented core
while preserving advanced capabilities through explicit profiles.

## 10. Delivery plan

### Milestone A: Trust the contract

Deliver R1 and publish an executable SDK/runtime compatibility matrix.

### Milestone B: Build arbitrary backends

Deliver R2 and R3. Prove with two domain modules written in different
languages and a cross-tenant isolation suite.

### Milestone C: Hand the project to a coding agent

Deliver R4 and R6. Run fixed Codex and Claude Code acceptance prompts against a
fresh scaffold and require deployable results without platform-code edits.

### Milestone D: Production SaaS foundation

Deliver R5, R7, and R8. Publish one verified Compose-to-Helm production path.

## 11. Success measures

- A fresh SaaS scaffold reaches its first authenticated domain action in under
  ten minutes.
- Codex and Claude Code complete the golden build without editing platform
  runtime code.
- Every public SDK method passes live conformance in Python and TypeScript.
- Every tenant-owned table passes automated RLS and isolation checks.
- Every retryable mutation passes idempotency tests.
- A production deployment passes security, backup/restore, and multi-replica
  validation before readiness.
- Optional features do not increase the default deployment's trusted surface.

## 12. Dependencies, risks, and implementation map

BackAI depends on the selected open-source systems remaining replaceable behind
its contracts. Provider-specific behavior must not leak into application code.

Primary risks:

- Breadth delays the module, CLI, and contract work that unlocks real apps.
- Security promises exceed enforcement and create false confidence.
- Handwritten SDK, OpenAPI, and runtime changes continue to drift.
- Optional adapters increase the trusted surface and maintenance burden.
- Generated apps bypass BackAI when the supported primitive is incomplete.

Implementation ownership:

| Area | Primary requirement |
| --- | --- |
| `services/runtime/` | Scope enforcement, idempotency, module loading, worker dispatch |
| `services/cli/` | Agent-ready init, lifecycle, database, validation, and JSON commands |
| `packages/sdk-py/`, `packages/sdk-ts/` | Explicit clients, parity, generated types, conformance |
| `workload-modules/` | Domain module contract and reference implementations |
| `apps/customer-app/` | Generic SaaS scaffold instead of SupportDesk coupling |
| `apps/dashboard/` | Operator evidence, connections, module and production health |
| `deploy/` | Verified Compose and Helm production paths |
| `skills/af-stack/` | Machine-readable workflow, boundaries, and current capability truth |

## 13. Release gate

BackAI should not claim that a coding agent can build any SaaS until:

1. Workload modules mount and migrate without runtime edits.
2. Python and TypeScript workers execute durably.
3. Key scopes, RLS, and idempotency are enforced and tested.
4. SDK, OpenAPI, CLI, and runtime contracts pass live conformance.
5. A fresh CLI scaffold survives the golden Codex and Claude Code builds and
   deploys through the documented production path.

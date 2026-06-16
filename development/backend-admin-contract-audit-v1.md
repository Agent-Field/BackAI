# BackAI Admin Backend Contract Audit v1

Date: 2026-06-16

This audit scopes the backend work needed for the new admin console. The admin UI must consume BackAI runtime APIs, not OSS service APIs directly. OSS systems such as LiteLLM, MinIO, Svix, AgentField, Postgres, River, and provider sidecars stay behind BackAI middleware contracts.

## Four Truth Layers

| Layer | Current truth | Needs |
|---|---|---|
| Actual backend | Broad runtime APIs exist for runs, cost, jobs, logs, admin tenants/users/keys/audit, budgets, DB, memory, search, storage, webhooks, notifications, approvals, LLM, sandbox, tools/MCP, skills, harnesses, crons, secrets, billing, OAuth, feature flags, realtime. | Product-specific gaps below, plus OpenAPI drift cleanup. |
| API contract docs | `development/ui-plan-v1.md` and `development/admin-api-gap-registry-v1.md` classify pages as backed, derived, degraded, or missing. | Keep page status separate from endpoint existence. Record backend gaps and frontend-wiring gaps separately. |
| Actual frontend | New admin shell has 48 pages, a common page model, runtime data loader, and seeded fallback. | Consume every real backend contract before showing derived/seeded rows; wire mutations per page. |
| Frontend docs | Product page inventory and action expectations exist. | Add implementation status per control/action: `wired`, `display-only`, `derived`, `seeded fallback`, or `blocked by backend`. |

## Implemented In This Pass

| Contract | Status | Notes |
|---|---|---|
| `GET /api/v1/admin/adapters` | Implemented | Runtime-owned adapter slot inventory. Returns active adapter, tier, kind, status, capabilities, swap method/env, and native admin URL when known. |
| Frontend API client | Implemented | `api.admin.adapters.list()` validates the runtime registry response. |
| New admin data loader | Implemented | Setup -> Adapters now consumes the runtime registry first, then supplements with tool/native/harness rows. |
| Existing docs | Updated | Setup -> Adapters moved from pending/degraded to backed for inventory. |

## Frontend Direct-Service Audit

Rule: the new admin UI must not fetch, subscribe to, or mutate OSS services directly. Browser data access must go through same-origin `/api/v1/...`, which is proxied by the dashboard server to the BackAI runtime. The runtime is the middleware boundary for LiteLLM, MinIO/S3, Svix, AgentField, River/Postgres-backed jobs, notification providers, billing providers, and future remote adapters.

| Surface | Current finding | Action |
|---|---|---|
| New admin browser REST calls | No direct service calls found. `apps/dashboard/src/lib/api.ts` routes browser requests through same-origin `/api/v1/...`; multipart upload also posts to `/api/v1/storage/upload`. | Keep this as the only allowed browser data path. |
| New admin realtime | No direct runtime/service URL found in new admin. Legacy live-runs uses same-origin `/api/v1/realtime/runs`. | Add a backend-owned realtime proxy before adding new-admin live streams. |
| New admin native service URLs | No new-admin data source consumes LiteLLM, MinIO, Svix, AgentField, Postgres, Redis, Stripe, or provider URLs directly. `admin_ui` is backend-returned metadata only and is not used as a data source. | Any future native admin link must be display/navigation only and must come from `GET /api/v1/admin/adapters`. |
| Dashboard server -> runtime | Expected boundary. Server-side `api.ts` and `app/api/v1/[...path]/route.ts` use `RUNTIME_URL` to talk to the BackAI runtime. | Allowed. This is the middleware path. |
| Dashboard server -> Postgres | Auth/session/bootstrap code uses `DATABASE_URL` / `AF_STACK_DATABASE_URL` for better-auth tables and `suite_operators`. | Allowed short-term as dashboard auth infrastructure, but not a page data path. Longer term, operator/session admin APIs should move fully behind runtime auth contracts. |
| Legacy `/old` pages | Legacy pages still build direct link-outs from `NEXT_PUBLIC_RUNTIME_UI_URL`, `NEXT_PUBLIC_LITELLM_UI_URL`, and provider/config env values. The audit found link-outs/config display, not direct browser data fetches to OSS APIs. | Do not copy this pattern into the new admin. Remove these link-outs when `/old` is retired or proxy them through runtime-owned metadata. |

Non-regression guard: new admin pages must not introduce `fetch()` to service URLs, `NEXT_PUBLIC_*_URL` service endpoints, provider SDK calls, direct Postgres/Redis/S3/Svix/LiteLLM calls, or direct WebSocket URLs outside same-origin `/api/v1/...`.

## Default OSS Deployment Services

Current local OSS compose includes:

| Service | Purpose |
|---|---|
| Postgres/pgvector | Runtime state, River queue tables, search/memory, tenancy, audit. |
| AgentField | Agent/runtime substrate, executions, run graph links. |
| LiteLLM | LLM provider routing and model compatibility. |
| MinIO | Local S3-compatible object storage. |
| Svix + Svix Postgres + Svix Redis | Outbound webhook delivery backend. |
| Runtime | BackAI middleware API surface. |
| Dashboard/customer-app/supportdesk-agent | UI and example app surfaces. |

Not present by default but needed for full admin depth:

| Service | Needed for |
|---|---|
| OTel collector + trace store such as Tempo, Jaeger, Honeycomb, or Langfuse | In-product trace browser. |
| Prometheus/Grafana or equivalent | Deep observability/metrics queries beyond runtime summary. |
| Remote adapter sidecars | Third-party slot implementations via `AF_STACK_<SLOT>_ADAPTER=remote`. |
| Resend/Postmark/Slack/SMS providers | Real notification channels beyond log adapter. |
| Stripe external or Lago service | Real billing provider behavior. |
| Railway/Fly/Render/Kubernetes/Nomad integrations | Deploy target status/provisioning. |

## Adapter Contract Scope

BackAI's universal adapter protocol requires every remote adapter to expose:

| Adapter endpoint | Runtime use |
|---|---|
| `GET /healthz` | Probe adapter readiness and report `healthy`, `degraded`, or `unhealthy`. |
| `GET /v1/capabilities` | Declare feature flags and limits for UI gating and runtime validation. |
| `GET /v1/info` | Optional operator metadata such as native admin URL/docs. |

BackAI aggregates those into `GET /api/v1/admin/adapters` for the dashboard.

| Slot | BackAI middleware requirement | Underlying service | Current state |
|---|---|---|---|
| `auth` | Capabilities, token/session verify/refresh/revoke, user lookup, optional session enumeration. | better-auth today; remote auth adapter later. | Protocol exists. Inventory row is live. Session enumeration remains unresolved. |
| `llm-chat` | Chat/embedding/model capabilities, fallback/tools/vision/streaming flags. | LiteLLM today; remote LLM provider later. | Inventory row is live. Typed capability extraction from gateway still pending. |
| `multimodal` | TTS/STT/image verb capabilities. | LiteLLM, ElevenLabs, Cartesia, fal, Flux. | Inventory row is live. Typed capability accessor still pending. |
| `storage` | Object storage capabilities and limits. | MinIO/S3-compatible storage. | Inventory row and synthesized capabilities are live. |
| `sandbox` | Run/stream/pool capabilities and limits. | Docker/gVisor/Firecracker/e2b/remote. | Inventory row and capabilities are live. |
| `notifications` | Channel capabilities, delivery status, retry support, metadata support. | log/Resend/Postmark/Slack/etc. | Inventory row is live. Full channel CRUD and typed capability accessor are pending. |
| `secrets` | Vault capabilities: reveal, rotation, versioning, metadata, KMS label. | envelope-local today; remote/KMS later. | Inventory row is live. Typed capability accessor remains pending. |
| `billing` | Customer/subscription/meter/portal/webhook capabilities. | Stripe/Lago/none. | Inventory row is live. Typed capability accessor remains pending. |
| `database` | Config-swappable Postgres health/capabilities. | Postgres/pgvector. | Inventory row is live. |
| `reasoning` | Agent runtime status/capabilities. | AgentField. | Inventory row is live. |
| `job-queue` | Queue status/capabilities. | River in Postgres. | Inventory row is live. |
| `webhooks` | Outbound/inbound webhook capability/status. | Svix and runtime inbound endpoint store. | Inventory row is live. |

## Backend APIs Still Missing

| BackAI API | Reason | Services required |
|---|---|---|
| `GET /api/v1/traces`, `GET /api/v1/traces/{id}` | In-product trace browser instead of run-derived trace placeholders. | OTel collector plus trace store. |
| `GET /api/v1/admin/errors` plus mute/resolve endpoints | Backend grouping/deduplication of operational errors. | Runtime log store or log sink. |
| `GET /api/v1/admin/health/deep` | DB, cert, worker, adapter, and backing service checks. | Postgres, AgentField, LiteLLM, MinIO/S3, Svix, Redis, River, adapter probes. |
| `GET /api/v1/search/indexes` | Index stats and health for Data Search. | Postgres FTS/pgvector. |
| `POST /api/v1/crons/{id}/trigger` | Manual cron execution. | River/job queue. |
| `GET /api/v1/reasoners/analytics` | Cross-agent latency/cost/error analytics. | AgentField runs plus cost events. |
| `GET /api/v1/tools/usage` | Native/MCP tool usage analytics. | Tool adapter calls, MCP calls, logs/cost/audit. |
| `GET/POST/PATCH/DELETE /api/v1/notifications/channels` | Real Setup -> Notifications channel configuration. | Notifications adapter plus durable runtime config. |
| Feature flag override/history endpoints | Tenant rollout history and override audit. | Feature flag store and audit. |
| OAuth refresh history endpoint | Debug external token refresh failures. | OAuth manager/store plus audit/log events. |
| `POST /api/v1/admin/keys/{id}/rotate` | Native key rotation rather than revoke plus issue. | API key store and LiteLLM virtual key mirroring. |
| Session list / force logout APIs | Customers -> Sessions security operations. | Auth adapter must support enumeration/revoke. |
| `GET /api/v1/admin/deploy-targets` | Runtime-owned deploy target status. | Provider integrations or static deployment inspection. |
| `GET /api/v1/admin/brand`, optional `PUT` | Runtime-owned brand contract instead of frontend/file guessing. | `brand.yaml` parser; no external service required. |
| OpenAPI/codegen download endpoint | API Explorer generated SDK/types. | OpenAPI generator or runtime schema transform. |

## Contract Drift Cleaned In This Pass

| Drift | Fix |
|---|---|
| `GET /api/v1/admin/keys/{id}/spend` had a handler/client but was not in OpenAPI registration during audit. | OpenAPI registration added. |
| `/api/v1/llm/images/edits` and `/api/v1/llm/images/variations` handlers existed while OpenAPI registered only non-`/llm` aliases. | Both aliases registered in OpenAPI. |

## Remaining Frontend Contract Debt

| Debt | Fix |
|---|---|
| Frontend still has seeded fallbacks for many pages. | Add page-level error/empty/missing states and reduce seeded fallback to demo mode only. |

## Next Backend Milestones

1. Finish typed capability accessors for all Tier-1 slots and remove `contract_pending` from synthesized capability objects.
2. Build trace browser middleware and add the required OSS trace service to dev compose if the product wants in-product traces.
3. Build notification channel CRUD.
4. Build auth session enumeration only if the active auth adapter can honestly support it.

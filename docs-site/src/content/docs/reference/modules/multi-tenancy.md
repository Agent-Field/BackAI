---
title: Module — Multi-tenancy
description: Opt-in tenant isolation, API keys, sessions, row-level security.
sidebar:
  order: 1
---

Opt-in module. Off by default — every request flows through a single implicit `default` tenant. Turn it on and the same runtime gains row-level security, per-tenant API keys, session resolution, audit log, and per-tenant rate limits.

## What it does

The `tenancy.Manager` is a Postgres-backed concrete struct (not an interface) that owns the data model for tenants, users, memberships, API keys, and audit entries. Wire shapes mirror the zod schemas in `apps/dashboard/src/lib/api.ts` exactly. The admin REST handlers in `services/runtime/internal/server/admin.go` are a thin REST veneer over Manager methods.

When the module is OFF the runtime injects `tenancy.DefaultTenantID` into every request context, no auth runs, and `/api/v1/admin/*` returns `503`.

## Configuration

```yaml
modules:
  enabled:
    multi-tenancy: true   # default: false
```

Env override:

```bash
AF_STACK_MODULE_MULTI_TENANCY=true
```

Required when on:

```bash
AF_STACK_AUTH_SECRET=<openssl rand -hex 32>   # session signing
AF_STACK_DATABASE_URL=postgres://...           # tenancy.Manager needs a pool
```

## REST endpoints

Full route table is registered in `services/runtime/internal/server/admin.go`. Summary:

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/admin/tenants` | List tenants. |
| `POST` | `/api/v1/admin/tenants` | Create tenant. |
| `GET` | `/api/v1/admin/tenants/{id}` | Get a tenant. |
| `GET` | `/api/v1/admin/tenants/{id}/drilldown` | Members + keys + usage + sparklines + recent runs + recent webhooks + billing (Phase 12.1). |
| `GET` | `/api/v1/admin/users` | List users. |
| `GET` | `/api/v1/admin/memberships` | List user/tenant bindings. |
| `GET` | `/api/v1/admin/keys` | List tenant API keys. |
| `POST` | `/api/v1/admin/keys` | Issue a key (returns one-time plaintext). |
| `DELETE` | `/api/v1/admin/keys/{id}` | Revoke a key. |
| `GET` | `/api/v1/admin/audit` | Tail the audit log. |

See the OpenAPI spec at `/openapi.json` for full schemas.

## Database tables

Owned by migration `00001_init.sql`:

- `suite_tenants` — tenants (id, slug, name, plan, settings, quota, created_at, deleted_at)
- `suite_users` — users (id, email, ...)
- `suite_memberships` — user x tenant role bindings
- `suite_api_keys` — tenant-scoped bearer keys (prefix + hashed secret)
- `suite_gateway_requests` — auth + routing audit
- `suite_audit_log` — per-tenant action log

Plus better-auth scaffolding in `00002_better_auth.sql` (`user`, `session`, `account`, `verification`) and RLS policies in `00004_rls.sql`.

## Env vars

| Env | Purpose |
|---|---|
| `AF_STACK_MODULE_MULTI_TENANCY` | Enable / disable the module. |
| `AF_STACK_AUTH_SECRET` | Session signing secret. Required when MT is on. |

## Code map

- `tenancy.go` — package doc, sentinel errors, wire types (`Tenant`, `User`, `Membership`, `APIKey`, `IssuedAPIKey`, `AuditEntry`).
- `manager.go` — Postgres-backed `Manager` (create/list/get tenants, issue/revoke keys, write audit, fire `HookTenantCreated`).
- `token.go` — API key encoding (`tnt_<prefix>_<secret>` format).
- `server/admin.go` — REST handlers.
- `server/tenant_resolver.go` — auth middleware: bearer key, then session, then default.

## Related

- Fires [`tenant.created`](../../hooks/#tenantcreated).
- Powers per-tenant gating for [Cost](./cost/), [Billing](./billing/), [Storage](./storage/), [Webhooks](./webhooks/), [Notifications](./notifications/).

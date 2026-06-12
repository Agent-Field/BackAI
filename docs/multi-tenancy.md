# Multi-tenancy

Multi-tenancy (MT) is an **opt-in** suite module. Off by default, every
request inherits a single implicit "default" tenant — the runtime behaves
like a single-tenant backend with no per-customer accounting. Turn it on
and the same suite gains hard isolation between customer orgs, with no
code changes in your agents or handlers.

This page covers what MT enables, how to switch it on, how tenant
context is resolved at request time, and how to verify isolation with
the bundled end-to-end test.

## What turning MT on changes

| Area | MT off | MT on |
|---|---|---|
| **Tenant context** | Implicit single `default` tenant on every request | Resolved per-request from API key prefix or session |
| **Data isolation** | All rows share one tenant | Postgres **row-level security** enforces `app.tenant_id` on every query |
| **Object storage** | Bucket key passes through verbatim | Object keys are silently prefixed with `tenants/<id>/` |
| **API keys** | Single suite-wide operator key | Tenant-scoped customer keys, with one-time reveal and revocation |
| **Rate limiting** | One bucket for the whole suite | Per-tenant token bucket (default `60 req/min`) |
| **Audit log** | Operator actions only | Every authenticated tenant action is appended to `audit_log` |
| **Dashboard `Customers/*` tabs** | Shows empty-state CTA pointing here | Renders real content (tenants list, users, keys, billing, audit) |
| **REST surface** | `/api/v1/agents/*`, `/api/v1/secrets/*`, … | Adds `/api/v1/admin/{tenants,users,memberships,keys,audit}` |

Nothing about your agents changes. The same `app.harness()` and
`app.ai()` calls work identically — but every call now flows through a
tenant-bound request context and writes tenant-scoped rows.

## Turning it on

Edit `apps/backend/config.yaml` (create the file if it doesn't exist):

```yaml
modules:
  enabled:
    multi-tenancy: true
```

Then bounce the runtime so it picks up the new config:

```bash
docker compose up -d runtime
```

Confirm the runtime sees the change:

```bash
curl -s http://localhost:8080/api/v1/modules | jq '.multi_tenancy_enabled'
# → true
```

Once `multi_tenancy_enabled: true`:

- The dashboard's **Customers** group switches from empty-state to real
  pages (`/customers/tenants`, `/customers/users`, `/customers/api-keys`,
  `/customers/customer-billing`, `/customers/audit`).
- The admin REST endpoints become live.
- Every gateway call must carry a tenant-bound credential (API key or
  authenticated session) — anonymous calls now 401.

## How tenant context is resolved

The runtime resolves a tenant for every inbound HTTP request in this
order. The first match wins. If none match and MT is **on**, the request
fails closed (`401 TENANT_REQUIRED`). When MT is **off**, the runtime
falls back to the implicit `default` tenant.

1. **API key (Bearer header).** `Authorization: Bearer <value>` is
   matched against `api_keys.prefix` (lookup) and the supplied secret is
   verified against `api_keys.secret_hash`. The row carries the
   `tenant_id`, which becomes the request's tenant.
2. **Session cookie.** A signed-in dashboard or end-user session
   resolves to a `users.id`. The runtime joins through `memberships` to
   find a tenant the user belongs to. When a user belongs to more than
   one tenant, an `X-Tenant: <slug>` header (or `?tenant=<slug>`) picks
   which one. With no hint and multiple memberships, the request fails
   `400 TENANT_AMBIGUOUS`.
3. **Default tenant (MT off only).** With MT disabled, every request is
   pinned to the bootstrap tenant `00000000-…-default` so handlers and
   queries don't have to special-case the off state.

The resolved tenant is exposed two ways:

- **In Go (request context).** `tenancy.FromContext(r.Context())` returns
  the resolved tenant struct.
- **In Postgres (per-connection session var).** Every request acquires a
  connection from the pool, runs
  `SET LOCAL app.tenant_id = '<uuid>'`, and the RLS policies on every
  tenant-scoped table reference `current_setting('app.tenant_id')`. This
  means even a hand-written SQL query in `apps/backend/handlers/*.go`
  cannot accidentally bypass isolation — Postgres rejects rows that
  don't match the session var.

> **The `app.tenant_id` pattern is the foundation.** Per-connection
> RLS via a Postgres session variable means *the same Go code, the same
> SQL, gets per-tenant row filtering for free*. No `WHERE tenant_id = …`
> sprinkled through application code. No risk of a forgotten join
> condition leaking another tenant's data.

## API key issuance: the one-time-reveal contract

Customer API keys are the primary credential for programmatic access.
Issuance returns the secret value **exactly once**.

```bash
curl -X POST http://localhost:8080/api/v1/admin/keys \
  -H 'Content-Type: application/json' \
  -d '{
    "tenant_id": "<tenant-uuid>",
    "name": "ci-bot",
    "scopes": ["agents:invoke","secrets:read"]
  }'
```

Response (POST only; never returned again):

```json
{
  "id": "k_01J9…",
  "tenant_id": "t_01J9…",
  "prefix": "afsk_live_8x2a",
  "value": "afsk_live_8x2a.abcdefghijklmnopqrstuvwxyz0123456789",
  "scopes": ["agents:invoke","secrets:read"],
  "created_at": "2026-06-06T09:30:00Z"
}
```

Subsequent `GET /admin/keys` returns the same shape **without** `value`.
The dashboard surfaces the secret in a one-time reveal modal — copy it
to a vault before closing.

To rotate: revoke the old key (`DELETE /admin/keys/{id}`), issue a new
one, swap the credential in the customer's deployment.

## Rate limiting + audit middleware

With MT on, two cross-cutting middlewares are wired in:

- **Rate limiter**: a token-bucket per `tenant_id`. Defaults
  to `60 req/min` per tenant; override per tenant via the
  `quota.rate_limit` field on `PATCH /admin/tenants/{id}`. Burst beyond
  the bucket returns `429 RATE_LIMITED` with a `Retry-After` header.
- **Audit middleware**: every request that mutates state, or
  hits an admin endpoint, appends a row to `audit_log` with `tenant_id`,
  `user_id`, `api_key_id`, `action`, and freeform `metadata`. Query via
  `GET /admin/audit?tenant=…&action=…&from=…&to=…`.

## Verifying isolation end-to-end

The bundled script exercises the full happy path against a running
stack:

```bash
./scripts/test-multi-tenancy.sh
```

What it checks:

1. The runtime reports `multi_tenancy_enabled: true` after the config
   change.
2. Two tenants (`acme-corp`, `globex-inc`) can be created via the admin
   API.
3. Issuing an API key returns `value` exactly once; listing keys does
   not leak it.
4. Both keys can invoke `sample.echo` through the gateway.
5. `GET /admin/audit?tenant=<acme>` contains acme's key id and **never**
   globex's key id (audit scope is per-tenant).
6. A secret written by acme is **not visible** when listed as globex
   (RLS holds for the `suite_secrets` table).
7. Bursting more than `60 req/min` as one tenant trips the rate limiter
   (`HTTP 429`).
8. Cleanup: keys revoked, tenants soft-deleted.

The script is **safe to run against any environment**: if MT isn't
enabled, it prints
`SKIP` with the reason and exits 0 instead of failing.

## Turning it back off

Set the flag back to `false` (or remove the line) and bounce the
runtime. Existing tenant rows stay in Postgres — they're inert until MT
is re-enabled. All requests revert to the implicit `default` tenant.

This is deliberate: turning MT off should never destroy customer data.

## See also

- `apps/backend/config.yaml` — the on/off switch and adapter selection.
- `services/runtime/internal/tenancy/` — the Go tenancy manager.
- `services/runtime/internal/server/admin*.go` — admin REST handlers.
- `docs/dashboard-ia.md` — where the `Customers/*` tabs live in the IA.
- `scripts/test-multi-tenancy.sh` — this page's executable companion.

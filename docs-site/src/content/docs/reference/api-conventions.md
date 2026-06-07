---
title: API Conventions
description: Auth, errors, pagination, rate limits, idempotency, versioning.
---

Hand-written companion to the [API Reference](./api/). Everything below
applies to every endpoint under `/api/v1/*` unless an endpoint's spec
entry says otherwise.

## Auth

Two credential types are accepted on every authenticated route. The
runtime resolves them in this order, first match wins.

### 1. API key (preferred for machines)

```
Authorization: Bearer af_<prefix>_<secret>
```

Issue keys from the dashboard (Settings > API keys) or via
`POST /api/v1/admin/keys`. Each key is bound to one tenant; the runtime
attaches both `tenant_id` and `api_key_id` to the request context.

The bearer string is matched literally. There is no JWT, no rotation
window — old keys keep working until revoked.

### 2. Session cookie (preferred for browsers)

```
Cookie: better-auth.session_token=<token>
Cookie: __Secure-better-auth.session_token=<token>   # over TLS
```

Set by better-auth at the dashboard sign-in endpoint
(`POST /api/auth/sign-in/email`). The runtime walks
`session.userId -> suite_users.id -> suite_memberships -> tenant_id`.

When multi-tenancy is **off** (single-tenant deploy), unauthenticated
requests fall through to the default tenant. When multi-tenancy is
**on**, unauthenticated requests get `401` (no Bearer) or `403`
(session has no membership).

Public routes that bypass auth entirely: `/health`, `/ready`,
`/metrics`, `/openapi.json`, and a small number of dashboard
read-only endpoints. See `isPublicPath()` in
`services/runtime/internal/server/tenant_resolver.go`.

## Error envelope

Every non-2xx response uses one shape:

```json
{
  "error": {
    "code": "VALIDATION_FAILED",
    "message": "limit must be a positive integer",
    "details": { "field": "limit" }
  }
}
```

`code` is a stable machine-readable enum (`VALIDATION_FAILED`,
`RATE_LIMITED`, `GATEWAY_NOT_CONFIGURED`, `NOT_FOUND`, ...). `message`
is for humans. `details` is a free-form object scoped per code — most
codes carry no details, some carry enough to drive UI (e.g. a `429`
includes `details.retry_after_seconds`).

The envelope is declared in the OpenAPI spec under
`components.schemas.ErrorEnvelope` and `$ref`d from every operation's
`400 / 401 / 403 / 404 / 429 / 500 / 502 / 503` response.

## Pagination

Two styles are in use today depending on the backing store.

### Offset + limit (default for SQL-backed lists)

```
GET /api/v1/runs?limit=20&offset=40
GET /api/v1/jobs?name=foo&state=completed&limit=10&offset=0
GET /api/v1/notifications?limit=5
GET /api/v1/webhooks/deliveries?limit=10
```

* `limit` clamps to `[1, 100]` (some lists clamp at 1000 — check the
  per-endpoint spec).
* `offset` defaults to 0.
* Response includes a `total` count so clients can compute "more
  available".

### Opaque cursor (for object stores)

```
GET /api/v1/storage?prefix=tenant-a/&limit=100&next_token=<opaque>
```

* Endpoints that paginate over an external key/value store (S3-style)
  return `next_token` on partial pages.
* Pass it back unchanged on the next call.
* Empty / missing `next_token` means "end of the list".

**Gap:** there is no unified `cursor`/`next_cursor` convention across
endpoints. The two styles above are the current state; a v2 may
standardise on opaque cursors everywhere. Track this in the roadmap
before consolidating.

## Idempotency

**Current state: no endpoint accepts an `Idempotency-Key` header
today.** A grep across `services/runtime/internal/` returns zero hits.

Until the header lands, the practical guidance is:

* `POST /api/v1/webhooks/send` deduplicates internally by
  `(tenant, url, event_type, body_hash)` within a short window.
* `POST /api/v1/notifications` does **not** deduplicate. Retrying a
  network-timed-out call may send the same notification twice.
* `POST /api/v1/sandbox/run` is non-idempotent by design — every call
  is a new sandbox lifecycle.

**Planned:** every mutating route accepts
`Idempotency-Key: <client-chosen-uuid>` and replays the same response
for 24h. The PR will land alongside the Stripe-style idempotency store
in `services/runtime/internal/idempotency/`. Track the gap on the
roadmap; do not assume safety until it ships.

## Rate limits

In-process token bucket per `(tenant_id, route)`. See
`services/runtime/internal/ratelimit/ratelimit.go`.

| Knob | Default | Source |
|---|---|---|
| Steady refill | **60 requests / minute** per tenant | `DefaultRPM` const |
| Burst capacity | **10 requests** | `defaultBurst` const |
| LRU cap (process-wide) | 10 000 buckets | `defaultMaxBuckets` |

Per-tenant overrides come from `suite_tenants.quota.rpm` (jsonb) when
the column is set; otherwise the default applies.

### Headers on 429

```
HTTP/1.1 429 Too Many Requests
Retry-After: 3
Content-Type: application/json

{
  "error": {
    "code": "RATE_LIMITED",
    "message": "request exceeded per-tenant rate limit",
    "details": { "retry_after_seconds": 3 }
  }
}
```

* `Retry-After` is the seconds the client should wait, rounded up,
  floored at 1.
* The JSON envelope's `details.retry_after_seconds` carries the same
  value for clients that prefer not to parse headers.

**Gap:** the runtime does **not** emit `X-RateLimit-Limit`,
`X-RateLimit-Remaining`, or `X-RateLimit-Reset` headers today. Clients
that want a remaining-quota display should consume `/api/v1/admin/quotas`
(per-tenant aggregate) until those response headers are added.

### Fail-open

If the limiter itself errors (e.g. the v2 Redis adapter has a network
blip), the request is **admitted** with a warning log. Treat
rate-limit headers as soft hints, not contract.

## Versioning

* Every public route is mounted under `/api/v1/`.
* The contract under `v1` is **stable through 1.x** — additive changes
  only (new fields, new endpoints, new optional params). No field
  renames, no enum value removals, no required-field additions, no
  breaking error code changes.
* Breaking changes ship under `/api/v2/`. `v1` keeps responding to
  every existing route for at least 12 months after `v2` ships.
* `info.version` in `openapi.json` tracks the runtime release, not the
  API version. A `0.1.0` runtime can serve the `v1` API contract just
  as well as a `1.0.0` runtime.

When in doubt, the `openapi.json` snapshot in
[`docs-site/public/openapi.json`](https://github.com/Agent-Field/backai/blob/main/docs-site/public/openapi.json)
is the source of truth. Re-run `docs-site/scripts/fetch-openapi.sh`
to refresh.

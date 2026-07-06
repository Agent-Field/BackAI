# BackAI Adapter Protocol — Universal Contract

> Single source of truth for every remote adapter. Per-slot protocols
> inherit from this document.

## 1. Purpose

BackAI is composed of pluggable subsystems (sandbox, object storage,
notifications, secrets, billing, multimodal LLM, ...). Each subsystem has
a small **Go interface** inside the runtime and at least one **built-in
adapter** that satisfies it.

The **remote-adapter protocol** in this document lets a third party host
their own adapter as a separate process and have the BackAI runtime use
it without code changes to the runtime. The third party can write in any
language. The runtime ships a generic "remote shim" per slot that
satisfies the Go interface by speaking this HTTP protocol.

Two principles:

1. **The HTTP protocol mirrors the Go interface 1:1** for every slot.
   Adding methods to the Go interface adds endpoints to the protocol;
   nothing else.
2. **Every adapter (built-in or remote) declares its capabilities** so
   the runtime — and the dashboard — can adapt to what's actually
   supported without dead code paths.

## 2. Transport and shape

| Aspect | Decision |
|---|---|
| Transport | HTTP/1.1 or HTTP/2; runtime uses Go `net/http` client |
| Encoding | JSON (`application/json; charset=utf-8`) for control plane |
| Streams | `text/event-stream` (SSE) for log tails and incremental progress |
| Binary uploads | `application/octet-stream` with `X-BackAI-Content-Type` header for nested type |
| Versioning | Path prefix `/v1`. Future versions live at `/v2`. Adapters may serve multiple major versions concurrently. |
| Field convention | `snake_case` JSON keys. ISO-8601 timestamps in UTC with `Z` suffix. |
| Nullability | Missing fields and explicit `null` are equivalent. Adapters MUST tolerate either. |
| Unknown fields | Adapters MUST ignore unknown JSON fields (forward compatibility). |

## 3. Authentication

Two modes the runtime supports out of the box:

### 3.1 Bearer token (default)

```
Authorization: Bearer <token>
```

The token is read from an env var per slot, e.g.
`AF_STACK_SANDBOX_ADAPTER_TOKEN`. Adapters validate the token on every
request. Health and capability endpoints **also** require auth — there
is no anonymous endpoint.

### 3.2 None (development only)

For local development the runtime supports `AF_STACK_<SLOT>_ADAPTER_AUTH=none`.
Adapters are free to ignore the `Authorization` header in this mode.

mTLS is out of scope for v1; operators who need it can terminate at a
sidecar reverse proxy.

## 4. Required envelope

Every request the runtime sends carries these headers (in addition to
auth):

| Header | Purpose | Required |
|---|---|---|
| `X-BackAI-Request-Id` | Unique id (uuid) per request; used for log correlation | Yes |
| `X-BackAI-Idempotency-Key` | Stable key for retry-safe operations; identical key + same body MUST produce identical effect | Required for write ops; ignored for reads |
| `X-BackAI-Tenant-Id` | The originating tenant (empty for operator-side calls). Adapters MUST NOT enforce tenant isolation themselves — the runtime does that. They MAY record the value for audit. | Set when known |
| `X-BackAI-Runtime-Version` | Semver of the runtime invoking the adapter | Set always |
| `Content-Type` | `application/json; charset=utf-8` for control plane; `application/octet-stream` for binary; `multipart/form-data` only where the per-slot protocol explicitly allows it | Yes |

## 5. Response envelope

Adapters return either:

**Success** — HTTP `200 OK` or `201 Created` for writes that produce a
new resource. Body is the slot-specific JSON.

**Empty success** — `204 No Content`. Allowed for idempotent deletes.

**Streaming success** — `200 OK` with `Content-Type: text/event-stream`.
Each event is a single JSON object per the per-slot protocol.

**Failure** — non-2xx. The body MUST be an [RFC 7807](https://www.rfc-editor.org/rfc/rfc7807)
problem details object:

```json
{
  "type": "https://docs.backai.dev/errors/sandbox/image-not-found",
  "title": "Image not found",
  "status": 404,
  "detail": "Image 'python:3.99' could not be pulled.",
  "code": "image_not_found",
  "request_id": "01HZ...",
  "retry_after_seconds": 0
}
```

The `code` field is a stable machine-readable error code. The set of
codes per slot is documented in that slot's protocol spec.

## 6. Required common endpoints

Every adapter, regardless of slot, MUST expose these.

### 6.1 `GET /healthz`

Liveness + readiness check.

**Response (200 OK)**:

```json
{
  "status": "healthy",
  "started_at": "2026-06-15T10:00:00Z",
  "uptime_seconds": 42,
  "dependencies": [
    {"name": "docker", "status": "healthy"},
    {"name": "outbound_dns", "status": "healthy"}
  ]
}
```

`status` is one of `healthy`, `degraded`, `unhealthy`. Adapters return
HTTP `200` even for `degraded` (the runtime decides what to do); only
`unhealthy` returns `503`.

### 6.2 `GET /v1/capabilities`

Declare what this adapter supports. The shape is per-slot, but every
response MUST include the common envelope:

```json
{
  "name": "docker",
  "version": "1.0.0",
  "slot": "sandbox",
  "protocol_version": "v1",
  "vendor": "BackAI",
  "homepage": "https://github.com/example/backai-docker-adapter",
  "capabilities": {
    "...": "slot-specific keys"
  }
}
```

`capabilities` is a flat object of feature flags and limits. Per-slot
protocols define the exact keys.

The runtime queries this endpoint at boot and at config-reload time.
Adapters MAY change capabilities between calls (e.g., to reflect
upstream provider degradation); the runtime re-fetches on a configurable
interval (default 5 minutes).

### 6.3 `GET /v1/info`

A free-form metadata endpoint. Optional. Returns operator-facing
information that doesn't fit elsewhere (e.g., a link to a custom admin
UI). Used to populate the "Open admin ↗" link on the Setup → Adapters
page in the dashboard.

**Response (200 OK)**:

```json
{
  "admin_ui": "https://my-sandbox-provider.example.com/admin",
  "docs": "https://my-sandbox-provider.example.com/docs",
  "support_email": "support@example.com"
}
```

Absent or `404` is allowed — the dashboard simply renders no admin link.

## 7. Streaming convention (SSE)

Adapters that stream (log tails, incremental progress) use Server-Sent
Events. Each event is one line of `data: ` followed by a single-line
JSON object, terminated by an empty line.

```
data: {"ts":"2026-06-15T10:00:01Z","stream":"stdout","text":"hello\n"}

data: {"ts":"2026-06-15T10:00:02Z","stream":"stdout","text":"world\n"}

data: {"event":"terminated","exit_code":0}

```

The final event for every stream is a `terminated` event with the
operation-specific outcome. After this the adapter closes the
connection.

If the client disconnects, the adapter MUST cancel any underlying
operation it started for the stream.

## 8. Idempotency

Write operations (`POST`, `PUT`, `DELETE`) accept `X-BackAI-Idempotency-Key`.
Adapters MUST:

1. Cache the response body keyed by `(method, path, idempotency_key)`
   for at least 10 minutes.
2. On a repeat request with the same key, return the cached response
   verbatim.
3. Reject (HTTP `409 Conflict`) if the same key is used with a
   different body — this catches client bugs.

The runtime always sends an idempotency key for writes. The runtime's
retry policy depends on this; an adapter that doesn't implement
idempotency will see duplicate work under network flakes.

## 9. Versioning policy

- The path prefix is `/v1`. Breaking changes go to `/v2`.
- Adding new optional fields to requests/responses is **not** breaking.
- Adding new methods is **not** breaking.
- Adding new capability keys is **not** breaking; clients ignore unknown keys.
- Renaming or removing fields, changing semantics, or tightening
  validation **is** breaking and must go to a new major version.

Adapters MAY serve multiple major versions concurrently from different
path prefixes. The runtime is configured per slot which major version
to use.

## 10. Discovery and configuration

Operators wire a remote adapter via two env vars per slot:

```
AF_STACK_<SLOT>_ADAPTER=remote
AF_STACK_<SLOT>_ADAPTER_URL=http://my-adapter:8080
AF_STACK_<SLOT>_ADAPTER_TOKEN=<bearer-token-or-empty>
AF_STACK_<SLOT>_ADAPTER_AUTH=bearer|none
```

At runtime start the BackAI runtime:

1. Reads each slot's adapter env vars.
2. If `=remote`, instantiates a slot-specific "remote shim" Go type that
   satisfies the slot's interface by speaking this protocol.
3. Calls `GET /v1/capabilities` and `GET /healthz` to verify the adapter
   is reachable and to populate the dashboard's adapter inventory.
4. Logs the adapter name + version on every gateway call so operators
   can trace which adapter handled what.

If the adapter is unreachable at boot, the runtime starts in a degraded
state for that slot and surfaces it on `GET /api/v1/admin/adapters`.

**Selector validation.** The `AF_STACK_<SLOT>_ADAPTER` value is validated
against the slot's known adapters at boot. An unsupported value fails fast
(rather than being silently ignored and falling back to a default).

**Credentials via the admin UI.** For slots that opt in, the URL + token
(and provider keys) need not be env vars — operators can set them from the
dashboard → Platform → Integrations page (`PUT /api/v1/admin/integrations/{slot}`),
which stores them in the secrets vault under `integration/{slot}/{field}`.
The factories resolve env first, then the vault credential. UI changes take
effect on the next runtime restart; the API never returns raw secret values
(masked status only). See [`README.md`](README.md#configuring-adapter-credentials-env-or-admin-ui).

## 11. Operator-visible introspection

The runtime exposes `GET /api/v1/admin/adapters` to the dashboard:

```json
{
  "slots": [
    {
      "slot": "sandbox",
      "tier": 1,
      "active": {
        "name": "docker",
        "version": "1.0.0",
        "status": "healthy",
        "kind": "builtin",
        "capabilities": {"max_timeout_s": 86400, "supports_gpu": false}
      },
      "available_builtin": ["docker", "gvisor", "firecracker", "e2b"],
      "swap_method": "env_var",
      "swap_env": "AF_STACK_SANDBOX_ADAPTER",
      "admin_ui": null
    }
  ]
}
```

`tier` reflects how swap-able the slot is:

- **1** — hot-swappable: drop-down in dashboard, change env, restart.
- **2** — config-swappable: same protocol (e.g., Postgres-compatible), edit connection string.
- **3** — interface-swappable: only one impl today, write your own.
- **4** — foundational: not swap-able (e.g., the agent runtime itself).

`kind` is `builtin` (Go-in-tree adapter) or `remote` (HTTP shim talking
to a sidecar).

## 12. Conformance test harness

A separate Go binary `cmd/backai-adapter-conformance` (see
`docs/adapters/CONFORMANCE.md`) hits a target adapter URL and validates:

1. Required endpoints respond (`/healthz`, `/v1/capabilities`).
2. Headers are accepted and not echoed back.
3. Idempotency works (POST same key twice returns same body).
4. RFC 7807 error envelope on failures.
5. SSE termination event arrives for streaming endpoints.
6. Per-slot protocol conformance (driven by the slot spec).

Any adapter — built-in or remote — should pass the harness for its
slot. Built-in adapters expose an HTTP-test wrapper so the same harness
can validate them too; this is how we keep the protocol and the Go
interface in sync.

## 13. What this protocol does NOT cover

- Multi-region deployment of an adapter (handled at the operator's
  infrastructure layer).
- Adapter-to-adapter calls (adapters don't call each other directly).
- Authentication of the dashboard to the adapter (dashboard talks to
  the runtime, not adapters).
- Per-tenant adapter selection (operators choose one adapter per slot
  per fork in v1; per-tenant routing is a future concern).

## 14. Status

This protocol is the v1 contract. The slots that ship with it:

- Sandbox (`docs/adapters/protocols/sandbox-v1.md`)
- Object storage (`docs/adapters/protocols/storage-v1.md`)
- Notifications (`docs/adapters/protocols/notifications-v1.md`)
- Secrets (`docs/adapters/protocols/secrets-v1.md`)
- Billing (`docs/adapters/protocols/billing-v1.md`)
- Multimodal LLM (`docs/adapters/protocols/multimodal-v1.md`)
- **LLM chat gateway** (`docs/adapters/protocols/llm-chat-v1.md`)
- **Auth** (`docs/adapters/protocols/auth-v1.md`)
- **Logs** (`docs/adapters/protocols/logs-v1.md`)
- **Traces** (`docs/adapters/protocols/traces-v1.md`)
- **Metrics** (`docs/adapters/protocols/metrics-v1.md`)
- **Errors** (`docs/adapters/protocols/errors-v1.md`)

Job queue, outbound webhooks, and the reasoning layer are not covered
by this protocol in v1. The job queue and reasoning layer remain
hardcoded to their respective OSS (River, AgentField); outbound
webhooks are handled by the runtime's own native outbox (PG queue +
tick worker, HMAC signing, retry with backoff, delivery ledger). They
will be added as adapter slots in later versions once the Go interfaces
are extracted.

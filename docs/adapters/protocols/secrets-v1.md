# Secrets Adapter — Protocol v1

> Inherits from [`PROTOCOL.md`](../PROTOCOL.md).
>
> **Slot:** `secrets` · **Base path:** `/v1` · **Go interface:**
> `services/runtime/internal/secrets/Vault`

## Purpose

A secrets adapter stores, rotates, and reveals API keys and other
plaintext secrets the platform needs to call upstream services. Built-in
backend uses envelope encryption with a local KEK or KMS (AWS, Azure,
GCP). Remote adapters can wrap HashiCorp Vault, Doppler, AWS Secrets
Manager, etc.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/v1/secrets` | List secret names (no values) |
| `GET` | `/v1/secrets/{key}` | Get secret metadata (no value) |
| `PUT` | `/v1/secrets/{key}` | Set / overwrite a secret value |
| `POST` | `/v1/secrets/{key}/reveal` | Return the plaintext value (audit-logged) |
| `POST` | `/v1/secrets/{key}/rotate` | Rotate to a new value |
| `DELETE` | `/v1/secrets/{key}` | Delete a secret |
| `GET` | `/v1/capabilities` | Capability declaration |
| `GET` | `/healthz` | Liveness |
| `GET` | `/v1/info` | Optional metadata |

## 1. `GET /v1/secrets`

List secrets the caller can see. Values are never included.

**Query params**:

| Param | Default | Meaning |
|---|---|---|
| `prefix` | `""` | Filter by key prefix. |
| `limit` | `100` | Page size. |
| `token` | `""` | Continuation token. |

**Response (200 OK)**:

```json
{
  "secrets": [
    {
      "key": "openai_api_key",
      "version": 3,
      "created_at": "2026-06-15T10:00:00Z",
      "updated_at": "2026-06-15T10:00:00Z",
      "last_rotated_at": "2026-06-15T10:00:00Z",
      "metadata": {"vendor": "openai"}
    }
  ],
  "next_token": ""
}
```

## 2. `GET /v1/secrets/{key}[?version=N]`

Metadata only. **Never returns the plaintext value.** Use `/reveal` for
that.

When `?version=N` is supplied and `capabilities.supports_versioning`
is `true`, returns metadata for that specific version. Without the
parameter (or when versioning isn't supported), returns the current
version.

**Response (200 OK)**:

```json
{
  "key": "openai_api_key",
  "version": 3,
  "created_at": "2026-06-15T10:00:00Z",
  "updated_at": "2026-06-15T10:00:00Z",
  "last_rotated_at": "2026-06-15T10:00:00Z",
  "metadata": {"vendor": "openai"}
}
```

**404** with `code: "secret_not_found"` if missing.

## 3. `PUT /v1/secrets/{key}`

Set or overwrite a secret value. The body is the plaintext (encoded as
a JSON string, not a separate JSON field — keeps the protocol simple
for clients).

**Request body**:

```json
{
  "value": "sk-abc123...",
  "metadata": {"vendor": "openai"}
}
```

| Field | Required | Notes |
|---|---|---|
| `value` | yes | Plaintext. The adapter encrypts at rest. |
| `metadata` | optional | Free-form labels. Merged with existing metadata. |

**Response (200 OK)**:

```json
{
  "key": "openai_api_key",
  "version": 4,
  "created_at": "2026-06-15T10:00:00Z",
  "updated_at": "2026-06-15T10:00:00Z",
  "last_rotated_at": "2026-06-15T10:00:00Z",
  "metadata": {"vendor": "openai"}
}
```

Successful PUT increments `version`.

## 4. `POST /v1/secrets/{key}/reveal`

Return the plaintext. The adapter MUST emit an audit log entry every
time this is called, with the requesting `X-BackAI-Request-Id`,
`X-BackAI-Tenant-Id` (if set), and the secret key.

**Request body** (optional version pin):

```json
{"version": 3}
```

When `version` is supplied and `capabilities.supports_versioning` is
`true`, returns the plaintext for that specific version. Without it
(or when versioning isn't supported), returns the current version.
Empty body `{}` is the default ("current version").

**Response (200 OK)**:

```json
{
  "key": "openai_api_key",
  "version": 4,
  "value": "sk-abc123...",
  "revealed_at": "2026-06-15T10:00:00Z"
}
```

**404** if missing. **403** with `code: "reveal_forbidden"` if the
adapter has a policy against revealing this particular key (e.g., it's
write-only). The runtime then surfaces "this secret cannot be revealed"
in the UI.

## 5. `POST /v1/secrets/{key}/rotate`

Generate or accept a new value and store it. Two modes:

**Mode A — adapter generates** (default):

```json
{
  "length": 48,
  "alphabet": "alphanumeric"
}
```

Adapter generates and returns the new value. Useful for symmetric
secrets the adapter owns.

**Mode B — client provides**:

```json
{
  "value": "sk-new-value..."
}
```

Mode B is used when the upstream provider (e.g., OpenAI) generated a
new key and the operator is just storing it.

**Response (200 OK)**:

```json
{
  "key": "openai_api_key",
  "version": 5,
  "last_rotated_at": "2026-06-15T10:00:00Z"
}
```

The new plaintext is NOT returned (operator uses `/reveal` to get it).
Adapters that support immediate retrieval after rotation MAY return
the value, but the default is omit-for-safety.

## 6. `DELETE /v1/secrets/{key}`

Delete a secret. Idempotent — returns `204` whether or not the key
existed.

## 7. `GET /v1/capabilities`

```json
{
  "name": "envelope-local",
  "version": "1.0.0",
  "slot": "secrets",
  "protocol_version": "v1",
  "vendor": "BackAI",
  "capabilities": {
    "supports_versioning": true,
    "supports_rotation": true,
    "supports_rotation_generate": true,
    "supports_reveal": true,
    "supports_metadata": true,
    "kms_backend": "envelope-local",
    "max_value_bytes": 65536,
    "version_retention_count": 10,
    "audit_log_revealed": true
  }
}
```

| Key | Type | Meaning |
|---|---|---|
| `supports_versioning` | bool | Whether the adapter keeps version history. |
| `supports_rotation` | bool | Whether `/rotate` works at all. |
| `supports_rotation_generate` | bool | Whether mode A (adapter-generates) is supported. |
| `supports_reveal` | bool | Whether `/reveal` is implemented. Some backends are write-only by design. |
| `supports_metadata` | bool | Whether `metadata` is persisted. |
| `kms_backend` | string | Free-form label (`envelope-local`, `aws-kms`, `vault`, `doppler`). For dashboard display. |
| `max_value_bytes` | int | Cap on stored plaintext size. |
| `version_retention_count` | int | How many old versions are retained. Zero means no history. |
| `audit_log_revealed` | bool | Adapter promises an audit trail on every `/reveal`. The runtime requires this `true` to expose reveal in the dashboard. |

## 8. Error codes

| Code | HTTP | Meaning |
|---|---|---|
| `secret_not_found` | 404 | Unknown key. |
| `reveal_forbidden` | 403 | Policy bars reveal for this key. |
| `rotation_unsupported` | 422 | Adapter doesn't support rotation. |
| `value_too_large` | 413 | Exceeds `max_value_bytes`. |
| `invalid_key` | 400 | Key name contains forbidden characters. |
| `kms_unavailable` | 503 | KMS backend unreachable. |
| `adapter_unavailable` | 503 | Adapter alive but upstream down. |
| `unauthorized` | 401 | Bearer token rejected. |
| `internal_error` | 500 | Catch-all. |

## 9. Behavior notes

- **No plaintext in logs.** Adapters MUST NOT log secret values. Audit
  entries log key + operation + caller, never value.
- **Encryption at rest.** Adapters that persist to disk MUST encrypt
  values. The envelope key MAY come from an external KMS (declared in
  `kms_backend`).
- **Atomic writes.** Concurrent `PUT` calls to the same key MUST
  produce a totally-ordered sequence of versions; no torn writes.
- **Version retention.** Old versions can be GC'd according to
  `version_retention_count`. Setting `version_retention_count: 0`
  disables history entirely.

## 10. Mapping back to the Go interface

The current `secrets.Vault` is a struct, not an interface. Part of v1
adapter work: extract a `secrets.Store` interface so the same remote
shim can satisfy it. Methods:

| Go method | HTTP call |
|---|---|
| `Get(ctx, key)` (metadata) | `GET /v1/secrets/{key}` |
| `Reveal(ctx, key)` | `POST /v1/secrets/{key}/reveal` |
| `Put(ctx, key, value, metadata)` | `PUT /v1/secrets/{key}` |
| `Delete(ctx, key)` | `DELETE /v1/secrets/{key}` |
| `Rotate(ctx, key, opts)` | `POST /v1/secrets/{key}/rotate` |
| `List(ctx, prefix, token, limit)` | `GET /v1/secrets?prefix=...` |
| `Capabilities()` | cached result of `GET /v1/capabilities` |

## 11. Conformance checklist

- [ ] `PUT /v1/secrets/k1` with `{"value":"x"}` returns `200` + metadata
- [ ] `GET /v1/secrets/k1` returns metadata without value
- [ ] `POST /v1/secrets/k1/reveal` returns the value, increments adapter audit log
- [ ] `POST /v1/secrets/k1/rotate` (Mode A) generates a new value; `/reveal` returns it
- [ ] `DELETE /v1/secrets/k1` returns `204`; `GET` after returns `404`
- [ ] `PUT` of a value over `max_value_bytes` returns `413 + value_too_large`
- [ ] If `supports_reveal=false`, `/reveal` returns `403 + reveal_forbidden`
- [ ] Idempotent `PUT` with same `X-BackAI-Idempotency-Key` and same body returns identical metadata
- [ ] Bearer auth enforced

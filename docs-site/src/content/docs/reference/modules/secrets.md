---
title: Module — Secrets
description: Envelope-encrypted key/value vault. Plaintext only on reveal (audited).
sidebar:
  order: 9
---

Envelope-encrypted key/value vault. Values are encrypted with a per-secret data key, which is itself wrapped under the KMS key from env. Plaintext only leaves the runtime through the explicit reveal endpoint (audited).

## What it does

`secrets.Vault` is a Postgres-backed store. `Put` encrypts via AES-GCM under a data key wrapped by `AF_STACK_KMS_KEY`. `Reveal` decrypts and records the access. `List` / `Get` return metadata only — `value` is replaced with `secrets.RedactedMarker = "[REDACTED]"` in any other context (logs, spans, error messages).

When the vault is not wired (no DB or KMS key missing), `/api/v1/secrets/*` returns `503`.

## Configuration

No dedicated module flag. The vault constructs whenever a DB pool is present and `AF_STACK_KMS_KEY` is set.

```bash
AF_STACK_KMS_KEY=<openssl rand -hex 32>     # required
```

Vault uses the shared `AF_STACK_DATABASE_URL` pool.

## REST endpoints

Registered in `services/runtime/internal/server/secrets.go`:

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/secrets` | List secrets (metadata only). |
| `GET` | `/api/v1/secrets/{key}` | Get secret metadata (no value). |
| `PUT` | `/api/v1/secrets/{key}` | Create or replace a secret. |
| `DELETE` | `/api/v1/secrets/{key}` | Delete a secret. |
| `POST` | `/api/v1/secrets/{key}/reveal` | Reveal the plaintext value (audited). |
| `POST` | `/api/v1/secrets/{key}/rotate` | Rotate the stored value. |

## Database tables

Owned by migration `00003_secrets.sql`:

- `suite_secrets` — id, tenant, key, encrypted value, wrapped data key, metadata, created_at, rotated_at.

## Env vars

| Env | Purpose |
|---|---|
| `AF_STACK_KMS_KEY` | KMS key used to wrap per-secret data keys. Required. Read in `secrets/crypto.go`. |

## Code map

- `vault.go` — `Vault` + CRUD + reveal/rotate.
- `crypto.go` — envelope encryption, KMS key resolution.
- `errors.go` — sentinel errors.
- `server/secrets.go` — REST routes.

## Related

- Tenant-scoped via [Multi-tenancy](./multi-tenancy/).
- [MCP](./mcp/) consumes secrets via `mcp/secrets.go`.

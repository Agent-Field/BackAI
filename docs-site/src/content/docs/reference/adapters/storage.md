---
title: Storage adapters
description: minio and s3 — both speak the S3 protocol via minio-go.
sidebar:
  order: 2
---

Two adapters live under `services/runtime/internal/storage/adapters/`. Both wrap the `minio-go` client; the only difference is TLS defaults and the adapter label used in logs and metrics.

## Selection

```yaml
storage:
  adapter: minio              # minio | s3
  endpoint: http://minio:9000
  bucket: af-stack
  access_key: minio
  secret_key: minio-secret
  region: us-east-1
```

Env (overrides YAML):

```bash
AF_STACK_S3_ADAPTER=s3
AF_STACK_S3_ENDPOINT=s3.amazonaws.com
AF_STACK_S3_BUCKET=my-prod-bucket
AF_STACK_S3_ACCESS_KEY=AKIA...
AF_STACK_S3_SECRET_KEY=...
AF_STACK_S3_REGION=us-east-1
```

When `Endpoint` is empty, no adapter is wired and `/api/v1/storage/*` returns `503`.

## Capabilities matrix

Both adapters report the same `Capabilities` shape (`MaxObjectSizeBytes`, `SupportsMultipart`, `PresignTTLMaxSeconds`).

| Adapter | TLS default | Max object size | Multipart | Max presign TTL |
|---|---|---|---|---|
| `minio` | off (scheme picks) | 5 TiB (S3 limit) | yes | 7 days |
| `s3`    | on  | 5 TiB (S3 limit) | yes | 7 days |

`maxPresignSeconds = 7 * 24 * 60 * 60` is enforced in the adapter (`adapter.go`).

## When to pick which

### `minio` — local dev, self-hosted

The default. Used by `docker-compose.yml` (the bundled MinIO server). Scheme-based TLS detection: an `http://` endpoint disables TLS, `https://` enables it.

### `s3` — production on AWS

Forces TLS on. Otherwise identical to MinIO (same `minio-go` client underneath). Use when pointing at `s3.amazonaws.com` or a managed S3-compatible service (R2, Wasabi, etc.) over HTTPS.

## Endpoint parsing

`normaliseEndpoint(raw, override)` accepts either `host:port` or a full URL. When given a URL, the scheme picks the `Secure` flag unless explicitly overridden by the adapter type.

## Code map

- `adapters/minio/adapter.go` — MinIO adapter (TLS off default).
- `adapters/s3/adapter.go` — S3 adapter (TLS on default).

## Related

- [Storage module](../../modules/storage/) — what the adapters plug into.

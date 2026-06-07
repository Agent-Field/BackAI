---
title: Module — Storage
description: Object storage facade. MinIO or S3 adapter, presigned URLs, tenant prefix.
sidebar:
  order: 16
---

Object-storage facade. Small interface: upload, download, signed URLs, list, delete, plus `EnsureBucket` (bootstrap) and `Capabilities` (adapter limits). Two adapters — MinIO (local) and S3 (AWS); both speak the S3 protocol via `minio-go`.

## What it does

Object keys are forward-slash separated (e.g. `tenants/default/uploads/foo.png`). Adapters do not enforce key shape; that's the caller's responsibility. When [Multi-tenancy](./multi-tenancy/) is on, the upload handler silently prepends `tenants/<id>/` via `Storage.TenantPrefix`.

`Capabilities()` reports `MaxObjectSizeBytes`, `SupportsMultipart`, `PresignTTLMaxSeconds` — the upload handler uses these to reject oversize input before streaming.

When `Endpoint` is empty, the storage adapter is not wired and `/api/v1/storage/*` returns `503`.

## Configuration

```yaml
storage:
  adapter: minio              # minio (TLS off) | s3 (TLS on)
  endpoint: http://minio:9000
  bucket: af-stack
  access_key: minio
  secret_key: minio-secret
  region: us-east-1
  tenant_prefix: ""           # set per-request by tenant-scoped server.Deps
```

Env (mirrors `.env.example`):

```bash
AF_STACK_S3_ADAPTER=minio
AF_STACK_S3_ENDPOINT=http://minio:9000
AF_STACK_S3_BUCKET=af-stack
AF_STACK_S3_ACCESS_KEY=minio
AF_STACK_S3_SECRET_KEY=minio-secret
AF_STACK_S3_REGION=us-east-1
AF_STACK_S3_TENANT_PREFIX=tenants/<id>
```

See [Storage adapters](../../adapters/storage/) for the per-adapter matrix.

## REST endpoints

Registered in `services/runtime/internal/server/storage.go`:

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/storage/upload` | Upload an object (multipart). |
| `GET` | `/api/v1/storage/signed-url` | Generate a presigned GET URL. Query: `key`, `ttl` (default 3600, max 604800). |
| `GET` | `/api/v1/storage` | List objects under a prefix. Query: `prefix`, `next_token`, `limit`. |
| `GET` | `/api/v1/storage/{key...}` | Download an object by key. |
| `DELETE` | `/api/v1/storage/{key...}` | Delete an object by key. |

## Database tables

None. Object metadata lives in the storage backend.

## Env vars

| Env | Purpose |
|---|---|
| `AF_STACK_S3_ADAPTER` | Adapter selection (minio / s3). |
| `AF_STACK_S3_ENDPOINT` | Backend HTTP endpoint. |
| `AF_STACK_S3_BUCKET` | Default bucket. |
| `AF_STACK_S3_ACCESS_KEY` | Access key. |
| `AF_STACK_S3_SECRET_KEY` | Secret key. |
| `AF_STACK_S3_REGION` | Region. |
| `AF_STACK_S3_TENANT_PREFIX` | Static tenant prefix (usually set per-request). |

## Code map

- `interface.go` — `Storage` interface, `Object`, `UploadOpts`, `ListResult`, `Capabilities`, `ErrNotFound`.
- `adapters/minio/` — local-dev MinIO adapter.
- `adapters/s3/` — AWS S3 adapter.
- `server/storage.go` — REST routes + `HookStoragePreUpload` firing.

## Related

- Fires [`storage.pre_upload`](../../hooks/#storagepreupload).
- [Storage adapters](../../adapters/storage/) — per-adapter knobs.
- Stores artefacts from [Sandbox](./sandbox/).

# Storage Adapters

Storage backs object uploads, artifacts, and signed URLs.

## Active selector

Set:

```bash
AF_STACK_S3_ADAPTER=minio
```

Supported today:

| Adapter | Use |
|---|---|
| `minio` | Local development and self-hosted S3-compatible storage |
| `s3` | AWS S3 or compatible managed object storage (also covers R2 / GCS / Azure Blob via the S3 API) |
| `remote` | An out-of-process storage adapter speaking the [remote protocol](PROTOCOL.md) |

## Common env

```bash
AF_STACK_S3_ENDPOINT=
AF_STACK_S3_BUCKET=
AF_STACK_S3_ACCESS_KEY=
AF_STACK_S3_SECRET_KEY=
AF_STACK_S3_REGION=
AF_STACK_S3_TENANT_PREFIX=
```

## Remote adapter

Set `AF_STACK_S3_ADAPTER=remote` to front storage with your own sidecar:

```bash
AF_STACK_S3_ADAPTER=remote
AF_STACK_STORAGE_REMOTE_URL=https://storage-adapter.example.com
AF_STACK_STORAGE_REMOTE_TOKEN=<bearer-token>
```

These credentials can also be set from the dashboard → **Platform →
Integrations** instead of env (stored in the secrets vault). Env wins when
both are present; UI-set credentials take effect on the **next runtime
restart** (not hot-reloaded).

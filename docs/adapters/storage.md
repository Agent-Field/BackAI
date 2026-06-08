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
| `s3` | AWS S3 or compatible managed object storage |

Planned:

| Adapter | Notes |
|---|---|
| `r2` | Cloudflare R2 |
| `gcs` | Google Cloud Storage |
| `azure-blob` | Azure Blob Storage |

## Common env

```bash
AF_STACK_S3_ENDPOINT=
AF_STACK_S3_BUCKET=
AF_STACK_S3_ACCESS_KEY=
AF_STACK_S3_SECRET_KEY=
AF_STACK_S3_REGION=
AF_STACK_S3_TENANT_PREFIX=
```

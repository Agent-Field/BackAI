# Object Storage Adapter — Protocol v1

> Inherits from [`PROTOCOL.md`](../PROTOCOL.md).
>
> **Slot:** `storage` · **Base path:** `/v1` · **Go interface:**
> `services/runtime/internal/storage/Storage`

## Purpose

Object storage adapters handle file upload/download/list/delete for
the platform. Built-in adapters use the S3 protocol (MinIO in dev, AWS
S3 / R2 / GCS / Azure Blob in prod). Remote adapters can wrap anything
that exposes object semantics.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `PUT` | `/v1/objects/{key}` | Upload an object (streaming body) |
| `GET` | `/v1/objects/{key}` | Download object (streaming response) |
| `HEAD` | `/v1/objects/{key}` | Metadata only |
| `DELETE` | `/v1/objects/{key}` | Delete object (idempotent) |
| `POST` | `/v1/objects/{key}/signed-url` | Mint a time-limited URL |
| `GET` | `/v1/objects` | List objects under prefix (paged) |
| `POST` | `/v1/bucket/ensure` | Create the bucket if missing |
| `GET` | `/v1/capabilities` | Capability declaration |
| `GET` | `/healthz` | Liveness |
| `GET` | `/v1/info` | Optional metadata |

Object keys are URL-encoded slash-separated paths
(`tenants/acme/uploads/foo.png`). Keys MUST tolerate any RFC 3986
unreserved character; adapters MUST URL-decode the path segment before
storing.

## 1. `PUT /v1/objects/{key}`

Upload an object. Body is the raw object bytes.

**Required headers**:

```
Content-Type: <object's MIME>
Content-Length: <bytes>
X-BackAI-Content-Type: <object's MIME>   (mirrors Content-Type)
```

The runtime sets both `Content-Type` and `X-BackAI-Content-Type` because
the HTTP `Content-Type` is consumed by middleware in some setups.

**Optional headers**:

```
X-BackAI-Metadata-<Name>: <value>
```

Any number of metadata headers; adapter persists them as object metadata
where the backend supports it.

**Response (200 OK)**:

```json
{
  "key": "tenants/acme/uploads/foo.png",
  "size": 1234,
  "content_type": "image/png",
  "etag": "d41d8cd98f00b204e9800998ecf8427e",
  "last_modified": "2026-06-15T10:00:00Z"
}
```

## 2. `GET /v1/objects/{key}`

Download object body. Response is streaming bytes. Headers carry
metadata:

```
Content-Type: <stored MIME>
Content-Length: <bytes>
ETag: <etag>
Last-Modified: <RFC 7231 date>
X-BackAI-Content-Type: <stored MIME>
```

**404** if the key doesn't exist. RFC 7807 envelope with `code:
"object_not_found"`.

The runtime streams the body to the client; adapters SHOULD support
`Range:` requests (HTTP `206 Partial Content`) but are not required to.

## 3. `HEAD /v1/objects/{key}`

Same headers as `GET` but no body. Used by callers checking existence
cheaply.

## 4. `DELETE /v1/objects/{key}`

Idempotent. `204 No Content` whether or not the key existed.

## 5. `POST /v1/objects/{key}/signed-url`

**Request**:

```json
{
  "ttl_seconds": 900,
  "method": "GET"
}
```

`method` is `GET` (default) or `PUT`. `ttl_seconds` is capped by
`capabilities.presign_ttl_max_seconds`.

**Response (200 OK)**:

```json
{
  "url": "https://...",
  "expires_at": "2026-06-15T10:15:00Z",
  "method": "GET"
}
```

For adapters without true presigned URLs (e.g., wrapping a filesystem
backend), they MAY return a URL pointing back at their own
`/v1/objects/{key}` endpoint with a short-lived signed token in the
query string.

## 6. `GET /v1/objects?prefix=X&token=Y&limit=Z`

**Query params**:

| Param | Type | Default | Meaning |
|---|---|---|---|
| `prefix` | string | `""` | Filter by key prefix. |
| `token` | string | `""` | Continuation token from a previous response. |
| `limit` | int | `1000` | Max items. Capped server-side. |

**Response (200 OK)**:

```json
{
  "objects": [
    {
      "key": "tenants/acme/uploads/foo.png",
      "size": 1234,
      "content_type": "image/png",
      "etag": "d41d8cd98f...",
      "last_modified": "2026-06-15T10:00:00Z"
    }
  ],
  "prefix": "tenants/acme/uploads/",
  "next_token": ""
}
```

Empty `next_token` means the listing is complete.

## 7. `POST /v1/bucket/ensure`

Idempotent bucket creation. Called at runtime startup.

**Request body**: empty `{}`.

**Response (200 OK)**:

```json
{"ensured": true}
```

`ensured: true` whether or not the bucket already existed. Errors
return RFC 7807.

## 8. `GET /v1/capabilities`

```json
{
  "name": "minio",
  "version": "1.0.0",
  "slot": "storage",
  "protocol_version": "v1",
  "vendor": "BackAI",
  "homepage": "https://min.io",
  "capabilities": {
    "max_object_size_bytes": 5368709120,
    "single_put_max_bytes": 5368709120,
    "presign_ttl_max_seconds": 604800,
    "supports_range_requests": true,
    "supports_metadata_headers": true,
    "supports_signed_uploads": true,
    "max_keys_per_list": 1000,
    "bucket_required": true
  }
}
```

| Key | Type | Meaning |
|---|---|---|
| `max_object_size_bytes` | int64 | Hard upper bound for any single object the adapter will store. |
| `single_put_max_bytes` | int64 | Largest body the adapter accepts via a single `PUT /v1/objects/{key}`. For larger objects, clients MUST request a signed PUT URL and upload directly. |
| `presign_ttl_max_seconds` | int | Largest TTL the adapter signs. |
| `supports_range_requests` | bool | Whether `GET` honors `Range:`. |
| `supports_metadata_headers` | bool | Whether `X-BackAI-Metadata-*` is persisted. |
| `supports_signed_uploads` | bool | Whether `/signed-url?method=PUT` works. |
| `max_keys_per_list` | int | Adapter's cap for the `limit` query param. |
| `bucket_required` | bool | Whether `/v1/bucket/ensure` must be called before objects. |

## 9. Error codes

| Code | HTTP | Meaning |
|---|---|---|
| `object_not_found` | 404 | Key doesn't exist (on `GET`/`HEAD`/`POST signed-url`). |
| `object_too_large` | 413 | Body exceeds `max_object_size_bytes`. |
| `invalid_key` | 400 | Key contains forbidden characters or is empty. |
| `bucket_not_found` | 404 | Backend bucket missing; runtime should retry after `POST /v1/bucket/ensure`. |
| `presign_unsupported` | 422 | Adapter cannot mint a signed URL with the requested method. |
| `quota_exceeded` | 429 | Storage tier full. |
| `adapter_unavailable` | 503 | Backend (S3, MinIO daemon, etc.) down. |
| `unauthorized` | 401 | Bearer token rejected. |
| `internal_error` | 500 | Catch-all. |

## 10. Behavior notes

- **Key normalization.** Adapters MUST NOT silently rewrite keys.
  Reject any key containing `..`, `//`, or NUL with `invalid_key`.
- **Large objects.** For uploads larger than
  `capabilities.single_put_max_bytes`, clients MUST request a signed
  PUT URL via `POST /v1/objects/{key}/signed-url` with
  `{"method":"PUT"}` and upload directly to the backend. The adapter
  does not need to support multi-part HTTP uploads through
  `PUT /v1/objects/{key}` itself — that endpoint is for objects that
  fit in a single body within `single_put_max_bytes`. Single-PUT caps
  for the major S3-compat backends are 5 GiB (S3, R2, MinIO); set
  yours accordingly.
- **Concurrent writes.** Two concurrent `PUT` calls to the same key
  MUST be last-write-wins (or the adapter's underlying backend
  semantics). No locking is required.
- **Streaming downloads.** Adapters MUST stream the body — do not load
  large objects into memory.

## 11. Mapping back to the Go interface

| Go method | HTTP call |
|---|---|
| `Upload(ctx, key, r, opts)` | `PUT /v1/objects/{key}` |
| `Download(ctx, key)` | `GET /v1/objects/{key}` |
| `SignedURL(ctx, key, ttl)` | `POST /v1/objects/{key}/signed-url` |
| `Delete(ctx, key)` | `DELETE /v1/objects/{key}` |
| `List(ctx, prefix, token, limit)` | `GET /v1/objects?prefix=...&token=...&limit=...` |
| `EnsureBucket(ctx)` | `POST /v1/bucket/ensure` |
| `Capabilities()` | cached result of `GET /v1/capabilities` |

## 12. Conformance checklist

- [ ] `PUT /v1/objects/k1` with a 100-byte body returns `200` and metadata
- [ ] `GET /v1/objects/k1` streams back the same bytes
- [ ] `HEAD /v1/objects/k1` returns the same headers as `GET` without body
- [ ] `DELETE /v1/objects/k1` returns `204`; repeat returns `204`
- [ ] `GET /v1/objects/k1` after delete returns `404` + RFC 7807 `object_not_found`
- [ ] `POST /v1/objects/k2/signed-url` returns a usable URL
- [ ] `GET /v1/objects?prefix=k` returns at least the keys created
- [ ] `POST /v1/bucket/ensure` returns `200` on a fresh bucket and on a repeat
- [ ] Idempotent `PUT` with same `X-BackAI-Idempotency-Key` returns identical response
- [ ] Bearer auth enforced on all endpoints

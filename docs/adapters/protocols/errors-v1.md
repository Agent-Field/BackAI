# Errors Adapter Protocol v1

Remote errors adapters implement the universal BackAI adapter envelope in
[`../PROTOCOL.md`](../PROTOCOL.md) plus the endpoints below.

## Capabilities

`GET /v1/capabilities` returns:

```json
{
  "name": "errors-echo",
  "slot": "errors",
  "protocol_version": "v1",
  "capabilities": {
    "supports_list": true,
    "supports_get": true,
    "supports_mute": true,
    "supports_resolve": true,
    "supports_ingest": false,
    "supports_alerting": false,
    "native_query_lang": "sentry-search",
    "retention_days": 14,
    "persistence": "remote",
    "max_groups_per_page": 100
  }
}
```

`persistence` is `volatile`, `durable`, or `remote`.

## List

`POST /v1/errors/list`

Request:

```json
{
  "status": "open",
  "service": "runtime",
  "tenant_id": "tenant-id",
  "since": "2026-06-16T00:00:00Z",
  "limit": 50,
  "cursor": "opaque"
}
```

Response:

```json
{
  "groups": [
    {
      "id": "group-id",
      "title": "provider rate limit",
      "service": "runtime",
      "status": "open",
      "count": 12,
      "user_count": 2,
      "first_seen": "2026-06-16T00:00:00Z",
      "last_seen": "2026-06-16T00:05:00Z",
      "fingerprint": "hash",
      "permalink": "https://errors.example/issues/group-id",
      "culprit": "llm.gateway",
      "sample_event": { "msg": "provider rate limit" }
    }
  ],
  "next_cursor": "opaque",
  "has_more": true
}
```

## Get

`GET /v1/errors/{group_id}` returns one group object.

Adapters return RFC 7807 with `code: "error_group_not_found"` for unknown ids.

## Update

`PATCH /v1/errors/{group_id}`

Request:

```json
{ "status": "muted" }
```

Allowed statuses are `open`, `muted`, and `resolved`. Adapters that cannot
perform a requested transition return `422` with `code: "unsupported_capability"`.

## Builtins

- `logfilter`: default, groups active logs adapter error/fatal rows and stores
  mute/resolve state in process memory with `persistence: "volatile"`.
- `glitchtip`: GlitchTip/Sentry-compatible read adapter using organization issue
  list/get/update endpoints.
- `remote`: this protocol.

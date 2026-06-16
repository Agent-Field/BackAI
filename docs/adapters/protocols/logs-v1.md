# Logs Adapter — Protocol v1

> Inherits from [`PROTOCOL.md`](../PROTOCOL.md). Read that first.
>
> **Slot:** `logs` · **Base path:** `/v1` · **Go interface:**
> `services/runtime/internal/observability/logs.Store`

## Purpose

A logs adapter queries and tails normalized operational log lines for the
operator dashboard. Built-in adapters: ring buffer and Loki. Remote
sidecars can front Elasticsearch, Quickwit, Datadog, or another backend.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/logs/query` | Query a page of log entries |
| `GET` | `/v1/logs/tail` | Stream matching entries over SSE |
| `GET` | `/v1/capabilities` | Declare logs capabilities |
| `GET` | `/healthz` | Liveness + readiness |
| `GET` | `/v1/info` | Optional operator metadata |

## `POST /v1/logs/query`

Request body:

```json
{
  "services": ["runtime"],
  "levels": ["warn", "error"],
  "tenant_id": "tenant_123",
  "request_id": "req_123",
  "trace_id": "abc123",
  "search": "timeout",
  "search_is_regex": false,
  "from": "2026-06-16T12:00:00Z",
  "to": "2026-06-16T13:00:00Z",
  "limit": 200,
  "cursor": ""
}
```

Response:

```json
{
  "entries": [
    {
      "ts": "2026-06-16T12:34:56.000Z",
      "level": "info",
      "service": "runtime",
      "msg": "request complete",
      "agent": "support.triage",
      "tenant_id": "tenant_123",
      "request_id": "req_123",
      "trace_id": "abc123",
      "fields": {"path": "/api/v1/llm/chat/completions"}
    }
  ],
  "next_cursor": "",
  "has_more": false
}
```

## `GET /v1/logs/tail`

Query parameters mirror the query request with singular/repeated names:
`service`, `level`, `tenant`, `request_id`, `trace_id`, `search`,
`search_is_regex`, `from`, `to`, `limit`, `cursor`.

Response is `Content-Type: text/event-stream`.

```
data: {"ts":"2026-06-16T12:34:56Z","level":"info","service":"runtime","msg":"ready"}

data: [DONE]

```

If `supports_tail=false`, adapters MUST return `422` with RFC 7807
`code: "unsupported_capability"`.

## Capabilities

`GET /v1/capabilities` returns the universal envelope with:

```json
{
  "supports_tail": true,
  "supports_full_text": true,
  "supports_regex_search": false,
  "supports_trace_id": true,
  "native_query_lang": "logql",
  "retention_days": 30,
  "max_entries_per_page": 5000
}
```

`retention_days=0` means unknown or volatile. The ring buffer reports
unknown retention because it is process-local memory.

## Error Codes

| Code | HTTP | Meaning |
|---|---|---|
| `invalid_filter` | 400 | Filter validation failed |
| `unsupported_capability` | 422 | Tail or another requested feature is not supported |
| `adapter_unavailable` | 503 | Backend is unavailable |
| `upstream_error` | 502 | Backend returned an error |
| `unauthorized` | 401 | Bearer token rejected |
| `internal_error` | 500 | Catch-all |

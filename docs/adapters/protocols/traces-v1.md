# BackAI Traces Adapter Protocol v1

The traces adapter provides trace search and trace detail lookup for the
operator dashboard. The runtime default is the builtin `empty` adapter; remote
adapters implement this HTTP protocol.

## Capability Envelope

`GET /v1/capabilities` returns the universal adapter envelope with:

```json
{
  "name": "traces-echo",
  "slot": "traces",
  "protocol_version": "v1",
  "capabilities": {
    "supports_traceql": true,
    "supports_tag_search": true,
    "native_query_lang": "traceql",
    "retention_hours": 0,
    "max_results_per_query": 1000
  }
}
```

## Search

`POST /v1/traces/search`

Request:

```json
{
  "service": "runtime",
  "operation": "POST /api/v1/agents",
  "tag": { "http.method": "POST" },
  "min_duration": "100ms",
  "max_duration": "5s",
  "status": "error",
  "from": "2026-06-16T18:00:00Z",
  "to": "2026-06-16T19:00:00Z",
  "limit": 100
}
```

Response:

```json
{
  "traces": [
    {
      "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
      "root_service": "runtime",
      "root_operation": "POST /api/v1/agents",
      "start_time": "2026-06-16T18:12:30Z",
      "duration_ms": 245,
      "span_count": 7,
      "status": "ok"
    }
  ],
  "has_more": false
}
```

## Get Trace

`GET /v1/traces/{trace_id}`

Response:

```json
{
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "spans": [
    {
      "span_id": "00f067aa0ba902b7",
      "parent_span_id": "",
      "service": "runtime",
      "operation": "POST /api/v1/agents",
      "start_time": "2026-06-16T18:12:30Z",
      "duration_ms": 245,
      "status": "ok",
      "attributes": { "http.method": "POST" },
      "events": []
    }
  ]
}
```

## Tempo Backend Notes

The builtin Tempo adapter uses Grafana Tempo's documented HTTP API:

- TraceQL search: `GET /api/search?q=<TraceQL>`
- Legacy tag search: `GET /api/search?tags=<single logfmt string>`
- Trace detail: `GET /api/traces/{traceID}`
- Version probe: `GET /status/version`

Tempo versions `>= 2.0` activate `supports_traceql=true`. Older or
unparseable versions stay on tag search. The adapter does not call
`/api/v2/search`.

Trace detail decoding accepts both `scopeSpans` and older
`instrumentationLibrarySpans` OTLP JSON shapes. Parent span id
`0000000000000000` is normalized to an empty string at decode.

## Errors

| Code | HTTP | Meaning |
|---|---:|---|
| `invalid_trace_id` | 400 | Trace id is missing or invalid |
| `trace_not_found` | 404 | Trace does not exist |
| `traces_no_backend` | 503 | No trace backend is configured |
| `adapter_unavailable` | 503 | Backend is unavailable |
| `upstream_error` | 502 | Backend returned an error |
| `unauthorized` | 401 | Bearer token rejected |
| `internal_error` | 500 | Catch-all |

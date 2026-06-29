# BackAI Metrics Adapter Protocol v1

Metrics adapters expose PromQL-compatible instant and range queries to the
runtime. The default built-in adapter is `none`; remote adapters implement this
HTTP contract when `AF_STACK_METRICS_ADAPTER=remote`.

## Capabilities

`GET /v1/capabilities` returns the universal adapter envelope with:

```json
{
  "supports_instant_query": true,
  "supports_range_query": true,
  "supports_container_metrics": false,
  "native_query_lang": "promql",
  "retention_hours": 0,
  "max_series_per_query": 1000
}
```

## Instant Query

`POST /v1/metrics/query`

Request:

```json
{
  "query": "up{}",
  "time": "2026-06-16T12:00:00Z"
}
```

`time` is optional and may be RFC3339 or Unix seconds. Response:

```json
{
  "samples": [
    {
      "metric": {"__name__": "up", "job": "runtime"},
      "value": 1,
      "ts": "2026-06-16T12:00:00Z"
    }
  ]
}
```

Adapters should return an empty `samples` array when a valid query matches no
series. Invalid PromQL should use the universal RFC 7807 error envelope.

## Range Query

`POST /v1/metrics/query_range`

Request:

```json
{
  "query": "up{}",
  "from": "2026-06-16T12:00:00Z",
  "to": "2026-06-16T13:00:00Z",
  "step": "1m"
}
```

`from` and `to` are required. `step` uses Go/Prometheus duration strings such
as `30s`, `1m`, or `5m`. Response:

```json
{
  "series": [
    {
      "metric": {"__name__": "up", "job": "runtime"},
      "values": [
        {"ts": "2026-06-16T12:00:00Z", "value": 1},
        {"ts": "2026-06-16T12:01:00Z", "value": 1}
      ]
    }
  ]
}
```

Scalar and string result shapes are not part of this protocol. Return a
protocol error rather than coercing them into vectors.

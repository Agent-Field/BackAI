# Sandbox Adapter — Protocol v1

> Inherits from [`PROTOCOL.md`](../PROTOCOL.md). Read that first.
>
> **Slot:** `sandbox` · **Base path:** `/v1` · **Go interface:**
> `services/runtime/internal/sandbox/Sandbox`

## Purpose

A sandbox adapter executes an arbitrary container image with a command,
streams logs, and returns the terminal result. Built-in adapters: docker,
gvisor, firecracker, e2b.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/runs` | Start a run synchronously, return terminal result |
| `POST` | `/v1/runs/stream` | Start a run, stream log lines via SSE, return result as the terminating event |
| `GET` | `/v1/runs/{id}` | Fetch the state of a previously-started run |
| `DELETE` | `/v1/runs/{id}` | Cancel a running run (idempotent) |
| `GET` | `/v1/pool` | Adapter pool statistics |
| `GET` | `/v1/capabilities` | Capabilities declaration (see §6) |
| `GET` | `/healthz` | Liveness + readiness |
| `GET` | `/v1/info` | Optional operator-facing metadata |

## 1. `POST /v1/runs`

Run the spec to completion and return the terminal result.

**Request body**:

```json
{
  "id": "01HZAB...",
  "tenant_id": "acme",
  "workspace_id": "",
  "image": "python:3.12-slim",
  "command": ["python", "-c", "print(2 + 2)"],
  "files": {
    "/work/script.py": "print('hello')"
  },
  "env": {
    "PYTHONUNBUFFERED": "1"
  },
  "timeout_s": 300,
  "cpu": 2,
  "memory_gb": 4,
  "network": "restricted",
  "allow_egress": ["pypi.org"]
}
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `id` | string | yes | Idempotency root + adapter's container name prefix. The runtime fills this; adapters MUST use it verbatim. |
| `tenant_id` | string | optional | For audit only. Empty for operator calls. |
| `workspace_id` | string | optional | Mirrors `RunSpec.WorkspaceID`. |
| `image` | string | yes | OCI image reference. |
| `command` | string[] | yes | argv. Adapters MUST NOT execute through a shell. |
| `files` | object | optional | path → contents. Adapters write each path under the container working directory before exec. Binary content must be base64-encoded by the caller. |
| `env` | object | optional | Environment variables for the process. |
| `timeout_s` | int | yes | Hard kill after this many seconds. Adapters MUST enforce this; runtime also enforces context cancellation. |
| `cpu` | int | yes | CPU cores. |
| `memory_gb` | int | yes | Memory cap in gigabytes. |
| `network` | enum | yes | `open` (no restrictions), `restricted` (only `allow_egress` hosts reachable), `isolated` (no network). |
| `allow_egress` | string[] | when `network=restricted` | Hostnames the run may reach. |

**Response (200 OK)**: terminal result.

```json
{
  "status": "done",
  "exit_code": 0,
  "duration_s": 1,
  "cpu_seconds": 0.42,
  "memory_peak_mb": 38,
  "network_bytes_in": 0,
  "network_bytes_out": 0,
  "stdout_url": "https://adapter.example.com/logs/01HZAB/stdout",
  "stderr_url": "https://adapter.example.com/logs/01HZAB/stderr",
  "artifacts_url": "",
  "started_at": "2026-06-15T10:00:00Z",
  "ended_at": "2026-06-15T10:00:01Z"
}
```

| Field | Type | Notes |
|---|---|---|
| `status` | enum | `done` (exit 0), `failed` (non-zero or daemon error), `timeout`, `killed`. Adapters MUST NOT return `queued` or `running` from this endpoint — it's synchronous and terminal. |
| `exit_code` | int | The process exit code. 0 for `done`, non-zero for `failed` due to process. Adapter-internal failures still set `failed` but may use a sentinel like `-1`. |
| `duration_s` | int | Wall time. |
| `cpu_seconds` | float | Accumulated CPU time. May be 0 if the adapter doesn't measure it. |
| `memory_peak_mb` | int | Peak RSS. May be 0 if the adapter doesn't measure it. |
| `network_bytes_in`/`out` | int64 | Network usage. May be 0. |
| `stdout_url`/`stderr_url`/`artifacts_url` | string | URLs the runtime can fetch later. Empty string if not persisted. Adapters MAY require the same `Authorization` header on these URLs as on the original request. |
| `started_at` / `ended_at` | ISO-8601 timestamps | Wall-clock. |

**Errors**: see §7 for the code list.

## 2. `POST /v1/runs/stream`

Identical body to `POST /v1/runs`. Response is `Content-Type:
text/event-stream`.

Events:

```
data: {"ts":"2026-06-15T10:00:00.123Z","stream":"stdout","text":"hello\n"}

data: {"ts":"2026-06-15T10:00:00.456Z","stream":"stderr","text":"warn: x\n"}

data: {"event":"terminated","status":"done","exit_code":0,"duration_s":1,"cpu_seconds":0.42,"memory_peak_mb":38,"network_bytes_in":0,"network_bytes_out":0,"stdout_url":"","stderr_url":"","artifacts_url":"","started_at":"2026-06-15T10:00:00Z","ended_at":"2026-06-15T10:00:01Z"}
```

Two event shapes:

| Shape | Fields | Meaning |
|---|---|---|
| Log line | `ts`, `stream` (`stdout`/`stderr`), `text` | One line of output. |
| Termination | `event: "terminated"`, plus all fields from `POST /v1/runs` response | Final event. Adapter closes the connection after sending this. |

If the client disconnects mid-stream the adapter MUST cancel the
underlying container and not leak the goroutine.

## 3. `GET /v1/runs/{id}`

Fetch state of a run started earlier. Used by the runtime for status
checks if the streaming connection was lost.

**Response (200 OK)**:

```json
{
  "id": "01HZAB...",
  "status": "done",
  "exit_code": 0,
  "duration_s": 1,
  "cpu_seconds": 0.42,
  "memory_peak_mb": 38,
  "network_bytes_in": 0,
  "network_bytes_out": 0,
  "stdout_url": "",
  "stderr_url": "",
  "artifacts_url": "",
  "started_at": "2026-06-15T10:00:00Z",
  "ended_at": "2026-06-15T10:00:01Z"
}
```

For non-terminal runs `status` is `queued` or `running` and the
timestamps may be partial.

**404** if the adapter has no record of `id`. Adapters MAY garbage-
collect terminal run state after 24h.

## 4. `DELETE /v1/runs/{id}`

Cancel a running run. Idempotent. Returns `204 No Content` on success
or after the run is already terminated. `404` if the id is unknown.

The adapter MUST kill the container and update its internal state. The
runtime will then call `GET /v1/runs/{id}` to confirm status.

## 5. `GET /v1/pool`

Adapter pool statistics. Polled by the dashboard for the operator-
facing pool view.

**Response (200 OK)**:

```json
{
  "adapter": "docker",
  "warm": 0,
  "active": 2,
  "queued": 0,
  "total_runs_today": 142,
  "cpu_seconds_today": 384.2,
  "cost_usd_today": 0.0
}
```

Adapters that don't track historical metrics may zero `total_runs_today`,
`cpu_seconds_today`, `cost_usd_today`. The dashboard then surfaces
those columns from the runtime's own ledger instead.

## 6. `GET /v1/capabilities`

```json
{
  "name": "docker",
  "version": "1.0.0",
  "slot": "sandbox",
  "protocol_version": "v1",
  "vendor": "BackAI",
  "homepage": "https://github.com/Agent-Field/backai",
  "capabilities": {
    "max_timeout_s": 86400,
    "supports_gpu": false,
    "supports_network": true,
    "supports_mounts": true,
    "supports_streaming": true,
    "cold_start_ms": 200,
    "image_pull_required": true,
    "max_cpu": 32,
    "max_memory_gb": 64,
    "network_modes": ["open", "restricted", "isolated"],
    "allow_egress_supported": true,
    "artifacts_upload": false
  }
}
```

`capabilities` fields:

| Key | Type | Meaning |
|---|---|---|
| `max_timeout_s` | int | Largest timeout this adapter accepts. Higher values get clamped or rejected. |
| `supports_gpu` | bool | Whether the adapter can attach GPU. |
| `supports_network` | bool | Whether the adapter can give the container network access at all. |
| `supports_mounts` | bool | Whether `files` is honored. |
| `supports_streaming` | bool | Whether `POST /v1/runs/stream` is implemented. If `false`, the runtime falls back to `POST /v1/runs` even when callers ask for streaming. |
| `cold_start_ms` | int | Median cold-start latency. For dashboard display. |
| `image_pull_required` | bool | If `false`, only the adapter's own catalogue of pre-baked images is allowed; runtime rejects arbitrary image fields before the call. |
| `max_cpu` | int | CPU upper bound. |
| `max_memory_gb` | int | Memory upper bound. |
| `network_modes` | string[] | Which `network` enum values are honored. If `restricted` is missing, the runtime falls back to `open` or `isolated`. |
| `allow_egress_supported` | bool | Whether the adapter enforces `allow_egress`. If `false` and `network=restricted`, the runtime warns operators that egress isn't actually filtered. |
| `artifacts_upload` | bool | Whether the adapter populates `artifacts_url`. |

## 7. Error codes

All errors use the RFC 7807 envelope from `PROTOCOL.md` §5. Sandbox-
specific `code` values:

| Code | HTTP | Meaning |
|---|---|---|
| `invalid_spec` | 400 | Spec failed validation (e.g., negative timeout, missing image). |
| `unsupported_capability` | 422 | The spec asked for a feature the adapter doesn't support (e.g., `network=restricted` when `allow_egress_supported=false`). |
| `image_not_found` | 404 | The OCI image couldn't be resolved. |
| `image_pull_failed` | 502 | Image found but pull failed. |
| `run_not_found` | 404 | `GET /v1/runs/{id}` or `DELETE /v1/runs/{id}` with unknown id. |
| `adapter_unavailable` | 503 | Adapter is alive but its backend (Docker daemon, e2b API, etc.) is down. |
| `quota_exceeded` | 429 | Per-adapter quota hit. The response SHOULD include `Retry-After`. |
| `unauthorized` | 401 | Bearer token rejected. |
| `internal_error` | 500 | Catch-all. Adapter MUST log the underlying error. |

## 8. Behavior notes

- **Container naming.** Adapters SHOULD name the container after the
  request `id` for traceability, but MUST NOT rely on the name to find
  the container — keep a mapping internally.
- **Egress filtering.** When `network=restricted` and `allow_egress` is
  set, adapters that can't actually enforce DNS-level allow-listing
  SHOULD warn in their `/v1/capabilities` (`allow_egress_supported:
  false`) rather than silently allowing all egress. The runtime then
  surfaces a warning in the dashboard.
- **Streaming buffering.** Adapters MUST flush log events as they're
  produced; buffering more than ~64 KB before emitting an event is a
  protocol violation.
- **Cleanup.** Adapters MUST tear down the container on every
  termination path: normal exit, timeout, cancel, client disconnect,
  adapter crash recovery.
- **Resource limits.** If the requested cpu/memory exceeds the
  adapter's max (`/v1/capabilities`), reject with `unsupported_capability`
  rather than silently clamping.

## 9. Mapping back to the Go interface

The remote shim in
`services/runtime/internal/sandbox/adapters/remote/` satisfies the
`Sandbox` interface like this:

| Go method | HTTP call |
|---|---|
| `Run(ctx, spec)` | `POST /v1/runs` |
| `Stream(ctx, spec)` | `POST /v1/runs/stream` |
| `Stop(ctx, runID)` | `DELETE /v1/runs/{id}` |
| `Capabilities()` | cached result of `GET /v1/capabilities` |

`Pool()` (for the dashboard) is fetched by the runtime through the
shim's `Pool(ctx)` method that calls `GET /v1/pool`.

## 10. Conformance checklist

A sandbox adapter passes the conformance suite when:

- [ ] `/healthz` returns `200` with the envelope from PROTOCOL.md §6.1
- [ ] `/v1/capabilities` returns this slot's envelope and at least the required fields
- [ ] `POST /v1/runs` with a 5-second `python:3.12-slim` echo runs in <30s and returns `done`/`exit_code=0`
- [ ] `POST /v1/runs/stream` for the same spec emits at least one log event and a terminating event
- [ ] `DELETE /v1/runs/{id}` on a 60-second sleep returns `204` and the run becomes `killed` on subsequent `GET /v1/runs/{id}`
- [ ] Idempotency: `POST /v1/runs` twice with the same `X-BackAI-Idempotency-Key` and identical body returns the same response both times
- [ ] Bearer token rejection: any of the above with a wrong token returns `401`
- [ ] RFC 7807 envelope on every non-2xx response
- [ ] Capability claim integrity: if `supports_gpu=true`, a GPU-requesting spec must work; if `false`, it must return `unsupported_capability`

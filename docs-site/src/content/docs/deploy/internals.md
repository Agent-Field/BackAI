---
title: Production internals
description: How the runtime behaves under Kubernetes — probes, graceful shutdown, drain semantics.
sidebar:
  order: 3
---

How the AF Stack runtime behaves under Kubernetes / Fly / any orchestrator that sends SIGTERM and probes `/health` + `/ready`. Phase 14.3.

## Probes

### `GET /health` — liveness

Returns `200` as long as the process is running. Used by Kubernetes `livenessProbe` — a non-200 here causes the pod to be killed and restarted.

- **Never** checks downstream dependencies (DB, AF, MCP, etc.). A transient DB blip should not restart the pod; that's a *readiness* concern, not *liveness*.
- Cheap: no DB calls, no allocations beyond the response body. Responds in microseconds.
- Stays `200` during drain — pods that are gracefully shutting down are still *alive*, they just aren't accepting new traffic.

Response body:

```json
{
  "status":     "alive",
  "uptime_s":   42,
  "started_at": "2026-06-07T12:00:00Z"
}
```

Kubernetes config:

```yaml
livenessProbe:
  httpGet: { path: /health, port: 8080 }
  initialDelaySeconds: 5
  periodSeconds: 10
  timeoutSeconds: 2
  failureThreshold: 3
```

### `GET /ready` — readiness

Returns `200` ONLY when:

1. The process has finished startup (`MarkReady()` has been called by `main.go` after migrations + worker startup).
2. The DB is reachable (when configured).
3. The process is NOT in drain mode.

Otherwise returns `503` with one of three statuses:

| status            | meaning                                           | Retry-After |
|-------------------|---------------------------------------------------|-------------|
| `booting`         | Process is still starting (migrations, workers).  | `5`         |
| `db_unavailable`  | DB ping failed.                                   | `5`         |
| `draining`        | SIGTERM received; in graceful shutdown.           | `<remaining drain window>` |

Response body during drain:

```json
{
  "status":          "draining",
  "since_s":         3,
  "timeout_s":       30,
  "active_requests": 4,
  "error": {
    "code":    "NOT_READY",
    "message": "server is draining; retry on another instance",
    "details": { "reason": "draining", "since_s": 3 }
  }
}
```

Top-level `status` and `since_s` are probe-friendly (kubectl describe pod surfaces them). The `error` envelope mirrors the standard Phase 6 contract so SDKs can branch on `error.code == "NOT_READY"`.

Kubernetes config:

```yaml
readinessProbe:
  httpGet: { path: /ready, port: 8080 }
  initialDelaySeconds: 2
  periodSeconds: 5
  timeoutSeconds: 2
  failureThreshold: 2
```

## Graceful Shutdown

When the runtime receives `SIGTERM` (Kubernetes pod termination) or `SIGINT` (`Ctrl-C` in dev), it runs the following sequence:

```
1. Drain mode entered
     - drain.Start() flips the atomic flag.
     - /ready immediately returns 503 {"status":"draining"}.
     - New requests get 503 + Connection: close from the drain middleware.
     - In-flight requests run to completion (NOT cancelled).

2. Wait for in-flight requests to finish
     - Bounded by AF_STACK_SHUTDOWN_TIMEOUT (default 30s).
     - On timeout: log warning with active request count, proceed to step 3.

3. httpServer.Shutdown(ctx)
     - Stop accepting new connections.
     - Close idle keep-alive connections.
     - Wait up to the same timeout for active conns to finish writing.

4. Stop background workers
     - workersCtx is cancelled — every worker observes ctx.Done() in its
       select loop and returns:
         - notifications worker (2s tick)
         - webhooks outbound worker (poll interval)
         - crons scheduler (1m tick)
         - sandbox pool refresh
         - mcp pool refresh
         - llmcache eviction (5m tick)

5. Close MCP pool
     - mcpPool.Shutdown() — close every adapter, kill stdio child processes,
       tear down SSE connections.

6. Deferred close handlers run (in reverse order)
     - jobs manager Stop (gives River workers up to 10s to finish their jobs)
     - DB pool Close
     - OTel telemetry Shutdown (flush pending spans)

7. Process exits 0
```

### Drain Timeout Knob

`AF_STACK_SHUTDOWN_TIMEOUT` (also `cfg.server.shutdown_timeout` in YAML, default `30s`) controls the budget for steps 1-3 above. The same value is used for the HTTP server shutdown timeout — so a 30s drain timeout means up to 60s total (30s drain + 30s http shutdown) in the worst case.

For Kubernetes, set `terminationGracePeriodSeconds` *larger* than `AF_STACK_SHUTDOWN_TIMEOUT * 2` so the orchestrator doesn't SIGKILL mid-drain:

```yaml
spec:
  terminationGracePeriodSeconds: 90   # 30s drain + 30s http shutdown + headroom
  containers:
  - name: af-stack
    env:
    - name: AF_STACK_SHUTDOWN_TIMEOUT
      value: "30s"
```

### In-flight LLM Streaming Requests

LLM streaming responses (Server-Sent Events from `/api/v1/llm/chat/completions` with `stream=true`) get up to the drain timeout to finish. If the stream is still running when the timeout fires:

- The drain controller returns `context.DeadlineExceeded` with the active count logged.
- `httpServer.Shutdown(ctx)` cuts off the connection at its own ctx timeout — the streaming response gets a partial write + closed TCP connection.
- Clients should treat partial SSE streams as retryable (the upstream provider will return whatever it can; clients reconnect to a fresh pod).

If you run very long-running LLM completions (e.g. thinking models with multi-minute outputs), raise `AF_STACK_SHUTDOWN_TIMEOUT` and `terminationGracePeriodSeconds` accordingly. A 5-minute drain is reasonable for thinking-heavy workloads; anything beyond that suggests you want the request pattern to be async (POST to a job, poll for status) rather than long synchronous streams.

### Signals and the Dockerfile

`services/runtime/Dockerfile` uses `ENTRYPOINT ["..."]` (exec form) so `SIGTERM` reaches the `af-stack` binary directly. The shell form (`ENTRYPOINT /usr/local/bin/af-stack`) would interpose `/bin/sh -c`, which swallows signals — Kubernetes would then escalate to `SIGKILL` after `terminationGracePeriodSeconds` and graceful drain would never happen.

If you maintain a fork of the Dockerfile, **keep the exec form**.

### Why Liveness and Readiness Diverge During Drain

This is the critical insight: during drain, `/health` stays `200` and `/ready` returns `503`. Kubernetes uses each probe for a different purpose:

- `livenessProbe` 503 → "this pod is broken, kill and restart it" (would defeat graceful shutdown)
- `readinessProbe` 503 → "this pod is alive but not accepting traffic, route around it" (exactly what we want)

If both probes hit the same handler, Kubernetes would either restart pods that are intentionally draining (waste + dropped requests) or keep routing traffic to dead pods (errors). Splitting them is mandatory for production.

## Drain Middleware

The drain middleware (`services/runtime/internal/server/drain_middleware.go`) sits at the outermost layer of the handler chain — before CORS, logging, tracing, tenant resolution, and rate limiting. This ordering guarantees:

1. A draining server sheds load with minimal work (no per-request allocations beyond the 503 body).
2. The active-request counter is incremented BEFORE any other middleware runs, so drain.Start() sees an accurate count.

Excluded paths (do not increment the counter, do not get rejected during drain):

- `/health` — liveness must stay reachable for K8s.
- `/ready` — readiness must return 503 draining during drain (handled by handler logic, not middleware).
- `/metrics` — Prometheus scrapes on a 15s tick; a stuck scrape would extend the drain window for no benefit.
- `/openapi.json` — dashboard polls this on page-load; same reasoning.

## Verification

```bash
# Build
go build ./services/runtime/...

# Shutdown-specific tests
go test ./services/runtime/internal/server/... -count=1 -run Shutdown

# Full server suite
go test ./services/runtime/internal/server/... -count=1

# Full runtime suite
go test ./services/runtime/... -count=1
```

Manual smoke test (local Docker):

```bash
# Start the runtime
af-stack &
PID=$!

# Verify liveness + readiness
curl -s localhost:8080/health   # {"status":"alive","uptime_s":...}
curl -s localhost:8080/ready    # {"status":"ready","uptime_s":...}

# Send SIGTERM, observe drain
kill -TERM $PID &
sleep 0.5
curl -s localhost:8080/ready    # {"status":"draining","since_s":0,...}
curl -s localhost:8080/health   # {"status":"alive",...} (still 200)
curl -s -X POST localhost:8080/api/v1/agents/supportdesk.echo -d '{}'
# {"error":{"code":"DRAINING","message":"server is shutting down..."}}

# Wait for process to exit
wait $PID
```

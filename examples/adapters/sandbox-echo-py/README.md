# BackAI Sandbox v1 — Echo Reference Implementation

A minimal, non-functional sandbox adapter that echoes back sandbox specs instead of running containers. Demonstrates the BackAI remote adapter protocol shape end-to-end. Useful for testing remote adapter wiring in the BackAI runtime.

## What This Does

Instead of actually executing containers, the adapter:
- `POST /v1/runs` → echoes the command as a fake stdout line, returns terminal result with `status=done, exit_code=0`
- `POST /v1/runs/stream` → streams two SSE events (log line + termination)
- `GET /v1/runs/{id}` → returns a terminal result for any id
- `DELETE /v1/runs/{id}` → idempotent 204
- `GET /v1/pool` → returns zero utilization stats
- `GET /v1/capabilities` → declares support for all sandbox features (except GPU, egress filtering)
- `GET /healthz` → liveness check
- `GET /v1/info` → operator metadata

## Running Locally

### 1. Install dependencies

```bash
cd examples/adapters/sandbox-echo-py
pip install -r requirements.txt
```

### 2. Start the adapter

```bash
uvicorn main:app --port 8090
```

Server listens on `http://localhost:8090`.

### 3. Optional: Set a bearer token

```bash
BACKAI_ADAPTER_TOKEN=my-secret-token uvicorn main:app --port 8090
```

If `BACKAI_ADAPTER_TOKEN` is set, all requests must include `Authorization: Bearer my-secret-token`.

## Plugging Into BackAI

When running the BackAI runtime, set these env vars to use the echo adapter:

```bash
AF_STACK_SANDBOX_ADAPTER=remote
AF_STACK_SANDBOX_ADAPTER_URL=http://localhost:8090
AF_STACK_SANDBOX_ADAPTER_TOKEN=my-secret-token  # optional; omit if adapter has no token
```

The runtime will call `GET /v1/capabilities` and `GET /healthz` at startup to verify the adapter is reachable.

## Docker

Build:

```bash
docker build -t backai-sandbox-echo-py .
```

Run:

```bash
docker run -p 8090:8090 backai-sandbox-echo-py
# Or with auth token:
docker run -e BACKAI_ADAPTER_TOKEN=my-token -p 8090:8090 backai-sandbox-echo-py
```

## Protocol Compliance

This adapter implements the **Sandbox v1** protocol from:
- `/docs/adapters/protocols/sandbox-v1.md` (sandbox-specific)
- `/docs/adapters/PROTOCOL.md` (universal contract)

Key features:
- ✅ RFC 7807 error envelope on non-2xx
- ✅ Bearer token auth (if `BACKAI_ADAPTER_TOKEN` set)
- ✅ Idempotency caching by `X-BackAI-Idempotency-Key` (10-minute TTL)
- ✅ Server-Sent Events (SSE) streaming
- ✅ Header support: `X-BackAI-Request-Id`, `X-BackAI-Idempotency-Key`, `X-BackAI-Tenant-Id`
- ✅ Request body validation (Pydantic)

## Testing

Run the smoke test suite:

```bash
bash test_protocol.sh
```

This will:
1. Start the adapter in the background
2. Run curl-based tests for each endpoint
3. Print PASS/FAIL for each
4. Clean up the background process

Expected output: All tests PASS.

## Architecture

- **Framework**: FastAPI (async Python web framework)
- **Server**: Uvicorn
- **Models**: Pydantic for request/response validation
- **State**: In-memory dict (non-persistent; suitable for testing only)

Total code: ~280 lines (main.py), idiomatic FastAPI.

## Further Reading

- [BackAI Adapter Protocol — Universal Contract](../../docs/adapters/PROTOCOL.md)
- [Sandbox Adapter — Protocol v1](../../docs/adapters/protocols/sandbox-v1.md)
- [Conformance Test Harness](../../docs/adapters/CONFORMANCE.md)

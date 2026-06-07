---
title: Module — Sandbox
description: Sandboxed code execution. Docker, gVisor, Firecracker, or e2b.
sidebar:
  order: 5
---

Sandboxed code execution. Spins up a short-lived isolated environment to run an image+command, captures stdout/stderr, meters cpu/memory/network, persists artefacts to object storage.

## What it does

`sandbox.Service` orchestrates: validate input → insert ledger row at `status=queued` → hand off to the adapter (`Sandbox.Run` or `Stream`) → write terminal row (`done`/`failed`/`timeout`/`killed`) → emit a cost event. The adapter does the actual containment; the Service is adapter-agnostic.

Wire schemas in `apps/dashboard/src/lib/api.ts` (`SandboxRunSchema`, `SandboxRunListSchema`, `SandboxRunInputSchema`, `SandboxPoolStatsSchema`, `SandboxCapabilitiesSchema`) define the source of truth.

## Configuration

```yaml
sandbox:
  adapter: docker          # docker | gvisor | firecracker | e2b
  e2b_api_key: ""          # only when adapter=e2b
  e2b_base_url: ""         # optional; defaults to https://api.e2b.dev
```

Env overrides:

```bash
AF_STACK_SANDBOX_ADAPTER=gvisor
E2B_API_KEY=sk-...
AF_STACK_E2B_BASE_URL=https://api.e2b.dev
```

The runtime logs the selection at startup:

```
sandbox adapter ready  adapter=gvisor max_timeout_s=86400 ...
```

See [Sandbox adapters](../../adapters/sandbox/) for the per-adapter matrix.

## REST endpoints

Registered in `services/runtime/internal/server/sandbox.go`:

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/sandbox/run` | Run a sandboxed command (sync). |
| `GET` | `/api/v1/sandbox/runs` | List sandbox runs. Query: `tenant`, `status`, `limit`, `offset`. |
| `GET` | `/api/v1/sandbox/runs/{id}` | Get a single run. |
| `DELETE` | `/api/v1/sandbox/runs/{id}` | Stop a running sandbox. |
| `GET` | `/api/v1/sandbox/pool` | Pool stats + adapter capabilities. |
| `GET` | `/api/v1/sandbox/runs/{id}/logs` | SSE log stream from a running sandbox. |

## Database tables

Owned by migration `00008_sandbox.sql`:

- `suite_sandbox_runs` — every run (id, tenant, image, cmd, env, status, exit_code, timings, resource meters, storage keys for stdout/stderr/artefacts).

## Env vars

| Env | Purpose |
|---|---|
| `AF_STACK_SANDBOX_ADAPTER` | Adapter selection (docker / gvisor / firecracker / e2b). |
| `E2B_API_KEY` | Required when adapter=e2b. |
| `AF_STACK_E2B_BASE_URL` | Optional e2b endpoint override. |

## Code map

- `interface.go` — `Sandbox` interface, `RunSpec`, `RunResult`, `Capabilities`.
- `service.go` — adapter-agnostic orchestrator (validate, ledger, dispatch).
- `recorder.go` — `suite_sandbox_runs` row lifecycle.
- `adapters/docker/` — Docker daemon adapter.
- `adapters/gvisor/` — Docker w/ `runsc` runtime.
- `adapters/firecracker/` — Flintlock-backed (scaffold).
- `adapters/e2b/` — hosted e2b.dev.
- `server/sandbox.go` — REST routes.

## Related

- [Sandbox adapters](../../adapters/sandbox/) — picking the right backend.
- Emits cost events into [Cost](./cost/).
- Persists artefacts via [Storage](./storage/).

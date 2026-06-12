# Realtime run subscriptions

`suite.runs.subscribe(filter)` streams live agent-run events from
AgentField to a customer-app over a single WebSocket. It is the primitive
behind "agent is doing X" UIs — no polling, server-side filtering,
tenant-scoped by default.

```text
AgentField execution event bus
        -> BackAI runtime WebSocket /api/v1/realtime/runs
        -> suite.runs.subscribe({ tenant_id, user_id, agent, run_id, execution_id })
```

This is the AgentField-backed counterpart to `suite.realtime.subscribe`
(which streams Postgres NOTIFY for arbitrary table mutations). Reach for
this when you want **agent semantics** — start, step, completed, error.
Reach for `suite.realtime` when you want **data semantics** — row
inserted, row updated.

## How it works

The runtime bridges AgentField's event bus into a WebSocket on BackAI:

1. Caller opens `WS /api/v1/realtime/runs?[filter]`.
2. The runtime upgrades and chooses a source:
   - **Pinned to one execution** (`execution_id` set) →
     `GET /api/v1/executions/:execution_id/events` SSE.
   - **Filter-based** (any other filter) →
     `GET /api/ui/v1/executions/events` SSE (AgentField's global event
     stream).
3. Each SSE frame is parsed, mapped to the wire protocol, filtered
   server-side, and shipped as a JSON-encoded WebSocket message.
4. When AgentField is unreachable the runtime emits
   `{"type":"server.degraded"}` and closes — the SDK propagates it to
   the caller as a regular event so UI banners can show "stream
   degraded, reconnecting…".

When AgentField's SSE stream is unavailable for a pinned execution
(typically because the run already finished), the runtime polls
`/api/v1/executions/:execution_id` once and emits the cached final
state as a synthesised `run.completed` so the SDK closes cleanly with
a useful last message instead of a silent hang.

## Wire protocol

The runtime sends one JSON envelope per event. Fields besides `type`
are optional — they're populated only when the underlying AgentField
event carried the value. `raw` always carries the original payload
for debug introspection.

```jsonc
{
  "type": "run.started",
  "run_id": "run-abc",
  "execution_id": "exec-xyz",
  "agent": "notable-ai.summarize",
  "node_id": "notable-ai.summarize",
  "started_at": "2026-06-07T12:00:00Z"
}

{
  "type": "run.step",
  "run_id": "run-abc",
  "execution_id": "exec-xyz",
  "step_id": "step-1",
  "reasoner": "summarize",
  "status": "running",
  "started_at": "...",
  "ended_at": "...",
  "tool_calls": [...],
  "tokens": { "input": 123, "output": 456 }
}

{
  "type": "run.completed",
  "run_id": "run-abc",
  "execution_id": "exec-xyz",
  "status": "succeeded",
  "duration_ms": 1234,
  "cost_usd": 0.0042
}

{
  "type": "run.error",
  "run_id": "run-abc",
  "error": "downstream timeout"
}

{
  "type": "server.degraded",
  "reason": "agentfield unreachable: dial tcp …"
}
```

`status` on `run.completed` is one of `succeeded` / `failed` /
`cancelled`. `status` on `run.step` is freeform — the runtime forwards
whatever AgentField reported.

## Filters

All filter parameters are optional and applied server-side:

| Field          | Effect                                                         |
| -------------- | -------------------------------------------------------------- |
| `tenant_id`    | Auto-bound to caller's tenant when multi-tenancy is on         |
| `user_id`      | Match events for one suite user                                |
| `agent`        | Match events for one `agent_node_id`                           |
| `run_id`       | Match events for one AgentField run / workflow                 |
| `execution_id` | Pin to one execution (uses per-exec SSE; emits cached final)   |

When multi-tenancy is enabled on the runtime, the runtime overwrites
any client-supplied `tenant_id` with the caller's resolved tenant.
This is a safety invariant, not a bug — a subscriber can never receive
events tagged for another tenant. Events with no tenant tag are
dropped when multi-tenancy is on; they pass through otherwise.

## SDK — Python

```python
from af_stack import suite

async for evt in suite.runs.subscribe(run_id="run-abc"):
    if evt.type == "run.completed":
        print(f"{evt.agent} finished in {evt.duration_ms}ms")
        break
    elif evt.type == "server.degraded":
        # Treat as transient — reconnect with backoff in your loop.
        break
    else:
        print(evt.type, evt.agent, evt.status)
```

The Python SDK lazy-imports the optional `websockets` package. The
first `__anext__` raises `RuntimeError` with install instructions if
it's missing.

## SDK — TypeScript

```ts
import { suite } from "@af-stack/sdk"

const ws = suite.runs.subscribe({ run_id: "run-abc" })

ws.addEventListener("message", (e) => {
  const evt = JSON.parse(e.data)
  if (evt.type === "run.completed") {
    console.log(`${evt.agent} ${evt.status} in ${evt.duration_ms}ms`)
  }
})
ws.addEventListener("close", () => {
  // Reconnect with exponential backoff.
})
```

For runtimes without a global `WebSocket` (some Node configurations),
pass one via `opts.WebSocket`. Browsers and edge runtimes need
nothing extra.

## Operator demo

The Operate → Runs page in the dashboard mounts a
`<LiveRunsStrip />` that subscribes to the current tenant and renders
the last 10 events as an auto-scrolling ticker. Useful for a
glance-and-go view of activity; the run-detail pages still own the
deep inspector.

## When to use what

| You want…                                       | Use                          |
| ----------------------------------------------- | ---------------------------- |
| Live "agent is doing X" UI in a customer-app    | `suite.runs.subscribe`       |
| Notify on app data mutating (chat, todos, etc.) | `suite.realtime.subscribe`   |
| One-off final state after a run finished        | `suite.agents.status(id)`    |
| Server-side webhook on run completion           | AgentField webhooks |

## Reconnection & backoff

The runtime closes the WebSocket cleanly when its upstream source ends
(execution finished, AgentField restart, transient stream drop).
SDK callers should treat `close` as expected and reconnect with
exponential backoff (1s → 30s cap is a reasonable default; that's what
the dashboard's `LiveRunsStrip` uses).

`{"type":"server.degraded"}` is emitted before the runtime closes so
the SDK can surface a UI banner if it likes; treat the close itself as
the trigger to reconnect.

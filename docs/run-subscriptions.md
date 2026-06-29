# Realtime Run Subscriptions

BackAI exposes a dashboard-friendly WebSocket bridge for live
AgentField run events:

```text
GET /api/v1/runs/{run_id}/events
```

This endpoint does not store runs, spans, traces, tool calls, memory, or
workflow state in BackAI. It opens AgentField's Server-Sent Events
stream for the run/workflow and relays each event to the WebSocket
client. AgentField remains the source of truth.

## TypeScript

```ts
import { suite } from "@af-stack/sdk"

const socket = suite.runs.subscribe("run_abc123")

socket.addEventListener("message", (event) => {
  const msg = JSON.parse(event.data)
  if (msg.type === "agentfield.run_event") {
    console.log(msg.event, msg.data)
  }
})
```

Server runtimes without a global `WebSocket` can pass a constructor:

```ts
suite.runs.subscribe("run_abc123", { WebSocket: MyWebSocket })
```

## Browser URL

```text
ws://localhost:8080/api/v1/runs/run_abc123/events?api_key=af_...
```

## Message Shape

Each AgentField SSE frame becomes a JSON WebSocket message:

```json
{
  "type": "agentfield.run_event",
  "event": "execution.started",
  "data": {
    "type": "execution_started",
    "execution_id": "exec_123"
  },
  "path": "/api/v1/workflows/runs/run_abc123/events/stream"
}
```

If AgentField emits comment-style keepalives, BackAI forwards them as:

```json
{
  "type": "heartbeat",
  "raw": ": keep-alive"
}
```

## AgentField Compatibility

The bridge tries AgentField's current run-events SSE path first:

```text
/api/v1/workflows/runs/{run_id}/events/stream
```

Then it falls back to the older workflow-events path:

```text
/api/v1/workflows/{run_id}/events
```

If the connected AgentField control plane does not support run event
streaming yet, the BackAI endpoint returns `404
AGENTFIELD_RUN_EVENTS_NOT_FOUND` before upgrading the connection.

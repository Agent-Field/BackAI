# AgentField data in the dashboard

This doc explains how the af-stack operator dashboard surfaces
AgentField run / span / step data. The implementation (item #25 in
`MULTI-ITEM-PLAN.md`) deliberately avoids reimplementing AgentField's
own UI and instead composes against it.

## Principle: don't rebuild what's already excellent

AgentField already ships a rich run / DAG / step inspector at port
`:8081` (its control plane UI). The same engineering effort spent
mirroring it inside af-stack would buy us nothing — AgentField evolves
its own UI alongside its schema, and any af-stack clone would lag.

So af-stack's dashboard takes a two-layer approach:

1. **Link out** to AgentField's UI for deep inspection — the DAG view,
   step I/O, spans, tool calls.
2. **Inline a summary** (status, agent, timing, cost, approval status)
   on the run detail page so operators get the at-a-glance numbers
   without leaving the dashboard.
3. **Proxy control actions** (cancel / pause / resume / request
   approval) through af-stack routes so operators don't bounce between
   two UIs for routine ops, and so af-stack can layer audit / tenant
   scoping in front of the AgentField call.

## What's where

| Surface | Lives in | Notes |
|---|---|---|
| Run list, filters, paging | af-stack dashboard `/operate/runs` | Reads `suite_gateway_requests` (every gateway call is logged there) |
| Per-run summary card | af-stack dashboard `/operate/runs/[id]` | Reads `GET /api/v1/runs/{id}/agentfield` |
| Run controls (cancel / pause / resume / request-approval) | af-stack dashboard `/operate/runs/[id]` | Posts to `POST /api/v1/runs/{id}/{verb}` |
| DAG view, step inspector, span tree | **AgentField UI** at `:8081` | Reached via "View in AgentField" link-out |
| Real-time event stream | af-stack runtime (item #15) | Bridges AgentField's SSE to a WebSocket |
| Memory tab | af-stack dashboard | Reads `suite_memory` (canonical store per #31 audit) |

## Wire shape

### `GET /api/v1/runs/{id}/agentfield`

```jsonc
{
  "overview": {
    "execution_id": "exec_abc",
    "run_id": "run_xyz",
    "status": "running",
    "agent_name": "pricing",
    "reasoner": "estimate",
    "started_at": "2026-06-07T10:00:00Z",
    "ended_at": "",
    "duration_ms": null,
    "cost_usd": 0.0123,
    "approval_status": "auto",
    "extra": { /* raw AgentField response, forwarded verbatim */ }
  },
  "agentfield_url": "https://af.example.com",
  "details_url": "https://af.example.com/agent-api/executions/exec_abc/details",
  "actions_available": ["cancel", "pause", "request-approval"]
}
```

- `overview` is af-stack's projection of AgentField's
  `GET /agentic/run/:run_id` response. We only type the fields the
  dashboard renders; everything else passes through under `extra` so
  schema additions in AgentField don't require a runtime release.
- `agentfield_url` is the operator-reachable AgentField host (from
  `AF_STACK_AGENTFIELD_PUBLIC_URL`, falling back to the runtime's
  internal AgentField URL).
- `details_url` is the deep-link target used by the "View in
  AgentField" button.
- `actions_available` is a server-computed allowlist driven by the
  run's current status. The dashboard uses it to gray out invalid
  verbs (e.g. you can't resume a succeeded run); the runtime does not
  enforce it as a security gate — AgentField is the authority.

### Control verbs

All four POSTs share the same request shape (empty body) and response:

```jsonc
{
  "run_id": "run_xyz",
  "execution_id": "exec_abc",
  "action": "cancel",
  "status": "ok"
}
```

| af-stack route | proxied to |
|---|---|
| `POST /api/v1/runs/{id}/cancel` | `POST /agent-api/executions/{exec_id}/cancel` |
| `POST /api/v1/runs/{id}/pause` | `POST /agent-api/executions/{exec_id}/pause` |
| `POST /api/v1/runs/{id}/resume` | `POST /agent-api/executions/{exec_id}/resume` |
| `POST /api/v1/runs/{id}/request-approval` | `POST /agent-api/executions/{exec_id}/request-approval` |

The runtime resolves the execution id off the run id via
`GET /agentic/run/{run_id}` before invoking the verb, so dashboard
callers stay run-id-oriented (matches how the runs list addresses
rows).

## Failure modes

| Condition | Status | Code | Dashboard behavior |
|---|---|---|---|
| Runtime has no AgentField client wired | 503 | `AGENTFIELD_NOT_CONFIGURED` | Summary card renders "AgentField unavailable" badge |
| AgentField unreachable | 502 | `AGENTFIELD_UNREACHABLE` | Same as above |
| AgentField call exceeds timeout | 504 | `AGENTFIELD_TIMEOUT` | Same as above |
| Run id not found in AgentField | 404 | `RUN_NOT_FOUND` | Page renders the "no execution" empty state |
| Legacy run with no execution_id | 502 | `AGENTFIELD_NO_EXECUTION_ID` | Action buttons stay disabled |

The page never crashes on a missing or malformed AgentField response —
the catch in `runs/[id]/page.tsx` falls through to the unavailable
view.

## Env

```bash
# Customer-facing AgentField URL used by the dashboard for "View in
# AgentField" link-outs. Leave commented in dev to fall back to the
# runtime's internal AgentField URL; set this in prod to the
# externally-reachable host.
# AF_STACK_AGENTFIELD_PUBLIC_URL=https://af.example.com
```

## Files

| File | Role |
|---|---|
| `services/runtime/internal/agentfield/client.go` | `GetRunOverview`, `GetExecutionDetails`, `CancelAgentExecution`, `PauseAgentExecution`, `ResumeAgentExecution`, `RequestAgentApproval`, `AgentFieldURL` |
| `services/runtime/internal/server/run_agentfield.go` | Route handlers for `/api/v1/runs/{id}/(agentfield\|cancel\|pause\|resume\|request-approval)` |
| `services/runtime/internal/server/server.go` | Route registration |
| `services/runtime/internal/server/openapi_routes.go` | `registerRunAgentFieldOpenAPI` |
| `apps/dashboard/src/app/(admin)/operate/runs/[id]/page.tsx` | SSR page; fetches summary, hands to view |
| `apps/dashboard/src/app/(admin)/operate/runs/[id]/_components/run-detail-view.tsx` | Composes card + actions |
| `apps/dashboard/src/app/(admin)/operate/runs/[id]/_components/run-summary-card.tsx` | Inline summary card |
| `apps/dashboard/src/app/(admin)/operate/runs/[id]/_components/run-actions.tsx` | Cancel / pause / resume / request-approval / View in AgentField |
| `apps/dashboard/src/app/(admin)/operate/runs/_components/runs-view.tsx` | "View in AgentField" icon per row |
| `apps/dashboard/src/lib/api.ts` | `RunAgentFieldSchema`, `RunOverviewSchema`, `api.runAgentField()`, `api.runActions.*` |

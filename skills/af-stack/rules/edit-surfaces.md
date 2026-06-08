# Edit Surfaces — when to use which

The 4 edit surfaces in the user's fork. This file resolves the
"where does this code go?" question for any task.

## The map

| Surface | Path | Language |
|---|---|---|
| Customer App | `apps/customer-app/src/app/(app)/...` | TypeScript / React |
| Agent | `apps/backend/agents/<name>/` | Python |
| Workload Module | `examples/<id>/handlers/` (Python sidecar today) OR `services/runtime/internal/modules/<id>/` (Go, eventually) | Python (sidecar) or Go (in-runtime) |
| Dashboard Plugin | `apps/dashboard/plugins/<id>/` | TypeScript / React |

## Decision tree

```
The user wants ...

├─ a page in the customer-facing SaaS?
│    → Customer App
│      apps/customer-app/src/app/(app)/<route>/page.tsx
│
├─ a tab in the operator console showing some state?
│    → Dashboard Plugin
│      apps/dashboard/plugins/<id>/
│
├─ a backend HTTP route that's domain-specific?
│    → Workload Module (Python sidecar today)
│      examples/<id>/handlers/handler.py
│      + migrations/00001_<table>.sql
│
├─ an agent that uses LLM / harnesses / memory?
│    → Agent
│      apps/backend/agents/<name>/main.py
│
└─ a swappable backend for storage / sandbox / billing / notifications?
     → Adapter (rare — usually use what's there)
       services/runtime/internal/<area>/adapters/<id>/
       (and read rules/adapters.md first)
```

## Frequently combined surfaces

A real app shape often spans multiple surfaces. Common combinations:

### Pattern A — "Reactive agent" (e.g. Forge PR reviewer)

- **Workload Module**: webhook receiver (`POST /webhooks/github`) + job
  handler that calls the agent + tables for tracking runs.
- **Agent**: the actual review logic (uses harness + sandbox).
- **Dashboard Plugin**: operator view of "reviews today / cost / errors."
- **Customer App**: customer's view of their connected repos + reviews.

### Pattern B — "Knowledge Q&A" (e.g. DocuChat)

- **Customer App**: doc upload page + chat interface.
- **Workload Module**: `POST /documents` (upload + chunk + embed),
  `POST /search` (retrieval), `POST /ask` (RAG with LLM).
- **Agent**: the answer-synthesis reasoner (with citations).
- **Dashboard Plugin**: documents / queries / cost per tenant.

### Pattern C — "Autonomous worker" (e.g. Mercer SDR)

- **Customer App**: prospect list + status + drafted emails for review.
- **Workload Module**: cron-triggered "research next batch" + OAuth
  callback for Gmail + drafted-email storage.
- **Agent**: research reasoner + draft-writer reasoner + reply-handler
  reasoner.
- **Dashboard Plugin**: cross-tenant ops view of campaigns + costs.

### Pattern D — "Pure UI feature"

(e.g. a new settings page for the customer)

- **Customer App**: one or two pages.
- (Nothing else.)

### Pattern E — "Operator-only tool"

(e.g. a custom export script)

- **Dashboard Plugin**: page with a button.
- **Workload Module**: the route the button hits.

## Anti-patterns — putting code in the wrong surface

| User does | Why it's wrong | Where it belongs |
|---|---|---|
| Adds a route in `apps/customer-app/src/app/api/...` | API routes in the customer-app proxy to the runtime; don't add business logic there | Workload Module |
| Adds business logic in `services/runtime/internal/server/` | That's platform code, not your fork's domain | Workload Module |
| Adds a long-running computation in a dashboard plugin page | Plugin pages should be read-only views, not background work | Workload Module + Job |
| Calls an LLM directly from a customer-app page | LLM calls go through `suite.llm.*` from the runtime side, not the client side | Workload Module or Agent |
| Reads the DB directly from the customer-app | Customer-app shouldn't know about your DB schema | Workload Module |
| Writes to `suite_*` tables from a workload module | Those are platform-owned; you don't write them | Use the SDK methods that wrap them |

## When you need multiple surfaces (the wiring)

Surfaces talk to each other via SDK + REST. The wiring rules:

| From | Talks to | How |
|---|---|---|
| Customer App | Workload Module | `fetch("/workload/<id>/...")` (the customer-app proxies to the runtime) |
| Customer App | Runtime primitives | `suite.*` (TypeScript SDK) |
| Workload Module | Agent | `suite.agents.call("<name>.<reasoner>", input)` |
| Workload Module | Runtime primitives | `suite.*` (Python or Go SDK) |
| Agent | Runtime primitives (memory etc.) | `app.memory.*`, `app.ai()`, `app.harness()` (AgentField SDK) |
| Dashboard Plugin | Workload Module | `fetch("/workload/<id>/stats")` |
| Dashboard Plugin | Runtime primitives | `fetch("/api/v1/...")` (the operator session is auto-attached) |

Never: customer-app → DB directly. Never: agent → LiteLLM directly.
Never: workload module → agent runtime internals.

## How much code typically lives in each surface

Rough order of magnitude for a non-trivial app (like the 12 startups in
`examples/README.md`):

| Surface | Lines of code |
|---|---|
| Customer App | 500–2000 (3–8 pages, some components) |
| Workload Module | 200–800 (3–5 routes + 1–2 jobs + migrations) |
| Agent | 50–300 (1–5 reasoners) |
| Dashboard Plugin | 50–200 (1 page) |
| **Total user code** | **800–3000** |

That's it. The rest (auth, multi-tenancy, LLM gateway, sandboxes,
webhooks, jobs, storage, notifications, billing, audit, dashboards,
deploy) is platform code the user inherits from the fork.

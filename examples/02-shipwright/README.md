# Shipwright — autonomous code-agent demo

> Customer pastes a GitHub issue → an agent reads the repo, plans the
> change, edits code, runs tests, opens a PR. Iteration UI built on
> standard shadcn components. Real flow today uses a stub agent so you
> can iterate on the UX; swap in a real coding-agent library and the
> wiring stays identical.

## What's here

```
examples/02-shipwright/
├── agents/shipwright/        # AgentField agent (the "stub" — same node_id + reasoner names as the real one)
│   ├── Dockerfile
│   ├── main.py               # @app.reasoner def execute_task(payload: dict)
│   └── requirements.txt
├── handlers/                 # FastAPI workload-module sidecar
│   ├── Dockerfile
│   ├── handler.py            # /tasks POST/GET, /tasks/{id} GET, /tasks/{id}/cancel POST, /stats GET
│   └── requirements.txt
├── migrations/
│   └── 00001_shipwright.sql  # shipwright_tasks + shipwright_steps with RLS
└── docker-compose.yml        # Compose overlay
```

Customer-app pages (live in the main `apps/customer-app/`):

- `/shipwright` — queue table + new-task form
- `/shipwright/[id]` — task detail with step timeline + diff preview
- Proxy: `/api/customer/shipwright/[...path]` — forwards session-derived
  tenant + user as `x-af-stack-tenant-id` + `x-af-stack-user-id` headers

## Run it

```bash
# From repo root, with .env already populated (cp .env.example .env)
docker compose \
  -f docker-compose.yml \
  -f docker-compose.override.yml \
  -f examples/02-shipwright/docker-compose.yml \
  up -d

# Customer-app:    http://localhost:34000
# Operator:        http://localhost:33000
# Runtime API:     http://localhost:38080
# Shipwright API:  http://localhost:38201
```

Sign up at `http://localhost:34000/sign-up`. The first sign-up auto-
provisions a tenant + membership + API key. Click **Shipwright** in the
sidebar and submit a task.

## How the flow works

```
Customer-app /shipwright form (Next.js)
        ↓
POST /api/customer/shipwright/tasks  (proxy, attaches tenant + user)
        ↓
shipwright-api FastAPI sidecar (handler.py)
   ├── INSERT INTO shipwright_tasks (status=queued)
   └── asyncio.create_task(_drive_task)  ← non-blocking
        ↓
POST runtime /api/v1/agents/shipwright-v2.execute_task
        ↓
AgentField control plane routes to agent
        ↓
shipwright-agent (main.py) — the stub
   ├── prints "[shipwright] starting task ..."
   ├── 7 steps × asyncio.sleep + print → AgentField records spans
   └── returns RunResult{status, summary, diff_preview, steps}
        ↓
handler.py persists the result:
   UPDATE shipwright_tasks SET status='completed', summary, diff_preview
   INSERT 7 rows into shipwright_steps
        ↓
Customer-app polls every ~1s and renders the live state
```

## Swapping In A Real Coding Agent

The contract is just the AgentField reasoner. To swap:

1. Replace `agents/shipwright/main.py` with the real coding agent.
2. Keep `Agent(node_id="shipwright-v2")` and reasoner name
   `execute_task`. (Or bump the node_id to `shipwright-v3` and update
   `SHIPWRIGHT_AGENT` env var in `docker-compose.yml`.)
3. The reasoner takes `payload: dict` with `issue_url`, `title`,
   `description` and returns the same `RunResult` schema (status,
   summary, diff_preview, steps).
4. Everything else (UI, workload module, DB, polling) stays.

## Watch out for

- **Don't name a reasoner `run`** — that collides with `Agent.run()`
  (the decorator overrides the method and the agent exits immediately
  with a benign-looking RuntimeWarning).
- **AgentField caches reasoner metadata** keyed on node_id. If you
  rename a reasoner during iteration, bump the node_id (`shipwright`
  → `shipwright-v2`) to force a fresh registration.
- **Payload shape**: AgentField expects `{"input": {"payload": {...}}}`
  when the reasoner signature is `async def f(payload: dict)`.
- **Result shape**: the runtime returns `{"result": {...}}`, not
  `{"output": {...}}`.

## Screenshots

See `dashboard-screenshots/`:
- `shipwright-with-sidebar.png` — sidebar nav + queue view
- `shipwright-queue-mixed.png` — list with mixed running / completed
- `shipwright-detail-completed.png` — task detail with steps + diff

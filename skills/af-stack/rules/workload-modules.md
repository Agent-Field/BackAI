# Workload Modules — backend extensions in your fork

A workload module is the unit of *backend domain code*. Anything that
isn't a core platform primitive but needs HTTP routes / DB tables / jobs
goes here.

## Two flavors (current reality)

### Flavor 1 — Python FastAPI sidecar (canonical TODAY)

The pattern the Notable example uses
(`examples/01-notable/handlers/notes.py`). The module is a standalone
FastAPI service that runs alongside the runtime in `docker-compose`.

**Where**: `examples/<your-app>/handlers/` (alongside the rest of your
app's `agents/`, `migrations/`, etc.)

**Why this is the current pattern**: the Go in-runtime workload-module
loader (`services/runtime/internal/modules/`) is shippable but the
filesystem-driven discovery is on the eventually roadmap. Python sidecar
proves the multi-tenant + billing + agent-orchestration story end-to-end
today.

**Pros**:
- Any Python lib you want (parsers, scrapers, embedders).
- Hot-reload in dev (`uvicorn --reload`).
- Easy to grow into a more complex service.

**Cons**:
- One more container in compose.
- Routes mount under `/workload/<id>/...` via reverse proxy or are
  called directly via the Docker DNS name.

### Flavor 2 — Go in-runtime module (canonical FUTURE)

`services/runtime/internal/modules/<id>/` implements the `Module`
interface. Loaded at runtime startup, mounted under `/workload/<id>/...`.

**When to choose**: hot path / latency-critical / no Python deps
needed. Or once eventually formalizes the filesystem loader.

**Pros**:
- One binary, one deploy.
- Tightest integration with runtime hooks, RLS, audit.

**Cons**:
- Go only.
- Modifying the runtime feels like modifying platform code.

**The skill recommends**: Python sidecar by default. Go in-runtime only
when you have a specific reason (latency, no Python deps).

## The Python sidecar shape

```
examples/<your-app>/
├── handlers/
│   ├── Dockerfile
│   ├── requirements.txt
│   └── handler.py            ← FastAPI app
├── agents/                   ← your AgentField agents
│   └── <name>/
├── migrations/
│   └── 00001_<table>.sql     ← applied by runtime at startup
├── config.yaml               ← optional module config
├── docker-compose.yml        ← runs YOUR services on top of base compose
├── .env.example
└── README.md
```

Copy from `snippets/workload-module/` to start.

## Tenant context — the non-negotiable

Every route that touches data MUST:

1. Receive `x-af-stack-tenant-id` + `x-af-stack-user-id` headers
2. Reject missing headers with 401
3. Set `app.tenant_id` GUC on the DB connection before any query

See `snippets/workload-module/handler.py` for the canonical pattern:

```python
async with _tenant_conn(tenant_id) as conn:
    # All queries here are RLS-scoped to tenant_id automatically.
    rows = await (await conn.execute("SELECT * FROM your_table")).fetchall()
```

NEVER read `tenant_id` from the request body / query string. NEVER skip
the GUC setting. RLS is the safety net; without the GUC, every read
fails.

## Migrations

Place under `examples/<app>/migrations/`. Naming: `NNNNN_<description>.sql`
(numeric ordering). The runtime applies them at startup, tracking
state in its own migration table.

Pattern:
1. Create the table with `tenant_id` + `user_id` columns.
2. Add a composite index on `(tenant_id, ...)`.
3. `ALTER TABLE ... ENABLE ROW LEVEL SECURITY`.
4. Create the RLS policy.

See `snippets/workload-module/migration.sql`.

## Calling other AF Stack primitives

From your workload module handlers:

### Calling agents

```python
import httpx
runtime = httpx.AsyncClient(base_url=os.environ["AF_STACK_URL"])

resp = await runtime.post(
    f"/api/v1/agents/{node_id}.{reasoner}",
    json={"input": payload},
    headers={"x-af-stack-tenant-id": tenant_id},
)
output = resp.json()["output"]
```

(Or with the Python SDK: `from af_stack import suite; await
suite.agents.call("node_id.reasoner", payload)`.)

### Recording usage for billing

```python
await runtime.post(
    "/api/v1/billing/meter",
    json={"meter": "items_created", "qty": 1},
    headers={"x-af-stack-tenant-id": tenant_id},
)
```

### Enqueuing a background job

```python
await runtime.post(
    "/api/v1/jobs",
    json={"name": "your_task", "args": {...}},
    headers={"x-af-stack-tenant-id": tenant_id},
)
```

### Using storage

```python
from af_stack import suite
await suite.storage.upload(key=f"contracts/{file_id}", data=file_bytes)
```

### Using secrets

```python
github_token = await suite.secrets.get("github_app_token")
```

## Common routes you'll write

A typical workload module has 4–8 routes:

| Method | Route | Purpose |
|---|---|---|
| `POST` | `/items` | Create |
| `GET` | `/items` | List (paginated) |
| `GET` | `/items/{id}` | Detail |
| `PATCH` | `/items/{id}` | Update |
| `DELETE` | `/items/{id}` | Delete |
| `POST` | `/items/{id}/process` | Trigger an agent or job |
| `GET` | `/stats` | Dashboard plugin metrics |
| `POST` | `/webhooks/<provider>` | Inbound webhook (HMAC) |

For webhook receivers (PR-opened, Stripe-event, etc.), set up the route
in the runtime's webhooks-in tab pointing at your `/workload/<id>/webhooks/<provider>`
endpoint. The runtime verifies HMAC + dedups before forwarding.

## Connecting from the customer-app and dashboard plugin

The customer-app calls your module via the runtime's proxy:

```ts
const items = await fetch("/api/v1/proxy/workload/<your-id>/items", {
  credentials: "include",  // session cookie → tenant resolution
})
```

The dashboard plugin can call directly server-side:

```ts
const stats = await fetch(`${process.env.RUNTIME_URL}/workload/<your-id>/stats`)
```

## Anti-patterns

| Anti-pattern | Why wrong | Correct |
|---|---|---|
| Reading `tenant_id` from request body | Trivial security bug | Use the header (set by runtime) |
| Direct DB access without GUC | Bypasses RLS | Use `_tenant_conn` helper |
| Writing to `suite_*` tables | Platform-owned | Use SDK methods |
| Long-running work in a request handler | Blocks the client, timeouts | Enqueue a job |
| Storing chat history in your module's tables | Duplicates AgentField | Use Session-scope memory |
| Calling an LLM directly from a route | Bypasses cost ledger | Call an agent, or `suite.llm.chat` |
| Adding `is_admin` flags to your tables | Reimplements RBAC | Use the runtime's auth context |
| Hardcoded model names in code | Lock-in | Take from config / env |

## Testing locally

```bash
# Start base AF Stack
docker compose up -d

# Start your example app (includes your workload module sidecar)
cd examples/<your-app>
docker compose up -d

# Test a route
curl -X POST http://localhost:<your-port>/items \
  -H "Content-Type: application/json" \
  -H "x-af-stack-tenant-id: $TENANT_UUID" \
  -H "x-af-stack-user-id: $USER_UUID" \
  -d '{"title":"hello"}'
```

Use the existing `examples/01-notable/scripts/seed.sh` and
`smoke-test.sh` as references for your own scripts.

## When you really need a Go in-runtime module

If you've decided Python sidecar isn't right (latency, ops simplicity),
the Go pattern:

```
services/runtime/internal/modules/<your-id>/
├── module.go    # implements modules.Module interface
├── routes.go    # http.HandlerFunc registrations
├── store.go     # DB access (use the runtime's *pgxpool.Pool with RLS bound)
└── migrations/  # same SQL files as Python sidecar
```

See `services/runtime/internal/modules/modules.go` for the `Module`
interface contract. Registration happens in
`services/runtime/cmd/af-stack/main.go`. Don't add Go modules without
reading `development/strategy.md` first — eventually is when this becomes the canonical
shape.

# Build an App

You build on BackAI by editing one of **four surfaces**. Pick the surface
that matches what you're adding; everything else is platform you don't
touch. Or skip the repo entirely and
[attach an existing app](#attach-an-existing-app).

| Surface | You're adding… |
| --- | --- |
| [Agent](#1-agent) | An AI reasoner (multi-step LLM logic) |
| [Customer app](#2-customer-app) | Product UI your end-users see |
| [Workload module](#3-workload-module) | Tenant-scoped CRUD resources + migrations |
| [Dashboard plugin](#4-dashboard-plugin) | A page in the operator console |

---

## 1. Agent

**Lives in:** `apps/backend/agents/<name>/` · **Scaffold:** `af-stack agent new <name>`

An agent is an AgentField process. Inside it you use the **`app.*`** SDK
(not `suite.*`) to define reasoners.

**Files you create:**

```
apps/backend/agents/<name>/
  main.py           # defines `app` and its reasoners
  Dockerfile        # runs the agent (EXPOSE a port, e.g. 8090)
  requirements.txt  # agentfield>=0.4.0, pydantic>=2.0, …
  README.md
```

**Minimal `main.py`:**

```python
from agentfield import Agent, AIConfig

app = Agent(node_id="my-agent")

@app.reasoner("summarize")
async def summarize(text: str) -> str:
    return await app.ai(f"Summarize:\n{text}", config=AIConfig(model="gpt-4o-mini"))

if __name__ == "__main__":
    app.run()
```

**Contract:** the agent registers with AgentField on boot; callers reach
it via `suite.agents.call("<node_id>.<reasoner>", {...})` from anywhere
in the Suite. Inside the agent, `app.*` gives you `app.reasoner` (define),
`app.ai` (LLM), `app.call` (call another agent), `app.run` (serve).

---

## 2. Customer app

**Lives in:** `apps/customer-app/` — a Next.js app. Edit it directly.

This is *your* product surface. From it you call the runtime through the
app's own same-origin proxy at `src/app/api/v1/[...path]/route.ts`, which
forwards the customer's session so the runtime resolves the right tenant.
Full editing contract:
[`apps/customer-app/EDITING.md`](../../apps/customer-app/EDITING.md).

**Edit freely:**

- `src/app/<route>/page.tsx` — pages and routes (pattern:
  `src/app/dashboard/page.tsx`; sign-in pages live under `src/app/(auth)/`)
- `src/components/*` — product components
- `src/lib/api.ts` — client helpers for runtime calls
- `src/components/app-sidebar.tsx` — nav links (the inline `items` array
  passed to `<NavMain>`)

Start a new logged-in workflow from
`examples/starter/customer-app/first-action/page.tsx`.

**Do not hand-edit** `src/app/brand.css` or `src/lib/brand.ts` — they're
generated from root `brand.yaml` by `pnpm run generate:brand`.

```ts
// `@af-stack/sdk` is not a dependency of apps/customer-app — call the
// proxy, which is same-origin and carries the session cookie.
const response = await fetch("/api/v1/agents/my-agent.summarize", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ input: { text } }),
})
const out = await response.json()
```

---

## 3. Workload module

**Lives in:** `workload-modules/<id>/` · **Scaffold:** `af-stack module new <id>`

A module is a *declarative* backend capability: a manifest naming typed
resources, plus the versioned SQL that backs them. The runtime
auto-generates tenant-scoped CRUD from it — no handler code. Full
contract: [../workload-modules.md](../workload-modules.md).

The runtime discovers `<WORKLOAD_MODULES_PATH>/<id>/backai.module.yaml`
(env `WORKLOAD_MODULES_PATH`, default `./workload-modules`), applies
`migrations/` at boot, statically RLS-lints every resource table, and
serves `/api/v1/workload/<id>/<resource>`. Inspect what it found at
`GET /api/v1/admin/modules`.

**Files the scaffold writes:**

```
workload-modules/<id>/
  backai.module.yaml         # required — id, resources, typed fields
  migrations/00001_init.sql  # the table your resources are backed by
  README.md
```

**Minimal `backai.module.yaml`:**

```yaml
id: notes
name: Notes
version: 0.1.0
description: Per-tenant notes.
enabled: false
migrations: migrations

resources:
  - name: notes              # table: notes_notes (<module>_<resource>)
    fields:
      - name: title
        type: string
        required: true
      - name: done
        type: bool
        default: false
```

Field types are `string | int | bool | timestamp | json`. `id`,
`tenant_id`, `created_at` and `updated_at` are reserved — the runtime
manages them, and your migration must create them alongside `ENABLE` +
`FORCE ROW LEVEL SECURITY` and a tenant-isolation policy. A table without
tenant isolation refuses to load and only that module is skipped; the
runtime keeps serving everything else.

**Enable it.** Scaffolds ship `enabled: false`. Either flip that to `true`
in the manifest, or add the id to `modules.workload_modules` in
`config.yaml` (env `AF_STACK_WORKLOAD_MODULES=<ids>`), then restart.

**Check it first.** `af-stack module validate workload-modules/<id>`
(`--json` for a machine-readable report) runs the same manifest and RLS
gates offline, before you boot anything.

The manifest has no cron field — schedule work through the crons API /
SDK instead ([jobs.md](jobs.md#crons)).

---

## 4. Dashboard plugin

**Lives in:** `apps/dashboard/plugins/<id>/` · **Scaffold:** `af-stack plugin new <id>`

A plugin drops a page into the operator console sidebar without touching
shell code. A build-time scanner discovers it. Full contract:
[../dashboard-plugins.md](../dashboard-plugins.md).

The public shape (`PluginSchema`, mirror of `apps/dashboard/src/lib/api.ts`):

```ts
{
  id:          "usage",              // matches the folder name
  name:        "Usage",              // sidebar + ⌘K label
  description: "Per-tenant spend",   // command-palette line
  route:       "/usage",            // dashboard path
  icon:        "BarChart",          // lucide-react icon name
  group:       "Operate",           // Build | Operate | Customers | Plugins
  version:     "0.1.0"
}
```

---

## Attach an existing app

Don't want to build in the repo? Use BackAI as a **pure AI backend**. Keep
your app where it is and point its OpenAI SDK client at the gateway.

```ts
import OpenAI from "openai"

const client = new OpenAI({
  baseURL: "https://backai.example.com/api/v1/llm",
  apiKey: process.env.BACKAI_API_KEY,   // a tenant API key
})

const response = await client.chat.completions.create(
  { model: "qwen/qwen-2.5-72b-instruct", messages: [{ role: "user", content: "…" }] },
  { headers: { "X-Request-ID": requestId } },
)
```

You get the OpenAI-compatible gateway, tenant API keys, a cost ledger (pass
`X-Request-ID` to deep-link a customer action to spend), and the admin
dashboard — without moving your product database or calling AgentField
directly. Full guide: [../attach-existing-app.md](../attach-existing-app.md).

> Never ship a tenant API key in a browser client. Call the gateway
> server-side.

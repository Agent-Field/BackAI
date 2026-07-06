# Build an App

You build on BackAI by editing one of **four surfaces**. Pick the surface
that matches what you're adding; everything else is platform you don't
touch. Or skip the repo entirely and
[attach an existing app](#attach-an-existing-app).

| Surface | You're adding… |
| --- | --- |
| [Agent](#1-agent) | An AI reasoner (multi-step LLM logic) |
| [Customer app](#2-customer-app) | Product UI your end-users see |
| [Workload module](#3-workload-module) | Backend routes / crons / migrations |
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

This is *your* product surface. From it you call the runtime via the
**`suite.*`** SDK (TypeScript). Full editing contract:
[`apps/customer-app/EDITING.md`](../../apps/customer-app/EDITING.md).

**Edit freely:**

- `src/app/(app)/*` — pages and routes
- `src/components/*` — product components
- `src/lib/api.ts` — client helpers for runtime calls
- `src/components/layout/customer-sidebar.tsx` — nav links

Start a new logged-in workflow from
`examples/starter/customer-app/first-action/page.tsx`.

**Do not hand-edit** `src/app/brand.css` or `src/lib/brand.ts` — they're
generated from root `brand.yaml` by `pnpm run generate:brand`.

```ts
import { suite } from "@af-stack/sdk"

const out = await suite.agents.call("my-agent.summarize", { text })
```

---

## 3. Workload module

**Lives in:** `workload-modules/<id>/` · **Scaffold:** `af-stack module new <id>`

A module is backend capability — HTTP routes, cron schedules, SQL
migrations. Only `manifest.yaml` is required. Full contract:
[../workload-modules.md](../workload-modules.md).

> **Status: scaffold today; runtime mounting is on the roadmap.**
> `af-stack module new <id>` scaffolds the layout below (including a
> `handlers/routes.go.example` placeholder), but the runtime does **not**
> auto-load workload modules yet — there is no module loader and no
> `/workload/<id>/` route mounting wired in. Treat the route/cron/migration
> behavior described here as the design contract, not a live capability.

```
workload-modules/<id>/
  manifest.yaml       # required — metadata + requires + routes
  migrations/         # optional — versioned SQL applied at boot
  handlers/           # optional — Go (routes.go) or Python (handler.py)
  crons/seed.yaml     # optional — cron schedules seeded into suite_crons
  config.schema.yaml  # optional — operator-tunable config
```

**Minimal `manifest.yaml`:**

```yaml
id: notes
name: Notes
version: 0.1.0
requires:
  - multi-tenancy
  - llm-gateway
routes:
  - method: POST
    path: /notes
    handler: notes.Create   # -> mounted at /workload/notes/notes
```

Routes are *designed* to be prepended with `/workload/<id>/` so modules
never clash — this mounting is roadmap, not yet wired (see the status note
above). Crons declared here are covered in [jobs.md](jobs.md#crons).

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

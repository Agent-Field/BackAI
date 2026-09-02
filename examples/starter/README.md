# BackAI Starter

This is the canonical fork basis. The other examples show finished
products; this one shows the four places you edit when turning BackAI
into your own backend.

## What you copy

| Surface          | Starter path                         | Copy into your fork                               |
| ---------------- | ------------------------------------ | ------------------------------------------------- |
| Agent            | `agents/starter/`                    | `apps/backend/agents/<your-agent>/`               |
| Customer flow    | `customer-app/first-action/page.tsx` | `apps/customer-app/src/app/first-action/page.tsx` |
| Dashboard plugin | `dashboard-plugin/`                  | `apps/dashboard/plugins/<your-plugin>/`           |
| Workload module  | `workload-module/`                   | `workload-modules/<your-module>/`                 |

Start by branding the fork:

```bash
af-stack init --name "DocuChat" --color "#0A66C2"
# optional: --logo ./your-logo.svg sets the light+dark mark in brand.yaml
```

Then copy the starter pieces you want and rename `starter` to your
product slug.

## Agent

`agents/starter/main.py` registers one AgentField node named `starter`
with two reasoners:

- `starter.echo` returns input verbatim and needs no LLM key.
- `starter.summarize` uses `app.ai()` when an LLM key is configured.

Run it locally:

```bash
cd examples/starter/agents/starter
pip install -r requirements.txt
AGENTFIELD_SERVER=http://localhost:8081 NODE_ID=starter python main.py
```

Call it through the suite gateway:

```bash
curl -X POST http://localhost:8080/api/v1/agents/starter.echo \
  -H "Content-Type: application/json" \
  -d '{"input":{"message":"hello"}}'
```

## Customer app

`customer-app/first-action/page.tsx` is the first logged-in product
action after sign-up. It calls `/api/v1/agents/starter.echo` through the
customer app proxy, so users experience the backend immediately instead
of landing on an empty dashboard.

Add a sidebar link after copying:

```ts
{ href: "/first-action", label: "First Action", icon: Sparkles }
```

## Dashboard plugin

`dashboard-plugin/plugin.ts` declares a Build-group plugin and
`dashboard-plugin/page.tsx` renders a small custom metric card. After
copying the folder into `apps/dashboard/plugins/starter/`, run:

```bash
pnpm --filter dashboard prebuild
```

The dashboard generates the route proxy and sidebar entry.

## Workload module

`workload-module/` declares one route and one migration:

- `POST /workload/starter/events`
- `migrations/00001_starter_events.sql`

The handler example is intentionally tiny: decode JSON, insert one event
row, return the row id. Rename `handlers/routes.go.example` to
`handlers/routes.go` when you vendor it into a fork with the workload
handler package enabled.

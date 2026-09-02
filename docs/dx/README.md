# Developer Experience

The one place to go from empty repo to deployed app on BackAI (the
`af-stack` platform). Everything here is verified against the CLI and SDK
source — where this hub and older prose disagree, this hub wins.

## The golden path (CLI-first)

```bash
git clone https://github.com/Agent-Field/backai my-app && cd my-app
af-stack init --name "My App" # brand the fork: brand.yaml + default agent name (add --logo/--color to set those)
af-stack dev                  # preflight ports + docker compose up
# … edit one of the four surfaces (below) …
af-stack deploy helm          # ship it (helm | fly | railway | render)
```

Four commands, one loop: **init → dev → edit → deploy**, all inside the
clone. `af-stack init <name>` with a positional name is a different thing:
it scaffolds an app that *carries its own backend* — a `docker-compose.yml`
and a `backend/` directory pinned to the CLI's version — in any directory,
with no clone and no surfaces to brand or extend. `af-stack dev` inside that
app boots the backend from the published release images. See
[run.md](run.md) for what `af-stack dev` actually brings up and
[build-app.md](build-app.md) for the surfaces you edit.

## The four edit surfaces

You build by editing one of four places. Everything else is platform.

| Surface | Lives in | Scaffold with |
| --- | --- | --- |
| **Agent** (AgentField reasoner) | `apps/backend/agents/<name>/` | `af-stack agent new <name>` |
| **Customer app** (product UI) | `apps/customer-app/` | edit directly |
| **Workload module** (backend resources + migrations) — *scaffolds ship `enabled: false`; set `enabled: true` in `backai.module.yaml` (or list the id in `AF_STACK_WORKLOAD_MODULES`) and restart to mount it* | `workload-modules/<id>/` | `af-stack module new <id>` |
| **Dashboard plugin** (operator UI) | `apps/dashboard/plugins/<id>/` | `af-stack plugin new <id>` |

Or **don't build in the repo at all**: point your existing app at the
OpenAI-compatible gateway and use BackAI purely as an AI backend — see
the "attach existing app" path in [build-app.md](build-app.md#attach-an-existing-app).

## `app.*` vs `suite.*` — the SDK boundary

Two SDKs, two scopes. Getting this right avoids most confusion:

| | `app.*` | `suite.*` |
| --- | --- | --- |
| What | The **AgentField** SDK | The **Suite** SDK |
| Where | **Only inside an agent** (`main.py`) | **Everywhere else** — apps, modules, plugins, other agents |
| Purpose | *Define* reasoners: `app.reasoner`, `app.ai`, `app.call` | *Use* the platform: `suite.llm.chat`, `suite.agents.call`, `suite.jobs.enqueue`, … |
| Import | `from agentfield import Agent` | `from af_stack import suite` / `import { suite } from "@af-stack/sdk"` |

Full breakdown in [sdk.md](sdk.md).

## Pages

| Page | Covers |
| --- | --- |
| [build-app.md](build-app.md) | The four edit surfaces + attaching an existing app |
| [run.md](run.md) | Local run, ports, `.env`, personal vs saas mode, seeded operator |
| [jobs.md](jobs.md) | River-backed jobs + crons (and the Go-only-handler limitation) |
| [webhooks.md](webhooks.md) | Inbound receiver, outbound outbox, tenant pub-sub — no Svix |
| [adapters.md](adapters.md) | Swapping backends (storage/secrets/llm/notifications/…) + the admin-UI Integrations credentials flow |
| [sdk.md](sdk.md) | `suite.*` namespace reference + language parity |

## Deeper reference (existing docs)

| Topic | Doc |
| --- | --- |
| Operator CLI + minting an operator key | [../cli-admin.md](../cli-admin.md) |
| Deploying (helm/fly/railway/render) | [../deploy.md](../deploy.md) |
| Multi-tenancy & RLS | [../multi-tenancy.md](../multi-tenancy.md) |
| Workload modules (full contract) | [../workload-modules.md](../workload-modules.md) |
| Dashboard plugins (full contract) | [../dashboard-plugins.md](../dashboard-plugins.md) |
| Guardrails | [../guardrails.md](../guardrails.md) |
| Attach an existing app | [../attach-existing-app.md](../attach-existing-app.md) |
| Billing | [../billing.md](../billing.md) |
| Configuration reference | [../CONFIGURATION.md](../CONFIGURATION.md) |
| Product overview | [../product.md](../product.md) |

# Repo Map

BackAI is meant to be forked. The repo is organized by who owns each
piece after the fork, not by framework.

## First Files

| File | Purpose |
| --- | --- |
| `README.md` | First-run path, product positioning, and the shortest deploy path. |
| `.env.example` | Local and production environment surface. Public product name is BackAI; internal env vars keep the stable `AF_STACK_*` namespace. |
| `brand.yaml` | App name, colors, and logo source for generated brand files. |
| `docker-compose.yml` | Local first run: customer app, admin, runtime, LiteLLM, AgentField, Postgres, MinIO. |
| `deploy/railway/railway.json` | Hosted no-key SupportDesk demo plus real provider mode. |

## Product Surfaces

| Path | Owner after fork | What belongs here |
| --- | --- | --- |
| `apps/customer-app/` | Product team | End-user UI, onboarding, first actions, customer billing/API-key UX. |
| `apps/dashboard/` | Operator/product team | Admin console, operational views, custom dashboard plugins. |
| `apps/backend/` | Backend team | App agents, app handlers, jobs, crons, migrations, LiteLLM model config. |
| `workload-modules/` | Backend team | Reusable domain modules with routes, migrations, jobs, and crons. |

## Platform Surfaces

| Path | Purpose |
| --- | --- |
| `services/runtime/` | Go runtime that serves the public API, admin API, LLM gateway, costs, tenants, storage, jobs, and module hooks. |
| `services/cli/` | CLI used for initialization and repo operations. |
| `services/sandbox-host/` | Local sandbox execution support. |
| `packages/` | Shared libraries and SDKs for auth, DB, gateway clients, UI, TypeScript, Python, and Go. |
| `modules/` | Platform module specs and docs for shipped backend primitives. |
| `skills/` | Agent/automation skills used by this repo. |

## Documentation And Evidence

| Path | Purpose |
| --- | --- |
| `docs/` | Durable user/operator documentation. Keep current claims here. |
| `docs/archive/` | Historical plans and old phase documents. Useful context, not the current public path. |
| `development/` | Current branch planning, milestone evidence, worker briefs, and verification notes. This is project-management state, not product docs. |
| `docs/assets/dashboard-screenshots/` | README/admin screenshots. Refresh when the public UI changes. |

## Examples

| Path | Use when |
| --- | --- |
| `examples/starter/` | You want the smallest neutral fork basis. |
| `examples/03-llm-gateway-only/` | You already have an app and only want the AI gateway/cost ledger. |
| `examples/01-notable/` | You want a production-shaped SaaS example with tenants, billing, memory, and plugins. |
| `examples/06-deep-research/` | You want a long-running agent pattern with memory and cost shape. |
| `examples/02-shipwright/` | You want the heavier coding-agent example. This is intentionally not the default product path. |

Each example declares its scope in `capabilities.yaml`. Read that file
before copying an example into your product.

## Clean Fork Rules

- Put user-facing product work in `apps/customer-app/` first.
- Put admin/operator product work in `apps/dashboard/plugins/<id>/`
  unless it needs to change a shared dashboard primitive.
- Put app-specific backend state in `workload-modules/<id>/` or
  `apps/backend/migrations/`, not in `services/runtime/`.
- Put agent code in `apps/backend/agents/<name>/`. AgentField is the
  runtime substrate; BackAI should call it through the platform boundary.
- The default first-run agent is `apps/backend/agents/supportdesk/`. It
  registers `supportdesk.reply_plan` plus support triage, fact extraction,
  policy guardrail, and reply-brief reasoners with AgentField.
- Keep `services/runtime/` changes for shared platform capabilities:
  auth, tenants, keys, costs, gateway behavior, jobs, storage, billing,
  and deployment contracts.
- Keep heavy or niche examples out of the first-run path. Link them from
  `examples/README.md` with honest prerequisites.

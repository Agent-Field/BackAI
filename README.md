<div align="center">

# BackAI

### The open backend for AI products.

**Ship the product. Stop rebuilding the backend around it.**

BackAI gives your app one self-hosted backend for auth, tenants, LLMs, agents,
jobs, storage, billing, cost, and operations — already wired together.

[Quickstart](#quickstart) · [What you get](#what-you-get) · [Build with an agent](#built-for-codex-and-claude-code) · [Docs](docs/dx/README.md)

[![CI](https://github.com/Agent-Field/backai/actions/workflows/ci.yml/badge.svg)](https://github.com/Agent-Field/backai/actions/workflows/ci.yml)
[![Status: pre-alpha](https://img.shields.io/badge/status-pre--alpha-orange)](#project-status)
[![License: Apache 2.0](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

</div>

Think **Supabase for AI-native backends**: open source, self-hostable,
CLI-first, and built with agents, model calls, cost, and policy as backend
primitives — not application glue.

<div align="center">
  <img src="docs/assets/readme/operator-overview.png" alt="BackAI operator console showing API setup, cost, runs, activity, and service health" width="1000" />
  <br />
  <sub>One console for the backend behind your AI product.</sub>
</div>

## Quickstart

```bash
git clone https://github.com/Agent-Field/backai.git
cd backai

# Install the CLI, then start the complete local stack.
curl -fsSL https://raw.githubusercontent.com/Agent-Field/backai/main/scripts/install.sh | bash
af-stack dev
```

Open the customer app first at `http://localhost:34000`, then inspect what it
did in the operator console at `http://localhost:33000`.

No model key is required. The first run uses a deterministic demo provider but
still exercises the real gateway, tenant context, cost ledger, customer app,
and dashboard. Add an OpenRouter, OpenAI, Anthropic, Gemini, or other supported
provider key when you want live model calls through LiteLLM.

Prefer raw Compose? Run `node scripts/preflight.mjs --fix` and then
`docker compose up`. Preflight resolves local port conflicts and prints every
service URL.

## What you get

| Primitive            | Wired into BackAI                                                                                                           |
| -------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| **Agents**           | AgentField execution, multi-reasoner call graphs, streaming, approvals, cancellation, and run traces                        |
| **LLMs**             | OpenAI-compatible gateway, LiteLLM provider routing, streaming, embeddings, cache, guardrails, and model-level cost records |
| **Data**             | Postgres 16, pgvector, tenant-aware memory, search, realtime, migrations, and an operator database browser                  |
| **Identity**         | better-auth, users, sessions, tenants, memberships, roles, OAuth, and scoped API keys                                       |
| **Async work**       | River-backed jobs, crons, inbound webhooks, signed outbound webhooks, retries, and notifications                            |
| **Agent tools**      | Isolated sandboxes, MCP servers, native tools, skills, secrets, and coding-harness discovery                                |
| **Commercial layer** | Usage metering, cost ledger, per-tenant budgets, plan entitlements, and Stripe-shaped billing                               |
| **Operations**       | Admin console for runs, errors, logs, queue, spend, customers, keys, audit, integrations, and health                        |
| **Product shell**    | A customer-facing Next.js app and SupportDesk AI flow that can be replaced with your product                                |
| **Delivery**         | Local Compose plus production recipes for Helm, Fly.io, Railway, Render, and a single VM                                    |

These are one system, not a catalog of disconnected containers. Tenant identity
flows through model calls, jobs, storage, billing, audit, and cost so builders do
not have to recreate those connections for every product.

## What can you build?

- Multi-tenant AI SaaS and copilots
- Customer support, operations, and back-office agents
- Research, extraction, enrichment, and document workflows
- Coding-agent and long-running agent products
- AI features inside an existing web, mobile, or backend application
- A private AI backend in your cloud or VPC

Start from the neutral [`examples/starter/`](examples/starter/), the bundled
SupportDesk product, or a focused example: [LLM gateway](examples/03-llm-gateway-only/),
[multi-tenant notes](examples/01-notable/),
[deep research](examples/06-deep-research/), or
[coding agents](examples/02-shipwright/).

## Built for Codex and Claude Code

BackAI is designed to be handed to a coding agent after initialization. The
repo includes `AGENTS.md`, `CLAUDE.md`, and a versioned BackAI skill that explain
the architecture, safe edit boundaries, SDKs, and verification workflow.

```bash
# Scaffold a small app that consumes a BackAI deployment.
af-stack init my-ai-product

# Or customize a full fork and give it to your coding agent.
af-stack init --name "Acme AI" --color "#2563EB" --logo ./logo.png
af-stack agent new researcher
```

Inside a fork, builders and agents usually edit only four surfaces:

| Surface          | Path                           | Purpose                                    |
| ---------------- | ------------------------------ | ------------------------------------------ |
| Customer product | `apps/customer-app/`           | End-user UI and product flows              |
| Agent            | `apps/backend/agents/<name>/`  | AgentField reasoners and agent logic       |
| Workload module  | `workload-modules/<id>/`       | Domain routes, jobs, crons, and migrations |
| Dashboard plugin | `apps/dashboard/plugins/<id>/` | Product-specific operator views            |

The platform runtime stays behind those boundaries. Existing apps can skip the
customer shell and [attach BackAI as a backend](docs/attach-existing-app.md).

## One URL, two compatible interfaces

Use the standard OpenAI SDK by changing its base URL:

```ts
import OpenAI from "openai"

const ai = new OpenAI({
  baseURL: "https://backai.example.com/api/v1/llm",
  apiKey: process.env.BACKAI_API_KEY,
})

const answer = await ai.chat.completions.create({
  model: "qwen/qwen-2.5-72b-instruct",
  messages: [{ role: "user", content: "Summarize this support request." }],
})
```

Use the Suite SDK when you need the rest of the backend:

```python
from af_stack import suite

result = await suite.agents.call("researcher.run", {"topic": "AI backends"})
await suite.memory.put("latest-report", result)
await suite.billing.meter("reports_created", quantity=1)
```

The rule is simple:

- **`app.*` defines reasoners inside an AgentField agent.**
- **`suite.*` calls agents and every other BackAI primitive.**

Python is the canonical full SDK. TypeScript is near parity with documented
differences. REST and OpenAPI work from any language. The Go SDK is a version
stub today, not a usable client. See the [SDK reference](docs/dx/sdk.md).

## Security is a system property

BackAI wires the controls that are easy to omit when teams assemble an AI
backend service by service:

- Postgres row-level tenant isolation and scoped serving roles
- short-lived sessions plus issue, rotate, and revoke flows for API keys
- one LLM gateway for tenant, policy, budget, and audit enforcement
- encrypted secret storage with KMS adapter support
- PII redaction and configurable request/response moderation
- sandbox limits for CPU, memory, timeout, filesystem, and egress
- signed webhooks, replay protection, idempotency, retry, and delivery logs
- audit records for security-sensitive operator mutations
- separate health, readiness, metrics, backup, and restore paths

These defaults remove repeated integration work; they do not make an arbitrary
deployment automatically compliant or secure. Before production, replace all
development secrets, use external Postgres and object storage, choose a
production sandbox adapter, configure backups, TLS, provider keys, monitoring,
and your own threat model. Start with [deployment guidance](docs/deploy.md),
[configuration](docs/CONFIGURATION.md), and [the security policy](SECURITY.md).

## How it fits together

```text
Your product or agent
        │
        ▼
BackAI API + Suite SDK ─── auth · tenants · policy · cost · audit
        │
        ├── Postgres + pgvector     data · memory · jobs · billing
        ├── AgentField             reasoners · runs · traces · approvals
        ├── LiteLLM                model providers behind one gateway
        └── S3 + sandbox adapters  files · artifacts · isolated execution
```

Every app-level model call should go through `/api/v1/llm/*`. Direct provider
calls bypass BackAI's tenant identity, budgets, cost records, and guardrails.

<div align="center">
  <img src="docs/assets/dashboard-screenshots/agentfield-swe-af-run-detail.png" alt="AgentField run detail for a multi-reasoner coding-agent execution" width="1000" />
  <br />
  <sub>AgentField owns the reasoner graph and trace; BackAI connects it to the product backend.</sub>
</div>

## Deploy

Use the target that matches your operations posture:

| Target           | Best for                                           |
| ---------------- | -------------------------------------------------- |
| Docker Compose   | Local development or one controlled VM             |
| Railway / Render | Fast hosted evaluation and small deployments       |
| Fly.io           | A compact regional deployment                      |
| Helm             | Kubernetes, external state, and production scaling |

See [`deploy/`](deploy/) for the maintained artifacts and
[`docs/deploy.md`](docs/deploy.md) for required secrets, external services,
backups, and production caveats.

## Project status

BackAI is **pre-alpha**. The current golden path is the SupportDesk first run,
no-key demo mode, OpenAI-compatible gateway, Python/TypeScript SDKs, cost
ledger, customer app, operator console, and self-hosted deployment artifacts.

Two important limits are explicit:

- Workload modules scaffold today, but automatic runtime mounting is not wired.
- Durable jobs execute Go in-process handlers today; remote Python and
  TypeScript job handlers are not implemented yet.

Track shipped behavior in [`docs/product.md`](docs/product.md) and extension
contracts in [`docs/architecture.md`](docs/architecture.md).

## Documentation

- [Developer experience](docs/dx/README.md) — build surfaces, local run, SDK, jobs, and webhooks
- [Architecture](docs/stack.md) — layers, ownership, and OSS composition
- [Repository map](docs/repo-map.md) — what belongs where
- [Attach an existing app](docs/attach-existing-app.md) — gateway and tenant-key integration
- [OSS audit](docs/oss-audit.md) — what BackAI uses and why
- [Examples](examples/) — capability-declared product shapes

## Community

Read [`CONTRIBUTING.md`](CONTRIBUTING.md) before opening a change and use GitHub
Issues for bugs and product proposals. Security reports belong in the private
channel described in [`SECURITY.md`](SECURITY.md).

Apache 2.0. See [`LICENSE`](LICENSE).

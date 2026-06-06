<div align="center">

# AF Stack

### The open backend platform for the AI era.

*Self-host AgentField with everything else you need to ship a real product.*

[![Status: planning](https://img.shields.io/badge/status-pre--alpha-orange)](#)
[![License: Apache 2.0](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)
[![Built on AgentField](https://img.shields.io/badge/built%20on-AgentField-0A66C2)](https://github.com/Agent-Field/agentfield)

</div>

> **Working name**. The brand is being decided. See [`BRAND.yaml`](BRAND.yaml).

## What this is

A single fork-friendly monorepo that bundles **AgentField** with everything
around it: identity, multi-tenancy, sandboxes, storage, queues, public
APIs, billing, dashboard. Clone the repo. Customize. Deploy.

Not a hosted service. Not a closed SDK. Not a framework. **A complete,
self-hostable backend platform where AI is a native compute primitive.**

## Why

Building an AI-native product today means assembling 10+ services: auth,
db, storage, queue, gateway, agent runtime, sandboxes, webhooks, billing,
observability. Each integration costs weeks. Each vendor adds lock-in.

Supabase-shaped backends don't include AI primitives. AI platforms don't
include backend primitives. Builders rebuild the same plumbing for every
project.

AF Stack ships both halves. AgentField for agents. Postgres + Next.js +
the suite runtime for everything else.

## The invariant

**Every LLM call goes through AgentField.** No bypass. The
OpenAI-compatible endpoint at `/api/v1/llm/*` is a shim that routes
through AF so identity, traces, cost, policy, and audit are preserved.

## Two SDKs, clear boundary

> **`app.*` defines agents. `suite.*` calls them and runs everything else.**

| SDK | Use inside |
|---|---|
| **AgentField** (`app.*`) | Agent processes |
| **Suite** (`suite.*`) | App handlers, jobs, dashboard — anywhere outside an agent |

Plus a REST + OpenAPI surface so any language works.

## Quickstart

> Coming as Phase 2 lands. Expected shape:
>
> ```bash
> git clone https://github.com/Agent-Field/backai my-app
> cd my-app
> cp .env.example .env
> # edit .env: set OPENROUTER_API_KEY
> docker compose up
> # Dashboard at http://localhost:3000
> # API at http://localhost:8080
> ```

## Status

This is being built in the open. See [`ROADMAP.md`](ROADMAP.md) for the
phased build plan and [GitHub Issues](https://github.com/Agent-Field/backai/issues)
for what's in flight.

| Phase | What | Status |
|---|---|---|
| 0 | Foundations | in progress |
| 1 | Runtime + AF wiring | |
| 2 | First end-to-end + 60s quickstart | |
| 3 | Identity + dashboard shell | |
| 4 | Hero — Agent Runs | |
| 5 | Jobs + secrets + storage | |
| 6 | Multi-tenancy + gateway | |
| 7 | Hero — LLM Gateway | |
| 8 | Hero — DB studio + memory | |
| 9 | Hero — Sandboxes | |
| 10 | Notifications + webhooks + billing | |
| 11 | MCP + skills + harnesses | |
| 12 | Hero — Tenants + remaining tabs | |
| 13 | Examples + workload modules | |
| 14 | Deploy + production hardening | |
| 15 | Documentation + polish | |
| 16 | Security audit + launch | |

## Documentation

Architecture and product docs live in this repo:

- [`PRD.md`](PRD.md) — Product Requirements Document
- [`TECH-SPEC.md`](TECH-SPEC.md) — Technical specification
- [`ROADMAP.md`](ROADMAP.md) — Phased build plan
- [`PLAN.md`](PLAN.md) — Architectural plan (full)
- [`docs/`](docs/) — Validation walkthroughs, SDK strategy, AF analysis

## Built on AgentField

AgentField is the agent runtime at the core of AF Stack. AgentField
provides agents, the LLM gateway, memory, traces, MCP integration via
harnesses, verifiable credentials, and cryptographic identity. AF Stack
adds the production wrapping around it.

[AgentField repo →](https://github.com/Agent-Field/agentfield)

## License

Apache 2.0. See [`LICENSE`](LICENSE).

## Contributing

Read [`CONTRIBUTING.md`](CONTRIBUTING.md). Issues and PRs welcome.

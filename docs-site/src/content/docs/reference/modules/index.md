---
title: Modules reference
description: Every suite module exposed by the AF Stack runtime. Code-derived; one row per package under services/runtime/internal/.
sidebar:
  order: 0
---

One row per Go package under `services/runtime/internal/`. Each page is generated from package doc comments, config structs, route registrations, and migration SQL.

Module on/off is controlled by:

1. `config.yaml` under `modules.enabled.<id>: true|false`
2. Env var `AF_STACK_MODULE_<UPPER_SNAKE>=true` (overrides YAML, e.g. `AF_STACK_MODULE_MULTI_TENANCY=true`)

When a module is OFF or its dependencies are missing, the REST routes return `503` with an envelope like `{"code":"NOT_CONFIGURED","message":"..."}`.

## Module list

| Module | Purpose |
|---|---|
| [Multi-tenancy](./multi-tenancy/) | Opt-in tenant isolation, API keys, sessions, RLS. |
| [LLM Gateway](./llm-gateway/) | OpenAI-compatible proxy for chat, embeddings, images. |
| [LLM Cache](./llm-cache/) | Postgres-backed response cache keyed by request hash. |
| [Memory](./memory/) | Scope/key store with optional pgvector similarity search. |
| [Sandbox](./sandbox/) | Sandboxed code execution via Docker / gVisor / Firecracker / e2b. |
| [Notifications](./notifications/) | Outbox-style notification dispatcher (log, Resend). |
| [Webhooks](./webhooks/) | Inbound + outbound webhook delivery with HMAC verify + dedup. |
| [Billing](./billing/) | Stripe customers + per-tenant usage meters. |
| [Secrets](./secrets/) | Envelope-encrypted key/value vault. |
| [Cost](./cost/) | Per-call cost ledger + per-tenant budget guard. |
| [Jobs](./jobs/) | River-backed Postgres job queue with cron periodic jobs. |
| [Crons](./crons/) | DB-backed schedules that enqueue named jobs once per minute tick. |
| [MCP](./mcp/) | Model Context Protocol pool over stdio + SSE adapters. |
| [Skills](./skills/) | AF skillkit installer + per-harness attach. |
| [Harnesses](./harnesses/) | Probe-only inventory of installed CLI harnesses. |
| [Storage](./storage/) | Object-storage facade over MinIO and S3 adapters. |
| [DB Studio](./db-studio/) | Read-mostly DB introspection + SQL runner. |

## Related

- [Adapters reference](../adapters/) — per-adapter knobs.
- [Hooks reference](../hooks/) — cross-cutting hook points the runtime fires.

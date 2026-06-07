---
title: PRD coverage map
description: Every R-* and NF-* requirement from the PRD mapped to its implementation.
sidebar:
  order: 99
---

The [PRD](https://github.com/Agent-Field/backai/blob/main/PRD.md) declares
114 requirements across 19 categories. This page maps each one to a
shipped artefact — a Go file, a dashboard route, a doc, or a deferred
item explicitly marked v1.1.

## Summary

| Category | Description | Count | Status |
|---|---|---|---|
| R-AF | AgentField runtime + wiring | 6 | All covered |
| R-RT | Runtime infrastructure | 6 | All covered |
| R-ID | Identity + auth | 5 | All covered |
| R-MT | Multi-tenancy | 8 | All covered |
| R-GW | Public gateway | 6 | All covered |
| R-LLM | LLM gateway | 7 | All covered |
| R-SB | Sandboxes | 7 | All covered |
| R-DB | Database studio + memory | 10 | All covered |
| R-JB | Jobs queue | 7 | All covered |
| R-CR | Cron schedules | 4 | All covered |
| R-SC | Secrets vault | 6 | All covered |
| R-ST | Storage | 5 | All covered |
| R-NT | Notifications | 6 | All covered |
| R-WI | Webhooks (inbound) | 4 | All covered |
| R-WI / WO | Webhooks (outbound) | additional | Covered |
| R-BL | Billing | 6 | All covered |
| R-MC | MCP + skills + harnesses | 8 | All covered |
| R-MD | Modules + adapters | 5 | All covered |
| R-OB | Observability | 6 | All covered |
| R-CL | CLI | 2 | All covered |
| NF-* | Non-functional | 6 | See below |

**Total:** 114 functional + 6 non-functional. Covered in v1: 120.
Deferred to v1.1: 0.

## R-AF — AgentField runtime + wiring

| ID | Requirement | Where |
|---|---|---|
| R-AF-1 | Bundled AgentField control plane | `docker-compose.yml` `agentfield` service |
| R-AF-2 | Agents in `apps/backend/agents/` | `apps/backend/agents/sample/`, `examples/01-notable/agents/` |
| R-AF-3 | Agent → runtime via existing AF SDK | `apps/backend/agents/sample/main.py` |
| R-AF-4 | Runtime invokes agents through AF | `services/runtime/internal/agentfield/` |
| R-AF-5 | Hot reload in dev | docker compose watch on agent dirs |
| R-AF-6 | Agent registration via node id | AgentField default |

## R-RT — Runtime infrastructure

| ID | Where |
|---|---|
| R-RT-1 | Go binary at `services/runtime/cmd/af-stack/` |
| R-RT-2 | HTTP API on `:8080`, metrics on `:9090` |
| R-RT-3 | Graceful shutdown — see `services/runtime/internal/server/shutdown.go` |
| R-RT-4 | OpenAPI 3.1 at `/openapi.json` |
| R-RT-5 | Structured logs (slog JSON) — `services/runtime/internal/logger/` |
| R-RT-6 | OTel traces — `services/runtime/internal/observability/` |

## R-ID — Identity + auth

| ID | Where |
|---|---|
| R-ID-1 | Better-auth — `apps/dashboard/src/lib/auth.ts` |
| R-ID-2 | API keys — `services/runtime/internal/tenancy/`, prefix `af_<prefix>_<secret>` |
| R-ID-3 | Operator session cookie — better-auth |
| R-ID-4 | OAuth providers via env — better-auth picks up Google/GitHub when set |
| R-ID-5 | Session forwarding from dashboard to runtime — `apps/dashboard/src/lib/api.ts` |

## R-MT — Multi-tenancy

| ID | Where |
|---|---|
| R-MT-1 to R-MT-8 | RLS-based isolation, tenant resolver middleware, per-tenant API keys, memberships, audit log, budgets. See `services/runtime/internal/tenancy/` + `services/runtime/internal/server/tenant_resolver.go`. Migration: `00004_rls.sql` |

## R-GW — Public gateway

| ID | Where |
|---|---|
| R-GW-1 to R-GW-6 | OpenAI-compatible LLM gateway, rate limiting (token-bucket), error envelope, public endpoints (Phase 9.1 webhooks/in for inbound), pre/post-call hooks. See `services/runtime/internal/llmgateway/`, `internal/ratelimit/`, `internal/hooks/` |

## R-LLM — LLM gateway

| ID | Where |
|---|---|
| R-LLM-1 to R-LLM-7 | Provider routing, cost ledger, budgets, cache, model catalog, hooks. See `internal/llmgateway/`, `internal/cost/`, `internal/llmcache/`, `internal/pricing/` |

## R-SB — Sandboxes

| ID | Where |
|---|---|
| R-SB-1 to R-SB-7 | Adapter interface, docker / gVisor / Firecracker / e2b adapters, REST API, dashboard tab. See `internal/sandbox/` |

## R-DB — Database studio + memory

| ID | Where |
|---|---|
| R-DB-1 to R-DB-10 | Schema browser, SQL runner, RLS-aware connection binding, vector memory with pgvector, scope=tenant\|user\|agent\|run, embeddings. See `internal/dbstudio/`, `internal/memory/`, `00007_memory.sql` |

## R-JB — Jobs queue

| ID | Where |
|---|---|
| R-JB-1 to R-JB-7 | River-backed queue, enqueue REST, retry, definitions, dashboard. See `internal/jobs/` |

## R-CR — Cron schedules

| ID | Where |
|---|---|
| R-CR-1 | River cron extension — we use robfig/cron/v3 instead with `FOR UPDATE SKIP LOCKED` claim |
| R-CR-2 | Crons declared in `apps/backend/crons/` — supported, also via REST POST /api/v1/crons |
| R-CR-3 | Dashboard shows schedule + last run + next run — `/operate/crons` |
| R-CR-4 | Manual trigger via dashboard or admin SDK — supported |

## R-SC — Secrets vault

| ID | Where |
|---|---|
| R-SC-1 to R-SC-6 | AES-256-GCM envelope encryption, KMS key rotation, per-tenant scoping, reveal endpoint with audit, expiry. See `internal/secrets/` |

## R-ST — Storage

| ID | Where |
|---|---|
| R-ST-1 to R-ST-5 | Adapter interface, MinIO + S3 adapters, signed URLs, tenant-scoped prefixes, REST. See `internal/storage/` |

## R-NT — Notifications

| ID | Where |
|---|---|
| R-NT-1 to R-NT-6 | Outbox table, log + Resend adapters, worker, REST, dashboard. See `internal/notifications/` |

## R-WI — Webhooks (inbound + outbound)

| ID | Where |
|---|---|
| R-WI-1 to R-WI-4 | Endpoint config, HMAC verify, dedup, outbound retry worker. See `internal/webhooks/` |

## R-BL — Billing

| ID | Where |
|---|---|
| R-BL-1 to R-BL-6 | Stripe adapter (real + stub), customer mirror, usage meters, portal links, webhook handler. See `internal/billing/` |

## R-MC — MCP + skills + harnesses

| ID | Where |
|---|---|
| R-MC-1 to R-MC-8 | MCP host (stdio + SSE), skills install/attach, harness probe (claude-code / codex / gemini / opencode), CLI, dashboard tabs. See `internal/mcp/`, `internal/skills/`, `internal/harnesses/`, `services/cli/` |

## R-MD — Modules + adapters

| ID | Where |
|---|---|
| R-MD-1 to R-MD-5 | Module enable flags, adapter swap via env, workload modules pattern, hot module list. See `internal/modules/`, `internal/config/`, `docs/workload-modules.md` |

## R-OB — Observability

| ID | Where |
|---|---|
| R-OB-1 to R-OB-6 | OTel spans, Prometheus metrics, audit log, log streaming, metrics summary, dashboard tabs. See `internal/observability/`, `/api/v1/metrics/summary`, `/api/v1/logs` |

## R-CL — CLI

| ID | Where |
|---|---|
| R-CL-1 | `af-stack` operator CLI — `services/cli/cmd/af-stack/` |
| R-CL-2 | Subcommands: mcp, harness, migrate — `services/cli/internal/` |

## NF-* — Non-functional

| ID | Requirement | Where |
|---|---|---|
| NF-1 | 60-second quickstart | `docs-site/src/content/docs/get-started/quickstart.md` (validated) |
| NF-2 | Apache 2.0 license | `LICENSE` |
| NF-3 | Production deploys (k8s + 3 PaaS) | `deploy/helm/`, `deploy/fly/`, `deploy/railway/`, `deploy/render/` |
| NF-4 | Graceful shutdown + drain | `internal/server/shutdown.go` |
| NF-5 | Multi-replica safe | `docs/multi-replica.md` + all worker `FOR UPDATE SKIP LOCKED` |
| NF-6 | < 200ms p95 on hot path | Verified via load test against `/api/v1/cost`; see `scripts/load-test.sh` |

## What we deliberately deferred

Nothing structurally — the PRD's 114 + 6 are all covered for v1. The
following are explicitly v1.1 work because they need community input:

- Webhook handlers via TypeScript (not just Python / Go).
- Additional LLM providers beyond the catalog (Mistral, DeepSeek, etc.).
- Additional MCP adapter shapes (websocket transport).
- Workload modules for Examples 02 / 04 / 05 — see
  [`docs/workload-modules.md`](/reference/workload-modules/) for the
  pattern; the modules themselves are post-launch.

## Verifying coverage

If you find a PRD requirement that's not actually delivered: open a
GitHub issue with the requirement ID and your reproduction. We'll
treat it as a launch-blocker.

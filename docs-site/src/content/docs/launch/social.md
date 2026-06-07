---
title: Launch social drafts
description: Twitter thread + Show HN drafts.
sidebar:
  order: 2
---

*Drafts. Replace placeholder links with the real ones before posting.*

## Twitter / X — single tweet (announcement)

> We open-sourced AF Stack — the backend you'd build under your AI app
> if you had four months to build it right.
>
> Multi-tenant from day one, OpenAI-compatible gateway, sandboxes,
> vector memory, billing, webhooks, dashboard. `docker compose up`.
>
> Apache 2.0: https://github.com/Agent-Field/backai

(255 chars without the URL.)

## Twitter / X — thread (8 tweets)

**1/8**
> We're open-sourcing AF Stack: the open backend for the AI era.
>
> Multi-tenant from day one. OpenAI-compatible. Sandboxes. Vector
> memory. Billing. Webhooks. A dashboard.
>
> `docker compose up`, sixty seconds, you have it all.

**2/8**
> Why did we build this?
>
> Every AI app team has the same six-month story: roll your own auth,
> realize you need MT, bolt on cost tracking, panic when a tenant
> abuses the LLM budget, spend two weeks on a sandbox.
>
> We wanted to skip all of it.

**3/8**
> The gateway is OpenAI-compatible. Point any SDK at it:
>
> ```
> client = OpenAI(base_url="http://localhost:38080/api/v1/llm",
>                  api_key="af_...")
> ```
>
> Routes across OpenRouter, OpenAI, Anthropic, Google. Per-call cost
> attribution. Cache included.

**4/8**
> Multi-tenancy is Postgres RLS keyed on a per-session GUC. Buggy
> handlers can't leak across tenants — the database enforces it.
>
> The dashboard shows per-tenant cost, usage, members, API keys,
> recent runs, all in one drilldown.

**5/8**
> Sandboxes: docker for dev, gVisor / Firecracker / e2b for prod.
> Vector memory: pgvector with tenant-scoped namespaces. Billing:
> Stripe + a stub mode so the page works before you have keys.
> Webhooks: HMAC verify, dedup, replay protection.

**6/8**
> Six examples ship. Three are ready: gateway-only, a full SaaS demo
> (Notable, 3 agents), a fan-out research agent.
>
> The remaining three (Shipwright, Podcast creator, Reactive
> enrichment) are scaffolded and need their workload modules.

**7/8**
> Production-ready: Helm chart, Fly.io spec, Railway template, Render
> Blueprint, docker-compose.prod, Caddy auto-TLS. Graceful shutdown.
> Multi-replica safe (FOR UPDATE SKIP LOCKED everywhere).

**8/8**
> Apache 2.0. No open core. Same code we run is the code you can fork.
>
> 60s quickstart: https://docs.af-stack.dev/get-started/quickstart
>
> GitHub: https://github.com/Agent-Field/backai

## Show HN

**Title:** Show HN: AF Stack — open-source Supabase for AI backends
(`docker compose up`)

**Post body:**

> Hi HN — I'm Santosh, one of the people behind AF Stack.
>
> AF Stack is what I wanted to exist when I last built an AI SaaS and
> ended up reinventing the same six pieces: multi-tenant auth, an LLM
> gateway with cost attribution, a vector memory layer that respects
> tenancy, sandboxes for agent-generated code, billing, webhooks. We
> open-sourced it under Apache 2.0.
>
> What's in the repo:
>
> - Runtime in Go. OpenAI-compatible gateway routing across OpenRouter
>   / OpenAI / Anthropic / Google. Per-call cost ledger. In-memory
>   cache. Budget enforcement.
> - Postgres with pgvector. Row-level security keyed on a per-session
>   GUC, so tenant isolation is at the database boundary.
> - Sandboxes with adapters for docker (dev), gVisor (prod), Firecracker
>   (hard MT), e2b (managed).
> - Background jobs with cron schedules. PG-backed queue, retry worker.
> - Webhooks both directions. HMAC + dedup + replay protection inbound;
>   PG outbox + exponential backoff outbound.
> - Notifications (log adapter for dev, Resend for prod).
> - Stripe billing with a stub mode so the dashboard works before you
>   have keys.
> - MCP host so your agents can talk to GitHub / Slack / internal tools.
> - Skills + harnesses (Claude Code / Codex / Gemini integration).
> - Operator dashboard in Next.js. Cost charts, run inspector, sandbox
>   activity, memory browser, audit log, customizable via plugins +
>   CSS variables.
> - Helm chart. Fly.io + Railway + Render templates. docker-compose.prod.
>   Caddy auto-TLS.
> - Two SDKs (Python + TypeScript). OpenAPI spec.
>
> Six examples ship: 03-llm-gateway-only (the minimal "just the
> gateway" deploy), 01-notable (full MT SaaS with 3 agents + custom
> dashboard plugin), 06-deep-research (composite-reasoning fan-out),
> plus 02/04/05 scaffolded.
>
> The big architectural premise is that intelligence is in the
> composition, not the components. Individual LLMs reason at 0.3 — 0.4
> on a normalised scale; composed harnesses score 0.7 — 0.8. The
> primitives (`.harness()` for stateful tool-using agents, `.ai()`
> for fast bounded classification) are shaped around that. Architecture
> overview: https://docs.af-stack.dev/architecture/overview
>
> Repo: https://github.com/Agent-Field/backai
>
> Caveats: not a vector database (pgvector embedded, not Pinecone-scale),
> not a workflow engine (we have a queue, not Temporal), not opinionated
> about your agent framework (the gateway is provider-shaped so anything
> OpenAI-compatible works).
>
> Happy to answer questions about the architecture, the multi-tenancy
> model, sandbox isolation choices, or where this fits relative to
> Supabase / Firebase / Portkey.

## LinkedIn (short, professional)

> Today we open-sourced AF Stack — the backend platform we wanted to
> exist when building AI-first SaaS.
>
> Multi-tenant from day one. OpenAI-compatible gateway with cost
> attribution. Sandboxes (gVisor / Firecracker / e2b). Vector memory.
> Stripe billing. Webhooks. Production-grade dashboard.
>
> Apache 2.0. `docker compose up`, sixty seconds.
>
> https://github.com/Agent-Field/backai

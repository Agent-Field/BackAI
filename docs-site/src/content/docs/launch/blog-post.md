---
title: 'AF Stack: the open backend for the AI era'
description: Launch blog post draft.
sidebar:
  order: 1
---

*A draft. Edit before publishing.*

---

Every serious software company will run an AI backend in five years.
Not "use OpenAI's API" — a real backend, with multi-tenant boundaries,
cost discipline, sandboxes for agent-generated code, and integrations
into the rest of the stack. The shape of that backend is settling, and
it doesn't look like what Supabase or Firebase ship today.

We built AF Stack to be that backend.

## The gap

The current options for an AI app builder are:

1. **Roll your own.** OpenAI behind an Express endpoint, Postgres, a
   homebrew rate limiter, three weeks tracking down where your cost
   went, multi-tenancy as a TODO. This is what most teams do, and it's
   why most teams don't have a multi-tenant SaaS to show six months
   later.

2. **Build on a BaaS.** Supabase or Firebase. They're excellent —
   for CRUD. The moment you need a sandbox for agent-generated code,
   a vector memory layer that respects tenancy, or per-call cost
   attribution, you're back to rolling your own on top.

3. **Build on an AI gateway.** Portkey, Helicone, LiteLLM. These solve
   the LLM proxy problem. They don't solve the rest of the backend.

There's no Supabase for AI backends. AF Stack is what we wanted that
to look like.

## What's in the box

```bash
git clone https://github.com/Agent-Field/backai af-stack
cd af-stack
docker compose up
```

Sixty seconds later you have:

- **An OpenAI-compatible LLM gateway** that routes across OpenRouter,
  OpenAI, Anthropic, and Google. Per-call cost ledger. In-memory
  cache. Budget enforcement.
- **Multi-tenancy** with PostgreSQL row-level security. Tenant
  isolation at the database boundary, not in your handler code.
- **A vector memory layer** built on pgvector. Scope by tenant, user,
  agent, or run.
- **Sandboxes** for agent-generated code. Docker for dev, gVisor /
  Firecracker / e2b for production.
- **Background jobs** with cron schedules, a PG-backed queue, and a
  retry worker.
- **Webhooks** in both directions. HMAC verification, dedup, replay
  protection on the inbound side. PG outbox + exponential backoff on
  the outbound side.
- **Notifications** with a log adapter for dev, Resend for prod, and a
  worker that drains queued sends.
- **Billing** with Stripe (or a stub when you don't have keys yet),
  per-tenant meters, customer portal links.
- **An MCP host** so your agents can talk to GitHub / Slack / your
  internal tools.
- **Skills + harnesses** for Claude Code / Codex / Gemini integration.
- **A production-grade dashboard** with cost charts, run inspector,
  sandbox activity, memory browser, audit log, plugin system.

You also get a Helm chart, four PaaS deploy configs (Fly / Railway /
Render / docker-compose.prod), graceful shutdown, an OpenAPI spec, and
two SDKs (Python + TypeScript).

## Hello world

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:38080/api/v1/llm",
    api_key="af_<your-key-prefix>_<your-secret>",
)

resp = client.chat.completions.create(
    model="qwen/qwen-2.5-72b-instruct",
    messages=[{"role": "user", "content": "Hello world"}],
)
print(resp.choices[0].message.content)
```

Open the dashboard. Click **Operate → Cost**. Your call is there.
Click into it. Tokens, USD, latency, tenant attribution. Open the
**Logs** tab to see the request lifecycle.

That's the loop. Build something, the dashboard tells you how it's
behaving.

## Six examples

We ship three ready-to-run examples and three more scaffolded:

1. **[03 — LLM gateway only](https://github.com/Agent-Field/backai/tree/main/examples/03-llm-gateway-only)**
   — the minimal compose, gateway only.
2. **[01 — Notable](https://github.com/Agent-Field/backai/tree/main/examples/01-notable)**
   — a full multi-tenant SaaS with three AF agents (summarize,
   suggest_tags, todo_completer), memory, billing, a dashboard plugin.
3. **[06 — Deep research](https://github.com/Agent-Field/backai/tree/main/examples/06-deep-research)**
   — a long-running fan-out / synthesise agent that demonstrates the
   composite-reasoning pattern.
4. **02 — Shipwright** — SWE-AF as a SaaS (workload module pending).
5. **04 — Podcast creator** — ffmpeg + Whisper + Vision pipeline
   (multimodal-storage workload module pending).
6. **05 — Reactive enrichment** — PG + Mongo change stream subscriber
   (change-stream-listener workload module pending).

Workload modules are documented at
[`docs/workload-modules.md`](https://github.com/Agent-Field/backai/blob/main/docs/workload-modules.md);
the three pending examples are blocked on shipping those modules.

## Why composite reasoning

Individual LLMs reason at ~0.3 — 0.4 on a normalised scale. Composed
harnesses score 0.7 — 0.8 for specific domains. The intelligence is in
the composition, not the components.

We hold this premise so seriously that the framework primitives are
shaped around it. There are two:

- `.harness()` — stateful, multi-turn, tool-using. The orchestrator
  gives it a goal and verifies the outcome.
- `.ai()` — single-shot, no tools, Pydantic schema in, Pydantic schema
  out. For classification, routing, gates.

The
[Atomic Unit of Intelligence](https://www.santoshkumarradha.com/writing/atomic-unit-of-intelligence)
explains why the harness — not the LLM call — is the right atom. AF
Stack's runtime is the substrate that makes orchestrating harnesses
boring.

## Open source

Apache 2.0. No "open core". No "community edition". The same code we
run is the code you can fork.

We chose this because the platform is more useful as a default than as
a SaaS. If you want managed AF Stack, we hope to offer it; the
self-host story has to work first.

## What's next

- Validate the quickstart with external testers.
- Ship workload modules for 02 / 04 / 05.
- Demo video (5 — 10 minutes, screen recording of the dashboard +
  example walkthroughs).
- HN / Twitter / launch newsletter.

If you build something on AF Stack, we want to see it. PRs, issues, or
just a tweet — we read them.

[Get started](https://github.com/Agent-Field/backai) /
[Docs](https://docs.af-stack.dev) /
[Examples](https://github.com/Agent-Field/backai/tree/main/examples)

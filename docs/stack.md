# BackAI — Technical Layer Stack

The same shape Supabase, Firebase, Plane, and Cal.com use: a small number
of **horizontal bands**, each band labeled by concern. Services that sit
at the same logical layer go in the same band. Each band lists the open
source we use to fill it.

AgentField sits in the middle "Intelligence" band as one of the open-
source pieces, alongside LiteLLM and the MCP spec. No special framing.

---

## The stack — 5 bands, services as columns

```
┌─────────────────────────────────────────────────────────────────────┐
│                                                                     │
│  ① CLIENT                                                           │
│                                                                     │
│  Operator Dashboard · Customer App · Docs Site · SDKs · CLI         │
│  (Next.js · React · shadcn/ui · Scalar · Astro Starlight ·          │
│   Python · TypeScript · Go — planned)                               │
│                                                                     │
└─────────────────────────────────┬───────────────────────────────────┘
                                  │  HTTPS
┌─────────────────────────────────▼───────────────────────────────────┐
│                                                                     │
│  ② EDGE                                                             │
│                                                                     │
│  Caddy   (TLS termination, reverse proxy, auto-renew certs)         │
│                                                                     │
└─────────────────────────────────┬───────────────────────────────────┘
                                  │
┌─────────────────────────────────▼───────────────────────────────────┐
│                                                                     │
│  ③ API GATEWAY                                                      │
│                                                                     │
│  af-stack runtime  (Go single binary)                               │
│  ├─ HTTP routing + OpenAPI 3.1                                      │
│  ├─ better-auth          (sessions, OAuth, magic links)             │
│  ├─ Postgres RLS         (multi-tenancy via session GUC)            │
│  ├─ Audit log            (every admin mutation)                     │
│  └─ Secrets vault        (AES-256-GCM envelope)                     │
│                                                                     │
└────┬────────────────┬─────────────────┬───────────────┬─────────────┘
     │                │                 │               │
┌────▼────┐    ┌──────▼─────┐    ┌──────▼──────┐   ┌────▼──────────┐
│         │    │            │    │             │   │               │
│ ④       │    │ ⑤          │    │ ⑥           │   │ ⑦             │
│ INTELLI │    │ EXECUTION  │    │ DELIVERY    │   │ OBSERVABILITY │
│ -GENCE  │    │            │    │             │   │               │
│         │    │            │    │             │   │               │
│ Agent-  │    │ Sandboxes  │    │ Webhooks    │   │ OpenTelemetry │
│ Field   │    │  Docker    │    │ (native     │   │ (traces)      │
│         │    │  gVisor    │    │  outbox)    │   │               │
│ LiteLLM │    │  Firecrkr  │    │             │   │ Prometheus    │
│         │    │  e2b       │    │ Resend      │   │ (metrics)     │
│ MCP     │    │            │    │ (email)     │   │               │
│         │    │ River      │    │             │   │ slog          │
│ Harnes- │    │ (jobs)     │    │ Stripe      │   │ (logs)        │
│ ses     │    │            │    │ Lago        │   │               │
│ (Claude │    │ robfig/    │    │ (billing)   │   │               │
│  Codex  │    │ cron       │    │             │   │               │
│  Gemini)│    │            │    │             │   │               │
│         │    │ Webhooks   │    │             │   │               │
│         │    │ IN (HMAC)  │    │             │   │               │
└────┬────┘    └──────┬─────┘    └──────┬──────┘   └────┬──────────┘
     │                │                 │               │
     └────────────────┴────────┬────────┴───────────────┘
                               │
┌──────────────────────────────▼──────────────────────────────────────┐
│                                                                     │
│  ⑧ DATA                                                             │
│                                                                     │
│  Postgres 16 + pgvector  (relational · vector · queue · FTS)        │
│  MinIO / S3              (object storage)                           │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

Eight bands. Forty-something products. One repo.

---

## What lives in each band

### ① Client

Everything a human or another program talks to.

| What | OSS we use |
|---|---|
| Operator console | Next.js, React, shadcn/ui, base-ui, Scalar (API browser) |
| Customer-facing app | Same stack — forkable, separate brand |
| Docs site | Astro Starlight |
| SDKs | Hand-rolled Python, TypeScript (Go planned — stub today) |
| CLI | Hand-rolled Go |

### ② Edge

The door. Terminates TLS, routes to the right app, auto-renews certs.

| What | OSS we use |
|---|---|
| Reverse proxy + TLS | **Caddy** |

### ③ API Gateway

The Go runtime. The conductor. Owns request routing, OpenAPI generation,
and the four cross-cutting concerns every request crosses: auth, tenancy,
audit, secrets.

| What | OSS we use |
|---|---|
| HTTP routing + middleware | af-stack Go runtime (single binary) |
| Identity | **better-auth** |
| Multi-tenancy + AuthZ | **Postgres Row-Level Security** keyed on session GUC |
| Audit log | Hand-rolled (small, tenant-aware) |
| Secrets vault | AES-256-GCM envelope encryption (Go stdlib) |

### ④ Intelligence

The AI services. Where this stack differs from a regular SaaS backend.

| What | OSS we use |
|---|---|
| Agent runtime, memory (4 scopes), runs, spans | **AgentField** |
| LLM provider routing (100+ providers, virtual keys, budgets) | **LiteLLM** |
| Tool protocol (stdio + SSE) | **Model Context Protocol** |
| Coding harnesses | **claude-code**, **codex**, **gemini-cli**, **opencode** |

### ⑤ Execution

Compute and async work.

| What | OSS we use |
|---|---|
| Sandboxes (isolated code exec) | **Docker** (dev), **gVisor**, **Firecracker** + Flintlock, **e2b** |
| Job queue | **River** (Go, PG-backed) |
| Cron scheduler | **robfig/cron v3** |
| Inbound webhooks (HMAC + dedup) | Hand-rolled |

### ⑥ Delivery

Outbound to the world.

| What | OSS we use |
|---|---|
| Outbound webhooks | Hand-rolled (native in-process outbox: PG-backed queue, HMAC signing, retries with exponential backoff, delivery ledger) |
| Notifications (email / SMS / push) | Adapter pattern: log → **Resend** |
| Billing | Adapter pattern: **Stripe** → **Lago** |

### ⑦ Observability

What's actually happening inside the runtime.

| What | OSS we use |
|---|---|
| Traces | **OpenTelemetry** |
| Metrics | **Prometheus client_golang** |
| Logs | **slog** (Go stdlib) |

### ⑧ Data

Persistence. Two stores; we lean on Postgres heavily.

| What | OSS we use |
|---|---|
| Relational + vector + queue + FTS | **Postgres 16 + pgvector** (one database, four jobs) |
| Object storage | **MinIO** (dev) → **AWS S3 / R2 / GCS / Azure Blob** (prod) |

---

## How a request flows through the bands

**A customer LLM call** (the common case) crosses bands top → middle →
data:

```
Client ──▶ ① Customer app calls suite.llm.chat()
        ──▶ ② Caddy terminates TLS
        ──▶ ③ API Gateway:  better-auth ─▶ tenant resolver ─▶ secrets
                                                              ─▶ audit row
        ──▶ ④ Intelligence: AgentField records run ─▶ LiteLLM ─▶ provider
        ──▶ ⑧ Data: cost row written, span closed
        ◀── Response + cost headers
```

**A Shipwright coding task** (the harder case) fans out into Execution
and Intelligence:

```
Client ──▶ ① POST /shipwright/tasks
        ──▶ ②/③ Caddy + Gateway middleware
        ──▶ ⑤ River enqueues a job  (returns 202 immediately)
        ◀── 202 Accepted

[worker picks it up]
        ──▶ ⑤ Sandbox spins up (Docker dev / gVisor prod)
                  └─ runs claude-code harness inside container
                       ──▶ ④ LiteLLM calls
                       ──▶ ④ AgentField records every span + tool call
                       ──▶ ⑧ Postgres + MinIO reads/writes
        ──▶ ⑥ Native outbox delivers a "task completed" webhook
```

The pattern: **gateway routes, services specialize, data persists**.

---

## How a developer extends each band

Five extension points, one per typical thing you'd add:

| You want to… | You touch band(s) | How |
|---|---|---|
| Add an AI agent | ④ Intelligence | Drop `apps/backend/agents/<name>/` — agent registers with AgentField at startup, callable at `/api/v1/agents/<name>.<reasoner>` |
| Add a dashboard tab | ① Client | Drop `apps/dashboard/plugins/<id>/plugin.ts` + `page.tsx` — sidebar picks it up at next build |
| Add a workload module | ③ API + ⑧ Data | Drop `workload-modules/<id>/manifest.yaml` + Go handler + migrations — loader mounts at `/workload/<id>/...` |
| Swap an adapter | various | One env var (`AF_STACK_SANDBOX_ADAPTER=gvisor`, `AF_STACK_S3_ADAPTER=s3`, `AF_STACK_BILLING_ADAPTER=lago`, etc.) |
| Theme it | ① Client | `apps/dashboard/src/app/brand.css` with CSS variable overrides — every shadcn primitive + chart inherits |

---

## Image-generator prompt (8-band visual)

Paste this into your image generator. The result is a tall, clean
Supabase-style stack diagram with logos in their natural bands.

> A clean, minimal **technical stack diagram** for a self-hostable AI
> backend platform called **BackAI**, in the visual style of Supabase /
> Vercel / Linear marketing material. Light off-white background, single
> blue accent for the title bar, generous whitespace, rounded-corner
> slabs with very subtle drop shadows. **Eight horizontal bands stacked
> vertically**, connected by thin vertical arrows between bands.
>
> The middle bands (④ through ⑦) sit **side-by-side in a single row of
> four columns**, like a service row. The outer bands (①, ②, ③, and ⑧)
> span the full width above and below that row. Final layout shape: tall
> portrait, roughly **3:4 aspect ratio**.
>
> Each band has:
> - a **circled number** (top-left, small, monospace) — ①, ②, …, ⑧
> - a **band title** (bold sans-serif, ~20pt) — the all-caps name below
> - a **row of official product logos** in their real brand colors, ~32px
>   tall, evenly spaced
> - product names in small sans-serif under each logo
>
> Render the bands in this exact order, top to bottom, with the listed
> logos:
>
> **Band ① — CLIENT (full width)**
> Logos: Next.js, React, shadcn/ui, Scalar, Astro Starlight, Python,
> TypeScript, Go (Go SDK is planned — a stub today)
>
> **Band ② — EDGE (full width)**
> Logos: Caddy
>
> **Band ③ — API GATEWAY (full width)**
> Logos: Go (gopher), better-auth, PostgreSQL (with a small shield
> overlay to suggest RLS)
> Caption strip below the logos: "af-stack runtime · routing · OpenAPI ·
> audit · secrets"
>
> **Middle row — four columns side-by-side, equal width:**
>
> **Band ④ — INTELLIGENCE**
> Logos: AgentField (magenta wordmark), LiteLLM, Model Context Protocol,
> Claude (Anthropic), OpenAI (Codex), Gemini (Google)
>
> **Band ⑤ — EXECUTION**
> Logos: Docker, gVisor, Firecracker, e2b, River, a small clock icon
> labeled "robfig/cron"
>
> **Band ⑥ — DELIVERY**
> Logos: a small outbound-arrow/webhook icon labeled "Webhooks", Resend,
> Stripe, Lago
>
> **Band ⑦ — OBSERVABILITY**
> Logos: OpenTelemetry, Prometheus, a small terminal-prompt icon labeled
> "slog"
>
> **Band ⑧ — DATA (full width, foundational)**
> Logos: PostgreSQL (large, prominent — this is the foundation), pgvector
> wordmark, MinIO, AWS S3
>
> At the very top, title bar in a single soft-blue rectangle: **"BackAI"**
> (bold, white on blue) with a subtitle "The open backend for AI
> products" (smaller, slightly transparent white).
>
> At the very bottom, a single thin line of small text: "Apache 2.0 ·
> self-hostable · one `docker compose up`".
>
> Style notes: no 3D, no isometric, no gradients except the title bar.
> Crisp 1px borders on slabs. Connection arrows are thin dashed gray.
> The middle row of four columns has thin vertical separators between
> columns. Whitespace ~16px between bands. Output as high-resolution
> PNG, aspect ratio 3:4 portrait.

---

## Why this shape

This is the same mental model as every successful backend platform:

| Platform | Their bands |
|---|---|
| **Supabase** | Studio → APIs → (Auth · DB · Storage · Realtime · Functions · Vector) → Postgres |
| **Firebase** | Build → Release → Engage → Analytics |
| **Plane / Cal.com / Outline** | Frontend → API → Services → Postgres |
| **BackAI** | Client → Edge → API → (Intelligence · Execution · Delivery · Observability) → Data |

Two differences from Supabase that matter:

1. **Intelligence is a peer band.** AgentField + LiteLLM + MCP sit
   alongside Execution and Delivery, not bolted on the side. That's the
   shape that lets us claim "complete backend for AI products."
2. **Postgres is even more load-bearing.** We use it for relational,
   vector, queue, FTS, and audit — same image. One operational target
   instead of five.

Everything else is recognizable backend-template shape. That's the
point: a developer looking at this diagram should think "I know how to
extend this" within ten seconds.

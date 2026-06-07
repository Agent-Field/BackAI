---
title: Introduction
description: What AF Stack is, who it's for, and what's in the box.
sidebar:
  order: 1
---

AF Stack is an open backend for building AI products. It bundles the
unglamorous pieces every serious AI app needs — multi-tenancy, an
OpenAI-compatible gateway, cost tracking, auth, secrets, jobs, crons,
memory, sandboxes, dashboards — into a single self-hosted deploy. You
clone the repo, run `docker compose up`, and you have the operational
spine of a multi-tenant SaaS already wired up. From there you build
your actual product on top, not the seventh implementation of API key
rotation.

## What it is

AF Stack is a small set of services that fit together:

- A **runtime** (Go) that exposes one HTTP API: an OpenAI-compatible
  LLM gateway, a job runner, a cron scheduler, a sandbox launcher, and
  endpoints for memory, secrets, webhooks, and notifications. Every
  call is tenant-scoped at the database boundary via PostgreSQL row
  level security, not by string-matching tenant ids in app code.
- A **dashboard** (Next.js) for operators. Cost per tenant, job graphs,
  live agent runs, secret rotation, billing, plugins. Themed entirely
  through CSS variables, so a reskin is a single file.
- **PostgreSQL** as the only required datastore. Sessions, tenants,
  costs, jobs, crons, memory, and audit logs all live there. No Redis,
  no Kafka, no separate vector DB. The vector store is `pgvector`.
- **MinIO** (or any S3) for blob storage. Sandbox stdout, webhook
  payloads, file uploads.
- An optional **AgentField control plane** if you want the full
  reasoner/agent network. The runtime stands on its own without it.

## Who it's for

Two audiences.

The first is a small team — one to ten engineers — building a
multi-tenant AI product. You need real auth, real billing meters, real
cost attribution per customer, and a place for operators to actually
look at the system. You don't want to spend a quarter reimplementing
Stripe webhooks. AF Stack gives you the parts and stays out of your
way.

The second is a platform team inside a larger org. You already have
Postgres and Kubernetes. You want the AI-specific infrastructure
(gateway, sandbox, memory, agent orchestration) without taking a
dependency on a hosted vendor. The Helm chart deploys with HPA, PDB,
NetworkPolicy, and ServiceMonitor out of the box. Bring your own
Postgres and S3, install the chart, point your app at it.

If you're building a single-tenant, single-user toy, AF Stack is
overkill. Use the OpenAI SDK directly.

## What's in the box

Five things you don't have to build:

- **OpenAI-compatible LLM gateway.** Drop-in `/chat/completions`,
  embeddings, streaming, tools. Per-tenant cost ledger, in-process
  exact-match cache, configurable budgets.
- **Multi-tenant primitives.** Tenants, users, memberships, API keys,
  secrets vault (envelope-encrypted), audit log. All RLS-scoped.
- **Workload modules.** Drop a directory under `workload-modules/<id>/`
  with a `manifest.yaml`, some migrations, and a handler. The runtime
  loads it at boot. Used by the Notable, Podcast, and Reactive
  Enrichment examples.
- **Sandbox adapters.** Run untrusted code in `docker`, `gvisor`,
  `firecracker`, or hosted `e2b`. Pick by config; the rest of the
  runtime doesn't care.
- **Operator dashboard.** Cost, jobs, crons, secrets, memory,
  notifications, webhooks, plugins, settings. Theme it with one CSS
  file. Extend it with a plugin folder.

## Where to go next

Get the stack running locally first. The
[60-second quickstart](/get-started/quickstart/) clones the repo,
brings up the compose stack, and walks you through the first LLM call
through the gateway. After that, the
[architecture overview](/get-started/architecture/) explains how the
pieces fit together so you know where to put your own code.

If you're already convinced and want to deploy, jump straight to
[Deploy](/deploy/). If you want to see real product code, the
[examples](https://github.com/Agent-Field/backai/tree/main/examples)
are the canonical reference for how an AF Stack app actually looks.

# Multi-Replica Deployment

When and how to scale AF Stack horizontally. The runtime is the
component that benefits from replicas; the dashboard rarely needs more
than 2.

## What scales how

| Component | Scales? | Why |
|---|---|---|
| Runtime | Yes — horizontal | Stateless HTTP. Workers (notifications, webhook outbound, crons, sandbox refresh, MCP pool) use `FOR UPDATE SKIP LOCKED` so multiple replicas claim work safely. |
| Dashboard | Yes — but rarely needed | Stateless Next.js. Sits behind a CDN typically. Two replicas for HA, not throughput. |
| Postgres | Vertical first, then read replicas | The bottleneck for everything. Vertical scaling (more cores + RAM) buys you orders of magnitude before read replicas pay off. |
| MinIO / S3 | Out of scope | Use a managed service. AF Stack reads / writes blobs; the storage layer is the storage layer's problem. |
| AgentField control plane | Yes — horizontal | Per AF's own scaling model. |

## Recommended starting point

| Tier | Runtime replicas | Dashboard replicas | Postgres |
|---|---|---|---|
| Dev / staging | 1 | 1 | t3.small / db-f1-micro |
| Production small (< 100 tenants) | 2 (HPA 2 → 4) | 2 | db.t3.medium / db-g1-small |
| Production medium (100 – 1k tenants) | 3 (HPA 3 → 8) | 2 | db.m5.large + read replica |
| Production large (1k+ tenants) | HPA 5 → 20 | 2 | db.m5.2xlarge + 2 read replicas + connection pooler |

The Phase 14.1 Helm chart's `values-prod.yaml` ships HPA tuned for
"production small". Override at the values level when you outgrow it.

## What multiple replicas share

### Database

All replicas talk to the same Postgres. Connection pooling matters
once you have more than 3 runtime pods:

- Pool max per pod: 25 (runtime default).
- At 3 pods × 25 = 75 connections, comfortably under most managed PG
  defaults.
- At 8 pods × 25 = 200 connections — add PgBouncer in transaction mode
  in front of Postgres to multiplex.

The runtime uses pgx's built-in pool. Tune via the `?pool_max_conns=25`
query string on `AF_STACK_DATABASE_URL`.

### better-auth sessions

Sessions live in Postgres (`session` table). All replicas see the same
session state on every request — no sticky-session requirement.

### Background workers

| Worker | Multi-replica safety |
|---|---|
| Notifications drain | Safe — uses `FOR UPDATE SKIP LOCKED` |
| Webhook outbound | Safe — same pattern |
| Cron scheduler | Safe — `app.bypass_rls=on` + `SKIP LOCKED` per claim, advances `next_run_at` atomically |
| Sandbox pool refresh | Safe — each pod refreshes its own in-memory list independently |
| MCP pool refresh | Safe — same |

No leader election is needed in v1. If you eventually run >10 runtime
replicas and the polling cost adds up, consider a `replicaIndex` env
var so workers shard work, OR introduce leader election via
[`distributed-cron`](https://github.com/wedeploy/distributed-cron) or a
PG advisory lock.

### LLM cache

The cache (`suite_llm_cache`) is in Postgres, so all replicas hit the
same cache. Exact-match v1 means a cache write from pod A is visible
to pod B on its next read.

## Sticky sessions

**Don't enable them.** The dashboard's session resolution and the
runtime's API-key auth are both stateless. Sticky sessions hide bugs
and break drain mode.

## Graceful rollouts

Kubernetes:
- Rolling update with `maxSurge=25%` + `maxUnavailable=0%` is the
  default in the chart.
- `terminationGracePeriodSeconds: 60` matches `AF_STACK_DRAIN_TIMEOUT=30`
  + 30s buffer.
- `preStop` flips /ready to 503 immediately (so the load balancer
  stops sending new traffic) before SIGTERM.

Manual / docker compose:
- Spin up the new replica with a different port.
- Wait for /ready to return 200 on the new replica.
- Send SIGTERM to the old replica.
- Wait for the drain timeout.
- Stop the old replica.

## When to scale up

Watch these metrics:

| Metric | Signal |
|---|---|
| `http_p95_ms` from `/api/v1/metrics/summary` | > 200ms sustained = scale up |
| `goroutines` | > 2000 sustained = check for a leak before scaling |
| `heap_alloc_bytes` | Trending up across deploys = memory leak; profile |
| PG connection count | > 70% of max = add PgBouncer |
| Cron lag (oldest `next_run_at` in the past) | > 5 minutes = scheduler bottleneck (rare) |

The Phase 12.2 metrics tab surfaces these without leaving the dashboard.

## Limits we know about

- **Webhook outbound throughput** is capped at 32 deliveries per 2s tick
  per pod (Phase 10.3 worker config). At 3 pods that's 48 / second
  steady-state. For higher throughput, bump the batch size in the
  worker config.
- **Notifications drain** is the same shape; same headroom math.
- **Sandbox concurrency** depends on the adapter:
  - Docker (dev): bounded by host cores.
  - gVisor: same.
  - Firecracker: bounded by VM density on the host.
  - e2b: bounded by your e2b plan's parallel sandbox limit.

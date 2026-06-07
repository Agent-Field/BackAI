---
title: Module — Jobs
description: River-backed Postgres job queue. Name-based dispatch, multi-language workers, cron periodic jobs.
sidebar:
  order: 11
---

River-backed Postgres job queue. Name-based dispatch; multi-language workers (Go natively, others via subprocess); periodic jobs via River's `PeriodicJobs`.

## What it does

`jobs.Manager` wraps the River client and exposes the runtime's high-level surface: Enqueue, Get, List, Retry, Cancel, Summary. It owns:

- the **Registry** of job definitions (name, language, cron),
- the **River client** (Postgres queue, scheduler, fetcher),
- **cron tickers** for non-Go definitions (Go cron uses River's `PeriodicJobs` natively).

Lifecycle: `NewManager()` → `Register*(...)` → `Start(ctx)` → `Stop(ctx)`.

REST handlers in `services/runtime/internal/server/jobs.go` translate the Manager's results into `JobSchema` (from `apps/dashboard/src/lib/api.ts`).

## Configuration

No dedicated module flag. The Manager starts whenever a DB pool is present at boot.

## REST endpoints

Registered directly in `services/runtime/internal/server/server.go`:

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/jobs` | Enqueue a job. |
| `GET` | `/api/v1/jobs` | List jobs. |
| `GET` | `/api/v1/jobs/definitions` | List job kind definitions + stats. |
| `GET` | `/api/v1/jobs/{id}` | Get a single job. |
| `POST` | `/api/v1/jobs/{id}/retry` | Mark a job retryable. |

## Database tables

River-owned tables (created by `migrations.go` at boot). No `suite_jobs_*` table — River's own schema covers everything.

## Env vars

None directly.

## Code map

- `client.go` — `Manager`: Enqueue, Get, List, Retry, Cancel, Summary.
- `definitions.go` — definition shape (name, language, cron).
- `registry_test.go` — registry semantics under test.
- `worker.go` — Go-native worker glue.
- `migrations.go` — River schema bootstrap.

## Related

- Enqueued from [Crons](./crons/) ticks.

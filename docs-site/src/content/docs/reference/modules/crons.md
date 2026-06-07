---
title: Module — Crons
description: DB-backed schedules that enqueue named jobs on a once-per-minute tick.
sidebar:
  order: 12
---

DB-backed cron schedules. One row in `suite_crons` = "every `<schedule>`, enqueue job `<job_name>` with `<args>`". A single goroutine ticks once per minute, claims due rows, fires the enqueuer, writes `last_run_at` + `next_run_at`.

## What it does

`crons.Scheduler` owns the tick loop. The dashboard mutates rows directly through the REST handlers; the scheduler picks changes up on the next tick.

Schedule parsing uses `robfig/cron/v3` in standard 5-field mode + the `@hourly` / `@daily` / `@weekly` / `@monthly` / `@yearly` macros. Seconds-resolution dialect is NOT enabled.

When no store is wired, mutating endpoints return `503`; reads return empty pages.

Wire shapes (`CronSchema`, `CronListSchema`, `CreateCronInputSchema`) mirror `apps/dashboard/src/lib/api.ts`.

## Configuration

No dedicated module flag. The scheduler runs whenever a DB pool exists at boot.

## REST endpoints

Registered in `services/runtime/internal/server/crons.go`:

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/crons` | List schedules. |
| `POST` | `/api/v1/crons` | Create a schedule. |
| `GET` | `/api/v1/crons/{id}` | Get a single schedule. |
| `PUT` | `/api/v1/crons/{id}/active` | Toggle active. |
| `DELETE` | `/api/v1/crons/{id}` | Delete a schedule. |

## Database tables

Owned by migration `00014_crons.sql`:

- `suite_crons` — id, tenant, name, schedule, job_name, args JSONB, active, last_run_at, next_run_at.

## Env vars

None directly.

## Code map

- `interface.go` — wire types + `Scheduler` shape.
- `scheduler.go` — tick loop, claim-due, enqueue.
- `store.go` — Postgres queries.

## Related

- Enqueues into [Jobs](./jobs/) via a `JobEnqueuer` callback.

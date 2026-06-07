---
title: Module — DB Studio
description: Read-mostly DB introspection + SQL runner powering the dashboard's Database tab.
sidebar:
  order: 17
---

Read-mostly DB introspection + SQL runner behind the dashboard's "Database" tab.

## What it does

`dbstudio.Studio` wraps a `*pgxpool.Pool` and exposes four operations:

- `ListTables` — every user table/view/matview with row-estimate + size.
- `Table` — columns, indexes, RLS policies for a single relation.
- `Rows` — paginated `SELECT *` returning a `SQLRunResult`.
- `RunSQL` — arbitrary SQL with a read-only safety wrapper.

Output is encoded with snake_case JSON tags so the wire format matches `DBTableSchema` / `DBTableDetailSchema` / `SQLRunResultSchema` in `apps/dashboard/src/lib/api.ts`.

When no DB pool exists, `/api/v1/db/*` returns `503 DB_STUDIO_NOT_CONFIGURED`.

### Safety

Identifiers used in dynamically built SQL MUST be escaped via `pgx.Identifier.Sanitize()`. `Studio.validateIdent` guards every `Rows()` entry point with `^[a-zA-Z_][a-zA-Z0-9_]*$` and returns `ErrInvalidIdentifier` for anything else. `RunSQL` runs in a read-only transaction.

## Configuration

No dedicated module flag. Studio is constructed whenever the runtime has a DB pool at boot.

## REST endpoints

Registered in `services/runtime/internal/server/dbstudio.go`:

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/db/tables` | List tables/views/matviews. |
| `GET` | `/api/v1/db/tables/{schema}/{name}` | Columns + indexes + RLS for a single table. |
| `GET` | `/api/v1/db/rows` | Paginated row dump. |
| `POST` | `/api/v1/db/sql` | Run arbitrary SQL (read-only wrapped). |

## Database tables

None owned. Operates over the live database catalog.

## Env vars

None directly.

## Code map

- `studio.go` — `Studio` struct + all four operations.

## Related

- Backed by the same pool as [Multi-tenancy](./multi-tenancy/), [Cost](./cost/), [Memory](./memory/), [Jobs](./jobs/), and most other modules.

# Workload Modules

Workload modules are the way BackAI pulls in domain-specific features
(notes, podcast jobs, reactive enrichments, etc.) without forking the
runtime. Each module is a directory you drop under
`workload-modules/<id>/`, and it is **declarative**: a manifest that names
typed resources, plus the versioned SQL that backs them. The runtime scans
that directory at boot, applies each enabled module's migrations, and
auto-generates tenant-scoped CRUD behind the same auth + tenancy chain as
the built-in surfaces. Straight CRUD needs no handler code at all.

`workload-modules/notes/` is the worked reference that ships in the repo.
Copy it when you start your own.

## What the runtime does at boot

1. **Discover.** It scans `<WORKLOAD_MODULES_PATH>` (env
   `WORKLOAD_MODULES_PATH`, default `./workload-modules`) for
   `<id>/backai.module.yaml`. That filename is fixed — nothing else is
   discovered.
2. **Validate.** Each manifest is parsed with unknown keys rejected. An
   invalid manifest disables *that module only*: the runtime logs it and
   keeps serving everything else.
3. **RLS-lint.** Every `CREATE TABLE` in the module's migrations is
   statically checked for a `tenant_id` column, `ENABLE` + `FORCE ROW
   LEVEL SECURITY`, and at least one `CREATE POLICY`. A tenantless table
   is refused *before* any DDL runs, and only that module is skipped.
4. **Migrate.** Pending `migrations/*.sql` are applied, each in its own
   transaction, and recorded in the platform-owned
   `suite_module_migrations` table keyed by `(module_id, version)`.
5. **Mount.** Each resource gets five routes under
   `/api/v1/workload/<id>/<resource>`, registered in the OpenAPI spec.

Inspect the result at `GET /api/v1/admin/modules` (operator key): id,
name, version, enabled, health, and migration state (`applied`,
`pending`, `error`, `skipped`) per discovered module.

## Enabling a module

Scaffolds — and the `notes` reference — ship `enabled: false`, so a
discovered module never auto-serves. A module is active when **either**:

- its manifest sets `enabled: true`, **or**
- its id appears in the operator's enabled list: `modules.workload_modules`
  in `config.yaml`, or the env override
  `AF_STACK_WORKLOAD_MODULES=notes,billing-ops`.

Restart the runtime to apply. Disabling removes the routes; it does not
drop the table or the data.

## Directory layout

`af-stack module new <id>` writes exactly three files:

```
workload-modules/<id>/
  backai.module.yaml         # required — the declarative manifest
  migrations/00001_init.sql  # the table(s) your resources are backed by
  README.md
```

Migration files must be named `<version>_<description>.sql` with a numeric
prefix; they are applied in version order. A module may ship no migrations
at all, but a resource whose table does not exist fails on first query.

## `backai.module.yaml`

```yaml
id: notes
name: Notes
version: 0.1.0
description: Reference workload module — a tenant-scoped notes resource.
enabled: false
migrations: migrations        # optional; defaults to "migrations"

resources:
  # Backing table follows the <module>_<resource> convention: notes_notes.
  - name: notes
    fields:
      - name: title
        type: string
        required: true
      - name: body
        type: string
      - name: done
        type: bool
        default: false
```

What the parser enforces:

- `id`, `name`, `version` and at least one resource are required. `id` is
  lowercase alphanumeric with `-` / `_` separators; `version` is
  semver-ish (`N`, `N.N`, or `N.N.N` with an optional suffix).
- Field types are `string`, `int`, `bool`, `timestamp`, `json`.
- `id`, `tenant_id`, `created_at` and `updated_at` are **reserved** — the
  runtime manages those columns, so a manifest must not redeclare them as
  fields.
- Unknown keys are an error. The old imperative shape (`requires:`,
  `routes:`, `handler:`, `meters:`) is not accepted; `af-stack module
  validate` calls that shape out by name.

Validate offline, before you boot anything:

```bash
af-stack module validate workload-modules/notes     # add --json for a report
```

## The generated routes

For the resource `notes` in the module `notes`:

| Method | Path | Action |
| --- | --- | --- |
| GET | `/api/v1/workload/notes/notes` | list |
| POST | `/api/v1/workload/notes/notes` | create |
| GET | `/api/v1/workload/notes/notes/{id}` | get |
| PATCH | `/api/v1/workload/notes/notes/{id}` | update |
| DELETE | `/api/v1/workload/notes/notes/{id}` | delete |

List takes `?limit=` (default 50, capped at 200) and `?offset=`, and
returns `{items, total, limit, offset, has_more}`.

Every query is filtered by the tenant the request resolver bound, and the
table's RLS policy enforces the same thing inside Postgres. A client can
neither set `tenant_id` nor reach another tenant's rows.

## The migration

Your migration creates the backing table, and it has to satisfy the RLS
lint. Copy the shape the scaffold emits:

```sql
create table if not exists notes_notes (
  id         uuid        primary key default gen_random_uuid(),
  tenant_id  uuid        not null,
  title      text        not null,
  body       text,
  done       boolean     not null default false,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists notes_notes_tenant_idx
  on notes_notes (tenant_id, created_at desc);

alter table notes_notes enable row level security;
alter table notes_notes force row level security;

create policy tenant_isolation on notes_notes
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );
```

## Beyond CRUD

The manifest is declarative only. There is no in-module Go or Python
handler contract, and no `crons:` field. When a module needs behavior
rather than storage, reach for the surface that owns it:

- **Custom logic / LLM work** → an AgentField agent under
  `apps/backend/agents/<name>/`, invoked via `suite.agents.*`.
- **Scheduled work** → crons are rows in `suite_crons`, created through
  the API / `suite.crons.*` SDK and dispatched by the runtime's scheduler
  (robfig/cron v3, 60s tick, multi-replica safe). See
  [dx/jobs.md](dx/jobs.md#crons).
- **Background work** → the River-backed jobs queue.

## Removing a module

Set `enabled: false` in the manifest (and drop the id from the enabled
list). The routes disappear on the next boot; the data and the applied
migration rows stay, so you can re-enable without loss. To remove it for
good, delete the `workload-modules/<id>/` directory and drop the tables
with your own migration.

## Modules in the repo

- `workload-modules/notes/` — the worked reference: manifest, migration,
  README. Ships `enabled: false`, so enable it before you call it.
- `workload-modules/git-workload/` — an empty placeholder directory,
  reserved for deeper branch / diff / PR primitives once the production
  GitHub path lands. Shipwright's first slice is implemented as a core
  runtime metadata API plus an AgentField-backed example under
  `examples/02-shipwright/`.

## Limits

- No hot reload — module changes require a runtime restart.
- Resources are flat CRUD: no joins, no custom filters, no validation
  beyond field type and `required`.
- Modules can't import each other; they CAN call each other's routes over
  internal HTTP.
- The runtime never generates DDL. Adding a field to a resource means
  adding a migration for the column too.

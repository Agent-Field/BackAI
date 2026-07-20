# `notes` — reference workload module

A minimal, self-contained example of a **declarative workload module** (PRD
R2). It shows the whole contract: a manifest, a versioned RLS-compliant
migration, and the tenant-scoped CRUD the runtime auto-generates from them —
with zero handler code.

## What ships here

```
workload-modules/notes/
├── backai.module.yaml        # manifest: id, version, resources + fields
├── migrations/
│   └── 00001_notes.sql       # creates notes_notes with tenant_id + FORCE RLS
└── README.md
```

## The resource → table convention

A resource named `notes` in the module `notes` is backed by the table
`notes_notes` (`<module>_<resource>`, the module id's hyphens folded to
underscores). Your migration MUST create that exact table with the runtime's
managed columns plus your declared fields:

| Column       | Source                       |
| ------------ | ---------------------------- |
| `id`         | runtime-managed (uuid PK)    |
| `tenant_id`  | runtime-managed (RLS anchor) |
| `created_at` | runtime-managed              |
| `updated_at` | runtime-managed              |
| `title`      | declared field (`string`)    |
| `body`       | declared field (`string`)    |
| `done`       | declared field (`bool`)      |

Field types map to Postgres as: `string→text`, `int→bigint`, `bool→boolean`,
`timestamp→timestamptz`, `json→jsonb`.

## Tenant isolation is enforced, not trusted

Before applying a module's migration the runtime **statically lints the SQL**:
every `CREATE TABLE` must include a `tenant_id` column and be followed by
`ENABLE ROW LEVEL SECURITY`, `FORCE ROW LEVEL SECURITY`, and a `CREATE POLICY`.
A module whose migration omits any of these is refused (a structured boot
error, the module is disabled) while the runtime keeps serving everything
else. The generated CRUD queries always filter `tenant_id = $1` from the
resolver-bound tenant — a client can never read or write another tenant's
rows, and can never set `tenant_id` from the request body.

## Enabling it

The module is `enabled: false` so it is a living example, not an
auto-served surface. To turn it on, add `notes` to the enabled workload list
(config `modules.workload_modules`, e.g. via `AF_STACK_MODULE`-style config)
and restart. Routes appear on the next boot:

| Method   | Path                                   | Action |
| -------- | -------------------------------------- | ------ |
| `GET`    | `/api/v1/workload/notes/notes`         | list   |
| `POST`   | `/api/v1/workload/notes/notes`         | create |
| `GET`    | `/api/v1/workload/notes/notes/{id}`    | get    |
| `PATCH`  | `/api/v1/workload/notes/notes/{id}`    | update |
| `DELETE` | `/api/v1/workload/notes/notes/{id}`    | delete |

Disabling the module (removing it from the enabled list) removes the routes
on the next boot **without dropping the table** — the data is preserved.

Operators can inspect discovery + migration state at
`GET /api/v1/admin/modules`.

## Try it

```bash
curl -X POST http://localhost:8080/api/v1/workload/notes/notes \
  -H "Authorization: Bearer $AF_STACK_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"title":"first note","body":"hello"}'
```

Scaffold your own with `af-stack module new <id>`.

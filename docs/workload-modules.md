# Workload Modules

Workload modules are the way BackAI pulls in domain-specific features
(notes, podcast jobs, reactive enrichments, etc.) without forking the
runtime. Each module is a directory you drop under
`workload-modules/<id>/`; the runtime scans `config.yaml` at boot,
loads each enabled module, registers its routes + migrations + crons,
and exposes them through the same auth + tenancy chain as the built-in
modules.

The pattern was extracted from Example 01 (Notable) and Example 04
(Podcast). The examples that ship in the repo are the canonical
reference for how a real workload module looks.

## Directory layout

```
workload-modules/<id>/
  manifest.yaml          # required — declares the module's metadata
  migrations/            # optional — versioned SQL applied at boot
    00001_init.sql
  handlers/              # optional — Go or Python HTTP handlers
    routes.go            # for Go
    handler.py           # for Python
  crons/                 # optional — cron schedules seeded at boot
    seed.yaml
  config.schema.yaml     # optional — schema for the operator-tunable
                         # block in the runtime's config.yaml
```

Only `manifest.yaml` is required. A module can be 100% migrations + crons
if it doesn't need its own HTTP surface.

## `manifest.yaml`

```yaml
id: notes
name: Notes
version: 0.1.0
description: Per-tenant Markdown notes with summarize / suggest-tags
  agent integrations.

# Required platform features. Boot fails fast if any are off so the
# operator gets a clear error rather than mysterious 503s downstream.
requires:
  - multi-tenancy
  - llm-gateway
  - memory

# Optional: the routes the module wants to mount. The runtime prepends
# /workload/<id>/ so routes don't clash across modules.
routes:
  - method: POST
    path: /notes
    handler: notes.Create        # references a function in handlers/
  - method: GET
    path: /notes
    handler: notes.List
  - method: GET
    path: /notes/{id}
    handler: notes.Get

# Optional: meters this module pushes through the billing subsystem.
# Declared up front so the dashboard's billing tab can render them
# without round-tripping the runtime.
meters:
  - name: notable_notes_created
    unit: count
    description: One per POST /workload/notes.
```

## How the runtime loads it

At boot, `services/runtime/internal/workload/loader.go` reads
`config.yaml`:

```yaml
workload_modules:
  - id: notes
    enabled: true
    config:
      # Module-specific knobs read against config.schema.yaml.
      max_note_size_kb: 256
```

For each enabled entry:

1. **Validate** the module's `requires:` against the live module flags.
   Missing prereq → hard fail at boot.
2. **Apply migrations** under the suite's standard migration table
   namespace (`workload_<id>_schema_migrations`) so they version
   independently from core schema.
3. **Register routes** under `/workload/<id>/...`. They inherit the
   tenant resolver + auth middleware. Handlers receive the resolved
   tenant from the request context like any other route.
4. **Seed crons** declared in `crons/seed.yaml` into `suite_crons` so
   they appear in the dashboard's Crons tab and the runtime
   scheduler dispatches them on schedule.

## Authoring a Go handler

Workload modules have a tiny `WorkloadHandler` contract:

```go
package notes

import (
    "context"

    "github.com/Agent-Field/backai/services/runtime/internal/workload"
)

type CreateInput struct {
    Title string `json:"title"`
    Body  string `json:"body"`
    Tags  []string `json:"tags"`
}

func Create(ctx context.Context, req workload.Request) (workload.Response, error) {
    // req exposes:
    //   req.TenantID       — resolved tenant id
    //   req.UserID         — operator session, when set
    //   req.Body           — raw JSON body
    //   req.DB             — pgx pool already bound to the tenant context
    //   req.Billing        — meter() / has_budget()
    //   req.Memory         — put / get / search
    //   req.AgentField     — invoke an agent by node id

    var in CreateInput
    if err := req.Decode(&in); err != nil {
        return req.BadRequest("invalid body"), nil
    }

    // ...write to the per-tenant notes table...

    // Meter the action. Crashes here are isolated so the metering
    // failure doesn't roll back the note write.
    _ = req.Billing.Meter(ctx, "notable_notes_created", 1)

    return req.JSON(201, map[string]any{"id": newID}), nil
}
```

Handlers in `handlers/` are picked up by the loader via a register call
in `init()`. The convention is one file per resource:
`handlers/notes.go` registers `notes.Create`, `notes.List`, etc.

## Authoring a Python handler

For modules where the workload is Python-heavy (Notable's agents, deep
research, etc.), the handler can be a regular AF agent reasoner:

```python
# workload-modules/notes-py/handlers/notes_handler.py
from agentfield import Agent, AIConfig

app = Agent(node_id="notes-handler")

@app.reasoner(tags=["http"])
async def create_note(payload: dict[str, any]) -> dict[str, any]:
    title = payload["title"]
    body = payload["body"]
    tenant_id = payload["_tenant_id"]  # injected by the runtime
    # ...
    return {"id": new_id}
```

The runtime proxies HTTP requests at `/workload/notes-py/notes` to the
reasoner. The tenant id is injected as `_tenant_id` in the payload.

## Calling agents from a workload handler

Workload handlers can invoke AF agents the same way the rest of the
runtime does:

```go
result, err := req.AgentField.Invoke(ctx, "summarize", map[string]any{
    "note_id": id,
    "body":    body,
})
```

Cost from that invocation flows through the gateway and is attributed
to the calling tenant.

## Crons

`crons/seed.yaml` is upserted into `suite_crons` at boot:

```yaml
- name: notes-daily-digest
  job_name: notes-daily-digest
  schedule: "0 9 * * *"
  args:
    template: daily-digest
```

The runtime's existing cron scheduler handles dispatch.

## Removing a module

Set `enabled: false` in `config.yaml`. The loader skips registration but
leaves data + crons in place so the operator can re-enable without loss.
To actually remove, delete the `workload-modules/<id>/` directory and
manually drop the data via your normal migration tooling.

## Built-in modules in the repo

BackAI does not currently ship a ready workload module in this
directory. Shipwright's first slice is implemented as a core runtime
metadata API plus an AgentField-backed example under
`examples/02-shipwright/`; a future `workload-modules/git-workload/`
can add deeper branch / diff / PR primitives once the production GitHub
path lands.

The Notable example is implemented as example-local handlers today.
When a workload module ships, copy-paste an entire `workload-modules/<id>/`
into your own deploy to vendor it.

## Limits in v1

- No hot reload — module changes require a runtime restart.
- No cross-module dependencies — modules can't import each other's
  handlers (they CAN call each other's routes via internal HTTP).
- Python handlers run in the agent process pool, not a dedicated
  sandbox, so they share the same fate domain. Use a sandbox adapter
  for arbitrary code execution.

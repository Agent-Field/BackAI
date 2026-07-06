# Jobs & Crons

Background work runs on a **River-backed Postgres queue** — no Redis, no
separate broker. Jobs are durable, retried, and observable through the
same Postgres you already run.

## Enqueue a job

`suite.jobs.*` from anywhere in the Suite:

```python
from af_stack import suite

job = await suite.jobs.enqueue("send-digest", {"tenant": "acme"})
await suite.jobs.get(job["id"])       # inspect status
await suite.jobs.retry(job["id"])     # re-run a failed job
await suite.jobs.list()               # list jobs
```

| Method | Does |
| --- | --- |
| `suite.jobs.enqueue(name, args)` | Queue a job |
| `suite.jobs.get(id)` | Fetch one job |
| `suite.jobs.retry(id)` | Re-enqueue a failed job |
| `suite.jobs.list()` | List jobs |

## ⚠️ Current limitation: Go handlers only

**Only Go (in-process) job handlers execute today.** The single
in-process worker runs Go handlers registered in the runtime.

Remote **Python / TypeScript** job handlers are **not implemented yet**.
A remote definition registers fine, but when the worker picks up the job
it has no live handler and cancels it with an explicit error:

```
remote job pending dispatch (Phase 5+ cross-language handler not yet implemented)
```

So today: define job handlers in Go, or enqueue Go-backed job kinds.
Cross-language handlers are a roadmap item, not a shipped feature — don't
build a flow that depends on a Python/TS job handler running.

## Crons

Recurring schedules live under `suite.crons.*`:

```python
await suite.crons.list()
await suite.crons.create({...})
await suite.crons.get(id)
await suite.crons.set_active(id, True)
await suite.crons.delete(id)
```

> **`suite.crons` is Python-only today.** There is no `crons` namespace in
> the TypeScript SDK. See [sdk.md](sdk.md#language-parity).

### Declaring crons in a workload module

The durable way to ship a schedule is to declare it in a
[workload module](build-app.md#3-workload-module). A module's
`crons/seed.yaml` is upserted into `suite_crons` at boot:

```
workload-modules/<id>/
  crons/
    seed.yaml     # cron schedules seeded at boot
```

Because cron *dispatch* rides the same River queue, the Go-only-handler
limitation above applies to what a cron ultimately runs, too.

# apps/backend

Your application backend code for a BackAI deployment. Forks edit this
folder before they change shared platform code.

## Layout

| Folder | What lives here | Discovered as |
|---|---|---|
| `agents/` | AgentField agent processes (Python/Go/TS) | Each subfolder runs as its own container; registers with AgentField on startup |
| `handlers/` | HTTP request handlers (plain code, not agents) | Loaded by the suite runtime on boot |
| `jobs/` | Background jobs (River-backed) | Registered with the job runner |
| `crons/` | Scheduled jobs | Registered with the scheduler |
| `streams/` | SSE / WebSocket handlers | Registered with the suite runtime |
| `migrations/` | YOUR SQL migrations | Run alongside suite migrations on boot |
| `templates/` | Email + notification templates | Loaded by notifications module |

## Status

The root stack ships one sample agent for smoke tests. Product-specific
agents, handlers, jobs, crons, migrations, and templates belong here or
in `workload-modules/<id>/`.

For a tiny copyable example, start with
[`examples/starter/`](../../examples/starter/). For the repo ownership
model, see [`docs/repo-map.md`](../../docs/repo-map.md).

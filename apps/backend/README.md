# apps/backend

Your application code for an AF Stack deployment. Forks edit this folder.

## Layout

| Folder | What lives here | Discovered as |
|---|---|---|
| `agents/` | AgentField agent processes (Python/Go/TS) | Each subfolder runs as its own container; registers with AF on startup |
| `handlers/` | HTTP request handlers (plain code, not agents) | Loaded by the suite runtime on boot |
| `jobs/` | Background jobs (River-backed) | Registered with the job runner |
| `crons/` | Scheduled jobs | Registered with the scheduler |
| `streams/` | SSE / WebSocket handlers | Registered with the suite runtime |
| `migrations/` | YOUR SQL migrations | Run alongside suite migrations on boot |
| `templates/` | Email + notification templates | Loaded by notifications module |

## Status

This is scaffold-only in the early build. Real handlers / jobs / agents land
in Phase 2+ examples.

---
title: Module — MCP
description: Model Context Protocol runtime. Long-lived pool over stdio + SSE adapters.
sidebar:
  order: 13
---

Model Context Protocol (MCP) runtime. A `Pool` reconciles a connection set against `suite_mcp_servers`, refreshes the tool catalogue every 5 minutes, and proxies tool calls with per-call timeouts.

## What it does

Two transport adapters:

- **stdio** — child process speaking MCP over stdin/stdout.
- **sse** — HTTP+SSE endpoint.

Both implement a uniform `mcp.Adapter` contract. The HTTP layer in `server/mcp.go` is a thin veneer over the Pool — wire shapes mirror `MCPServerSchema` / `MCPToolSchema` / `CallMCPResultSchema` in `apps/dashboard/src/lib/api.ts`.

When no pool is wired, mutating endpoints return `503 MCP_NOT_CONFIGURED`; GETs return empty pages.

## Configuration

```yaml
modules:
  enabled:
    mcp: true
```

Env override:

```bash
AF_STACK_MODULE_MCP=true
```

Per-server credentials (API keys for SSE endpoints) are stored via the [Secrets](./secrets/) module and resolved at connect time (see `mcp/secrets.go`).

## REST endpoints

Registered in `services/runtime/internal/server/mcp.go`:

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/mcp/servers` | List registered MCP servers. |
| `GET` | `/api/v1/mcp/servers/{name}` | Get a single server. |
| `POST` | `/api/v1/mcp/servers` | Register a server. |
| `DELETE` | `/api/v1/mcp/servers/{name}` | Remove a server. |
| `PUT` | `/api/v1/mcp/servers/{name}/enabled` | Enable / disable. |
| `GET` | `/api/v1/mcp/tools` | Cached tool catalogue across all enabled servers. |
| `POST` | `/api/v1/mcp/call` | Invoke a tool by `{server, tool, args}`. |

## Database tables

Owned by migration `00012_mcp.sql`:

- `suite_mcp_servers` — name, transport, command/url, env, enabled, last_seen_at.

## Env vars

| Env | Purpose |
|---|---|
| `AF_STACK_MODULE_MCP` | Enable / disable. |

Per-server env (for stdio adapters) is passed through `MCPServerSchema.env`.

## Code map

- `interface.go` — `Adapter` interface.
- `pool.go` — connection pool + tool catalogue refresh.
- `store.go` — Postgres queries.
- `secrets.go` — per-server secret resolution.
- `adapters/stdio/` — child-process MCP.
- `adapters/sse/` — HTTP+SSE MCP.

## Related

- [MCP adapters](../../adapters/mcp/) — transport detail.
- Reads credentials from [Secrets](./secrets/).
- Used by [Skills](./skills/) and [Harnesses](./harnesses/).

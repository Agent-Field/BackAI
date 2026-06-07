---
title: MCP adapters
description: Two transports for Model Context Protocol — stdio (child process) and sse (HTTP).
sidebar:
  order: 4
---

Two transports for MCP servers. Both implement `mcp.Adapter`; the [MCP module](../../modules/mcp/) treats them uniformly.

## Selection

Transport is per-server, stored on `suite_mcp_servers.transport` and chosen when the server is registered. No global config knob.

```bash
# Add a stdio server
curl -X POST .../api/v1/mcp/servers -d '{
  "name": "fs",
  "transport": "stdio",
  "command": "mcp-server-filesystem",
  "args": ["/data"]
}'

# Add an SSE server
curl -X POST .../api/v1/mcp/servers -d '{
  "name": "context7",
  "transport": "sse",
  "url": "https://mcp.context7.com/sse",
  "secret_key": "ctx7_api_key"
}'
```

`secret_key` references a [Secrets](../../modules/secrets/) entry — the Pool resolves it at connect time via `mcp/secrets.go`.

## Capabilities matrix

| Adapter | Transport | Process model | Auth | Use case |
|---|---|---|---|---|
| `stdio` | stdin/stdout JSON-RPC | child process per server | env vars | local tools, filesystem, custom binaries |
| `sse` | HTTP + Server-Sent Events | HTTP client | API key (via Secrets) | hosted MCP services |

## When to pick which

### `stdio` — local tools

The Pool spawns a child process per registered server and speaks JSON-RPC over stdin/stdout. Pass env via the `env` field on `MCPServerSchema`. Best for filesystem servers, local CLI wrappers, custom executables.

### `sse` — hosted services

The Pool maintains an SSE connection to the configured URL. Authentication is via API key sourced from the Secrets module (the `secret_key` field names the secret).

## Env vars

None at the module level. Per-server env is part of the `MCPServerSchema` payload.

## Code map

- `adapters/stdio/adapter.go` — child-process spawn + JSON-RPC framing.
- `adapters/sse/adapter.go` — HTTP+SSE client.

## Related

- [MCP module](../../modules/mcp/) — pool + tool catalogue refresh.
- [Secrets module](../../modules/secrets/) — credential storage for SSE adapters.

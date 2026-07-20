# Built-in Tool Adapters

BackAI ships a tenant-scoped catalogue of built-in tool adapters:

| Adapter | Backend | Status when configured |
| --- | --- | --- |
| `http` | Runtime `safehttp` client | Always configured; SSRF-protected. |
| `sql` | Suite Postgres | Configured when the runtime has a DB. Read-only queries only. |
| `exec` | Sandbox service | Configured when a sandbox adapter is available. |
| `fs` | Sandbox service | Configured when a sandbox adapter is available; operates on ephemeral sandbox files, not the host filesystem. |
| `searxng` | SearXNG HTTP endpoint | Set `SEARXNG_URL`. |
| `browser-use` | browser sidecar HTTP endpoint | Set `BROWSER_USE_URL`. Reference sidecar: `examples/adapters/browser-use-sidecar` (compose profile `browser`). Add `AF_STACK_BROWSER_ALLOW_PRIVATE=true` when the sidecar lives on a loopback/private address (e.g. a compose service). |
| `steel` | Steel.dev hosted browsers (CDP) | Set `STEEL_API_KEY` (+ optional `STEEL_BASE_URL` for self-hosted steel-browser) and `AF_STACK_TOOL_BROWSER=steel`. |
| `browserbase` | Browserbase hosted browsers (CDP) | Set `BROWSERBASE_API_KEY` + `BROWSERBASE_PROJECT_ID` and `AF_STACK_TOOL_BROWSER=browserbase`. |
| `playwright` | any CDP/Playwright websocket endpoint | Set `PLAYWRIGHT_ENDPOINT` (e.g. Browserless `wss://chrome.browserless.io?token=KEY`) and `AF_STACK_TOOL_BROWSER=playwright`. |

Browser credentials can also be entered in the dashboard under **Platform →
Integrations** (browser slot); env wins, UI values apply on the next
runtime restart.

The runtime owns only tenant enablement/config and a small call audit row.
AgentField still owns agent runs, tool-call spans, traces, memory, and
session state.

## API

```bash
GET /api/v1/tools/adapters
PUT /api/v1/tools/adapters/{id}/enabled
POST /api/v1/tools/call
```

Enable one adapter for the caller tenant:

```bash
curl -X PUT http://localhost:8080/api/v1/tools/adapters/sql/enabled \
  -H "Authorization: Bearer $AF_STACK_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"enabled":true}'
```

Call a tool:

```bash
curl -X POST http://localhost:8080/api/v1/tools/call \
  -H "Authorization: Bearer $AF_STACK_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"adapter":"sql","tool":"query","arguments":{"sql":"select slug, name from suite_tenants","max_rows":10}}'
```

## SDK

TypeScript:

```ts
import { suite } from "@af-stack/sdk"

await suite.tools.setAdapterEnabled("http", true)
const result = await suite.tools.callAdapter("http", "request", {
  arguments: { url: "https://example.com" },
})
```

Python:

```python
from af_stack import suite

await suite.tools.set_adapter_enabled("http", True)
result = await suite.tools.call_adapter(
    "http",
    "request",
    {"url": "https://example.com"},
)
```

## Guardrails

- `http` uses the runtime's `safehttp` client and blocks loopback,
  private-network, link-local, and cloud metadata destinations by default.
- `sql` accepts only one read-only `select`, `with`, or `explain` statement.
- `exec` and `fs` run through the configured sandbox adapter.
- `fs` never reads or writes the runtime host filesystem.

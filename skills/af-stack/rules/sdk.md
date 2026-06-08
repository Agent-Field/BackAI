# SDK Reference — surface map for `suite.*` and `app.*`

The exhaustive lookup. When you need "is `suite.x.y()` real?", come
here. For the authoritative surface, read the SDK source itself
(linked).

## Python SDK — `from af_stack import suite, ctx`

Source: `packages/sdk-py/af_stack/`

### Tier 1 — App-building verbs (this is what 99% of your code uses)

| Namespace | What | Backing endpoints |
|---|---|---|
| `suite.agents.call(name, payload, opts)` | Invoke an AgentField reasoner | `POST /api/v1/agents/{name}` |
| `suite.agents.stream(name, payload)` | SSE stream of agent events | `POST /api/v1/agents/stream/{name}` |
| `suite.agents.async_call(name, payload)` | Fire-and-forget; returns execution_id | `POST /api/v1/agents/async/{name}` |
| `suite.agents.status(execution_id)` | Poll an async execution | `GET /api/v1/agents/executions/{id}` |
| `suite.agents.cancel(execution_id)` | Cancel an async execution | `DELETE /api/v1/agents/executions/{id}` |
| `suite.llm.chat({model, messages, ...})` | OpenAI-compatible chat completion | `POST /api/v1/llm/chat/completions` |
| `suite.llm.embed(model, input)` | OpenAI-compatible embeddings (single string or batch) | `POST /api/v1/llm/embeddings` |
| `suite.llm.models()` | List gateway-exposed models with pricing | `GET /api/v1/llm/models` |
| `suite.llm.cache_stats()` | Aggregated gateway cache stats | `GET /api/v1/llm/cache/stats` |
| `suite.memory.put(key, value, scope, scope_id, embed)` | Set a memory entry | `PUT /api/v1/memory` |
| `suite.memory.get(scope, scope_id, key)` | Get a memory entry | `GET /api/v1/memory/get` |
| `suite.memory.list(scope, scope_id, prefix, limit, offset)` | List memory entries | `GET /api/v1/memory` |
| `suite.memory.delete(scope, scope_id, key)` | Delete a memory entry | `DELETE /api/v1/memory` |
| `suite.memory.search(query, scope, scope_id, top_k, threshold)` | Vector similarity search | `POST /api/v1/memory/search` |
| `suite.storage.upload(key, data, content_type)` | Upload an object | `POST /api/v1/storage/upload` |
| `suite.storage.download(key)` | Download an object | `GET /api/v1/storage/{key}` |
| `suite.storage.signed_url(key, ttl_s)` | Pre-signed download URL | `GET /api/v1/storage/signed-url` |
| `suite.storage.delete(key)` | Delete an object | `DELETE /api/v1/storage/{key}` |
| `suite.storage.list(prefix, limit)` | List objects | `GET /api/v1/storage` |
| `suite.search(query, mode, opts)` | Hybrid FTS + vector search (mode: `text\|vector\|hybrid`) | `POST /api/v1/search` |
| `suite.search.upsert(doc)` | Index a document for search | `POST /api/v1/search/index` |
| `suite.search.delete(doc_id)` | Remove a document from the search index | `DELETE /api/v1/search/index/{id}` |
| `suite.sandbox.run({image, command, env, timeout_s, ...})` | Run code in a sandbox | `POST /api/v1/sandbox/run` |
| `suite.sandbox.list(...)` | List recent runs | `GET /api/v1/sandbox/runs` |
| `suite.sandbox.get(run_id)` | Get a run's status + output | `GET /api/v1/sandbox/runs/{id}` |
| `suite.sandbox.stop(run_id)` | Stop a running sandbox | `DELETE /api/v1/sandbox/runs/{id}` |
| `suite.sandbox.pool()` | Pool stats (warm / active / queued) | `GET /api/v1/sandbox/pool` |
| `suite.jobs.enqueue(name, args, opts)` | Enqueue a background job | `POST /api/v1/jobs` |
| `suite.jobs.get(job_id)` | Job status | `GET /api/v1/jobs/{id}` |
| `suite.jobs.retry(job_id)` | Retry a failed job | `POST /api/v1/jobs/{id}/retry` |
| `suite.jobs.list(opts)` | List jobs | `GET /api/v1/jobs` |
| `suite.crons.list(opts)` | List cron schedules for the tenant | `GET /api/v1/crons` |
| `suite.crons.create({name, expression, payload, ...})` | Create a cron schedule | `POST /api/v1/crons` |
| `suite.crons.get(cron_id)` | Fetch one cron schedule | `GET /api/v1/crons/{id}` |
| `suite.crons.set_active(cron_id, active)` | Enable / disable a cron schedule | `PATCH /api/v1/crons/{id}` |
| `suite.crons.delete(cron_id)` | Delete a cron schedule | `DELETE /api/v1/crons/{id}` |
| `suite.realtime.subscribe(table, rt_filter)` | WebSocket subscription (PG LISTEN/NOTIFY → WS); Python lazy-loads optional `websockets` pkg | `GET /api/v1/realtime` |
| `suite.secrets.get(key)` | Read a per-tenant secret (decrypted) | `GET /api/v1/secrets/{key}` |
| `suite.secrets.put(key, value)` | Create / upsert a per-tenant secret | `PUT /api/v1/secrets/{key}` |
| `suite.secrets.delete(key)` | Delete a secret | `DELETE /api/v1/secrets/{key}` |
| `suite.secrets.list()` | List secret keys (values masked) | `GET /api/v1/secrets` |
| `suite.secrets.reveal(key)` | One-shot audited reveal of a secret value | `POST /api/v1/secrets/{key}/reveal` |
| `suite.secrets.rotate(key, new_value)` | Rotate a secret's value in place | `POST /api/v1/secrets/{key}/rotate` |
| `suite.webhooks.send(event_type, payload, opts)` | Send via Svix | `POST /api/v1/webhooks/send` |
| `suite.webhooks.deliveries(opts)` | List recent outbound deliveries | `GET /api/v1/webhooks/deliveries` |
| `suite.notifications.send(channel, recipient, template, data)` | Send via configured adapter | `POST /api/v1/notifications` |
| `suite.billing.upsert_customer(tenant)` | Sync customer with Stripe/Lago | `POST /api/v1/billing/customers` |
| `suite.billing.record_usage(tenant, meter, qty)` | Meter increment | `POST /api/v1/billing/meter` |
| `suite.billing.portal_link(tenant, return_url)` | Customer-facing portal URL | `POST /api/v1/billing/portal` |
| `suite.tools.list_mcp_servers()` | List registered MCP servers | `GET /api/v1/tools/mcp/servers` |
| `suite.tools.list_mcp_tools(server)` | List tools the agent can call | `GET /api/v1/tools/mcp/tools` |
| `suite.tools.call_mcp(server, tool, args)` | Invoke an MCP tool | `POST /api/v1/tools/mcp/call` |
| `ctx.tenant_id`, `ctx.user_id`, `ctx.request_id` | Request-scoped context | — (set by middleware) |

### Tier 2 — Operator / inventory verbs (used by dashboards + ops tooling)

These power the operator console; you rarely call them from app code.
Listed for completeness.

| Namespace | What | Backing endpoints |
|---|---|---|
| `suite.harnesses.list()` | Which harnesses are detected per agent | `GET /api/v1/harnesses` |
| `suite.harnesses.get(provider)` | Status of one harness provider | `GET /api/v1/harnesses/{provider}` |
| `suite.harnesses.probe(provider)` | Re-run the probe for one provider | `POST /api/v1/harnesses/{provider}/probe` |
| `suite.cost.events(tenant, limit, since)` | Per-tenant LLM cost events (audit feed) | `GET /api/v1/cost/events` |
| `suite.cost.totals(tenant, by)` | Cost aggregations | `GET /api/v1/cost/totals` |
| `suite.tools.add_mcp_server(...)` | Register a new MCP server (operator) | `POST /api/v1/tools/mcp/servers` |
| `suite.tools.remove_mcp_server(name)` | Unregister an MCP server | `DELETE /api/v1/tools/mcp/servers/{name}` |
| `suite.tools.enable_mcp_server(name, enabled)` | Enable / disable a registered MCP server | `PATCH /api/v1/tools/mcp/servers/{name}` |
| `suite.admin.tenants.{list,get,create,update,delete}(...)` | Operator tenant verbs | `/api/v1/admin/tenants/*` |
| `suite.admin.users.list(...)` | List users across tenants | `GET /api/v1/admin/users` |
| `suite.admin.memberships.{list,add,remove}(...)` | Tenant membership management | `/api/v1/admin/memberships/*` |
| `suite.admin.keys.{list,issue,revoke}(...)` | API key lifecycle | `/api/v1/admin/api-keys/*` |
| `suite.admin.budgets.{list,get,set}(...)` | Per-tenant LLM budgets | `/api/v1/admin/budgets/*` |
| `suite.admin.skills.{list,install,uninstall}(...)` | Skill bundle lifecycle | `/api/v1/admin/skills/*` |
| `suite.admin.audit.list(...)` | Admin audit log entries | `GET /api/v1/admin/audit` |

## TypeScript SDK — `import { suite, ctx } from "@af-stack/sdk"`

Source: `packages/sdk-ts/src/`

Same surface as Python plus:

| Namespace | What | Status |
|---|---|---|
| `suite.realtime.subscribe(channel, opts)` | WebSocket subscription | ✅ shipped (server: `GET /api/v1/realtime`) |
| `suite.search(q, mode, opts)` | Hybrid FTS + vector search | ✅ |
| `suite.searchIndex.upsert(doc)` | Index a document for search | ✅ |

Plus exports for `SuiteError`, `HttpOptions`, `SseEvent`, all
Pydantic/Zod schema types as TS type aliases.

## Go SDK — `import "github.com/Agent-Field/backai/packages/sdk-go/suite"`

Source: `packages/sdk-go/suite/`

Same surface, Go-idiomatic. Used by:
- Go workload modules (when eventually formalizes them)
- Custom hooks
- Service-to-service calls inside the runtime

## AgentField SDK — `from agentfield import Agent, AIConfig`

Used **only inside an agent** (`apps/backend/agents/<name>/main.py`).
Source: `/Users/santoshkumarradha/Documents/agentfield/code/platform/agentfield/sdk/python/agentfield/`

| Call | What |
|---|---|
| `Agent(node_id="...")` | Agent definition; registers with control plane |
| `@app.reasoner(tags=[...])` | Expose a function as `/api/v1/agents/<node_id>.<reasoner-name>` |
| `app.ai(system, user, schema)` | Single LLM call with Pydantic schema (routes through gateway) |
| `app.harness(provider="claude-code").run(...)` | Run a CLI harness (claude-code / codex / gemini / opencode) |
| `app.memory.set(key, value, scope, scope_id)` | Set a memory entry |
| `app.memory.get(key, scope, scope_id)` | Get with hierarchical fallback |
| `app.memory.list_keys(scope, scope_id)` | List keys in scope |
| `app.memory.exists(key, scope, scope_id)` | Existence check |
| `app.memory.delete(key, scope, scope_id)` | Delete |
| `app.memory.set_vector(key, embedding, value, scope, scope_id)` | Vector-indexed value |
| `app.memory.similarity_search(query, scope, scope_id, k)` | Vector search |
| `app.mcp.call(server, tool, args)` | Call an MCP tool |
| `app.is_cancelled()` | Cancellation check |
| `app.run()` | Start the agent server (in `__main__`) |
| `__capabilities__` reasoner (required) | Harness + MCP runner detection |

## Where to look up the latest

When you need a specific call:

1. **Read the SDK source**:
   `packages/sdk-py/af_stack/<module>.py` — these files have Sphinx
   docstrings that map every call to its REST endpoint.
2. **OpenAPI**: `GET /openapi.json` on a running runtime. Live truth.
3. **Existing code**: search how the cost-explorer plugin / Notable
   example / sample agent use the SDK — those are tested patterns.

## LLM rate limits — 429 responses

LiteLLM enforces per-virtual-key `rpm_limit` / `tpm_limit` upstream (one
key per `suite_api_keys` row, issued by `tenancy.IssueAPIKey`). The
runtime no longer runs a local token-bucket; when LiteLLM returns 429
the LLM handler proxies the standard rate-limit headers through to the
client verbatim:

| Header | Source | Meaning |
|---|---|---|
| `Retry-After` | LiteLLM | Seconds the client SHOULD wait before retrying. |
| `X-RateLimit-Limit` | LiteLLM | The configured rpm/tpm ceiling. |
| `X-RateLimit-Remaining` | LiteLLM | Calls left in the current window. |
| `X-RateLimit-Reset` | LiteLLM | UNIX timestamp when the window resets. |

The error envelope is the standard OpenAI shape so `openai.RateLimitError`
in the Python SDK fires natively:

```json
{
  "error": {
    "code": "RATE_LIMIT_EXCEEDED",
    "message": "...",
    "type": "rate_limit_error",
    "details": { "retry_after": 30, "upstream_status": 429, ... }
  }
}
```

**SDK callers**: catch `openai.RateLimitError` (or the language
equivalent), read `Retry-After`, back off, and retry. The runtime owns
none of this — bumping the limit means raising the key's `rate_limit_rpm`
in the dashboard or via `suite.admin.api_keys.update(...)`.

## Anti-patterns

| Anti-pattern | Why wrong | Correct |
|---|---|---|
| Importing `agentfield` outside an agent process | The SDK assumes the AgentField runtime context | Use `suite.agents.call()` from runtime / workload module |
| Importing `suite` inside an agent | Wrong layer; use `app.*` (AgentField) | `app.memory.set()` etc. |
| Calling REST endpoints directly via httpx | Lose typing, retries, error envelope handling | Use the SDK |
| Reaching `litellm:4000` directly | Bypasses cost ledger | `suite.llm.chat()` |
| Reaching AgentField control plane directly | Bypasses tenant resolver | `suite.agents.call()` |
| Hardcoding URLs (`localhost:8080`) | Breaks in prod | Read `AF_STACK_URL` from env |

## Calling between SDKs

A typical end-to-end:

```python
# In a workload module (Python sidecar)
from af_stack import suite

result = await suite.agents.call("docuchat.answer", {"query": q})
# This call:
#   1. HTTP POST /api/v1/agents/docuchat.answer
#   2. Runtime resolves tenant from x-af-stack-tenant-id header
#   3. Runtime forwards to AgentField control plane
#   4. AgentField invokes the `answer` reasoner on the `docuchat` agent
#   5. Inside the agent, the reasoner uses app.ai(), app.memory.*, etc.
#   6. Agent returns; AgentField records the run + spans
#   7. Runtime returns {"output": ...} to the workload module
```

The boundaries:
- Workload module ↔ runtime: `suite.*`
- Agent process ↔ AgentField: `app.*`
- Customer app ↔ runtime: `fetch()` through the proxy

Never cross these boundaries directly.

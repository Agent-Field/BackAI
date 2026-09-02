# Primitives — the full reference

The SKILL.md primitives table is the lookup. This file is the deep
description: what each primitive does, when to use it vs alternatives,
what's free vs what you write, common mistakes.

Organized by the 8 bands of `docs/stack.md`.

## ④ Intelligence

### LLM call

- **In an agent**: `app.ai(system=..., user=..., schema=PydanticModel)`.
  Returns a parsed model instance. Use for fast structured classification
  — single LLM call, input fits in context, flat output schema.
- **In a runtime handler / workload module**: `suite.llm.chat({model,
  messages, ...})` → `POST /api/v1/llm/chat/completions`. OpenAI-compatible
  wire shape; LiteLLM routes upstream.
- **Embeddings** (shipped today): `suite.llm.embed(model, input)` →
  `POST /api/v1/llm/embeddings`. OpenAI-compatible; same gateway / cost
  ledger / tenant scoping as `chat`. `input` accepts a single string or
  a batch.
- **Adapter** (`AF_STACK_LLM_GATEWAY_ADAPTER=demo|litellm|remote`): picks
  which backend answers `/api/v1/llm/*`. `litellm` is the sidecar (100+
  providers; configure in `apps/backend/litellm-config.yaml`, pick model per
  call); `demo` is the deterministic no-key provider; `remote` fronts an
  out-of-process gateway (`AF_STACK_LLM_REMOTE_URL`/`_TOKEN`). Left unset,
  the runtime auto-selects demo vs litellm from `AF_STACK_DEMO_MODE`. The
  value is validated at boot (unsupported → fail fast; it used to be
  silently ignored).
- **What's free**: per-tenant cost in `suite_cost_events`, optional
  caching, budget enforcement (planned to move upstream via LiteLLM virtual
  keys).

**Common mistake**: using `.ai()` with a document longer than ~2-3
pages of input. Use `.harness()` instead — it can navigate. See
`rules/agents.md`.

### Memory (4 scopes)

AgentField owns this. From inside an agent, use `app.memory.*`. From
runtime / workload modules, use `suite.memory.*` (REST shim).

| Scope | Cleared when | Use for |
|---|---|---|
| **Global** | Manually | Shared knowledge across everything (config, prompts) |
| **Actor** | Manually (per-user, across sessions) | Per-user preferences, profile, history |
| **Session** | Session ends | Conversation context, chat history |
| **Workflow** | Run completes | Agent step state, scratch space |

`app.memory.get(key)` without an explicit scope resolves
`workflow → session → actor → global` and returns the first hit.

`app.memory.set_vector(key, embedding, value)` + `app.memory.similarity_search(query, k=10)`
covers RAG patterns. The pgvector store is built into AgentField — don't
add a second one.

### Harness

`app.harness(provider="claude-code"|"codex"|"gemini"|"opencode")` runs
a CLI agent inside the current container. Each provider needs:

- The binary installed in the agent's `Dockerfile` (e.g. `npm install -g
  @anthropic-ai/claude-code`)
- The env var set (e.g. `ANTHROPIC_API_KEY`)

Use harnesses for: code review, code generation, multi-step research
inside a sandbox, navigation of long documents. Don't use them for
single-shot classification — that's `.ai()` work.

**Capability declaration**: the agent's `__capabilities__` reasoner
reports which harnesses are present + ready / needs-auth / missing. The
runtime aggregates across agents to populate the Build → Agents
dashboard tab and the `/api/v1/harnesses` endpoint.

### MCP tool

MCP servers can be:

- **stdio** (e.g. `uvx mcp-server-github`) — the agent container runs
  the MCP server as a subprocess
- **SSE** (e.g. `https://your-server.com/mcp`) — agent connects over HTTP

The runtime's MCP host (`services/runtime/internal/mcp/`) catalogues
tools, scopes per tenant, and routes calls. Tools that need credentials
pull them from the secrets vault via `secret:<key>` env prefix.

`app.mcp.call(server="github", tool="create_issue", args={...})` from
inside an agent. From the runtime, use `suite.tools.*`.

## ⑤ Execution

### Sandbox

`suite.sandbox.run({image, command, env, timeout_s, cpu_limit,
memory_mb, ...})` → `POST /api/v1/sandbox/run`. Bounded, isolated code
execution.

**Adapters** (`AF_STACK_SANDBOX_ADAPTER`):
- `docker` — dev (mounts host docker.sock)
- `gvisor` — production single-host, userspace kernel for isolation
- `firecracker` — hard multi-tenant via Flintlock, micro-VMs
- `e2b` — managed remote sandboxes (needs `E2B_API_KEY`)
- `remote` — an out-of-process sandbox adapter over the remote protocol

Stream stdout/stderr via the streaming endpoint. Capture artifacts via
`suite.storage`. Every run is cost-tracked.

**Common mistake**: trying to run untrusted code in `docker` adapter in
production. Switch to `gvisor` or `firecracker` or `e2b` for prod
deploys with untrusted workloads.

### Jobs (River)

`suite.jobs.enqueue(name, args, opts={tenant_id, retry, ...})` →
`POST /api/v1/jobs`. PG-backed, multi-replica safe. Retries with
exponential backoff. Deadletter on max attempts.

Job handlers are Go functions registered in
`services/runtime/internal/jobs/`. For Python workload modules, enqueue
from Python; the handler runs in the runtime.

### Cron

`suite.crons.create({tenant_id, name, expression, payload})` → wraps
`robfig/cron v3`. Full crontab syntax + shortcuts (`@hourly`, `@daily`,
etc.). Multi-replica safe via `FOR UPDATE SKIP LOCKED`.

**Python SDK**: `suite.crons.list()`, `suite.crons.create(...)`,
`suite.crons.get(cron_id)`, `suite.crons.delete(cron_id)` are
module-level functions (no class wrapper) — same shape as the rest of
the SDK.

### Webhook in (HMAC + dedup)

Declare an endpoint via `POST /api/v1/webhooks/endpoints` (or in a
workload module manifest). Configure:
- HMAC algorithm + signature header
- Dedup token header (X-GitHub-Delivery, Stripe-Signature timestamp, etc.)
- Forward target: HTTP URL OR `af://agents/<agent-name>`

The runtime verifies HMAC, dedups, forwards. The receiving handler is
your workload module endpoint or an agent call.

## ⑥ Delivery

### Webhook out

`suite.webhooks.send({event_type, payload, tenant_id})` → enqueues onto
the native in-process outbox. The runtime handles delivery: a PG-backed
queue + tick worker with HMAC signing, retries with exponential backoff,
and a persisted delivery ledger.

### Notification

`suite.notifications.send({channel, recipient, template, data})`.
Adapter (`AF_STACK_NOTIFICATIONS_ADAPTER`):
- `log` — default, prints to logs (dev)
- `resend` — email; set `RESEND_API_KEY`
- `slack` — set `AF_STACK_SLACK_WEBHOOK_URL`
- `sms` (alias `twilio`) — set `AF_STACK_TWILIO_ACCOUNT_SID` / `_AUTH_TOKEN` / `_FROM_NUMBER`
- `push` (alias `fcm`) — set `AF_STACK_FCM_PROJECT_ID` / `_ACCESS_TOKEN`
  (the access token is a short-lived OAuth token minted at your ops layer)
- `remote` — out-of-process adapter (`AF_STACK_NOTIFICATIONS_ADAPTER_URL`/`_TOKEN`, env-only)

Channel creds (resend/slack/twilio/fcm) can also be set from the dashboard
→ Platform → Integrations page; env wins, UI applies on next restart.
Channels are also managed via `GET|POST|PATCH|DELETE
/api/v1/notifications/channels` (upsert keyed on `kind`) and the
**Activity → Notifications** page. Note: the outbox is workspace-level —
`POST /api/v1/notifications` resolves the default tenant, not the caller's
(per-customer-tenant inboxes are roadmap). See `docs/adapters/notifications.md`.

### Billing

`suite.billing.*`:
- `upsert_customer(tenant)` → creates / fetches Stripe / Lago customer
- `record_usage(tenant, meter, qty)` → meter increment
- `portal_link(tenant, return_url)` → customer-facing portal URL
- `handle_webhook(body, sig)` → Stripe / Lago webhook handler

Adapter: `AF_STACK_BILLING_ADAPTER=stripe|lago|none`. The interface is
the same across adapters; the operator picks.

## ⑧ Data

### Postgres (RLS auto-bound)

Direct DB access from your workload module. Get a connection, set
`SET LOCAL app.tenant_id = ...`, do your work. RLS policies on
tenant-scoped tables enforce isolation automatically.

For cross-tenant operator queries (rare): `SET LOCAL app.bypass_rls =
'on'` inside an audited operator route.

See `rules/multi-tenancy.md` for the full pattern.

### Object storage

`suite.storage.upload(key, bytes)`, `suite.storage.download(key)`,
`suite.storage.signed_url(key, ttl_s)`, `suite.storage.delete(key)`,
`suite.storage.list(prefix)`.

Adapter (`AF_STACK_S3_ADAPTER`):
- `minio` — default dev (S3-compatible)
- `s3` — AWS S3 / R2 / GCS / Azure Blob (all via the S3 API)
- `remote` — out-of-process storage adapter (`AF_STACK_STORAGE_REMOTE_URL`/`_TOKEN`,
  also settable via the Integrations UI)

Objects are per-tenant scoped (key prefix). File transforms (thumbnail,
resize) are planned.

## ③ API Gateway

### Tenant context

The runtime resolves the tenant from the API key (Authorization
header) or session cookie. The `tenantctx.TenantID(ctx)` (Go) gives you
the tenant. From Python / TS workload modules, the runtime forwards
`x-af-stack-tenant-id` + `x-af-stack-user-id` headers.

### Secrets

`suite.secrets.put(key, value)`, `suite.secrets.get(key)`,
`suite.secrets.delete(key)`. AES-256-GCM envelope encryption; the KEK is
`AF_STACK_KMS_KEY` (32-byte hex).

Store adapter (`AF_STACK_SECRETS_ADAPTER=vault|remote`, validated at boot —
was previously silently ignored):
- `vault` — default; the built-in Postgres-backed AES-256-GCM vault.
- `remote` — **roadmap.** Selectable, validated, and capability-probed, but
  the server still binds the concrete vault type, so a remote backend
  cannot yet fully back `/api/v1/secrets` end-to-end (generalizing the
  server's secrets dependency to the `Store` interface is a follow-up).
  Remote creds (`AF_STACK_SECRETS_REMOTE_URL`/`_TOKEN`) are env-only — the
  vault can't configure its own backend. Treat `vault` as the only
  production store today.

`AF_STACK_KMS_PROVIDER` (env/aws/gcp/azure) is a separate axis — it governs
how the data key is wrapped, not where secrets are stored.

Per-tenant scoped. Use for: GitHub OAuth tokens, OpenAI keys (per
tenant if you allow BYOK), MCP server credentials (`secret:<key>`
prefix).

### Audit

Every admin mutation (tenant create/delete, api_key create/revoke,
secret put/delete, budget set, membership change) writes to
`suite_audit_log` automatically. You don't call this directly; it's
fired by middleware.

### Cost ledger

Every LLM call writes a `suite_cost_events` row with `model`, `tokens`,
`cost_usd`, `cache_hit`. Aggregated in the Operate → Cost dashboard.
Per-tenant budgets returning `HTTP 402 BUDGET_EXCEEDED` when crossed.

## Roadmap primitives (yet to ship)

`docs/product.md` tracks what's REAL vs planned. If the user
needs them today, propose a workaround or wait.

| Primitive | Status | Workaround until shipped |
|---|---|---|
| Multimodal (TTS/STT/image) | 🚧 | Call provider via LiteLLM's audio/image endpoints; we have models in litellm-config.yaml |
| Tool adapters | 🚧 | Use MCP servers from agent container (uvx / npx); declare in `__capabilities__` |
| PII redaction | 🚧 | Add to your agent reasoner before sending to LLM |

## Common composition patterns

These are NOT primitives — they're combinations of primitives. Your
workload module composes them.

### RAG (retrieval-augmented Q&A)

```
storage (upload)
  → your parser (text extraction)
    → suite.llm.embed(model, chunks)
      → suite.memory.put with vector
        → suite.memory.search (at query time)
          → suite.llm.chat (with retrieved context)
```

### Chat with history

```
agent reasoner
  → app.memory.get(scope=Session, scope_id=conversation_id)  ← prior turns
    → app.ai(system, user)
      → app.memory.set(scope=Session, scope_id=conversation_id, key=turn_n)
```

### Long-running agent task

```
workload module POST endpoint
  → suite.jobs.enqueue("your_task", args)
    → River worker picks up
      → suite.agents.call("your-agent.run")  ← the agent runs in its container
        → app.harness() / app.ai() / app.memory.*
          → returns structured output
      → workload module job handler writes result to DB
      → suite.webhooks.send("task.done")  ← notify the customer
```

### Approval-gated action

Approvals are shipped and tenant-scoped: `POST /api/v1/approvals` to
request, `POST /api/v1/approvals/{id}/decide` for the operator, and
`GET /api/v1/approvals/{id}` to poll.

```
workload module POST endpoint
  → POST /api/v1/approvals {kind: "deploy_to_prod", payload}
    → poll GET /api/v1/approvals/{id} until decided
  → operator approves in dashboard (POST /api/v1/approvals/{id}/decide)
    → workload module job handler proceeds
      → suite.sandbox.run(...) etc.
```

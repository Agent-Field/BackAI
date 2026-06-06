# SDK Strategy

## The invariant

**All model calls go through AgentField.** No direct provider calls. No
`suite.llm.chat()` that bypasses AF. The OpenAI-compatible endpoint at
`/api/v1/llm/*` exists for ecosystem compatibility, but underneath it routes
through AF so identity, traces, cost, policy, and audit are preserved.

Every LLM call = an AF execution. Single source of truth.

## Two SDKs, clear boundary

> **"`app.*` defines agents. `suite.*` calls them and runs everything else."**

This is the whole DX model.

| SDK | Owns | Use inside |
|---|---|---|
| **AgentField SDK** (`app.*`) | Defining agents: `.ai()`, `.harness()`, `.call()`, `.memory.*`, `.pause()`, `@app.reasoner()`, `app.run()` | AF agent processes |
| **Suite SDK** (`suite.*`, `ctx.*`) | Invoking agents from app code + suite infrastructure (jobs, secrets, storage, notifications, billing, sandbox, etc.) | App handlers, jobs, crons, dashboard, anywhere outside an agent |

Inside an agent, both work. Use `app.*` for AF-native features; use `suite.*`
for cross-cutting infra (billing, secrets, storage).

Outside an agent there is no `app` — you only have `suite.*`.

## Where developers hold the SDK (four locations)

| Location | Primary SDK | Why |
|---|---|---|
| Inside an AF agent (reasoner/skill code) | `app.*` (AF) | Direct control plane access, traced, cheap |
| Inside an app handler / job / cron | `suite.*` | Goes through gateway, auth resolved, tenant scoped |
| Inside the dashboard or frontend | `suite.*` (browser variant) | Session auth, tenant scoped, public gateway |
| From other languages (Rust/Java/etc.) | REST + OpenAPI | No SDK, just HTTP |

A `suite.agents.call("foo.func", input)` from a handler and an
`app.call("foo.func", input)` from an agent both end in the same AF execution.
The difference is the entry path (gateway vs control plane direct) and the
context resolution (HTTP session vs agent identity).

## Two-tier SDK split (the standard pattern)

Real SDKs split operational from administrative. Supabase has
`supabase.auth.admin.*`. Firebase has a separate `firebase-admin` package.
Stripe gates admin methods by restricted keys. We follow the same pattern:

| Tier | Lives in | When to use |
|---|---|---|
| **Main SDK** (`suite.*`) | `@af-stack/sdk` (TS), `af_stack` (Py) | App code, daily operations. ~22 methods. |
| **Admin SDK** (`suite.admin.*`) | `@af-stack/sdk-admin`, `af_stack.admin` | Onboarding flows, batch ops, custom dashboards. Separate auth. ~30 methods. |
| **Dashboard + CLI** | n/a | Schema migrations, module config, log tailing, replay, billing portal. Not in any SDK. |
| **Raw REST + OpenAPI** | n/a | Languages without SDKs, custom power-user needs. |

The split keeps the main SDK small enough that users can learn it.

## Main SDK (operational verbs, ~22 methods)

### `ctx.*` — request context (foundational, auto-injected)

```python
ctx.tenant_id      # current tenant (multi-tenancy module resolves it)
ctx.user_id        # current authenticated user (or None for system calls)
ctx.api_key_id     # if called via API key
ctx.request_id     # for log correlation
```

Set by middleware on every entry point (HTTP, job, cron, webhook). Used by
every other `suite.*` method.

### `suite.agents.*` — invoking AgentField

```python
# Invocation
result = await suite.agents.call("ns.func", input={...}, timeout_s=30)
exec   = await suite.agents.call_async("ns.func", input={...}, webhook_url="...")
async for event in suite.agents.stream("ns.func", input={...}):
    ...
status = await suite.agents.status(exec.id)
await suite.agents.cancel(exec.id, reason="...")

# HITL
pending = await suite.agents.pending_approvals()
await suite.agents.approve(exec.id, by_user_id=ctx.user_id, note="...")
await suite.agents.deny(exec.id, reason="...")
```

8 methods. Covers invocation + HITL.

### `suite.memory.*` — AF's distributed memory

```python
# Scopes: global | tenant | agent | session | run
await suite.memory.set(key, value, scope="session", session_id="...")
val = await suite.memory.get(key, scope="session", session_id="...")
hits = await suite.memory.search(query="...", top_k=5, scope="agent", agent="...")
```

3 methods.

### Infrastructure surface (one method per common verb)

```python
suite.jobs.enqueue(name, args)               # River-backed background jobs
suite.secrets.get(key)                       # per-tenant vault (read only)
suite.storage.upload(bytes, key="...")       # object storage
suite.storage.signed_url(key, expires_in_s)
suite.storage.download(key)
suite.notifications.email(to, template, data)
suite.billing.meter("llm_tokens", 1234, model="...")
suite.billing.has_budget(amount_usd=10)
suite.sandbox.run(image, command, files, timeout_s)
suite.webhooks.send(url, payload, secret)
suite.pubsub.publish(channel, message)
async for msg in suite.pubsub.subscribe(channel):
    ...
```

11 methods. Common stuff you reach for in handlers and jobs.

**Total main SDK: ~22 methods, 6 namespaces.** Supabase-shaped. Learnable.

## Admin SDK (`suite.admin.*`, ~30 methods)

Lives in a separate package, requires admin credentials. Use for:
onboarding flows, batch operations, custom dashboards, programmatic deploys.

```python
# Agent management
suite.admin.agents.versions("notable-ai")
suite.admin.agents.discover(tags=["ml*"])
suite.admin.agents.schema("ns.func")
suite.admin.agents.set_weight("notable-ai", version="2.1.0", weight=0.05)
suite.admin.agents.promote("notable-ai", version="2.1.0")
suite.admin.agents.rollback("notable-ai", to_version="2.0.0")
suite.admin.agents.replay(execution_id, edit={...})
suite.admin.agents.trace(execution_id)
suite.admin.agents.dag(execution_id)

# Policy
suite.admin.policy.allow(caller_tags=["finance"], target_tags=["payments"])
suite.admin.policy.deny(caller_tags=["intern"], target_tags=["billing"])
suite.admin.policy.check(caller=user, target_agent="payments.charge")

# MCP
suite.admin.mcp.list()
suite.admin.mcp.install(url="https://...")
suite.admin.mcp.attach(agent="my-agent", server_id="...", tools=[...])

# Secrets (admin: write/list/delete; main SDK is read-only)
suite.admin.secrets.set(tenant_id, key, value, rotate_in_days=90)
suite.admin.secrets.list(tenant_id)
suite.admin.secrets.delete(tenant_id, key)
suite.admin.secrets.rotate(tenant_id, key)

# Tenants / users / keys
suite.admin.tenants.create(slug, name, plan)
suite.admin.tenants.list()
suite.admin.tenants.update(tenant_id, fields)
suite.admin.users.create(email, name, tenants=[...])
suite.admin.users.list(filters)
suite.admin.keys.issue(tenant_id, scopes=[...])
suite.admin.keys.rotate(key_id)
suite.admin.keys.revoke(key_id)

# Audit
suite.admin.audit.search(tenant_id, date_range, filters)
suite.admin.audit.verify_credential(vc)
suite.admin.audit.export(tenant_id, format="csv")

# Harness (direct dispatch without writing a reasoner; rare)
suite.admin.harness.run(prompt, provider, tools, max_budget_usd)
```

~30 methods across 8 namespaces. Discoverable when needed.

## Not in any SDK (CLI + dashboard + REST)

- Schema migrations → `af-stack db migrate`
- Module enable/disable → `af-stack module enable X`
- Adapter swap → edit `config.yaml`, restart
- Log tailing → `af-stack logs tail` or dashboard
- Live trace inspection → dashboard
- Cost dashboards → dashboard
- Stripe billing portal → embedded link
- Plugin install → `af-stack plugin install <gh-repo>`

Power users who need any of these in code can hit the REST endpoints.

### OpenAI-compatible endpoint (NOT a separate path)

```bash
# Devs can point any OpenAI client at the suite:
POST /api/v1/llm/chat/completions
POST /api/v1/llm/embeddings
POST /api/v1/llm/images/generations
```

Internally these route through AF. They are NOT a back-door that bypasses AF.
They exist so existing tooling (Vercel AI SDK, LangChain, openai-python,
Cursor) just works. Cost, traces, identity, policy still apply.

There is no `suite.llm.chat()` SDK method. If you want OpenAI-format from
suite code, write a small AF reasoner or use the harness primitive.

## Three access tiers

| Tier | Use when |
|---|---|
| **AF SDK** (`app.*`) | Inside an AF agent process |
| **Suite SDK** (`suite.*`) | Anywhere outside an agent (handler, job, dashboard) |
| **REST + OpenAPI** | Languages without an SDK, frontend code without auth context, external systems |

All three target the same backend. SDKs are convenience.

## What the Suite SDK does NOT do

- **No direct provider calls.** Everything routes through AF.
- **No `suite.llm.*` standalone path.** OpenAI-compatible endpoint is the
  external-facing shim; internal code calls AF reasoners or
  `suite.harness.run()`.
- **No mock or stub mode.** Tests hit a real backend (compose) or mock at HTTP.
- **No client-side state.** No caches, no session managers. Tenant context
  comes from `ctx`, set by middleware.
- **No DSL for chaining agent calls.** Use `app.call()` (AF) inside agents or
  compose calls in your code outside them.

## Build order for v1

**Main SDK first (the 22 methods devs use daily):**

1. `ctx` (tenant + user + request context) — foundational
2. `suite.agents.call`, `.call_async`, `.stream`, `.status`, `.cancel`
3. `suite.jobs.enqueue`
4. `suite.secrets.get`
5. `suite.storage.upload`, `.signed_url`, `.download`
6. `suite.notifications.email`
7. `suite.billing.meter`, `.has_budget`
8. `suite.memory.get`, `.set`, `.search`
9. `suite.agents.approve`, `.deny`, `.pending_approvals`
10. `suite.sandbox.run`
11. `suite.webhooks.send`
12. `suite.pubsub.publish`, `.subscribe`

**Admin SDK after** (ship the 30 admin methods as a separate package once
main SDK has shipped and we have v1 users).

Python first. TypeScript parity. Go after.

Every method maps to a documented REST endpoint. OpenAPI spec auto-generated.

## The level-of-abstraction principle

Prefer **semantic verbs** over generic operations:

| Don't | Do |
|---|---|
| `suite.http_post(url, body)` | `suite.webhooks.send(url, payload)` |
| `suite.send_to_queue(name, args)` | `suite.jobs.enqueue(name, args)` |
| `suite.smtp_send(to, body)` | `suite.notifications.email(to, template)` |
| `suite.kms_encrypt(plaintext)` | `suite.secrets.set(key, value)` |
| `suite.execute_function(name, args)` | `suite.agents.call(name, input)` |

Generic verbs leak implementation. Semantic verbs name intent.

The exception: `suite.agents.call` is the generic "invoke any agent" verb.
A v2 polish would generate per-agent typed clients (`suite.agents.notable_ai.summarize_page(...)`)
from the registry — Convex-style. Defer to v2.

## Topics to groom later (open design questions)

These need dedicated grooming sessions before locking SDK shape:

| Topic | Open questions |
|---|---|
| **Agent identity / DIDs in app code** | What does app code need from DIDs? Just `suite.audit.verify_credential()`? Or do we expose `suite.agents.identity_of(agent)` for trust flows? |
| **Verifiable credentials surface** | `suite.audit.verify_credential(vc)` is one method, but what about generating VCs from app code (e.g., when a user signs off on a result)? |
| **Audit log API shape** | Compliance reports, customer-facing audit trails, search filters, export formats. |
| **Cost attribution when agent A calls agent B** | Who's billed in cross-agent calls? Tenant-bound rules. |
| **Cross-tenant agent calls** | Platform-owner agents calling tenant-scoped agents. Allowed? Auth shape? |
| **Agent commerce primitives** | Agents acting on behalf of users, signed actions, payments. The DID story productized. Could be a workload module. |

Each is half a day of focused design.

## Versioning

- AF SDK versions per AF release
- Suite SDK versions per AF Stack release
- AF Stack release pins a compatible AF version
- OpenAPI spec is the contract — additive changes only between minor versions

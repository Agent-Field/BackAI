# Primitives — Building blocks, not application-specific features

> *"We don't build for specific applications. We have one way and users
> can write their own and use as well, beyond chatbots."*

This is the canonical reference for AF Stack's abstraction hierarchy.
Every gap fix below must satisfy:

1. **General primitive** — works for chat, agents, RAG, content,
   whatever. Never name a primitive after a use case.
2. **Adapter pattern** — interface + swappable implementations. Config
   in YAML or env. Dashboard surfaces the active adapter.
3. **Workload-module-optional** — anything domain-specific (document
   parsers, web scrapers, search adapters) is a module the operator
   enables, not core.
4. **AgentField for observability** — runs, traces, DAGs, retry-from-step
   come from AgentField. Never vendor LangFuse/etc. — that's our own
   layer's job.
5. **LiteLLM for everything LLM** — provider routing, virtual keys,
   per-user budgets, per-key rate limits, cost analytics. We expose
   the unified view; LiteLLM enforces.

## Core primitives (already shipped)

| Primitive | What it is | Used for |
|---|---|---|
| **Tenant** | RLS-scoped data namespace | Anything multi-customer |
| **User** | Identity inside a tenant | Anything authenticated |
| **API key** | Bearer token bound to a tenant | SDK access |
| **Secret** | Encrypted KV | Any external integration |
| **Job** | Background task with retry | Anything async |
| **Cron** | Scheduled job | Anything periodic |
| **Memory entry** | Key-value with optional embedding, scoped | Any state |
| **Sandbox run** | Isolated code execution | Anything that runs untrusted code |
| **Webhook (in / out)** | HMAC-verified delivery | Any external event |
| **Notification** | Outbox-style delivery | Any user-facing message |
| **Cost event** | Per-call LLM ledger row | Any LLM spend |
| **Audit entry** | Append-only admin action log | Any admin mutation |

These are application-agnostic. A chat app uses memory + cost. An
agent app uses sandbox + jobs + memory. A RAG app uses memory + jobs.
Same primitives, different composition.

## Primitives we still need (within philosophy)

### 1. Sessions (NOT "conversation threads")

Append-only ordered sequence with optional auto-summarization. Works
for any app shape where you accumulate context over time.

```
suite_sessions          (id, tenant_id, scope, scope_id, summary, summary_at, created_at)
suite_session_entries   (session_id, idx, role, content, tokens, created_at)
```

SDK:
```python
session = app.sessions.create(scope="user", scope_id="alice")
session.append(role="user", content="What's our latency SLA?")
session.append(role="assistant", content="...")
# Auto-summarize when token count crosses threshold (config-driven).
# Hook is overridable per app.
session.read(limit=10)  # last 10 entries
session.summary()       # rolling summary string
```

**Used for:**
- Chat history (chat app)
- Agent step log (agent app)
- RAG conversation context (RAG app)
- Audit trail for a specific entity (any app)

**Auto-summarize hook** is a primitive: an interface the app can
override. Default summarizer uses our LLM gateway. Apps that don't want
LLM summarization swap in their own.

### 2. Content (NOT "document ingestion pipeline")

Raw input → chunk → embed → store in memory. General. Application
decides what's "content" — a markdown blog post, a PDF, a webpage, a
voice transcript, a code repo file.

```
suite_content       (id, tenant_id, source, mime, status, ingested_at)
suite_content_chunks (content_id, idx, text, embedding, metadata)
```

SDK:
```python
# General: pass bytes + mime type. Adapter handles parsing.
content = app.content.ingest(source="alice.pdf", bytes=b"...", mime="application/pdf")
# OR URL — uses the configured web fetcher adapter.
content = app.content.ingest(source="https://example.com/post")
# Retrieval — vector search over chunks, scoped by tenant.
results = app.content.search(query="our latency SLA", k=5)
```

**Adapters** (each is optional, configured via `config.yaml`):
- **Parsers:** `unstructured`, `marker`, `tika`, `none` (text passthrough)
- **Web fetchers:** `firecrawl`, `crawl4ai`, `trafilatura`, `simple` (just fetch)
- **Chunkers:** `recursive`, `markdown`, `code`, `sentence`, `none`
- **Embedders:** `litellm` (default), `local-bge`, `local-nomic`

**Used for:** any app that needs to ingest external knowledge. Chat
apps for long-term memory. RAG apps for knowledge base. Agent apps
for tool docs. Content apps for source material.

### 3. Tools (modular, swappable, configured)

Tools that agents call. Interface + adapter set, configured via
`config.yaml`. Same pattern as our LLM provider or sandbox adapter.

```go
type Tool interface {
    Name() string
    Description() string
    InputSchema() jsonschema.Schema
    Invoke(ctx context.Context, args map[string]any) (Result, error)
}
```

**Built-in tools** (each is a swap-able adapter):

| Tool | Best OSS | Notes |
|---|---|---|
| **Browser** | [browser-use](https://github.com/browser-use/browser-use) | LLM-driven browser control. Becoming the standard. Adapter: `Steel`, `Playwright`, `Browserless`. |
| **File system** | Sandbox-scoped FS we already have | Just expose as a tool wrapper |
| **Web search** | [SearXNG](https://github.com/searxng/searxng) self-hosted by default; adapters for Tavily / Brave / Exa / DuckDuckGo | One interface |
| **Code execution** | Our existing sandbox | Wrap as a tool |
| **HTTP fetch** | Just net/http behind safehttp | Generic HTTP from agents |
| **Shell command** | Our sandbox + a shell adapter | For ops agents |
| **SQL query** | Our DB studio query path | Read-only access for analytics agents |

Operator enables tools per tenant in the dashboard. Agent declares
which tools it uses. Sandbox adapter actually runs them.

Config example:
```yaml
tools:
  enabled:
    browser:
      adapter: browser-use
      headless: true
    web_search:
      adapter: searxng
      url: http://searxng:8080
    file_system:
      adapter: sandbox
```

**Used for:** any agent that needs to do anything beyond text.

### 4. Approvals (general, not "human-in-the-loop for agents")

Pause-for-acknowledgement primitive. Any flow can request an
approval; any operator can grant or deny it.

```
suite_approvals (id, tenant_id, requested_by, kind, payload, status, decided_by, decided_at)
```

```python
approval = app.approvals.request(kind="agent.tool_call", payload={...})
await approval.wait()   # blocks until decided
if approval.status == "approved":
    proceed()
```

**Used for:** agent ops review, content moderation queue, billing
override request, sensitive data access. Not chat-specific. Not
agent-specific.

## What we DON'T add (within philosophy)

- ❌ LangFuse / Helicone / Portkey — duplicate of AgentField + our gateway
- ❌ "Conversation threads" — that's an app of the Sessions primitive
- ❌ "Chat memory" — that's an app of the Memory + Sessions primitives
- ❌ "Document ingestion pipeline" — that's an app of the Content primitive
- ❌ "RAG framework" — that's a composition of Content + Memory + Tools
- ❌ "Agent observability dashboard" — extend the existing Runs tab
  with AgentField span data, don't add a separate tool

## Billing — abstraction, not Stripe-direct

You're right we haven't abstracted billing well. Today
`internal/billing/stripe_client.go` is Stripe-direct, which makes
[Lago](https://github.com/getlago/lago) (the OSS billing layer
sponsored by Y Combinator) hard to swap in.

Refactor (queued):

```go
type BillingAdapter interface {
    UpsertCustomer(ctx, tenant) (Customer, error)
    RecordUsage(ctx, tenant, meter, qty) error
    PortalLink(ctx, tenant, returnURL) (URL, error)
    HandleWebhook(ctx, body, sig) error
}
```

Adapters:
- `stripe` — current
- `lago` — new
- `none` — no billing

Operator picks via `AF_STACK_BILLING_ADAPTER`. Dashboard reflects.

## LLM cost + limits — through LiteLLM, not parallel

LiteLLM has all of:
- **Virtual keys** per user with budgets + rate limits enforced server-side
- `/spend/users` analytics endpoint per user
- `/spend/keys` analytics endpoint per key
- `/key/info/{key}` for live budget remaining

The refactor:

1. When we issue a `suite_api_key`, also create a LiteLLM virtual key
   keyed off the same id.
2. Per-user budget set in dashboard → POST to LiteLLM `/key/update`.
3. Per-user rate limit → same.
4. Our cost dashboard pulls from `/spend/...` endpoints, not our own
   `suite_cost_events`. We keep `suite_cost_events` as a write-through
   audit trail.
5. Drop the budget-gate hook on our side — LiteLLM enforces, returns
   429 with proper headers, we surface it.

**Result:** one source of truth. LiteLLM is the LLM-spend layer.

## Observability — through AgentField

AgentField already records per-run, per-step, per-tool-call data. The
fix is **surface it in our dashboard**:

- `/operate/runs` already lists agent executions
- Add a per-run **DAG view** that reads AgentField's span data
- Add **re-run from step** which AgentField already supports
- Add **tool call inspector** per step

No new tool. Extend existing UI.

## Layered diagram (corrected)

```
Customer apps + Operator dashboard
            ↓ REST / SDK
       AF Stack runtime  ←── all customer LLM calls go here
            ↓                  (we add tenant + per-user limits)
        LiteLLM proxy    ←── routes to 100+ providers,
            ↓                  enforces virtual-key budgets,
   OpenAI / Anthropic /        analytics endpoints we surface
   OpenRouter / etc.

            ↓ JSON-RPC
        AgentField      ←── orchestrates AF agents
            ↓                 records every span / tool call
       Agent containers  ←── host claude-code, codex,
   (one per agent)            harnesses, MCP servers,
                              tool adapters (browser, search)
```

## What this means for the next batch

Re-prioritized list of what to ship, all general primitives, all
vendoring OSS where it exists:

1. **Per-user budgets + per-key rate limits via LiteLLM virtual keys**
   — refactor `suite_api_keys` to also mint a LiteLLM key. Dashboard
   surfaces limits + spend per user. Drop our hand-rolled budget gate.

2. **Billing adapter interface** — extract Stripe-direct code, add a
   Lago adapter. Config-selectable.

3. **Sessions primitive** — `suite_sessions` + entries. Optional
   summarizer hook. Generic.

4. **Content primitive** — `suite_content` + chunks. Configurable
   adapters for parsers / web fetchers / chunkers / embedders.
   Knowledge-management workload module bundles common ones.

5. **Tools primitive** — adapter set for browser/search/fs/etc.
   Operator enables per tenant. Agents declare which they use.

6. **Approvals primitive** — generic pause-for-decision flow.

7. **Sub-task DAG + step inspector** — extend `/operate/runs` to read
   AgentField span data. No new sidecar.

Each one is a primitive in the platform sense. Apps compose them.
None of them is named after an app type.

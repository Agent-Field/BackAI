# Boundaries — what NOT to build on AF Stack

This is the loud rules file. When the user asks you to do something that
violates one of these, **STOP and propose the correct primitive
instead**. These boundaries exist for hard-won reasons; bypassing them
is the path to a fork that drifts from the platform and becomes
unmaintainable.

## The five hard boundaries

### B1 — Never duplicate AgentField

**Don't add** any of these to AF Stack core or to user workload modules:

- `suite_sessions` / `suite_threads` / `suite_chat_messages` —
  AgentField has Session-scope memory; use it.
- A second vector store — AgentField uses pgvector via memory; use
  `app.memory.similarity_search(...)` (in-agent) or
  `suite.memory.search(...)` (runtime).
- A trace / span / run store — AgentField records every span; surface it
  via dashboard reads from AgentField (planned).
- An eval framework — AgentField owns this.
- A prompt management UI — devs version prompts in git.

**Why**: AgentField IS the AI runtime. Duplicating its primitives in
af-stack creates two sources of truth, drifts on schema, and confuses
which one the user should use. The platform boundary in `STRATEGY.md`
is the contract.

**The correct primitive**:

| User says | You write |
|---|---|
| "I need chat history" | AgentField Session-scope memory: `app.memory.set(key, value)` in the agent, where the scope is Session and `scope_id` is the conversation token |
| "I need to store agent state" | AgentField Workflow-scope memory (cleared when run completes) |
| "I need per-user preferences" | AgentField Actor-scope memory |
| "I need a vector store" | Use `suite.memory.search(scope="...", query="...")` — already pgvector |
| "I need to see agent traces" | They're in AgentField's span store; the the planned AgentField dashboard work surfaces them |

### B2 — Never write a model provider client

**Don't add** an Anthropic / OpenAI / Google / Mistral / Groq / Cohere
client into the runtime or a workload module or an agent. **All LLM
calls go through LiteLLM** via:

- Runtime / workload module: `suite.llm.chat(...)` →
  `POST /api/v1/llm/chat/completions` (OpenAI-compatible).
- Inside an agent: `app.ai(...)` (which routes through the suite gateway).

**Why**: per-tenant cost attribution depends on every LLM call landing
in `suite_cost_events`. The LiteLLM sidecar handles 100+ providers — we
don't write provider clients ever. Lock-in lives in vendor SDKs; we
avoid all of it.

**The correct primitive**:

| User says | You write |
|---|---|
| "Let me add Anthropic SDK directly" | Use `suite.llm.chat({model: "claude-3-5-sonnet", messages: [...]})` — LiteLLM routes |
| "I want OpenRouter" | Same. Set `OPENROUTER_API_KEY` in `.env`. Pick a model with `openrouter/*` prefix |
| "I need a model not in the list" | Edit `apps/backend/litellm-config.yaml` to add it; no code change needed |
| "I want my own gateway" | Use the existing one. If you want hooks, see `services/runtime/internal/hooks/` and `internal/llmgateway/` |

### B3 — Adapters swap via env, not code

**Don't add** runtime-detected switching between storage / sandbox /
billing / notifications adapters. Don't write `if AWS_REGION { use s3 }
else { use minio }`. The adapter pattern is config-driven.

The right shape is what's already there: `internal/<area>/adapters/<id>/`
with an env var that picks one.

**Why**: the operator picks the adapter once, in env. Code that
auto-switches makes deploys non-deterministic and dashboards lie about
which adapter is active.

**The correct primitive**:

| User says | You write |
|---|---|
| "Detect if we're on AWS and use S3" | Don't. Operator sets `AF_STACK_S3_ADAPTER=s3` in prod, `minio` in dev |
| "Fall back to log notifications if Resend fails" | The adapter chosen is fixed. Health failures are operational concerns, not code-paths |
| "Add a new storage backend" | Implement the `Storage` interface in `internal/storage/adapters/<id>/`. Add the case to the factory. Document the env var |

### B4 — No app-shape primitives in core

**Don't add** to AF Stack core or to default workload-modules anything
named after an app type:

- "Conversation" / "Chat" / "Thread" — those are app shapes built ON
  primitives (memory + LLM gateway), not primitives.
- "Document" / "Article" / "Note" — your app's domain; not core.
- "RAG framework" — composition of storage + embeddings + memory +
  search. Not a primitive.
- "Knowledge base" — same.
- "Customer support ticket" — domain composition.

**Why**: AF Stack must work for chat apps, agent apps, multimodal apps,
analytics agents, voice apps — all of them. Naming a primitive after
one shape breaks the others.

**The correct primitive**:

| User wants | You compose with |
|---|---|
| Chat history | AgentField Session-scope memory + `suite.llm.chat()` |
| Doc upload + Q&A | `suite.storage` + chunker (user code) + `suite.embeddings` (not yet) + `suite.memory.search` |
| Knowledge base | Same as above, plus a workload module for ingestion routes |
| Tickets | A workload module with a `tickets` table + RLS + agent reasoner for triage |

Domain compositions live in **user workload modules** (the user's fork),
**example apps** (educational), or **opt-in workload modules** they
write. Never in `services/runtime/internal/`.

### B5 — Repo IS the product

**Don't add** code paths that depend on a SaaS we run. No "AF Cloud"
gates. No "this feature requires a managed offering" branches. No
feature flags that toggle between "OSS edition" and "Enterprise edition."

**Why**: AF Stack is Apache 2.0 and forkable. The repo the user clones
IS the running product. No hosted version exists to compete with the
fork. (This is the core differentiator from Supabase / Appwrite / Nhost —
see `POSITIONING.md`.)

**The correct primitive**: every feature is in the repo. Enterprise
controls like SSO, RBAC, BYOK, and GDPR (planned, tracked in `STRATEGY.md`) ship
in-tree. Operator opts in via env / config.

## Other rules with similar weight

These are conceptually weaker than B1-B5 but still load-bearing.

### B6 — Multi-tenancy is automatic. Never bypass.

Don't read `tenant_id` from a request body or query string. Don't pass
`tenant_id` as a function parameter to "skip" the resolver. RLS via the
`app.tenant_id` GUC is the safety net; bypassing it = a security bug.

If you need a cross-tenant operator query, use `app.bypass_rls = 'on'`
inside an audited operator route. See `rules/multi-tenancy.md`.

### B7 — Don't write to env from the UI

The dashboard is read-only on tier-1 + tier-2 config (per
`NAVBAR.md`). If the user wants to change adapters / providers /
modules, they edit `.env` or `config.yaml` and restart. The dashboard
shows what's active; it doesn't change it.

### B8 — Don't add tools to runtime handlers

Agent tools (browser-use, web search, file system, SQL, exec) live in
the agent container (declared in `__capabilities__`) or as MCP servers
that the agent calls. Don't wire tools into Go runtime handlers — the
runtime is the gateway, not the agent.

### B9 — Don't reinvent the customer app shell

`apps/customer-app/` already has: better-auth pages, dashboard layout,
sign-up flow (auto-provisions tenant + membership + API key), brand
theming via CSS variables. Edit pages under `(app)/`. Don't rewrite
`(auth)/` or the layout.

### B10 — Don't fork the agent SDK

`from agentfield import Agent` is the canonical agent definition. Don't
create your own `Agent` base class or wrap AgentField in your own
abstraction. Doing so disconnects you from AgentField's memory / spans /
harness invocation.

## How to apply these

When the user asks for X:

1. Check: does X violate B1-B5? If yes, STOP and explain. Quote the
   relevant boundary and propose the correct primitive.
2. Check: does X violate B6-B10? If yes, push back gently and offer
   the canonical pattern.
3. Otherwise, proceed using the patterns in `rules/`.

Examples of common requests + the correct response:

| User asks | Correct response |
|---|---|
| "Add a `chats` table for messages" | "Chat history belongs in AgentField Session-scope memory, not a runtime table. Here's how to call `app.memory.set(...)` in your agent: ..." |
| "Use the OpenAI SDK directly" | "All LLM calls route through LiteLLM. Use `suite.llm.chat({model: 'gpt-4o', ...})`. Add `OPENAI_API_KEY` to `.env` and LiteLLM picks it up." |
| "Build a RAG framework as a workload module" | "RAG is a composition, not a primitive. Compose `suite.storage` + `suite.embeddings` (not yet) + `suite.memory.search()` in your workload module's routes. See `examples/forge.md` for the composition pattern." |
| "Detect if Stripe is configured and fall back to Lago" | "Adapters don't auto-switch. The operator picks `AF_STACK_BILLING_ADAPTER` in `.env`. If you want graceful degradation, that's a config-time choice (`=none`)." |
| "Add a 'managed' tier with extra features" | "AF Stack is Apache 2.0; every feature ships in the repo. Add the feature for everyone or don't add it." |

## When in doubt

Read `POSITIONING.md` Part 1 (the strategic frame) and `STRATEGY.md`
("Ownership Boundary"). Those two are the source of truth for what
belongs where.

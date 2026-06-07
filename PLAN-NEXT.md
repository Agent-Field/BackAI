# Plan Next — Final, Honest, Within Philosophy

> **AgentField is the AI layer. af-stack is the SaaS platform around it.**
> Never duplicate what AgentField gives us.

This supersedes PRIMITIVES.md, COMPLETENESS-AUDIT.md, CAPABILITY-MATRIX.md
on the AI-side primitives question. The older docs drifted toward
re-implementing AgentField inside af-stack. Corrected here.

## The clear split

| Layer | Owns | Doesn't own |
|---|---|---|
| **AgentField** | AI-stateful: memory (4 scopes), vector store, session context, actor state, run/workflow state, agent registration, reasoner lifecycle, harness invocation, MCP host, skills, tool calls, every span/trace | Tenants, billing, auth, dashboards, public REST gateway |
| **af-stack runtime** | Tenants, users, memberships, API keys, secrets, the public LLM gateway (OpenAI-compat → LiteLLM), cost ledger, budgets/rate limits surfaced via LiteLLM, sandboxes (which AgentField agents use), webhooks, jobs, crons, notifications, storage, billing, audit, the dashboards | Memory, sessions, agent state — that's AgentField |
| **LiteLLM** | LLM provider routing (100+), virtual keys per user, per-key budgets + rate limits, cost analytics | Anything outside LLM |

If we ever find ourselves about to build "memory" or "session" or
"trace" inside af-stack — stop. That's AgentField.

## What AgentField already provides (verified from sdk/python/agentfield/memory.py)

**4 memory scopes**, hierarchical resolution:

```
Global  (shared across everything; retained until deleted)
  ↑
Session (per-conversation; cleared when session ends)
  ↑
Actor   (per-user, across sessions)
  ↑
Workflow (per-run; cleared when run completes)
```

`memory.get(key)` without an explicit scope resolves
`workflow → session → actor → global` and returns first hit.

This means **every AI-side state need** — chat history, agent step
log, RAG context, tool memory, scratch state — uses these scopes.
We don't add `suite_sessions`. We don't add `suite_threads`. We don't
add `suite_chat_messages`. We use AgentField.

## af-stack — what actually remains to ship

Pure platform/SaaS work. No AI-state duplication.

### Tier 1 — must ship next (each ~1 week)

#### 1. LiteLLM virtual keys → per-user budgets + per-key rate limits

- When we issue a `suite_api_keys` row, also POST to LiteLLM `/key/new`
  with the budget + rate limit configured in dashboard.
- Dashboard pulls live spend from `/spend/keys/{key}` and
  `/spend/users/{user}`.
- Drop our hand-rolled budget gate hook — LiteLLM enforces.
- `suite_cost_events` becomes a write-through audit table.
- Adds "budget remaining" + "rate limit" columns to dashboard's
  API-keys page, editable in the issue dialog.

#### 2. Billing adapter (Stripe + Lago)

- Extract `internal/billing/stripe_client.go` into a `BillingAdapter`
  interface.
- Lago becomes the second adapter (OSS, YC-backed, self-hostable).
- Operator picks via `AF_STACK_BILLING_ADAPTER=stripe|lago|none`.
- Dashboard reflects active adapter. Portal link works for both.

#### 3. Surface AgentField data in dashboard

- Extend `/operate/runs` with:
  - **DAG view** per run from AgentField span data
  - **Step inspector** showing tool call I/O at each step
  - **Re-run from step** (AgentField supports it)
  - **Memory tab on run sheet** showing workflow-scope entries
- Extend `/build/database`'s memory tab with a scope picker across the
  4 scopes.
- Zero new tables on af-stack side. UI work reading from AgentField.

#### 4. Approvals primitive (general SaaS need)

- Schema: `suite_approvals` (id, tenant_id, requested_by, kind,
  payload, status, decided_by, decided_at).
- General: any flow can request, any operator can decide.
- SDK: `app.approvals.request(...)` blocks; dashboard `Approvals` tab
  surfaces pending; operator approves or denies.
- Uses: ops review of destructive job, gating high-budget LLM call,
  content moderation queue, billing override.

#### 5. Code Helper → Shipwright (autonomous AI agent factory)

See the dedicated section below — this replaces the current toy code
helper.

### Tier 2 — bigger (~1-2 weeks each)

#### 6. Browser tool adapter set

- `Tool` interface in `internal/tools/`. NOT AgentField — these are
  tools agents call into.
- Adapters: `browser-use` (de-facto OSS standard), `Steel`,
  `Playwright`.

#### 7. Web search tool adapter set

- Same pattern. Adapters: `SearXNG` (self-hostable default), `Tavily`,
  `Brave`, `Exa`, `DuckDuckGo`.

#### 8. File system + code exec tool adapters

- File system wraps existing storage as a tool. Code exec wraps
  existing sandbox as a tool. Both small.

#### 9. Knowledge as workload module (OPT-IN, not core)

- `workload-modules/knowledge/` — Firecrawl + Unstructured + chunkers
  + embedders.
- Output is just AgentField memory entries scoped per tenant.
- Operator enables when they need RAG; ignores when they don't.

### Tier 3 — enterprise (~1 week each)

10. SSO/SAML via Authentik or WorkOS
11. RBAC via Casbin or Oso
12. BYOK secrets — adapter for cloud KMS
13. GDPR data export + erase endpoints

## Shipwright — the autonomous AI agent factory

Your direct words: **"SWE-AF is an autonomous AI agent factory."**

The current `/code-helper` is just an LLM-question demo — that's the
toy. Shipwright is:

- Customer pastes a task (or links a GitHub issue)
- af-stack spawns an autonomous AgentField agent in a fresh sandbox
- Agent uses claude-code / codex / gemini harness to autonomously:
  - Read the codebase
  - Plan the change
  - Make edits
  - Run tests
  - Iterate until passing
- Customer watches real-time progress (streaming logs + step DAG)
- Multiple agents can run in parallel — **factory**
- Final result: a PR, a patch file, or a deployed change

Reference points:
- **Devin** (Cognition) — closed
- **OpenHands** (was OpenDevin) — OSS
- **SWE-agent** (Princeton) — OSS
- **Cline** — VS Code extension

### Shipwright architecture (uses what we already have)

```
Customer → Customer-app /shipwright page
              ↓ submit task
         af-stack runtime
              ↓ enqueue job
         AgentField agent (shipwright-controller)
              ↓ spawn
         Sandbox (gVisor in prod, docker in dev)
              ↓ install
         claude-code / codex / gemini harness
              ↓ autonomous loop
         AgentField records every step + tool call
              ↓ status updates
         Customer-app subscribes to run events
              ↓ progress UI
         Customer sees streaming logs + DAG
```

Everything in this diagram already exists. Shipwright is the
**composition**.

### Shipwright tables (af-stack side, minimal)

```
suite_shipwright_tasks    (id, tenant_id, user_id, title, description,
                           repo_url, status, run_id, created_at)
suite_shipwright_patches  (task_id, ref, summary, diff_url, created_at)
```

Everything else (agent memory, step log, tool calls, every span) lives
in AgentField.

### Customer-app `/shipwright` (replaces `/code-helper`)

- Task submission form: title + description + (optional GitHub repo URL)
- Active tasks panel: list of running tasks with status badges
- Task drilldown:
  - Real-time agent log (subscribes via SSE to the AgentField run)
  - Step DAG (renders AgentField spans)
  - Final result (patch / diff / link to PR)
- "Spawn another" button — easy parallel work

## What we DON'T add (final, definitive)

- ❌ `suite_sessions` — AgentField has Session scope
- ❌ `suite_chat_messages` — chat history goes in AgentField session memory
- ❌ `suite_threads` — same
- ❌ Vector store on af-stack side beyond `suite_memory` — AgentField
  has the vector primitive; our `suite_memory` may even be redundant
  (audit later)
- ❌ Eval framework — AgentField will own this
- ❌ Prompt management — AgentField will own this
- ❌ LangFuse / Helicone / Portkey — we have AgentField + LiteLLM
- ❌ Anything called "conversation" or "document" or "RAG" — those are
  domain compositions, not platform primitives

## Customer-app URLs (after Tier 1)

- `localhost:34000/dashboard` — KPIs, recent calls, API key panel
- `localhost:34000/shipwright` — task submission + live agent runs
  (NEW, replaces code-helper)
- `localhost:34000/billing` — usage + plan (Lago or Stripe backed)
- `localhost:34000/api-key` — manage keys + per-key budgets + rate
  limits (NEW — pulled from LiteLLM virtual keys)
- `localhost:34000/approvals` — pending approvals (NEW)

## Order of operations

Strictly:

1. ✅ Streaming fix (commit bffd086)
2. **LiteLLM virtual keys** — Tier 1.1
3. **Billing adapter (Stripe + Lago)** — Tier 1.2
4. **Shipwright** — replaces code-helper, real customer demo — Tier 1.5
5. **AgentField DAG + step inspector in dashboard** — Tier 1.3
6. **Approvals primitive** — Tier 1.4
7. **Tool adapters: browser, search, fs, exec** — Tier 2.6–2.8
8. **Knowledge workload module (OPT-IN)** — Tier 2.9
9. **SSO / RBAC / BYOK / GDPR** — Tier 3

Each item ships independently. Each is general (no app-type naming).
Each either uses AgentField for AI state or stays out of AI state.

## Audit of what to potentially remove from af-stack

Suspect we may be duplicating AgentField in `services/runtime/internal/memory/`.
Need to verify:

- Does af-stack's `suite_memory` overlap with AgentField's memory?
- If yes, deprecate `suite_memory` and route the SDK's `memory.*`
  through AgentField.
- The dashboard's `/build/database` memory tab should query AgentField,
  not our PG table.

This is a queued cleanup, not a Tier 1 ship.

## Acknowledgment

I made the wrong calls in the last few docs by trying to invent
af-stack-side primitives for things AgentField already handles. The
right framing:

- **af-stack = SaaS platform** (tenants, billing, auth, gateways,
  sandboxes, jobs, webhooks, dashboards)
- **AgentField = AI brain** (memory, sessions, agents, traces, runs)
- **LiteLLM = LLM router** (providers, virtual keys, spend)

Anything AI-stateful → AgentField. Anything LLM-routing → LiteLLM.
Anything else → af-stack. Domain-specific stuff (knowledge ingestion,
specific tool integrations) → workload modules.

This is the plan. Tell me which item to start.

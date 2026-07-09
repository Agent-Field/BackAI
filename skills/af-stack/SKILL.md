---
name: af-stack
description: Build AI products on AF Stack — a Supabase-shape, Cal.com-forkable, Apache 2.0 self-hostable backend for AI. Use when asked to build, extend, or scaffold anything on the AF Stack platform (agents, workload modules, dashboard plugins, customer-app pages, adapters, deploys).
---

# AF Stack — Skill

You are building on **AF Stack**, an open-source self-hostable backend
platform for AI products. Architecture is Supabase-shape (Postgres + auth
+ storage + jobs + realtime + AI primitives). Distribution is Cal.com-style
forkable — the repo IS the product. AgentField is the AI runtime that
ships at one of the layers, peer to LiteLLM and Postgres.

**The primary path is the CLI.** `af-stack init --template <t>` scaffolds a
branded, batteries-included app; `af-stack dev` runs the whole stack locally;
the `deploy/` targets ship it. Your job is to help the user build on top of
that scaffold — never to rebuild what the platform already gives them.

Working directly in a raw fork of the repo (clone → brand → edit in-tree) is
the **fallback** for deep platform customization; reach for it only when the
CLI scaffold + the four edit surfaces below don't cover the need.

## Start here — the CLI (primary path)

```bash
af-stack init acme-coder --template coding-agent  # scaffold a branded app
cd acme-coder
af-stack dev                                       # whole backend + apps up
af-stack mcp add github --transport stdio \        # register tool servers
  --command "uvx mcp-server-github" --env GITHUB_TOKEN=secret:github_token
# edit the four surfaces below, then ship via deploy/ (Helm/Fly/Railway/Render/compose)
```

`af-stack init` writes the app under your cwd (a coding agent, customer-app,
multi-tenancy ON, a GH_TOKEN secret slot). Everything after is editing the four
surfaces. Prefer these commands over hand-copying files.

## Read these first

If unsure of the strategic frame, primitives table, or canonical DX:

- `docs/stack.md` — the 8-band layered architecture (Client / Edge / API
  Gateway / Intelligence / Execution / Delivery / Observability / Data)
- `development/positioning.md` — the canonical fork-and-edit DX + vocabulary
  (Workload Module · Dashboard Plugin · Adapter)
- `development/strategy.md` — the ownership boundary (AgentField vs af-stack vs
  LiteLLM) and the 4-phase plan
- `docs/product.md` — what's REAL vs needs-key vs not-in-v1

## The 4 edit surfaces (the most important table)

When the user asks you to build, you'll touch one or more of these.
Every other directory is platform code you don't edit.

| Surface | Where | Language | What goes here |
|---|---|---|---|
| **Customer App** | `apps/customer-app/src/app/(app)/...` | TypeScript / React | Branded SaaS pages the customer sees (sign-up, dashboard, billing, app-specific UI) |
| **Agent** | `apps/backend/agents/<name>/` | Python | AgentField agent definition + reasoners + harness use + MCP server registration |
| **Workload Module** | `workload-modules/<id>/` (scaffold with `af-stack module new <id>`) | Go in-runtime handler is the intended path, but the runtime loader isn't wired yet — a Python workload runs as an AF agent today | Backend HTTP routes + DB migrations + jobs + crons that aren't core platform |
| **Dashboard Plugin** | `apps/dashboard/plugins/<id>/` | TypeScript / React | Operator-console read-only tabs (charts, lists, status) |

Plus:
- `brand.yaml` — single brand config that drives both apps' CSS
- `.env` — operator config (provider keys, adapter choices)

## Available primitives (the second-most-important table)

This is the lookup. When deciding "which primitive do I use for X?",
consult this table. Status column: ✅ = shipped today · 🚧 = not yet
shipped (workaround below the table).

### Tier 1 — App-building primitives

These are what 99% of user code (agents · workload modules · customer-app)
calls. Two columns: how you call it from inside an agent (`app.*`) and
how you call it from a workload module / runtime handler (`suite.*`).

| Band | Primitive | `app.*` (agent) | `suite.*` (runtime) | Adapter env var | Status |
|---|---|---|---|---|---|
| ④ Intelligence | AgentField run data in dashboard | — (AgentField owns the deep view) | dashboard inlines summary card via `GET /api/v1/runs/{id}/agentfield`; controls via `POST /api/v1/runs/{id}/(cancel\|pause\|resume\|request-approval)`; "View in AgentField" link-out for DAG / step inspector | `AF_STACK_AGENTFIELD_PUBLIC_URL` (customer-facing AgentField URL) | ✅ |
| ④ Intelligence | LLM call | `app.ai(...)` | `suite.llm.chat(...)` | `AF_STACK_LLM_GATEWAY_ADAPTER=demo\|litellm\|remote` (validated; litellm picks model per call) | ✅ |
| ④ Intelligence | Embeddings | — | `suite.llm.embed(model, input)` | LiteLLM-routed (OpenAI-compatible) | ✅ |
| ④ Intelligence | Multimodal — TTS, STT, image gen/edit/variations | — | `suite.audio.speech/transcribe/translate(...)`, `suite.images.generate/edit/variations(...)` | LiteLLM for OpenAI catalog (tts-1, whisper-1, dall-e-{2,3}, gpt-image-1); first-party adapters for `elevenlabs/*`, `cartesia/*`, `flux/*`, `fal/*` (env-keyed). See `docs/multimodal.md`. | ✅ (#14) |
| ④ Intelligence | Memory (4 scopes: Global/Session/Actor/Workflow) | `app.memory.set/get/exists/delete/list/similarity_search(...)` | `suite.memory.put/get/list/search/delete(...)` | AgentField + pgvector | ✅ |
| ④ Intelligence | Harness (claude-code / codex / gemini / opencode) | `app.harness(provider="claude-code").run(...)` | — | installed in agent container | ✅ |
| ④ Intelligence | MCP tool | `app.mcp.call(...)` / declared in `__capabilities__` | `suite.tools.*` | MCP host (stdio + SSE) | ✅ |
| ④ Intelligence | Agent tool adapters (browser / search / fs / exec / http / sql) | `app.mcp.call("native:<tool>", "<verb>", {...})` | `suite.tools.invoke_native(tool, verb, args)` | `AF_STACK_TOOL_BROWSER`, `AF_STACK_TOOL_SEARCH`, `BROWSER_USE_URL`, `SEARXNG_URL`, etc. | ✅ |
| ⑤ Execution | Sandbox | — | `suite.sandbox.run(...)` | `AF_STACK_SANDBOX_ADAPTER=docker\|gvisor\|firecracker\|e2b\|remote` | ✅ |
| ⑤ Execution | Job (fire-and-forget) | — | `suite.jobs.enqueue(...)` | River (PG-backed). **Go in-process handlers only** — enqueuing a Python/TS (remote-language) job fails fast with `ErrRemoteJobsNotSupported`; cross-language dispatch is roadmap. | ✅ (Go) |
| ⑤ Execution | Cron | — | `suite.crons.list/create/get/set_active/delete(...)` | robfig/cron v3 | ✅ |
| ⑤ Execution | Webhook in (HMAC + dedup) | — | declare endpoint route + handler | (built-in) | ✅ |
| ⑤ Execution | Realtime | — | `suite.realtime.subscribe(table, rt_filter)` | PG LISTEN/NOTIFY → WebSocket at `GET /api/v1/realtime` | ✅ (Python lazy-loads optional `websockets` pkg) |
| ④ Intelligence | Realtime run subscriptions | — | `suite.runs.subscribe({tenant_id, user_id, agent, run_id, execution_id})` | AgentField SSE → WebSocket at `GET /api/v1/realtime/runs` | ✅ (#15; see `docs/realtime-runs.md`) |
| ⑥ Delivery | Webhook out | — | `suite.webhooks.send(...)` | native in-process outbox (PG-backed, HMAC signing, retry + exponential backoff, delivery ledger) | ✅ |
| ⑥ Delivery | Notification | — | `suite.notifications.send(...)` | `AF_STACK_NOTIFICATIONS_ADAPTER=log\|resend\|slack\|sms(twilio)\|push(fcm)\|remote` | ✅ |
| ⑥ Delivery | Billing | — | `suite.billing.*` | `AF_STACK_BILLING_ADAPTER=stripe\|lago\|none` (Lago adapter not yet) | ✅ (Stripe), 🚧 (Lago) |
| ⑧ Data | Postgres (RLS auto-bound) | — | direct via your handler's PG pool | (built-in) | ✅ |
| ⑧ Data | pgvector | — | via `suite.memory.search(...)` | (built-in) | ✅ |
| ⑧ Data | Storage (object) | — | `suite.storage.upload/download/signed_url/delete/list(...)` | `AF_STACK_S3_ADAPTER=minio\|s3\|remote` | ✅ |
| ⑧ Data | Search (FTS + vector hybrid) | — | `suite.search(query, mode)` | PG FTS + pgvector; mode = `"fts"\|"vector"\|"hybrid"` | ✅ |
| ③ API Gateway | Tenant context | — | `tenantctx.TenantID(ctx)` (Go) / `ctx.tenant_id` (Py/TS) | `AF_STACK_AUTH_ADAPTER=better-auth` (only impl; validated, not silently ignored) + RLS | ✅ |
| ③ API Gateway | Secrets | — | `suite.secrets.get/put/delete/list/reveal/rotate(...)` | `AF_STACK_SECRETS_ADAPTER=vault\|remote` (AES-256-GCM envelope; `remote` is roadmap — server still binds the vault type) | ✅ (`vault`), 🚧 (`remote`) |
| ③ API Gateway | OAuth-on-behalf-of-user | — | `suite.oauth.authorize_url(provider, scopes, return_to)`, `suite.oauth.connected()`, `suite.oauth.token(provider, user_id)`, `suite.oauth.disconnect(provider)` | `OAUTH_<NAME>_CLIENT_ID` / `_SECRET` per provider, or the **People → OAuth** setup dialog (vault-stored, no restart; vault wins over env) | ✅ (GitHub + Google shipped; Notion / Slack / Linear stubbed — see `docs/oauth.md`) |
| ③ API Gateway | Audit | — | auto on admin mutations | (built-in) | ✅ |
| ③ API Gateway | Cost ledger | — | auto on every LLM call. Source of truth: LiteLLM `/spend/keys` per AF Stack api key (item #22). `suite_cost_events` is write-through audit. | (built-in) | ✅ |
| ③ API Gateway | Per-key budget + rate limit | — | enforced upstream by LiteLLM. Set `budget_max_usd` / `rate_limit_rpm` / `rate_limit_tpm` on `POST /api/v1/admin/keys` (item #22). LiteLLM returns 429 / 402 when caps hit; AF Stack surfaces them as OpenAI-shaped errors. | LiteLLM (built-in) | ✅ |

### Yet to ship — workarounds below

These are real primitives we don't have yet. Each row describes the
intent, the workaround you can use today, and what the eventual surface
will be when it lands.

| Band | Primitive | Intent | Workaround today |
|---|---|---|---|
| ④ Intelligence | **Video generation** | Same shape as image generation but for `/api/v1/video/generations`. Provider routing: Pika / Runway / Luma when their LiteLLM coverage lands, or first-party adapter behind them. | Call the provider directly from a workload module (loses cost attribution). |
| ③ API Gateway | **PII redaction + moderation** | Pre/post hooks on the LLM gateway: regex-default + adapter for Presidio / AWS Comprehend. Transparent — no SDK call; just config. | Run redaction inside your reasoner before calling `app.ai(...)`. |
| ③ API Gateway | **Approvals** | Pause-for-decision primitive. Surface: `suite.approvals.request(kind, payload)`, dashboard tab for operators to decide. | Build it yourself in a workload module: `approvals` table + a poll loop in your agent that blocks until status changes. Common enough that the primitive is queued. |

### Tier 2 — Operator / inventory verbs

You'll rarely call these from app code. They power the operator
dashboard and admin tooling. Listed for completeness.

| Band | Primitive | `suite.*` (runtime) | Used by |
|---|---|---|---|
| ④ Intelligence | Harness inventory | `suite.harnesses.list/get/probe(...)` | Dashboard Build → Agents tab; healthcheck scripts |
| ⑥ Delivery | Cost events (audit log) | `suite.cost.events(tenant, limit, since)` | Dashboard Operate → Cost; audit exports |
| ③ Admin | Tenants | `suite.admin.tenants.list/get/create/update/delete(...)` | Dashboard Customers → Tenants |
| ③ Admin | Users | `suite.admin.users.list(...)` | Dashboard Customers → Users |
| ③ Admin | Memberships | `suite.admin.memberships.list/add/remove(...)` | Dashboard Customers |
| ③ Admin | API keys | `suite.admin.keys.list/issue/revoke(...)` | Dashboard Customers → API Keys |
| ③ Admin | Budgets | `suite.admin.budgets.list/get/set(...)` | Dashboard Operate → Cost |
| ④ Skills | Skill bundles | `suite.admin.skills.list/install/uninstall(...)` | Dashboard Build → Skills |
| ③ Audit | Audit log entries | `suite.admin.audit.list(...)` | Dashboard Customers → Audit |

If you're writing a normal app (agent · workload module · customer-app
page), you don't need Tier 2. Use the dashboard, or call these from an
operator-only workload-module route gated by your auth rules.

**Authoritative source**: for the live REST surface, read
`/api/v1/openapi.json` on a running runtime (or `apps/backend/static/openapi.json`).
For the Python SDK, see `packages/sdk-py/af_stack/`. For the TS SDK, see
`packages/sdk-ts/src/`. For the Go SDK, see `packages/sdk-go/suite/`.

## The 8 layered bands

See `docs/stack.md` for the diagram. Quick summary:

```
① Client         Dashboard · Customer App · Docs · SDKs · CLI
② Edge           Caddy (TLS, routing)
③ API Gateway    af-stack Go runtime (routing, auth, tenancy, audit, secrets)
④ Intelligence   AgentField · LiteLLM · MCP · Harnesses
⑤ Execution      Sandboxes · Jobs (River) · Crons · Webhooks IN
⑥ Delivery       Webhooks OUT (native outbox) · Notifications · Billing
⑦ Observability  OpenTelemetry · Prometheus · slog
⑧ Data           Postgres + pgvector · MinIO/S3
```

## Critical rules

These are non-negotiable. Each has a detailed rationale in `rules/`.

1. **Multi-tenancy is automatic.** Never read `tenant_id` from a query
   string. Tenant comes from session/API-key via the resolver; PG RLS
   enforces. See [`rules/multi-tenancy.md`](rules/multi-tenancy.md).
2. **Don't reinvent AgentField primitives.** Memory / sessions / runs /
   spans / vector store all belong to AgentField. Never add
   `suite_sessions`, `suite_threads`, `suite_chat_messages`, or a
   second vector store. See [`rules/boundaries.md`](rules/boundaries.md).
3. **Don't write LLM provider clients.** All LLM calls go through
   LiteLLM via `suite.llm.*` (runtime) or `app.ai(...)` (agent). Adding
   a direct Anthropic/OpenAI client is wrong. See [`rules/boundaries.md`](rules/boundaries.md).
4. **Agent tools = MCP or `app.tools.*`.** Tools that agents call live
   in the agent container (claude-code, codex) or as MCP servers (stdio
   via uvx / SSE). Don't wire tools into runtime handlers.
5. **Workload modules live under `workload-modules/<id>/`** (scaffold with
   `af-stack module new <id>`).
   Don't add new backend HTTP routes directly into
   `services/runtime/internal/server/`. See [`rules/workload-modules.md`](rules/workload-modules.md).
6. **Dashboard plugins are read-only.** Operator console shows state;
   config still happens via env vars + `brand.yaml`. Don't add settings
   UIs that write to env. See [`rules/dashboard-plugins.md`](rules/dashboard-plugins.md).
7. **Adapters swap via env var.** Don't add runtime-detected switching
   between storage / sandbox / billing / notifications adapters. See
   [`rules/adapters.md`](rules/adapters.md).
8. **Brand state lives in `brand.yaml`; generated `brand.css` files are
   outputs.** Don't
   hardcode product name, colors, or logos in TS/Go. See
   [`rules/customer-app.md`](rules/customer-app.md).
9. **No bypass of the LLM gateway.** Every LLM call hits `/api/v1/llm/*`.
   Per-tenant cost attribution depends on it. Rate limits are enforced
   **upstream** by LiteLLM via per-virtual-key `rpm_limit` / `tpm_limit`
   (item #22); the runtime no longer runs a local limiter (item #32 —
   `services/runtime/internal/ratelimit` was deleted). 429 responses
   carry `Retry-After` + `X-RateLimit-*` headers proxied through. See
   [`rules/sdk.md`](rules/sdk.md) → "LLM rate limits — 429 responses".
10. **The repo IS the product.** No "managed offering" code paths, no
    "free tier" feature gates in OSS. We don't ship code that depends on
    a SaaS we run. See `development/positioning.md`.

## Canonical workflow — when the user says "build X on AF Stack"

Follow this sequence. Don't skip steps.

1. **Classify the app shape.** Is X a chatbot? agent? multimodal?
   workflow? data tool? operational? See
   [`examples/README.md`](examples/README.md) for the shape catalog.
2. **Pick edit surfaces.** Which of the 4 does X need?
   - Most non-trivial apps need: agent + workload module + dashboard
     plugin + customer-app pages.
   - Pure UI feature: customer-app + maybe a dashboard plugin.
   - Pure backend / pipeline: agent + workload module.
3. **Map primitives.** Walk the primitives table; mark which rows X
   uses. If any are 🚧 (roadmap), warn the user and propose a workaround
   or wait.
4. **Scaffold.** For a NEW project, start from the CLI:
   `af-stack init <name> --template <coding-agent|node>`. To add a surface to
   an EXISTING project, copy the matching template from `snippets/`:
   - New agent → `snippets/agent.py`
   - New workload module (Python sidecar) → `snippets/workload-module/`
   - New dashboard plugin → `snippets/dashboard-plugin/`
   - New customer-app page → `snippets/customer-app-page.tsx`
   Drop into the right surface path. Rename + edit.
5. **Wire with SDK only.** Connect surfaces using `suite.*` (runtime
   handlers, dashboard, customer-app) or `app.*` (inside agents). Never
   reach the DB / LiteLLM / AgentField directly from outside its layer.
6. **Run locally.** `af-stack dev` brings up the whole stack (falls back to
   `docker compose up` in a raw fork). Use the runtime's
   `/api/v1/openapi.json` to verify your routes are registered.
7. **Deploy.** `deploy/` has Helm / Fly / Railway / Render / compose — pick a
   target and ship the whole unit.

**If a request would violate a Critical Rule, STOP.** Propose the
correct primitive instead. Example: user says "let me add a
`suite_chat_messages` table for chat history" → respond "use AgentField
Session-scope memory; see `rules/boundaries.md`."

## SDK reference

- **Python**: `packages/sdk-py/af_stack/` — `suite.{agents, llm, memory,
  storage, sandbox, jobs, crons, secrets, billing, notifications,
  webhooks, cost, harnesses, tools, search, realtime}` + `ctx` (request
  context). Realtime server bridge is shipped (`GET /api/v1/realtime`,
  Postgres LISTEN/NOTIFY → WebSocket). Python `realtime.subscribe`
  lazy-imports the optional `websockets` package.
- **TypeScript**: `packages/sdk-ts/src/` — same shape.
- **Go**: `packages/sdk-go/suite/` — empty stub today (`doc.go` + version
  only); the Go SDK is planned, not yet implemented.
- **AgentField (inside agents)**: `from agentfield import Agent, AIConfig`
  + `app.ai(...)`, `app.memory.*`, `app.harness(...)`, `@app.reasoner(...)`.
- **OpenAPI (machine-readable)**: `GET /openapi.json` on a running
  runtime, or `apps/backend/static/openapi.json`.

## Detailed references (fetch on demand)

| Rule | When to fetch |
|---|---|
| [`rules/boundaries.md`](rules/boundaries.md) | Before adding anything that *might* duplicate AgentField / LiteLLM / a core primitive |
| [`rules/primitives.md`](rules/primitives.md) | Need the deep description of a primitive (gotchas, alternatives) |
| [`rules/edit-surfaces.md`](rules/edit-surfaces.md) | Deciding which surface(s) to use |
| [`rules/multi-tenancy.md`](rules/multi-tenancy.md) | Writing any handler that touches DB rows |
| [`rules/agents.md`](rules/agents.md) | Writing an AgentField agent (Python) |
| [`rules/workload-modules.md`](rules/workload-modules.md) | Writing a Python sidecar service or Go workload module |
| [`rules/dashboard-plugins.md`](rules/dashboard-plugins.md) | Adding an operator-console tab |
| [`rules/customer-app.md`](rules/customer-app.md) | Editing the customer-facing Next.js app |
| [`rules/adapters.md`](rules/adapters.md) | Swapping a primitive's backend (storage/sandbox/billing) |
| [`rules/deploy.md`](rules/deploy.md) | Production deployment |
| [`rules/sdk.md`](rules/sdk.md) | Looking up specific SDK calls |

## Templates

| Snippet | Use when |
|---|---|
| [`snippets/agent.py`](snippets/agent.py) | New AgentField agent |
| [`snippets/workload-module/`](snippets/workload-module/) | New Python sidecar service (current canonical pattern) |
| [`snippets/dashboard-plugin/`](snippets/dashboard-plugin/) | New operator-console tab |
| [`snippets/customer-app-page.tsx`](snippets/customer-app-page.tsx) | New customer-facing page |

## Worked examples

| Example | App shape | Primitives shown |
|---|---|---|
| [`examples/forge.md`](examples/forge.md) | Reactive single-shot agent (GitHub PR reviewer) | Webhooks-in · Agents · Sandboxes · Harnesses · Multi-tenancy |
| [`examples/README.md`](examples/README.md) | App-shape catalog with 12 startup ideas | Use as a starting point when classifying |

## Final reminder

**Lead with the CLI** (`af-stack init/dev`, the `deploy/` targets) — that is
how the user scaffolds and runs their app. The scaffold + the four edit
surfaces are where their code lives; everything else is platform code you
don't touch. (Under the hood the app IS a fork of AF Stack the user owns —
but only fall back to raw fork-and-edit when the CLI + surfaces don't cover
the need.) When in doubt, ask "is this user code or platform code?" and keep
your edits firmly in user code.

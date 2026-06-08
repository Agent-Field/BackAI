# AF Stack Claude Skill — Plan

> **Goal**: a skill bundle that, when loaded into Claude Code (or any
> agent), lets it build any AI application on AF Stack without
> hallucinating APIs, without reinventing primitives, and without
> violating platform boundaries. Extends cleanly as the platform grows.
>
> **Scope of this doc**: the *plan* for the skill — directory layout,
> contents of each file, anti-hallucination measures, what's
> intentionally excluded, and the update protocol. The skill itself is
> written as a follow-up once this plan is approved.

---

## 1. Design principles

Five rules that determine every decision below.

1. **Reference, don't duplicate.** Where canonical info already lives in
   the repo (STACK.md, POSITIONING.md, STRATEGY.md, OpenAPI), the skill
   *links* rather than re-stating. Avoids drift.
2. **Verifiable, not generative.** Every API endpoint mentioned must
   exist in `/openapi.json`. Every SDK call must exist in
   `packages/sdk-{py,ts,go}`. Every file path must exist in the repo.
   No "you might use suite.x.y()" unless `suite.x.y` is real.
3. **Pattern-first, snippet-second.** The skill teaches *patterns* (how
   to think) before giving *snippets* (how to type). A patternless
   snippet is what makes agents copy-paste without judgment.
4. **Extensible by single-file edit.** New primitive → one row in one
   table. New adapter → one row. The skill must be diff-friendly so
   Phase 1/2/3 work doesn't require rewrites.
5. **Explicit boundaries.** The skill must say what NOT to do as
   loudly as what to do. "Don't build memory in AF Stack" is more
   load-bearing than "here's how to write a Go handler."

---

## 2. Directory layout

```
skills/af-stack/
├── SKILL.md                          # entry point — loaded first
├── rules/
│   ├── primitives.md                 # what's available + when to use what
│   ├── edit-surfaces.md              # the 4 edit surfaces in detail
│   ├── multi-tenancy.md              # RLS + tenant context patterns
│   ├── agents.md                     # writing AgentField agents
│   ├── workload-modules.md           # writing Go workload modules
│   ├── dashboard-plugins.md          # writing operator console plugins
│   ├── customer-app.md               # extending the customer-facing app
│   ├── adapters.md                   # swapping adapters
│   ├── boundaries.md                 # what NOT to build (loud)
│   ├── deploy.md                     # deploy targets + brand.yaml
│   └── sdk.md                        # suite.* and app.* surface map
├── snippets/
│   ├── agent.py                      # canonical agent template
│   ├── workload-module/
│   │   ├── manifest.yaml
│   │   ├── handlers/handler.go
│   │   └── migrations/00001_init.sql
│   ├── dashboard-plugin/
│   │   ├── plugin.ts
│   │   └── page.tsx
│   └── customer-app-page.tsx
├── examples/
│   ├── forge.md                      # GitHub PR reviewer walkthrough
│   ├── (future).md                   # one per shipped example app
│   └── README.md                     # picker / contrast table
└── meta/
    ├── version.json                  # { skill_version, repo_sha, generated_at }
    └── verification.json             # auto-generated: { route_count, sdk_call_count, ... }
```

**Why this shape**:
- `SKILL.md` is short — Claude can hold it all in context.
- `rules/` and `snippets/` are *deep links* — Claude fetches when needed.
- `examples/` is the inspiration layer — patterns proven in real apps.
- `meta/` is for skill maintenance + drift detection.

---

## 3. SKILL.md — the entry point

The single file that's always read first. Keeps it tight (~200 lines).

### Section structure

| Section | Purpose | Length |
|---|---|---|
| 1. What AF Stack is | One paragraph; links to POSITIONING.md | 5 lines |
| 2. The 4 edit surfaces | Table: name · path · what you put there | 15 lines |
| 3. Primitives table | The 24-row primitive map (band · primitive · SDK · adapter) | ~30 lines |
| 4. The 8 layered bands | Reference link to STACK.md; list the bands | 10 lines |
| 5. Critical rules | The hard boundaries (8-10 bullets, each links to rules/) | 25 lines |
| 6. Canonical workflow | "When asked to build X, follow this sequence" | 20 lines |
| 7. SDK reference | Where to look up `suite.*` and `app.*` (links) | 5 lines |
| 8. Detailed references | Index of `rules/`, `snippets/`, `examples/` | 15 lines |

### Sections in detail

#### §1 — What AF Stack is

```markdown
AF Stack is a self-hostable backend platform for AI products
— Supabase-shape architecture, Cal.com-shape forkability,
AgentField as the AI runtime. The repo IS the product:
users clone, brand, write their agents + customer app +
workload modules + dashboard plugins in-tree, and deploy
the whole thing as one unit.

For positioning: POSITIONING.md
For layered architecture: STACK.md
For strategy + ownership boundary: STRATEGY.md
```

#### §2 — The 4 edit surfaces (the most important table in the skill)

| Surface | Path | What goes here | Language |
|---|---|---|---|
| **Customer App** | `apps/customer-app/src/app/(app)/...` | The branded SaaS pages your customers see | TypeScript / React |
| **Agent** | `apps/backend/agents/<name>/` | AgentField agents + reasoners + harnesses | Python |
| **Workload Module** | `workload-modules/<id>/` | Go HTTP routes + DB migrations + jobs | Go |
| **Dashboard Plugin** | `apps/dashboard/plugins/<id>/` | Operator-console tabs (read-only views) | TypeScript / React |

Plus `brand.yaml` (single brand config) and `.env` (operator config).

#### §3 — Primitives table (the second-most-important table)

This is the lookup. Claude consults this when deciding "which primitive
do I use for X?"

Columns: **Band · Primitive · How to call from agent (`app.*`) · How to
call from runtime (`suite.*`) · Adapter env var · Status**.

Example rows:

| Band | Primitive | `app.*` (agent) | `suite.*` (runtime) | Adapter | Status |
|---|---|---|---|---|---|
| ④ Intelligence | LLM call | `app.ai(...)` | `suite.llm.chat(...)` | LiteLLM (model picked per call) | ✅ |
| ④ Intelligence | Memory (4 scopes) | `app.memory.set/get(...)` | `suite.memory.put/get(...)` | AgentField (pgvector) | ✅ |
| ④ Intelligence | Harness | `app.harness("claude-code").run(...)` | — | Claude Code / Codex / Gemini | ✅ |
| ④ Intelligence | MCP tools | `app.tools.*` | — | MCP host (stdio/SSE) | ✅ |
| ⑤ Execution | Sandbox | — | `suite.sandbox.run(...)` | `AF_STACK_SANDBOX_ADAPTER=docker\|gvisor\|firecracker\|e2b` | ✅ |
| ⑤ Execution | Job (River) | — | `suite.jobs.enqueue(...)` | (built-in) | ✅ |
| ⑤ Execution | Cron | — | `suite.crons.create(...)` | (built-in) | ✅ |
| ⑥ Delivery | Webhook in | — | declares endpoint in workload manifest | (built-in HMAC verify) | ✅ |
| ⑥ Delivery | Webhook out | — | `suite.webhooks.send(...)` | Svix sidecar | ✅ |
| ⑥ Delivery | Notification | — | `suite.notifications.send(...)` | `AF_STACK_NOTIFICATIONS_ADAPTER=log\|resend` | ✅ |
| ⑥ Delivery | Billing | — | `suite.billing.*` | `AF_STACK_BILLING_ADAPTER=stripe\|lago` | ✅ (Phase 3) |
| ⑧ Data | Postgres (RLS) | — | `suite.db.*` (auto-bound) | (built-in) | ✅ |
| ⑧ Data | Storage | — | `suite.storage.put/get/url(...)` | `AF_STACK_S3_ADAPTER=minio\|s3\|r2\|gcs` | ✅ |
| ③ API Gateway | Auth | — | `tenantctx.TenantID(ctx)`, `userctx.UserID(ctx)` | better-auth | ✅ |
| ③ API Gateway | Secrets | — | `suite.secrets.get/put(...)` | AES-256-GCM envelope | ✅ |
| ③ API Gateway | Audit | — | (auto on admin mutations) | (built-in) | ✅ |
| ② Frontend | brand.yaml | — | — | single source of brand state | Phase 1 |
| (Phase 2) | Realtime | — | `suite.realtime.subscribe(...)` | PG LISTEN/NOTIFY → WS | 🚧 |
| (Phase 2) | Embeddings | — | `suite.embeddings.create(...)` | LiteLLM-routed | 🚧 |
| (Phase 2) | Search | — | `suite.search(q, mode)` | PG FTS + pgvector | 🚧 |
| (Phase 2) | Multimodal | — | `suite.audio.*`, `suite.images.*` | LiteLLM + adapters | 🚧 |
| (Phase 2) | Tool adapters | `app.tools.browser/search/fs/exec/...` | — | per-tenant enable | 🚧 |
| (Phase 2) | PII redaction | — | (gateway pre/post hooks) | regex / Presidio | 🚧 |
| (Phase 2) | OAuth-on-behalf | — | `suite.oauth.authorize_url(...)` | per-provider | 🚧 |

✅ = shipped today · 🚧 = in roadmap (Phase 2)

Status column is the **extensibility lever**: as items land, change 🚧
to ✅. One column edit per feature.

#### §4 — The 8 layered bands

```markdown
See STACK.md for the layered architecture.

① Client      — Dashboard, Customer App, Docs, SDKs, CLI
② Edge        — Caddy (TLS, routing)
③ API Gateway — af-stack Go runtime (routing, auth, tenancy, audit, secrets)
④ Intelligence — AgentField · LiteLLM · MCP · Harnesses
⑤ Execution   — Sandboxes · Jobs (River) · Crons · Webhooks IN
⑥ Delivery    — Svix (webhooks OUT) · Notifications · Billing
⑦ Observability — OpenTelemetry · Prometheus · slog
⑧ Data        — Postgres + pgvector · MinIO/S3 · Redis (Svix-private)
```

#### §5 — Critical rules

```markdown
The hard boundaries. Each links to rules/<file>.md for detail.

1. **Multi-tenancy is automatic.** Never read tenant_id from a query
   string. Tenant comes from session/API-key via the resolver; PG RLS
   enforces. See rules/multi-tenancy.md.

2. **Don't reinvent AgentField primitives.** Memory / sessions / runs /
   spans / vector store all belong to AgentField. Never add
   `suite_sessions`, `suite_threads`, `suite_chat_messages`, or a
   second vector store. See rules/boundaries.md.

3. **Don't write LLM provider clients.** All LLM calls go through
   LiteLLM via `suite.llm.*` or `app.ai(...)`. Adding a direct
   Anthropic/OpenAI client is wrong. See rules/boundaries.md.

4. **Tools = MCP or `app.tools.*`.** Agent tools live in the agent
   container (browser-use, claude-code) or as MCP servers. Don't wire
   tools into runtime handlers.

5. **Workload modules live under workload-modules/.** New backend HTTP
   routes go here, not in `services/runtime/internal/server/`. See
   rules/workload-modules.md.

6. **Dashboard plugins are read-only.** Operator console shows state;
   config still happens via env vars. Don't add "Settings" UIs that
   write to env. See rules/dashboard-plugins.md.

7. **Adapters swap via env var.** Don't add code paths that switch
   storage/sandbox/billing/notifications by runtime detection. See
   rules/adapters.md.

8. **Brand state lives in brand.yaml.** Don't hardcode product name,
   colors, or logos in TS/Go. See rules/customer-app.md.

9. **No bypass of the LLM gateway.** Every LLM call hits
   `/api/v1/llm/*`. No direct provider calls. Per-tenant cost is the
   reason.

10. **The repo IS the product.** No "managed offering" code paths, no
    "free tier" gates in OSS. We don't ship code that depends on a
    SaaS we run. See POSITIONING.md.
```

#### §6 — Canonical workflow

```markdown
When the user says "build me X on AF Stack":

1. **Classify**: which app shape? (chat / agent / multimodal /
   workflow / data / operational) — see examples/README.md
2. **Pick edit surfaces**: which of the 4 surfaces does X need?
   - Almost all: agent + workload module + dashboard plugin + customer app
   - Pure UI feature: customer app + maybe dashboard plugin
   - Pure backend feature: agent + workload module
3. **Map primitives**: look at the primitives table; mark which rows X
   uses. Anything 🚧 = roadmap; warn the user if X depends on it.
4. **Scaffold**:
   - `af-stack agent new <name>` (when CLI v2 lands; today copy from
     snippets/agent.py)
   - `af-stack module new <id>` (or copy from snippets/workload-module/)
   - `af-stack plugin new <id>` (or copy from snippets/dashboard-plugin/)
   - Customer app: add pages under apps/customer-app/src/app/(app)/
5. **Wire**: connect agent ↔ workload-module ↔ dashboard-plugin ↔
   customer-app using SDK calls (suite.*) — never direct DB / direct
   LiteLLM.
6. **Test**: `af-stack dev` (when CLI lands; today `docker compose up`).
7. **Deploy**: `af-stack deploy <target>` (helm/fly/railway/render).

If the user asks for something that would violate a Critical Rule (§5),
STOP and propose the correct primitive instead.
```

#### §7 — SDK reference

```markdown
Python SDK (`packages/sdk-py/af_stack/`): see packages/sdk-py/README.md
TypeScript SDK (`packages/sdk-ts/src/`): see packages/sdk-ts/README.md
Go SDK (`packages/sdk-go/suite/`): see packages/sdk-go/README.md
OpenAPI (all REST routes, machine-readable): GET /openapi.json on
  running runtime, or check apps/backend/static/openapi.json
```

#### §8 — Detailed references

Index of `rules/<topic>.md`, `snippets/<template>/`, `examples/<app>.md`.

---

## 4. rules/ — detailed reference files

Each is fetched only when relevant. Together they cover everything
SKILL.md links to.

### rules/primitives.md (~150 lines)
- Full primitive descriptions per row of the §3 table
- Each row: what the primitive does, when to use it vs alternatives,
  what's free vs what you write, a tiny code snippet

### rules/edit-surfaces.md (~80 lines)
- The 4 surfaces in detail
- When to use which (decision tree)
- Anti-patterns (e.g. don't put backend routes in customer-app)

### rules/multi-tenancy.md (~100 lines)
- How RLS works (`app.tenant_id` GUC)
- How tenant context is bound (middleware)
- Patterns for cross-tenant operator queries
- Common mistakes (querying without context, leaking IDs)

### rules/agents.md (~150 lines)
- How to write an Agent (the Python pattern)
- `app.ai()`, `app.harness()`, `app.tools.*`, `app.memory.*`
- Reasoner patterns, structured outputs (Pydantic)
- The decision tree from CLAUDE.md (`.ai()` vs `.harness()` from your
  existing project guide — pull it in)
- Streaming + cancellation
- Anti-patterns

### rules/workload-modules.md (~120 lines)
- The manifest.yaml schema
- Handler signatures (input ctx, write to RLS-bound pool)
- Migrations (one per module, own schema)
- Routes mount under `/workload/<id>/`
- Public vs authenticated routes
- Anti-patterns (e.g. don't reach into other modules' tables)

### rules/dashboard-plugins.md (~80 lines)
- The plugin.ts manifest
- page.tsx as React Server Component
- Using `@/lib/api` helpers
- Read-only constraint
- Sidebar group choice (Build / Operate / Customers / Infrastructure)

### rules/customer-app.md (~80 lines)
- Edit zones: free vs do-not-touch vs brand-only
- `suite.*` SDK from the customer side
- Auth: better-auth pages are pre-wired; don't edit
- Routing under `(app)/`, `(auth)/`
- Brand override via brand.yaml + brand.css

### rules/adapters.md (~80 lines)
- The interface pattern
- Where adapters live (`internal/<area>/adapters/<id>/`)
- How operator picks (env var)
- How to add a new adapter (rare; this is mostly read-only knowledge)
- Adapter catalogue (matches primitive table column)

### rules/boundaries.md (~100 lines)
- The hard boundaries (Critical Rules expanded)
- AgentField boundary (memory / sessions / runs / spans)
- LLM boundary (no direct providers; LiteLLM only)
- Adapter boundary (no code-detected switching)
- App-shape vs primitive boundary (no chat / document / conversation as
  core)
- Why each boundary exists (with the historical lesson where one
  applies)

### rules/deploy.md (~80 lines)
- The deploy targets (helm / fly / railway / render / compose)
- brand.yaml + env vars per target
- Health/readiness probes
- Multi-replica gotchas (RLS already handles, queue is shared)

### rules/sdk.md (~80 lines)
- `suite.*` surface map (every namespace + what it does)
- `app.*` surface map (every namespace from AgentField)
- Where to look up the latest (auto-generated SDK docs)
- Common patterns (paginated list, streaming, cancellation)

---

## 5. snippets/ — copy-paste templates

Real, tested, paste-and-edit. Each snippet has a comment header explaining
what to change.

| Snippet | What | Used when |
|---|---|---|
| `agent.py` | Minimal Agent with one reasoner and `.ai()` | New agent |
| `workload-module/manifest.yaml` | Required fields + comments | New module |
| `workload-module/handlers/handler.go` | Handler signature + RLS-bound pool access | New module |
| `workload-module/migrations/00001_init.sql` | RLS-enabled table template | New module |
| `dashboard-plugin/plugin.ts` | Plugin manifest | New plugin |
| `dashboard-plugin/page.tsx` | RSC page with `@/lib/api` | New plugin |
| `customer-app-page.tsx` | Customer-app page with `suite.*` SDK | New customer page |

These are *literal templates* — Claude can copy and edit. NOT
prose-described patterns; actual files.

---

## 6. examples/ — proven walkthroughs

Each is a doc walking through a real app built on AF Stack, like the
Forge walkthrough I just gave you. Acts as "this pattern definitely
works" reference.

| Example | App shape | Primitives exercised |
|---|---|---|
| `forge.md` | Reactive single-shot agent (PR review) | Webhooks-in · Agents · Sandboxes · Harnesses · Multi-tenancy |
| (future) `mercer.md` | Parallel long-running agents (SDR) | Tools · OAuth-on-behalf · Jobs · Memory |
| (future) `halcyon.md` | Real-time multimodal (medical scribe) | STT · Realtime · Audit · BYOK |
| (future) `pinion.md` | Operational AI (bookkeeper) | Approvals · Webhooks-in · RAG patterns |
| (future) `atlas.md` | Tool-using agent over data (NL BI) | SQL tool · Sandbox · Realtime |

`examples/README.md` has the contrast table so Claude knows which
example to reference when the user describes a new app shape.

---

## 7. meta/ — skill-maintenance machinery

### meta/version.json

```json
{
  "skill_version": "0.1.0",
  "af_stack_repo_sha": "ae41f49abc...",
  "agentfield_version": "1.2.3",
  "litellm_version": "1.40.x",
  "svix_version": "1.40",
  "generated_at": "2026-06-07T00:00:00Z"
}
```

Lets Claude reason about *when* this skill was written (in case the
codebase has drifted).

### meta/verification.json

Auto-generated by a script (`scripts/verify-skill.mjs`):

```json
{
  "openapi_routes_referenced": ["/api/v1/llm/chat/completions", ...],
  "openapi_routes_actual": ["/api/v1/llm/chat/completions", ...],
  "missing_routes": [],
  "sdk_calls_referenced": ["suite.storage.put", "app.ai", ...],
  "sdk_calls_actual": ["suite.storage.put", ...],
  "missing_sdk_calls": [],
  "snippet_files_compile": true,
  "snippet_go_files_build": true
}
```

If `missing_routes` or `missing_sdk_calls` is non-empty, the skill is
**out of sync** with the codebase. Caught in CI.

---

## 8. Anti-hallucination measures

The skill is verifiable. Six mechanisms:

| Mechanism | What it prevents |
|---|---|
| **Routes verified against /openapi.json** | Made-up REST endpoints |
| **SDK calls verified against `packages/sdk-{py,ts,go}/`** | Made-up SDK methods |
| **File paths verified against `git ls-files`** | Pointing at nonexistent paths |
| **Snippet `.go` files compiled in CI** | Snippets that don't build |
| **Snippet `.py` files parsed + linted in CI** | Snippets with syntax errors |
| **Snippet `.tsx` files type-checked in CI** | Snippets with type errors |

The verification script (`scripts/verify-skill.mjs`) runs in CI on every
PR. If the skill references something that doesn't exist, the build
fails.

---

## 9. What's intentionally NOT in the skill

Things I considered and excluded, with rationale:

| Excluded | Why |
|---|---|
| **LiteLLM internals** | It's a sidecar; the user doesn't call it directly. Claude only needs to know "LLM calls go through `suite.llm.*` / `app.ai()`" |
| **AgentField internals** | Same reason; `app.*` is the surface |
| **PG schema beyond public-facing tables** | Internal schemas drift; the user touches their own tables, not `suite_*` |
| **Deploy target internals (Helm chart YAML)** | Just point at `deploy/helm/` README |
| **Caddy config** | Operator concern, not app concern |
| **Auth provider configuration** | Operator concern; the app reads `tenantctx.TenantID(ctx)` |
| **Webhook-out internals (Svix payloads)** | The user calls `suite.webhooks.send()`; the rest is plumbing |
| **Free-form "general AI advice"** | Skill is platform-specific, not LLM-prompting-101 |
| **Marketing language ("Supabase for AI")** | Positioning is for humans; the skill is for agents |

This keeps the skill **specific to AF Stack** and not "what Claude
already knows about backends."

---

## 10. Update protocol (extensibility)

How the skill stays current as the platform evolves.

### When a new primitive lands

Example: Phase 2 ships Realtime.

1. Edit `SKILL.md` primitives table: change Realtime row from 🚧 to ✅
2. Add a tiny snippet to `snippets/` if it's a new pattern
3. Add a section to `rules/primitives.md` if it has unique gotchas
4. Re-run `scripts/verify-skill.mjs` — should pass

That's it. ~5 lines of diff per new primitive.

### When a new adapter lands

Example: We ship Lago billing adapter.

1. Edit `SKILL.md` primitives table: update the adapter column in the
   billing row
2. Edit `rules/adapters.md` adapter catalogue: add Lago row
3. Re-run verification

### When a new example app ships

1. Add `examples/<name>.md` with the walkthrough (use Forge as a
   template)
2. Add row to `examples/README.md` contrast table
3. Optionally link from SKILL.md §8 if it's a canonical pattern

### When a Critical Rule changes

(Should be rare — these are the boundaries.)

1. Edit `SKILL.md` §5
2. Edit `rules/boundaries.md`
3. Stamp `meta/version.json` to flag the change

### When a CLI command lands (Phase 1)

1. Edit `SKILL.md` §6 (Canonical workflow)
2. Update `rules/deploy.md`

---

## 11. Writing order

When you say "go write the skill," here's the order:

1. **`SKILL.md`** (the entry point — most-used; gets it right first)
2. **`snippets/`** (immediately useful for the agent; tied to real code)
3. **`rules/boundaries.md`** (the loud "don't do this" — guards against
   the worst mistakes)
4. **`rules/primitives.md`** (the deep reference for the §3 table)
5. **`rules/agents.md` + `rules/workload-modules.md` +
   `rules/dashboard-plugins.md` + `rules/customer-app.md`** (the 4
   edit surfaces, in detail)
6. **`rules/multi-tenancy.md`** (load-bearing for correctness)
7. **`rules/adapters.md` + `rules/deploy.md` + `rules/sdk.md`** (the
   rest)
8. **`examples/forge.md`** (port the Forge walkthrough we already have)
9. **`meta/version.json`** + **`scripts/verify-skill.mjs`** (the
   verification pipeline)

Total skill weight, when done: ~2000 lines across ~25 files. About the
same size as the shadcn skill.

---

## 12. Open decisions (for you to confirm before writing)

### Q1. Skill location — in this repo or separate?

- **(a) In this repo**: `skills/af-stack/` at the root. Versions with
  the codebase. CI verifies. Tight loop.
- **(b) Separate repo**: `github.com/Agent-Field/af-stack-claude-skill`.
  Cleaner separation. Has to track AF Stack version.

**Recommendation**: (a) — in this repo. Same reason we want
POSITIONING.md and STRATEGY.md in this repo: source of truth lives with
the code.

### Q2. Skill scope — Claude Code only, or general?

- **(a) Claude Code only**: optimized for the `~/.claude/skills/`
  format. Tight, opinionated.
- **(b) Format-agnostic**: structured so any agent (Cursor, Codex,
  Aider) can consume it. Slightly more verbose.

**Recommendation**: (a) primary, (b) friendly. The skill is written in
the Claude format but reads as plain Markdown — any agent can consume
it, but we optimize for Claude.

### Q3. Verification strictness

- **(a) CI-blocking**: a missing route or SDK call fails CI.
- **(b) CI-warning**: missing items log a warning but pass.

**Recommendation**: (a) for routes/SDK/paths; (b) for snippets (because
ports/dev-env quirks can be flaky in CI).

### Q4. Update cadence

- **(a) Per-PR**: every PR touching the runtime updates the skill
  alongside.
- **(b) Per-phase**: skill updates batched at end of each Phase
  (1/2/3/4).
- **(c) Auto-generated**: a script reads OpenAPI + SDK + repo tree,
  generates the tables.

**Recommendation**: (a) for tables (low cost); (c) eventually, when
the verification script can also generate. (b) for examples + rules.

---

## 13. Summary

The skill is:

- **One root file (`SKILL.md`)** — the entry point, ~200 lines
- **~10 rules files** — deep references, fetched as needed
- **~7 snippets** — real templates, copy-paste-edit
- **~5 examples** (eventually) — walkthroughs of real apps
- **Meta layer** — version stamps + CI verification

Total: ~2000 lines across ~25 files. Updates per-primitive via single
table-row edits. CI verifies against the live codebase so it can never
drift far. Specific to AF Stack — no generic AI advice.

When loaded into Claude Code, it gives the agent everything it needs to
build any of the 12 startup ideas in the previous conversation, without
hallucinating, without violating boundaries, and without leaving
anything out.

---

## 14. Ready to write

Confirm Q1-Q4 (or accept the recommendations) and I'll start writing in
the order from §11.

The first file (`SKILL.md`) takes me ~1 hour to write well. The full
skill is probably ~6-8 hours of work. I'd plan to ship it as one PR.

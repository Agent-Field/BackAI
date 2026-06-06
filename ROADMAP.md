# AF Stack: Build Roadmap

Phased plan to go from empty repo to v1 launch. Each phase has an
acceptance criterion. Each phase is independently demoable.

## Phasing principles

- **Vertical slices**: every phase delivers a runnable end-to-end thing,
  not horizontal layers
- **Demoable at every phase**: by the end of each phase you can show
  someone something working
- **Quickstart sacred**: never break the 60-second quickstart from Phase 2
  onward
- **Test-driven**: each phase includes its tests; not "polish at the end"

## Phase 0: Foundations (1 week)

**Goal**: empty repo to skeleton with conventions locked.

### Deliverables

- [ ] Monorepo scaffolding (pnpm workspace + Python uv workspace + Go module)
- [ ] CI: lint, type-check, unit test stubs (passing on green hello world)
- [ ] License (Apache 2.0), CONTRIBUTING, CODE_OF_CONDUCT
- [ ] Naming locked (`BRAND.yaml` finalized)
- [ ] Repo skeleton matches `TECH-SPEC.md` section 2
- [ ] `.env.example` with all variables
- [ ] Makefile targets: `make dev`, `make test`, `make lint`, `make build`
- [ ] Docker-compose stub (services declared but not all built)

### Acceptance

- `git clone` → `make test` passes on hello-world stubs
- CI green on main branch

## Phase 1: Suite runtime + AF wiring (2 weeks)

**Goal**: minimum Go binary that boots, talks to AF, talks to PG.

### Deliverables

- [ ] Suite runtime `cmd/af-stack/main.go` boots, reads config.yaml
- [ ] PG connection pool with health check
- [ ] AF control plane container in docker-compose with PG mode
- [ ] AF reachable from suite runtime (verify with `app.discover()`)
- [ ] Suite runtime exposes `/health`, `/metrics`, `/openapi.json` (empty)
- [ ] Suite runtime registers OTel SDK and emits a startup trace
- [ ] Module loader infrastructure (load manifests from `modules/`)
- [ ] Hook engine skeleton (register + fire, no hooks yet)
- [ ] Logging infrastructure (structured JSON)
- [ ] Config loader (YAML + env override + JSON Schema validation)
- [ ] `docker-compose up` brings: PG, AF, suite runtime, all healthy

### Acceptance

- `docker compose up` → `curl localhost:8080/health` returns 200
- AF dashboard at `:8081` reachable (AF default)
- Suite runtime logs include OTel trace IDs

## Phase 2: First end-to-end agent call + 60-second quickstart (2 weeks)

**Goal**: minimum viable "user clones repo, calls an agent."

### Deliverables

- [ ] `apps/backend/` skeleton with sample agent (`agents/sample/echo.py`)
- [ ] Sample agent registers with AF on startup
- [ ] Public gateway endpoint `POST /api/v1/agents/{ns}.{func}` forwards to AF
- [ ] Gateway returns AF response with proper status code
- [ ] Streaming endpoint `/agents/stream/{ns}.{func}` works (SSE)
- [ ] Async endpoint `/agents/async/{ns}.{func}` works
- [ ] Python SDK `af_stack` package: `suite.agents.call()` + `ctx`
- [ ] Single integration test: `docker compose up` → call sample agent → assert response
- [ ] CI quickstart test (validates 60-second quickstart)
- [ ] README with quickstart commands
- [ ] OpenCode CLI bundled in default agent base image

### Acceptance

- Fresh clone + one env var (`OPENROUTER_API_KEY`) + `docker compose up`
- `curl POST /api/v1/agents/sample.echo -d '{"text":"hi"}'` returns echo
- CI quickstart test green
- Quickstart docs work end to end

**Milestone**: viral artifact's foundation is alive.

## Phase 3: Identity + dashboard shell (3 weeks)

**Goal**: dashboard you can log into, see something useful.

### Deliverables

- [ ] better-auth integrated with PG
- [ ] Email+password, magic link, Google OAuth working
- [ ] Sessions (cookies) + bearer tokens
- [ ] Next.js dashboard scaffolded with shadcn/ui + Tremor
- [ ] Login page, account settings page
- [ ] Operator role check on `/(admin)/*` routes
- [ ] Sidebar navigation matches the 5 groups (Build/Run/Connect/Manage/Observe)
- [ ] All tabs present as routes (empty if not ready)
- [ ] Dashboard reads from suite runtime via REST
- [ ] Dark mode default
- [ ] First-run setup: create initial operator account

### Acceptance

- Open `:3000` in browser, signup, log in
- Land on dashboard, see all five groups in sidebar
- Account settings page works (edit name, change password)

**Milestone**: dashboard story exists, ready for hero tabs.

## Phase 4: Hero tab 1 — Agent Runs (2 weeks)

**Goal**: AF DAG visualization at production polish.

### Deliverables

- [ ] `/(admin)/agents` page: agent catalog from AF discover API
- [ ] Per-agent detail: reasoners list, recent runs, P50/P99 latency, cost
- [ ] `/(admin)/agents/runs` page: paginated run list with filters
- [ ] Per-run detail: DAG visualization (D3 or React Flow), node click → details
- [ ] Token stream live view for running executions
- [ ] Replay button (calls AF replay endpoint with edits)
- [ ] Trace tab: full call tree, durations, costs
- [ ] Filters: tenant, agent, status, time range, cost range
- [ ] Functional CRUD for: AF schemas, AF discover

### Acceptance

- Run a sample agent, see it appear live in dashboard
- Click into DAG, navigate tree, see costs and durations
- Hit replay, modify input, see new run appear

**Milestone**: AF showcase is live. First viral screenshot is possible.

## Phase 5: Jobs + secrets + storage (3 weeks)

**Goal**: the infrastructure SDK that handlers and jobs need.

### Deliverables

- [ ] River integrated (PG-backed jobs)
- [ ] `apps/backend/jobs/` discovered at startup
- [ ] `suite.jobs.enqueue()` Python + TS
- [ ] River dashboard tab: queued/running/failed counts, recent jobs
- [ ] Crons via River cron: `apps/backend/crons/`, scheduled, manual trigger
- [ ] Cron tab: schedule, last run, next run, manual trigger
- [ ] Secrets vault: PG + KMS envelope encryption (env-provided key)
- [ ] `suite.secrets.get()` Python + TS
- [ ] Admin SDK `suite.admin.secrets.{set,list,delete,rotate}`
- [ ] Secrets dashboard tab (redacted view, rotation UI)
- [ ] MinIO bundled in docker-compose
- [ ] `suite.storage.{upload,signed_url,download}` Python + TS
- [ ] Storage adapter interface; MinIO + S3 adapters
- [ ] Storage dashboard tab (bucket browser)

### Acceptance

- Sample job enqueued, runs, appears in dashboard
- Sample cron scheduled, fires on time
- Secret set via CLI, read in handler, displayed in dashboard
- File uploaded via SDK, downloaded via signed URL

**Milestone**: the suite SDK feels real.

## Phase 6: Multi-tenancy + public gateway hardening (3 weeks)

**Goal**: multi-tenant SaaS shape works.

### Deliverables

- [ ] Multi-tenancy module: PG RLS + migration to add `tenant_id` everywhere
- [ ] Middleware: resolve tenant from API key or session, set `app.tenant_id`
- [ ] `suite_tenants`, `suite_memberships` tables + admin endpoints
- [ ] Admin SDK: `suite.admin.tenants.{create,list,update,delete}`
- [ ] Admin SDK: `suite.admin.users.{create,list,update,disable}`
- [ ] Admin SDK: `suite.admin.keys.{issue,rotate,revoke,list}`
- [ ] `/(admin)/tenants` page: list, detail, per-tenant drilldown
- [ ] Per-tenant drilldown: usage, runs, jobs, secrets, members
- [ ] API key management UI
- [ ] Rate limiting per tenant (token bucket in suite runtime)
- [ ] Schema validation at gateway (Pydantic/Zod schemas from agent registry)
- [ ] OpenAPI 3.1 spec auto-generated from registered routes
- [ ] `/docs` interactive API docs (Scalar or Swagger UI)
- [ ] Multi-tenancy enabled via config flag + migration script
- [ ] Comprehensive RLS test suite (tenant isolation guaranteed)

### Acceptance

- Toggle MT on via config + migration
- Create two tenants, two users, assign memberships
- Tenant A's API key cannot access tenant B's resources (verified by tests)
- Per-tenant rate limits enforced
- OpenAPI spec accurate, `/docs` browsable

**Milestone**: ready for SaaS-shape examples.

## Phase 7: Hero tab 2 — LLM Gateway (2 weeks)

**Goal**: OpenAI-compatible shim + cost dashboard hero.

### Deliverables

- [ ] `POST /api/v1/llm/chat/completions` accepts OpenAI format
- [ ] Routes through AF (uses AF's LiteLLM bridge)
- [ ] `POST /api/v1/llm/embeddings` and `images/generations`
- [ ] Streaming SSE in OpenAI format
- [ ] Per-tenant per-model cost tracking
- [ ] Budget caps enforcement (`suite.billing.has_budget` integration)
- [ ] Existing OpenAI SDK works pointed at suite (test with Python `openai` lib)
- [ ] Dashboard `/(admin)/gateway`: live call stream, cost charts, per-tenant breakdown
- [ ] Cache hit rate visualization (if cache module on)
- [ ] Budget alerts list

### Acceptance

- `openai.ChatCompletion.create(base_url=..., api_key=...)` works
- All calls visible in dashboard with cost
- Tenant exceeding budget gets 402 Payment Required
- Cost dashboard renders at Linear-grade polish

**Milestone**: lowest-commitment viral wedge ships. Devs can adopt gateway alone.

## Phase 8: Hero tab 3 — Database studio + memory (2 weeks)

**Goal**: Supabase-Studio-grade DB browsing + AF memory exposure.

### Deliverables

- [ ] Embed Supabase Studio components (or vendor source)
- [ ] `/(admin)/db` page: table list, table editor, SQL runner
- [ ] RLS policy editor (read view; write requires CLI)
- [ ] AF memory exposure: `/(admin)/memory` page
- [ ] Memory scopes (global/tenant/agent/session/run) browsable
- [ ] Vector search playground in dashboard
- [ ] `suite.memory.{get,set,search}` Python + TS

### Acceptance

- Browse PG tables in dashboard
- Run a SELECT in SQL runner, see results
- Browse AF memory by scope
- Vector search returns expected matches

**Milestone**: breadth signal ships. Suite feels like a real platform.

## Phase 9: Hero tab 4 — Sandboxes (3 weeks)

**Goal**: sandbox infrastructure with four adapters.

### Deliverables

- [ ] Sandbox adapter interface (Go)
- [ ] Docker adapter (default, works in compose)
- [ ] gVisor adapter (docker-runtime config)
- [ ] Firecracker adapter (Flintlock integration)
- [ ] e2b adapter (API client)
- [ ] `suite.sandbox.run()` Python + TS
- [ ] `app.sandbox.run()` in AF SDK (PR to AF if needed)
- [ ] Per-tenant workspace isolation when MT on
- [ ] Network policies per adapter
- [ ] Cost tracking (cpu-seconds → meter event)
- [ ] `/(admin)/sandboxes` page: live pool, recent runs, per-run logs/artifacts
- [ ] Capabilities API per adapter

### Acceptance

- Run a `node:20 npm test` sandbox via SDK, see logs + artifacts
- Swap adapter docker → e2b via config, same code works
- Per-tenant isolation verified by tests
- Sandbox tab shows live pool status

**Milestone**: SWE-AF-shape workloads possible.

## Phase 10: Notifications + webhooks + billing (3 weeks)

**Goal**: SaaS-shape essentials.

### Deliverables

- [ ] Notifications outbox + log-stub adapter
- [ ] Resend adapter (one env var)
- [ ] Email templates (React Email)
- [ ] `suite.notifications.email()` Python + TS
- [ ] Notifications dashboard tab
- [ ] Svix bundled in compose
- [ ] Incoming webhooks via `gateway.yaml` declaration
- [ ] HMAC verification, dedup, replay protection
- [ ] Outgoing webhooks via PG outbox + retry worker
- [ ] `suite.webhooks.send()` Python + TS
- [ ] Stripe direct adapter (customer, subscription, invoice)
- [ ] Stripe webhook handler (subscription updates, payment success)
- [ ] `suite.billing.meter()` + `has_budget()` Python + TS
- [ ] Per-tenant meter aggregation
- [ ] Billing dashboard tab (per-tenant usage, Stripe portal link)

### Acceptance

- Email sent via `suite.notifications.email()`, appears in inbox (Resend on)
- GitHub webhook received, verified, forwarded to agent
- Outgoing webhook delivered, retries on failure
- Stripe customer created, subscription updated, usage metered

**Milestone**: SaaS shape complete. Ready for production deploys.

## Phase 11: MCP client + skills + harnesses (2 weeks)

**Goal**: tool integration story complete.

### Deliverables

- [ ] Anthropic MCP Python SDK bundled in `af_stack`
- [ ] Anthropic MCP TS SDK bundled in `@af-stack/sdk`
- [ ] Suite runtime hosts MCP server connections (stdio + SSE)
- [ ] `config.yaml` block for MCP servers
- [ ] `suite.tools.list_mcp_tools()` + `call_mcp()`
- [ ] CLI: `af-stack mcp add/remove/list`
- [ ] Dashboard MCP tab (installed servers + tools)
- [ ] Per-tenant MCP isolation when MT on
- [ ] `suite.admin.skills.{install,list,attach}` wrapping AF skillkit
- [ ] Dashboard skills tab
- [ ] CLI: `af-stack harness install claude-code|codex|gemini`
- [ ] Harness install scripts documented
- [ ] OpenCode already in default agent image (no work)

### Acceptance

- Install GitHub MCP server via CLI
- Call `suite.tools.call_mcp("github", "search_repos", ...)` from handler
- Install Claude Code via CLI, `app.harness(provider="claude-code")` works
- Skills installed via CLI, attached to harness

**Milestone**: tools story complete. AF agents have rich tool access.

## Phase 12: Hero tab 5 — Tenants + remaining tabs (2 weeks)

**Goal**: dashboard feature complete.

### Deliverables

- [ ] `/(admin)/tenants` page already exists from Phase 6; polish to hero grade
- [ ] Per-tenant drilldown: usage charts, recent runs, members, plan, billing
- [ ] All remaining tabs at functional CRUD level:
  - Auth, API Keys, Webhooks, Notifications, Secrets, Modules, MCP, Skills,
    Storage browser, Memory browser, Logs, Metrics, Crons, Jobs
- [ ] Dashboard plugin system: drop file in `plugins/`, register tab
- [ ] First-party plugin example: cost-explorer
- [ ] Theming: CSS variables, branding swap docs

### Acceptance

- Click into tenant, see everything about them in one screen
- Every tab works (functional, may not be polished)
- Sample plugin appears in dashboard nav
- Theme overrides work without forking

**Milestone**: dashboard feature complete.

## Phase 13: Examples + workload modules (3 weeks)

**Goal**: six end-to-end examples ship.

### Deliverables

- [ ] Example 01 — Notable (Notion-with-AI)
  - 3 small AF agents (summarize, suggest, todo_completer)
  - Custom pages handlers and UI
  - MT enabled, billing on
- [ ] Example 02 — Shipwright (SWE-AF SaaS)
  - SWE-AF imported as module
  - Custom estimator + classifier agents
  - Sandbox (e2b in default, Firecracker doc'd)
  - GitHub OAuth, git-workload module
- [ ] Example 03 — LLM gateway only
  - Minimal compose: PG + AF + suite runtime + nothing else
  - Direct OpenAI-compat usage demo
- [ ] Example 04 — Podcast creator
  - Multimodal-storage workload module
  - ffmpeg + Whisper + Vision in sandbox
- [ ] Example 05 — Reactive enrichment
  - Change-stream-listener workload module
  - PG + Mongo adapters
- [ ] Example 06 — Deep research
  - Long-running fan-out, AF memory heavy use
- [ ] Each example: README, walkthrough, deploy buttons
- [ ] Workload modules implemented:
  - git-workload (clone/branch/PR)
  - multimodal-storage (transcode/transcribe/thumbnail)
  - change-stream-listener (PG + Mongo)

### Acceptance

- Each example: clone → set keys → compose up → working demo
- README walkthrough validates end-to-end
- Deploy buttons (Fly, Railway, Render) functional

**Milestone**: viral artifacts ready.

## Phase 14: Deploy targets + production hardening (2 weeks)

**Goal**: production deploys work.

### Deliverables

- [ ] Helm chart for k8s
  - Configurable values
  - HPA on suite runtime
  - PVC conditional on storage mode
  - Network policies
- [ ] Nomad job spec
- [ ] Fly.io app spec with one-click button
- [ ] Railway template
- [ ] Render.yaml
- [ ] `docker-compose.prod.yml` with external PG/MinIO assumed
- [ ] Caddy reverse proxy + automatic TLS
- [ ] Backup recommendations documented
- [ ] Multi-replica deploy validated (load test)
- [ ] Graceful shutdown (drain mode)
- [ ] Healthcheck endpoints (`/health`, `/ready`)

### Acceptance

- Deploy to minikube, app works
- Deploy to Fly via button, app works
- Sustained 1k req/s on 2-replica deployment
- Graceful shutdown drains in-flight requests

**Milestone**: production-ready.

## Phase 15: Documentation + polish (2 weeks)

**Goal**: launchable.

### Deliverables

- [ ] Documentation site (Mintlify, Nextra, or Astro Starlight)
- [ ] Quickstart doc validated end to end
- [ ] Module reference for each module
- [ ] Adapter reference for each adapter
- [ ] Hook reference
- [ ] Customize-dashboard guide
- [ ] Swap-defaults guide
- [ ] Deploy guides per target
- [ ] API reference auto-gen from OpenAPI
- [ ] Architecture diagram + explainer
- [ ] Demo video (5-10 min)
- [ ] Launch blog post
- [ ] Twitter/HN thread drafted

### Acceptance

- Docs site live at chosen domain
- Quickstart works for 5+ external testers without help
- Demo video shows hero flow

**Milestone**: ready to launch.

## Phase 16: Security audit + launch (1-2 weeks)

**Goal**: ship publicly.

### Deliverables

- [ ] Internal security audit complete
- [ ] External security review (sandbox, MT isolation, secrets)
- [ ] Penetration test of MT and sandbox boundaries
- [ ] Dependency vulnerability scan clean
- [ ] License confirmed, headers added everywhere
- [ ] Trademark search and decision
- [ ] GitHub repo public
- [ ] HN, Twitter, Discord launches
- [ ] Monitor first 48 hours

### Acceptance

- All R-* and NF-* PRD requirements met
- 60-second quickstart validated by 5+ external testers
- Launched to public

**Milestone**: v1 shipped.

## Total timeline

| Phase | Duration | Cumulative |
|---|---|---|
| 0. Foundations | 1 week | 1 week |
| 1. Runtime + AF wiring | 2 weeks | 3 weeks |
| 2. First end-to-end + quickstart | 2 weeks | 5 weeks |
| 3. Identity + dashboard shell | 3 weeks | 8 weeks |
| 4. Hero — Agent Runs | 2 weeks | 10 weeks |
| 5. Jobs + secrets + storage | 3 weeks | 13 weeks |
| 6. Multi-tenancy + gateway | 3 weeks | 16 weeks |
| 7. Hero — LLM Gateway | 2 weeks | 18 weeks |
| 8. Hero — DB studio + memory | 2 weeks | 20 weeks |
| 9. Hero — Sandboxes | 3 weeks | 23 weeks |
| 10. Notif + webhooks + billing | 3 weeks | 26 weeks |
| 11. MCP + skills + harnesses | 2 weeks | 28 weeks |
| 12. Hero — Tenants + tabs | 2 weeks | 30 weeks |
| 13. Examples + workload modules | 3 weeks | 33 weeks |
| 14. Deploy + hardening | 2 weeks | 35 weeks |
| 15. Documentation + polish | 2 weeks | 37 weeks |
| 16. Security audit + launch | 1-2 weeks | ~38-39 weeks |

**Approximately 9 months** for a 2-3 person focused team. Compresses with
more engineers on parallel modules (Phases 4-12 can parallelize).

## Parallelization opportunities

Once Phase 3 is done, many phases can parallelize:

- **Phase 4 (Agent Runs)** independent of Phase 5 (Jobs+Secrets+Storage)
- **Phase 5** independent of Phase 6 (MT+Gateway)
- **Phase 7 (LLM Gateway)** independent of Phase 8 (DB Studio) and Phase 9 (Sandboxes)
- **Phase 10 (Billing)** can start with Phase 5
- **Phase 11 (MCP)** can start with Phase 5
- **Phase 13 (Examples)** can start during Phase 12

With 4-5 engineers, **5-6 months** is realistic.

## Critical path

The serial dependencies:
- Phase 0 → 1 → 2 (foundations)
- Phase 3 (auth) gates dashboard work
- Phase 6 (MT) gates SaaS examples (13.01, 13.02)
- Phase 9 (sandboxes) gates Shipwright example
- Phase 14 (deploy) gates launch
- Phase 16 (audit) gates public launch

Everything else can flex.

## What gets cut if we're behind

In priority order (cut from bottom):

1. Examples 04, 05, 06 (keep 01, 02, 03)
2. Workload modules (multimodal, change-stream) — defer to community
3. Nomad spec — keep helm + compose
4. Plugin system polish — ship later
5. Skills wrapping — defer to v1.1
6. e2b adapter — defer if Firecracker works

Never cut:
- Quickstart (Phase 2)
- Multi-tenancy (Phase 6)
- LLM Gateway shim (Phase 7)
- Sandbox (Phase 9)
- Documentation (Phase 15)
- Security audit (Phase 16)

## Risk-driven sequencing

Hardest things sequenced earlier to surface risk:

- **AF integration risk**: Phase 1 (week 2) — if AF doesn't cooperate, we
  know immediately
- **Stateless scaling risk**: Phase 1 + 14 (load test) — early validation
- **Sandbox security risk**: Phase 9 (early enough to redesign if needed)
- **Multi-tenancy isolation risk**: Phase 6 with comprehensive RLS tests
- **Dashboard polish risk**: Phase 4 hero tab early to establish bar

## Definition of "done" per phase

Each phase is done when:

- [ ] All deliverables shipped to main
- [ ] Tests pass (unit + integration for the phase scope)
- [ ] Quickstart still works end-to-end
- [ ] Phase demo recorded (90-second video showing the new capability)
- [ ] Documentation updated for the phase scope
- [ ] No P0/P1 bugs open in phase scope

## Where to start

After reading PRD + TECH-SPEC + this:

1. Lock name (`BRAND.yaml`)
2. Set up `git init` and CI on day 1
3. Phase 0: scaffold the monorepo
4. Phase 1: get the runtime + AF talking
5. Phase 2: make the quickstart real

By end of week 5 (Phase 2 done), the viral artifact has its foundation.
Everything after is depth and breadth.

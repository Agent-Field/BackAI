# OSS Audit — What we should swap to vendored open source

> *"We should never hard code anything if it's already there in open
> source for us to use on popular."*

## Hierarchy

For every layer in the stack, three questions:

1. Does a popular open-source thing already do this well?
2. If yes, are we using it, or did we hand-roll?
3. If hand-rolled, what's the migration?

The answer for most of the platform is "we vendor good OSS". The
holes are listed below in priority order.

## Already vendoring (no change needed)

| Layer | What we use | Notes |
|---|---|---|
| Auth | **better-auth** | Modern Node auth with OAuth providers built in |
| LLM provider routing | **LiteLLM** (sidecar) | 100+ upstream providers, OpenAI-compat surface |
| Job queue | **River** | PG-backed, multi-replica, no Redis |
| Cron parsing | **robfig/cron/v3** | Industry standard |
| Vector store | **pgvector** | Avoids Pinecone op cost |
| Reverse proxy | **Caddy** | Auto-TLS, zero config |
| Docs site | **Astro Starlight** | Fast static, code-heavy friendly |
| UI | **shadcn/ui + base-ui** | Copy + own, no runtime dep |
| API browser | **Scalar** | Modern, looks great |
| OpenAPI v3.1 schemas | **kin-openapi** style (we render JSON) | Could use **swag** but our hand-rolled emitter is small and works |
| MinIO + S3 | **AWS SDK + MinIO client** | Single adapter covers both |
| Stripe | **stripe-go v82** | Direct integration |
| Sandbox: gVisor | **runsc** | We just set `Runtime="runsc"` on Docker |
| Sandbox: Firecracker | **Flintlock** | Stub today; swap to flintlockd when needed |
| Sandbox: e2b | **e2b API** | Real client, paid service |
| MCP protocol | **modelcontextprotocol.io** spec | We use JSON-RPC framing per spec |
| MCP runners | **uv / uvx** in the agent container | Agent containers can spawn stdio MCP servers via `uvx` |
| Coding harnesses | **claude-code / codex / gemini-cli / opencode** in the agent container | Agents declare available harnesses through AgentField capabilities |
| Logging | **slog** (stdlib) | Ring buffer is ours (tiny) |
| Tracing | **OpenTelemetry SDK** | Standard wiring |
| Metrics | **Prometheus client_golang** | Standard wiring |

## Should swap (hand-rolled where OSS exists)

### 1. LLM provider routing → **LiteLLM** — **DONE**

**Landed:** BackAI runs a LiteLLM Proxy sidecar
(`ghcr.io/berriai/litellm:main-stable`) in docker-compose. The runtime
gateway forwards every `/api/v1/llm/*` call to it via
`services/runtime/internal/llmgateway/litellm_provider.go`. LiteLLM
handles 100+ upstream providers via
`apps/backend/litellm-config.yaml`.

**Kept on BackAI's side:** cost ledger, budgets, cache, hooks,
per-tenant API keys, the OpenAI-compatible customer surface.

**Dropped:** 4 hand-rolled provider clients (OpenRouter, OpenAI,
Anthropic, Google direct), per-provider key selection logic in
`buildLLMGateway`. The static `pricing.go` catalog is kept as a
fallback for `EstimateCostUSD` — LiteLLM injects `response_cost` on
known models and the runtime prefers that number when present.

**Net:** ~600 lines of Go gone; adding a new provider is now a config
edit, not a code change. Mistral, DeepSeek, Groq, Cohere, Bedrock,
plus everything else LiteLLM supports, all work by dropping in an
`..._API_KEY`.

**Virtual keys + spend (item #22, landed):** BackAI also uses
LiteLLM's master-key-protected admin surface. `IssueAPIKey` mints a
matching LiteLLM virtual key (`/key/generate`) alongside every
`suite_api_keys` row, with the operator-supplied `budget_max_usd`,
`rate_limit_rpm`, and `rate_limit_tpm` forwarded as `max_budget`,
`rpm_limit`, and `tpm_limit`. The LiteLLM secret is stored encrypted
in the BackAI secrets vault under `litellm/key/{api_key_id}`; only
an alias + SHA-256 hash live on the row. The LLM gateway reads the
per-tenant key at request time and uses it for the upstream call so
LiteLLM enforces budget + rate limit upstream and the dashboard reads
live spend from `/spend/keys`. `suite_cost_events` is downgraded to a
write-through audit table — LiteLLM is the canonical balance. Legacy
keys without a LiteLLM mapping keep working: the provider falls back
to `LITELLM_MASTER_KEY`. See
`services/runtime/internal/llmgateway/litellm_admin.go` and
`services/runtime/internal/tenancy/litellm_mirror.go`.

**Rate limiting moved entirely upstream (item #32, landed):** the
runtime no longer runs a local token-bucket — `services/runtime/internal/ratelimit`
is deleted. LiteLLM's per-virtual-key `rpm_limit` / `tpm_limit` is the
sole enforcement layer. When LiteLLM returns 429, the LLM handler
proxies `Retry-After` and the `X-RateLimit-Limit / Remaining / Reset`
trio through to the client unchanged, and the error envelope returns
`{"error":{"code":"RATE_LIMIT_EXCEEDED","type":"rate_limit_error","details":{"retry_after":N}}}` —
the standard OpenAI SDK rate-limit signal. See `internal/llmgateway/litellm_provider.go`
(`extractRateLimitHeaders`, `upstreamErr`) and `internal/server/llm.go`
(`writeOpenAIErrorWithHeaders`).

### 3. LLM cache → **GPTCache** or **OpenLIT**

**Today:** `services/runtime/internal/llmcache/` is a simple
exact-match cache in PG (Phase 7.3).

**Should be:** **GPTCache** for semantic caching (vector match on
prompt instead of exact-string match) or **OpenLIT** for the
observability + cache combo.

**Migration:** ~1 day. GPTCache exposes a Python HTTP API; we'd call
it pre-LLM-call. We'd drop our `llmcache.go` cache logic.

**Considerations:** semantic cache hit-rate is higher (good) but
introduces an embedding step per query (some cost). Worth A/B.

### 6. Audit log SAW → **dlog** or stay home-grown

**Today:** our `audit.Write()` writes directly to `suite_audit_log`
via a goroutine.

**Should be:** could use **dlog** (Postgres audit logger) or stay
home-grown. Our path is small (~100 LoC) and integrates with the
tenant context cleanly. **Verdict: keep, but reconsider if we add
RBAC.**

### 7. Better-auth user → suite_users mirror

**Today:** `databaseHooks.user.create.after` in
`apps/dashboard/src/lib/auth.ts` mirrors better-auth users into
`suite_users`.

**Should be:** could use better-auth's `additionalFields` to put the
suite-side state inline on the `user` row, OR have a Postgres trigger
do the mirror. Both are about the same size as the JS hook.

**Verdict: keep as-is.** It's small and the failure mode is clearer
in JS than in a PG trigger.

### 8. Tenant resolver middleware

**Today:** `services/runtime/internal/server/tenant_resolver.go`
parses Authorization header / session cookie, resolves the tenant.

**Should be:** could use **Casbin** (PERMs) or **Oso** if we wanted
RBAC, but tenant resolution itself is small enough to own. The piece
that warrants OSS is the RLS GUC pattern — and PG's GUC + RLS IS the
OSS we're using.

**Verdict: keep, but layer Oso/Casbin on top when we add RBAC.**

### 9. Sandbox docker adapter → **Daytona** or **dagger.io**

**Today:** `services/runtime/internal/sandbox/adapters/docker/`
shells to docker.sock directly.

**Should be:** **Daytona** does sandboxed dev environments very well;
**dagger.io** wraps OCI sandbox semantics with a nicer SDK.

**Verdict: punt.** Our docker adapter is dev-only. Production uses
gVisor / Firecracker / e2b. Replacing the dev adapter buys little.

### 10. Plugin discovery → no obvious OSS

**Today:** `apps/dashboard/scripts/generate-plugins-manifest.mjs`
scans `apps/dashboard/plugins/*/plugin.ts` and emits a generated TS
file.

**Should be:** no obvious OSS to replace this. It's a 100-line build
script. **Verdict: keep.**

## Priority order

1. **LiteLLM virtual keys** — DONE (item #22 landed). Per-user budgets,
   per-key rate limits, and `/spend/*` are now the source of truth;
   `suite_cost_events` is audit-only. The internal rate limiter has
   been retired (item #32 landed) — LiteLLM is the sole rate-limit
   enforcer, and 429s flow back with `Retry-After` + `X-RateLimit-*`
   headers proxied through.
2. **Billing adapter** — DONE. Stripe and Lago share one provider
   interface selected by `AF_STACK_BILLING_ADAPTER=stripe|lago|none`.
3. **Shipwright** — autonomous AI agent factory on top of AgentField,
   sandboxes, and harnesses.
4. **AgentField data in dashboard** — DAG, step inspector, workflow memory,
   and rerun-from-step without duplicating AgentField state.
5. **Approvals primitive** — general human decision point for any flow.

Completed swaps: LiteLLM, `uvx` in agent containers, and harnesses
in agent containers.

## What we KEEP hand-rolled (and why)

- **PG RLS tenant pattern keyed on session GUC** — this IS the OSS
  (Postgres feature). Our code is just middleware + connection
  binding.
- **Cost ledger** — none of the OSS LLM observability tools
  (Langfuse, Helicone, Portkey, OpenLIT) gives us per-tenant
  isolation + budget enforcement the same way. Our hook into the
  gateway is small.
- **Sandbox adapter portfolio** — there's no OSS that abstracts
  docker + gVisor + Firecracker + e2b under one interface. That IS
  the AF-native value.
- **Workload module loader** — yes, this pattern is standard (see
  development/operator-console-inventory.md), but our specific manifest.yaml + Go handler loader is
  small and targeted.
- **Dashboard plugin scanner** — see above.
- **Audit writer** — small, tightly coupled to tenant context.

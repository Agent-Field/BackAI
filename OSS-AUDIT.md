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
| Logging | **slog** (stdlib) | Ring buffer is ours (tiny) |
| Tracing | **OpenTelemetry SDK** | Standard wiring |
| Metrics | **Prometheus client_golang** | Standard wiring |

## Should swap (hand-rolled where OSS exists)

### 1. LLM provider routing → **LiteLLM** *(highest priority)*

**Today:** `services/runtime/internal/llmgateway/providers/` has four
hand-rolled provider clients (OpenRouter, OpenAI, Anthropic, Google).
We maintain the model catalog by hand in `pricing.go`.

**Should be:** spin up **LiteLLM** as a sidecar container (or use its
Python proxy mode). Our gateway calls LiteLLM's OpenAI-compatible
HTTP endpoint. LiteLLM handles the 100+ provider routing. We keep:

- Cost ledger / budgets / cache (AF-native value)
- Per-tenant API keys, hooks, rate limits
- The OpenAI-compatible shape we expose

**Drops:** 4 provider clients, manual model catalog, per-provider
pricing maintenance.

**Migration:** 1–2 days. Add `litellm` service to docker-compose, point
gateway upstream URL at it.

### 2. Webhooks delivery → **Svix** *(was in original plan)*

**Today:** `services/runtime/internal/webhooks/` has our own outbox +
retry worker (Phase 10.3). ~600 lines.

**Should be:** **Svix** (the company that makes the webhooks layer for
Resend, Clerk, Lago, etc.). Self-hostable. Battle-tested at scale.
Includes a UI, retries, signing, dedup, replay protection, all of it.

**Drops:** outbound.go, worker.go, deliveries.go, the whole outbox
pattern, ~600 LoC.

**Keeps:** our inbound HMAC verify path (it's small and tied to our
forward_to semantics).

**Migration:** 2–3 days. Svix is a Docker container; we point our
`POST /api/v1/webhooks/send` at Svix's API. The deliveries we render in
`/operate/webhook-activity` come from Svix's API instead of our table.

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

### 4. MCP — `uv` in the agent container

**Today:** the runtime container is distroless, so `uvx
mcp-server-github` fails because `uv` isn't there. We end up with all
MCP servers marked "errored" in the dashboard.

**Should be:** add `uv` to the **agent** container (where AF agents
run) and let agents declare their MCP servers at startup. The runtime
queries AgentField for "which agents have which MCP servers ready".

This is the same architectural change as harnesses (option A).

**Migration:** 1 day. `apt-get install uv` in
`apps/backend/agents/sample/Dockerfile`, then add MCP-registration to
the AF agent SDK.

### 5. Harnesses — agent container too

**Today:** probe-only in runtime container = always "missing".

**Should be:** same as above. Install harnesses in agent container, AF
agent declares at startup, runtime queries AF for availability.

`apps/backend/agents/sample/Dockerfile` adds:
```
RUN npm install -g @anthropic-ai/claude-code @openai/codex \
    @google/gemini-cli
```

The `/build/harnesses` top-level tab moves into a column on
`/build/agents`.

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

Doing the top 2 takes ~5 days and replaces ~1500 lines of our code
with battle-tested OSS:

1. **LiteLLM** for LLM gateway — unlocks 100+ models, drops 4
   hand-rolled provider clients (~600 lines)
2. **Svix** for webhooks outbound — drops outbox + retry worker
   (~600 lines)
3. **uv in agent container + MCP refactor** — unlocks real
   MCP-server-github, mcp-server-postgres, etc.
4. **Harnesses → agent container** — unlocks claude-code etc.
   actually being usable

Items 3 + 4 are coupled because both are "move the binary from runtime
container to agent container, declare at registration".

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
  NAVBAR.md), but our specific manifest.yaml + Go handler loader is
  small and targeted.
- **Dashboard plugin scanner** — see above.
- **Audit writer** — small, tightly coupled to tenant context.

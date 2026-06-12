# AF Stack — What it is, what it isn't, what it feels like to use

## What it is

AF Stack is the **open backend platform for AI products**. You clone it,
`docker compose up`, and 60 seconds later you have everything an
AI-first SaaS actually needs to ship:

- An OpenAI-compatible LLM gateway with cost ledger
- Multi-tenant Postgres with row-level isolation
- Vector memory (pgvector) scoped per tenant / user / agent / run
- A sandbox for running agent-generated code
- Background jobs + cron scheduler
- Inbound + outbound webhooks (HMAC + dedup + retry)
- Notifications (outbox + worker)
- Stripe-shaped billing (works in stub mode without keys)
- MCP server hosting for tools
- Skills + harness integration for Claude Code / Codex / Gemini / OpenCode
- Two dashboards (operator console + customer-facing app)
- Python + TypeScript SDKs

It's positioned for the seam Supabase / Firebase / AI gateways don't
cover: the AI-native part of a production backend. You self-host. The
code you fork is the code we run.

**Apache 2.0. No open-core. No paid tier we ship a worse version of.**

## What's REAL in v1 (works end-to-end with one env var)

These work the moment you `docker compose up` with just an
`OPENROUTER_API_KEY` set:

| Surface | What runs |
|---|---|
| **LLM gateway** | OpenAI-compatible front; LiteLLM sidecar upstream for 100+ providers (OpenRouter / OpenAI / Anthropic / Google / Mistral / DeepSeek / Groq / Cohere / Bedrock / ...). Per-call cost ledger in `suite_cost_events`, in-memory cache, budget enforcement at request time |
| **Multi-tenancy** | PG-RLS keyed on `app.tenant_id` GUC. Buggy handlers cannot leak across tenants — the database enforces. |
| **Memory** | `app.memory.put/get/search` with pgvector. Scope by tenant, user, agent, or run. |
| **Sandbox runs** | Docker adapter for dev (real containers spun up via host's docker.sock, stdout/stderr captured to MinIO). gVisor / Firecracker / e2b adapters for production. |
| **Jobs queue** | River-backed Postgres queue. Retry, retry-with-backoff, deadletter. |
| **Cron scheduler** | robfig/cron v3 parsing (`@hourly`, full crontab syntax). 60-second tick. Multi-replica safe via `FOR UPDATE SKIP LOCKED`. |
| **Webhooks inbound** | Public `/webhooks/in/{slug}` route with HMAC verify (sha256/sha1 incl. GitHub-style `sha256=` prefix), dedup token per provider (X-GitHub-Delivery, Stripe-Signature timestamp, X-Shopify-Webhook-Id, body-hash fallback), forward to HTTP URL or `af://agents/<name>`. |
| **Webhooks outbound** | PG outbox + 2s tick worker + exponential backoff (max 8 attempts capped at 5min). |
| **Notifications** | Outbox + 2s tick worker. Log adapter by default; switches to Resend when `RESEND_API_KEY` set. |
| **Audit log** | Every admin mutation (tenant create/delete, api_key create/revoke, secret put/delete/reveal, budget set, membership change) writes a row with actor, IP, user agent, metadata. |
| **MCP host** | stdio + SSE adapters with JSON-RPC framing, 5-minute tool catalogue refresh, per-tenant scoping, env from secrets vault via `secret:<key>` prefix. |
| **Skills** | Install bundles, attach to agents, query installed list. |
| **Harnesses** | Probe-only — detects whether claude-code/codex/gemini/opencode is available in the agent container and what auth it needs. |
| **Operator dashboard** | Cost charts, run inspector, sandbox activity, memory browser, audit log, tenant drilldown, plugin system, theming via CSS variables. |
| **Customer-facing app** | Sign-up → tenant + membership + API key minting → code-helper that streams real LLM calls → billing page. Separate brand, same auth DB. |
| **OpenAPI 3.1** | Auto-generated at `/openapi.json` with 86+ routes, 21 routes with curl+Python+TS code samples. |
| **Python + TypeScript SDKs** | `suite.notifications.*`, `suite.webhooks.*`, `suite.billing.*`, `suite.sandbox.*`, `suite.memory.*`, `suite.tools.*` (MCP), `suite.admin.skills.*`, `suite.harnesses.*`. Pydantic + zod parity. |
| **CLI** | `af-stack mcp list/add/remove/call`, `af-stack harness list/install`. |
| **Helm chart** | Production-ready with HPA, NetworkPolicy, PDB, ServiceMonitor. Both `values-dev.yaml` (in-chart PG+MinIO) and `values-prod.yaml` (external everything). Helm lint passes both. |
| **PaaS configs** | Fly.io (2 apps via flycast), Railway template, Render Blueprint, `docker-compose.prod.yml`, Caddy with auto-TLS. |
| **Graceful shutdown** | `/health` is cheap liveness, `/ready` returns 503 during boot+drain+DB-down with proper `Retry-After`. SIGTERM triggers ordered shutdown: HTTP drain → workers cancel → DB close. |

## What's INTENTIONALLY NOT real (needs external key)

These ship as **stubs that compile** so the page works without the key.
Set the key and the same code path makes a real call.

| Surface | Why it's stubbed | How to make it real |
|---|---|---|
| **Stripe billing** | Real Stripe needs `sk_test_…` or `sk_live_…` | Set `STRIPE_SECRET_KEY` in `.env`. Stub mode → live mode automatically. Portal links become real Stripe URLs. |
| **Resend email** | Real email needs `RESEND_API_KEY` | Set the key + `AF_STACK_NOTIFICATIONS_ADAPTER=resend`. The log adapter becomes the resend adapter. |
| **OpenAI / Anthropic / Google / Mistral / DeepSeek / Groq direct** | Each upstream needs its own provider key | Set the matching `..._API_KEY` and the matching `model_list` entry in `apps/backend/litellm-config.yaml` activates automatically. The LiteLLM sidecar handles the routing — no code changes needed for new providers. |
| **e2b sandbox** | Needs `E2B_API_KEY` | `AF_STACK_SANDBOX_ADAPTER=e2b` + the key. Default in prod compose. |
| **GitHub MCP** (and other vendor MCPs) | Each MCP server needs its provider's token | Store the token in secrets vault, reference as `secret:<key>` in the server's env config. |
| **OAuth providers** | Google / GitHub OAuth need client IDs | `GOOGLE_CLIENT_ID` + `GOOGLE_CLIENT_SECRET` (or GitHub equivalent). Better-auth picks them up at boot. |

## What's NOT in v1 at all

We deferred these because they need community pull or domain commitment:

- **Lago billing adapter** — Stripe is the only billing backend. Lago support is a 1-day adapter add when someone asks.
- **Additional LLM providers** beyond the catalog — Mistral, DeepSeek, xAI. The provider abstraction handles them; we haven't shipped the configs.
- **Websocket MCP transport** — only stdio + SSE today.
- **Workload modules for Examples 02 / 04 / 05** — Shipwright (SWE-AF SaaS), Podcast (multimodal), Reactive Enrichment (PG/Mongo change streams). The pattern is documented in `docs/workload-modules.md`; the modules themselves are post-launch.
- **TypeScript webhook handlers** in workload modules — Go + Python handler shapes work; TS handler scaffolding hasn't shipped.
- **Hosted SaaS version** — there isn't one. The self-host story has to work first.
- **Native mobile SDKs** — REST + the OpenAI SDK shape work from anything that speaks HTTP.

## Developer experience

What it actually feels like to build on this.

### Adding your own agent

```python
# apps/backend/agents/my-agent/main.py
from agentfield import Agent
from pydantic import BaseModel

app = Agent(node_id="my-agent")

class Result(BaseModel):
    tldr: str
    key_points: list[str]

@app.reasoner(tags=["text"])
async def summarize(payload: dict) -> dict:
    result = await app.ai(
        system="Summarize in 1 sentence then 3 bullet points.",
        user=payload["text"],
        schema=Result,
    )
    return result.model_dump()

if __name__ == "__main__":
    app.run()
```

Drop the directory in, `docker compose up`, and the agent shows up in
the dashboard's `/build/agents` tab. Calls are made at
`POST /api/v1/agents/my-agent.summarize`.

### Adding a dashboard tab

```ts
// apps/dashboard/plugins/sales-dashboard/plugin.ts
import type { Plugin } from "@/lib/api"
const p: Plugin = {
  id: "sales-dashboard",
  name: "Sales",
  description: "Pipeline metrics.",
  route: "/plugins/sales-dashboard",
  icon: "TrendingUp",
  group: "Operate",
  version: "0.1.0",
}
export default p
```

```tsx
// apps/dashboard/plugins/sales-dashboard/page.tsx
import { api } from "@/lib/api"
export default async function SalesPage() {
  const cost = await api.cost()
  return <div>30d spend: ${cost.period_total_usd}</div>
}
```

The next build discovers the manifest, the sidebar adds your tab,
done. No fork.

### Adding a workload module

```yaml
# workload-modules/notes/manifest.yaml
id: notes
name: Notes
version: 0.1.0
requires: [multi-tenancy, llm-gateway]
routes:
  - { method: POST, path: /notes, handler: notes.Create }
meters:
  - { name: notes_created, unit: count }
```

Go handler at `workload-modules/notes/handlers/notes.go`, migration at
`workload-modules/notes/migrations/00001_init.sql`. The loader picks
it up at boot, mounts routes at `/workload/notes/...`, applies
migrations under its own schema_migrations table.

### Swapping a default

Edit one env var.

```
AF_STACK_SANDBOX_ADAPTER=gvisor          # docker → gvisor
AF_STACK_S3_ADAPTER=s3                   # minio → external S3
AF_STACK_NOTIFICATIONS_ADAPTER=resend    # log → resend
AF_STACK_MODULE_BILLING=false            # disable a whole module
```

Restart. No config file fork. The dashboard's `/build/modules` tab
reflects the live state.

### Theming

Drop `apps/dashboard/src/app/brand.css` with your CSS variable
overrides, import it after `globals.css`. Every shadcn primitive,
every chart, every plugin tab picks up the new palette. No theme
provider, no prop-drilling.

### Authentication

You don't write any. Better-auth handles email+password, magic links,
Google, GitHub. The session cookie automatically resolves to a tenant
via `suite_memberships`. Your handler reads
`tenantctx.TenantID(ctx)` and you're done. The DB enforces
isolation; a buggy handler can't leak.

### Cost discipline

Every LLM call goes through the gateway. Every gateway call writes a
`suite_cost_events` row with model + tokens + USD. The dashboard's
Cost tab shows per-model, per-agent, per-tenant breakdowns with a
24-bucket sparkline. Set a budget and the runtime returns 402 when
the tenant exceeds it. None of this is opt-in.

### Observability

`/api/v1/metrics/summary` returns the at-a-glance numbers (p95
latency, goroutines, heap, top-10 routes). The dashboard renders
them. Full Prometheus surface at `/metrics` for your existing
scraper. OpenTelemetry traces stream to whatever collector you point
`OTEL_EXPORTER_OTLP_ENDPOINT` at. Structured logs (slog JSON)
visible in `/operate/logs` for the last N entries from an in-process
ring; ship the rest to your log aggregator however you usually do.

### Production deployment

You have five paths. Pick whichever your team already knows:

- **Helm** → `helm install af-stack ./deploy/helm/af-stack -f values-prod.yaml`
- **Fly.io** → `flyctl launch --from <repo>`, set 5 secrets, done
- **Railway** → click the template, set the env vars
- **Render** → click the Blueprint, same
- **docker-compose.prod.yml + Caddy** → on a VPS for the smallest deploys

Each path has graceful shutdown wired (SIGTERM → drain → close), real
readiness probes, working autoscaler hooks, and a documented backup
+ DR procedure.

## What we get RIGHT that's hard

These are the things that take three months when you build from
scratch. AF Stack ships them ready.

- **Multi-tenancy at the DB boundary, not in handler code.** Most
  teams reach for SaaS auth (Clerk / WorkOS), then hand-roll tenant
  scoping at the application layer. We use PG RLS keyed on a GUC the
  middleware sets per request, so even a handler that forgets to filter
  can't leak.
- **Sandbox abstraction that means anything.** Most projects either
  ship `subprocess.run` and call it a sandbox or wire e2b and call it
  done. We have the same adapter interface across docker / gVisor /
  Firecracker / e2b, so you start with docker for dev and switch to
  Firecracker for hard-multi-tenant prod without changing handler
  code.
- **HMAC + dedup + replay protection on every inbound webhook by
  default.** Most people remember HMAC and forget dedup until a
  webhook fires twice in production and creates two charges.
- **OpenAI-SDK compatibility.** Your existing code that uses the
  OpenAI Python or TS SDK just points at our gateway. No new client
  to learn. No proprietary shape. Costs flow into your tenant ledger
  automatically.
- **Per-tenant cost attribution from day one.** Every LLM call → row
  in `suite_cost_events` → aggregated into `/api/v1/cost` → rendered
  in the dashboard. You don't bolt on Helicone three months later.
- **Operator console + customer-facing app in one repo.** The
  same auth DB. The same patterns. You aren't running two stacks for
  one product.

## What we get WRONG (or haven't gotten to)

Honest list. These are the rough edges in v1.

- **Idempotency keys** — the API surface doesn't accept
  `Idempotency-Key` headers yet. If a network blip retries your
  webhook send, you get two deliveries. Documented in
  `/reference/api-conventions`.
- **Rate-limit headers** — the runtime enforces per-tenant rate
  limits but doesn't return the standard
  `X-RateLimit-Limit/Remaining/Reset` headers yet. Just `Retry-After`
  on 429.
- **Schema migrations under live writes** — we run migrations at
  boot before serving traffic. Online schema changes (à la
  pg_repack) aren't shipped; you'd take a brief downtime to deploy
  a schema migration on a multi-replica cluster.
- **Workload modules** — the pattern is documented but the loader
  doesn't hot-reload. Adding a workload module = runtime restart.
- **No streaming yet for sandbox stdout** — completed runs return
  full stdout/stderr; you can't tail an in-progress sandbox. The
  Stream method on the adapter interface exists but is a v1.1 wire.
- **The Notable example doesn't ship a workload module yet** — the
  notes service is built as a sidecar FastAPI process, not a
  workload module. Refactor is straightforward (pattern is in the
  docs); we just haven't done it.
- **Demo video** — recording brief is in
  `/launch/demo-video-brief`; the video itself is your day-of work.

## Comparison to nearby tools

| Tool | What it solves | What it doesn't | Where AF Stack fits |
|---|---|---|---|
| **Supabase / Firebase** | Auth, DB, storage, real-time | LLM cost attribution, agent sandboxes, per-tenant rate limits, MCP, harnesses | We are these + the AI primitives |
| **Helicone / Portkey** | LLM proxy with cost + cache | Multi-tenancy, sandboxes, jobs, webhooks, the whole rest of the backend | The gateway is one of our modules. We are the rest. |
| **AgentField / Mastra / CrewAI** | Agent framework + orchestration | Backend plumbing | We host AgentField. They're great. Use them on top of us. |
| **e2b / Modal** | Sandbox-as-a-service | Everything else | We use them as one sandbox adapter among four. |
| **Dify / Langflow** | Visual agent builder | Backend you can self-host with multi-tenancy + billing + custom dashboards | Different shape — we're for teams building products, not workflows-in-the-UI |
| **Inngest / Hatchet / Trigger.dev** | Background jobs | LLM gateway, sandboxes, billing, dashboards | River-based jobs are one of our modules |
| **Svix** | Webhook delivery | Everything else | Webhook delivery is one of our modules |

## The 60-second promise

```bash
git clone https://github.com/Agent-Field/backai supportdesk-ai
cd supportdesk-ai
cp .env.example .env
# Edit .env: set OPENROUTER_API_KEY
docker compose up -d
# Open localhost:33000 (operator) + localhost:34000 (customer app)
# 30 seconds later, make a call:
python -c "
from openai import OpenAI
c = OpenAI(base_url='http://localhost:38080/api/v1/llm', api_key='af_…')
print(c.chat.completions.create(model='qwen/qwen-2.5-72b-instruct',
      messages=[{'role':'user','content':'hello'}]).choices[0].message.content)
"
# Cost shows up in the dashboard immediately. Audit log too.
```

That's the whole pitch. The rest is unfair amount of plumbing that
already exists so you can build the thing you actually want to build.

## Where to go next

- [Get Started → Quickstart](/get-started/quickstart) — the 60-second walkthrough
- [Architecture → Overview](/architecture/overview) — how it's built
- [Reference → API](/reference/api) — interactive Scalar browser
- [Examples](https://github.com/Agent-Field/backai/tree/main/examples) — three ready-to-run apps
- [`docs/archive/PRD-v0.md`](docs/archive/PRD-v0.md) — original product requirements doc with all 120 mapped to code

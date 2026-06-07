# AF Stack — Architecture & DX

## The shape of the product

**AF Stack is a forkable template, not a hosted service.** You clone
the repo, customize the layers you care about, and deploy the whole
stack as one thing. The customer-facing app, the operator console, the
runtime, and the agents all live in your fork — they're meant to be
deployed together.

This is the Cal.com / Plane / Outline pattern, applied to AI
backends.

## Layer stack (what we ship, what we vendor)

```
                                                Your customers
                                                       ↓
┌──────────────────────────────────────────────────────────────────┐
│ apps/customer-app/                                               │
│   YOUR product. Next.js. You edit this to be your SaaS.          │
│   Auth, sign-up, dashboard, code-helper demo,                    │
│   billing page, API key panel.                                   │
│   Brand: customize via brand.css.                                │
└────────────────────────────┬─────────────────────────────────────┘
                             │ session cookie + REST
┌──────────────────────────────────────────────────────────────────┐
│ apps/dashboard/                                                  │
│   Operator console. Next.js. View-only on most config;           │
│   CRUD for tenants, keys, secrets, MCP, skills, crons,           │
│   webhooks. Plugin system for custom tabs.                       │
└────────────────────────────┬─────────────────────────────────────┘
                             │ same-origin proxy
                             ↓
┌──────────────────────────────────────────────────────────────────┐
│ services/runtime/                                                │
│   Go. Single binary. Hosts:                                      │
│                                                                  │
│   ┌─ LLM Gateway ──┐ ┌─ Sandboxes ─┐ ┌─ MCP Host ──┐ ┌─ Jobs ─┐  │
│   │  Cost ledger   │ │  Adapters   │ │  stdio / SSE│ │ River  │  │
│   │  Budgets       │ │  Pool       │ │  Secret env │ │ Crons  │  │
│   │  Cache         │ │             │ │             │ │        │  │
│   └────────────────┘ └─────────────┘ └─────────────┘ └────────┘  │
│   ┌─ Memory ───────┐ ┌─ Webhooks ──┐ ┌─ Notifications ─────────┐ │
│   │  pgvector      │ │ In + Out    │ │  Outbox + workers       │ │
│   │  Scope-aware   │ │ HMAC + dedup│ │  Adapters (log/resend)  │ │
│   └────────────────┘ └─────────────┘ └─────────────────────────┘ │
│   ┌─ Multi-tenancy ┐ ┌─ Secrets ───┐ ┌─ Audit ─────────────────┐ │
│   │  PG RLS keyed  │ │  AES-256-GCM│ │  Append-only            │ │
│   │  on session    │ │  envelope   │ │  Every mutation         │ │
│   │  GUC           │ │  encryption │ │                         │ │
│   └────────────────┘ └─────────────┘ └─────────────────────────┘ │
│   ┌─ Billing ──────┐ ┌─ Storage ───┐ ┌─ Observability ─────────┐ │
│   │  Stripe        │ │  MinIO / S3 │ │  slog ring + /api/logs  │ │
│   │  Customer +    │ │  Adapter    │ │  Prometheus /metrics    │ │
│   │  meter sync    │ │             │ │  OTel tracing           │ │
│   └────────────────┘ └─────────────┘ └─────────────────────────┘ │
└────────────────────────────┬─────────────────────────────────────┘
                             │
        ┌────────────────────┼───────────────────────┐
        ↓                    ↓                       ↓
┌────────────────┐  ┌──────────────────┐  ┌──────────────────────┐
│   Postgres     │  │  Object storage  │  │  AgentField          │
│   pgvector     │  │  MinIO / S3 / R2 │  │  control plane       │
│                │  │                  │  │  (vendored)          │
└────────────────┘  └──────────────────┘  └──────────┬───────────┘
                                                     │ JSON-RPC
                                                     ↓
                            ┌────────────────────────────────────┐
                            │  apps/backend/agents/<name>/       │
                            │    Python AF agents. ONE container │
                            │    per agent (or one for all dev). │
                            │                                    │
                            │    HARNESSES install HERE:         │
                            │    - claude-code (CLI)             │
                            │    - codex                         │
                            │    - gemini                        │
                            │    - opencode                      │
                            │                                    │
                            │    Each agent declares which       │
                            │    harnesses it has at startup.    │
                            └────────────────────────────────────┘
```

### Outbound to external providers

```
LLM Gateway → LiteLLM (NEXT) → 100+ providers
                                  OpenRouter / OpenAI / Anthropic /
                                  Google / Mistral / DeepSeek / Groq /
                                  Cohere / Bedrock / etc.

Sandbox → adapter:
   - docker (dev, mounts host docker.sock)
   - gVisor (prod, userspace kernel)
   - Firecracker (hard MT, microVMs via Flintlock)
   - e2b (managed, needs E2B_API_KEY)

Webhooks → outbound delivery via safehttp (blocks private CIDRs)

MCP servers → stdio (uvx, npx, local binary)
            → SSE (https://mcp.example.com/sse)

Storage → S3-compatible (MinIO dev, AWS S3 / R2 / Cloudflare prod)
```

## What's vendored vs what's ours

**Vendored open-source pieces** (we configure + integrate, don't
reimplement):

| Piece | Used for | Why |
|---|---|---|
| **better-auth** | Operator + customer sessions | Best modern Node auth, OAuth-providers built in |
| **River** | Job queue | PG-backed, no Redis, multi-replica safe out of the box |
| **pgvector** | Memory store | Vector + relational in one DB; no Pinecone op cost |
| **robfig/cron/v3** | Cron parsing | The industry-standard Go crontab parser |
| **AgentField** | Agent runtime + lifecycle | We're co-developed; this is the canonical AF deployment |
| **shadcn/ui + base-ui** | Dashboard + customer-app UI | Pragmatic — copy components, own them |
| **Astro Starlight** | Docs site | Fast static build, good for code-heavy docs |
| **stripe-go v82** | Billing | Direct integration, no Stripe.js needed on operator side |
| **MinIO + AWS SDK** | Storage | One adapter shape covers both |
| **Scalar API Reference** | API browser | Modern OpenAPI viewer |
| **Caddy** | Reverse proxy with auto-TLS | Drop-in, no Let's Encrypt scripting |
| **LiteLLM (planned)** | Multi-provider LLM routing | Gives us 100+ providers for the cost of one integration |

**Pieces we wrote** (where we add AI-native value):

- Multi-tenant Postgres RLS pattern keyed on a session GUC
- Per-tenant cost attribution + ledger + budget enforcement
- Sandbox adapter interface across docker/gVisor/Firecracker/e2b
- Workload module loader (drop-in feature packs)
- Dashboard plugin system (file-discovery + lucide icons)
- AF Stack CLI

## The DX flow — how a developer uses this

### Day 1 — fork and run

```bash
git clone https://github.com/Agent-Field/backai my-product
cd my-product
cp .env.example .env
# Edit .env: set OPENROUTER_API_KEY
docker compose up -d
```

Open `localhost:33000` (operator) and `localhost:34000` (customer).
The customer app works end-to-end out of the box.

### Day 1 — make it yours

| What | Where |
|---|---|
| Product name + favicon | `apps/customer-app/public/`, `apps/customer-app/src/app/layout.tsx` |
| Brand colors | `apps/customer-app/src/app/globals.css` and (mirrored) `apps/dashboard/src/app/brand.css` |
| Sign-up copy | `apps/customer-app/src/app/(auth)/sign-up/page.tsx` |
| Landing page | `apps/customer-app/src/app/page.tsx` |
| Operator dashboard sidebar tweaks | Add plugins under `apps/dashboard/plugins/` |

### Day 2 — add your domain logic

```python
# apps/backend/agents/my-pricing/main.py
from agentfield import Agent
from pydantic import BaseModel

app = Agent(node_id="pricing")

class PricingResult(BaseModel):
    recommended_price: float
    confidence: float
    reasoning: str

@app.reasoner(tags=["sales"])
async def estimate(payload: dict) -> dict:
    result = await app.ai(
        system="You are a pricing analyst. Output JSON only.",
        user=payload["context"],
        schema=PricingResult,
    )
    return result.model_dump()
```

Restart. Your agent is now callable at
`POST /api/v1/agents/pricing.estimate` from your customer-app code, and
cost flows into your tenant's ledger automatically.

### Day 2 — add a domain table

```
workload-modules/contracts/
  manifest.yaml
  migrations/
    00001_init.sql           ← CREATE TABLE contracts (...) with RLS
  handlers/
    contracts.go             ← POST/GET/etc., tenant scoping automatic
```

Restart. Tables created. Routes mounted at `/workload/contracts/...`.
RLS applies. Done.

### Day 3 — deploy

```bash
# Helm
helm install my-product ./deploy/helm/af-stack -f values-prod.yaml

# OR Fly.io
flyctl launch --from .

# OR Railway / Render / docker-compose.prod.yml on a VPS
```

## Where harnesses ACTUALLY belong

The current model is wrong (probe-only on the runtime). The correct
model:

```
apps/backend/agents/<name>/
  Dockerfile          ← installs claude-code / codex / gemini / opencode
  main.py             ← declares available_harnesses=[...] at registration
```

The agent declares at registration which harnesses it has installed.
AgentField stores this. The runtime queries AgentField for "which
agents have harness X ready?" rather than probing its own PATH.

**The `/build/harnesses` tab as a top-level item should be merged into
`/build/agents`** as a "harnesses available" column per agent.

This refactor is queued — should land in the next push.

## Where LiteLLM belongs

The current model: `services/runtime/internal/llmgateway/providers/`
has hand-rolled OpenRouter / OpenAI / Anthropic / Google clients.

The correct model: spin up LiteLLM as a sidecar (or embed via its
Python proxy mode), and our gateway calls LiteLLM instead of speaking
each provider's API directly. We keep:

- Our cost ledger / budgets / cache layer (the AF-native value)
- Per-tenant API keys, rate limits, hooks
- The OpenAI-compatible shape we expose to customers

We drop:

- 4 hand-maintained provider clients
- The manual model catalogue (`pricing.go`)

Net: ~100+ models work without us shipping a config per model. This is
also queued for the next push.

## When you'd run the customer-app SEPARATELY

The fork pattern is the primary use case. But if you want to run YOUR
app on another framework (Rails, Django, Phoenix, mobile):

```python
from openai import OpenAI
client = OpenAI(
    base_url="https://your-deploy.example.com/api/v1/llm",
    api_key="af_<your-prefix>_<your-secret>",
)
client.chat.completions.create(model="qwen/qwen-2.5-72b-instruct", ...)
```

That works. The runtime treats you exactly like the customer-app would
— same auth, same tenant resolution, same cost attribution. You just
manage the front-end on your own.

The only thing you give up is the dashboard plugin pattern (because
plugins live inside the dashboard fork). Tradeoff.

## Summary of pending refactors

In order of impact:

1. **Harnesses → AgentField agent layer** (you flagged this twice now)
2. **LLM Gateway → LiteLLM** (delete 4 hand-rolled provider clients)
3. **Notable plugin → moved to example** (done in this commit — was
   leaking into operator console)
4. **Skills installer → real local-path support** (in progress)
5. **MCP → bundle uv in agent container so uvx-based servers work**
6. **Deep research example → wire end-to-end**

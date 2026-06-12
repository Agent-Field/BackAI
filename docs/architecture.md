# AF Stack — Architecture & DX

## The shape of the product

**AF Stack is a forkable template, not a hosted service.** You clone
the repo, customize the layers you care about, and deploy the whole
stack as one thing. The customer-facing app, the operator console, the
runtime, and the agents all live in your fork — they're meant to be
deployed together.

This is the Cal.com / Plane / Outline pattern, applied to AI
backends.

## Layered architecture

For the layered stack diagram (8 bands, Supabase-shaped, with all OSS
choices logged), see [`docs/stack.md`](stack.md).

This document focuses on **extension points and adapter contracts** —
how to add agents, dashboard tabs, workload modules, and swap adapters.

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
| **LiteLLM** | Multi-provider LLM routing (sidecar) | Gives us 100+ providers for the cost of one integration. Runs as a docker-compose sidecar; AF Stack forwards `/api/v1/llm/*` to it and keeps tenant resolution, cost ledger, budgets, cache, and hooks on its side. |

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

The standalone `/build/harnesses` tab is folded into `/build/agents`.
The REST surface stays in place as the data source, but the dashboard
presents harness readiness where it belongs: next to registered agents.

## How LiteLLM is wired

The gateway no longer ships hand-rolled OpenRouter / OpenAI /
Anthropic / Google clients. AF Stack runs a **LiteLLM Proxy sidecar**
(image `ghcr.io/berriai/litellm:main-stable`) in docker-compose; the
runtime forwards every `/api/v1/llm/*` call to it. LiteLLM handles
100+ upstream providers via `apps/backend/litellm-config.yaml`.

AF Stack keeps on its side (where the AI-native value sits):

- Per-tenant API keys, rate limits, cost attribution
- Cost ledger / budgets / cache layer
- Pre/post-call hooks
- The OpenAI-compatible shape customers point their SDK at

LiteLLM is purely an internal routing layer — customers never see it
directly. Adding a new provider is now a config edit, not a code
change. See `services/runtime/internal/llmgateway/litellm_provider.go`
for the runtime wiring.

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

1. **Skills installer → real local-path support** (in progress)
2. **Deep research example → wire end-to-end**
3. **LiteLLM virtual keys** — see [`development/strategy.md`](../development/strategy.md)
4. **Billing adapter** — Stripe + Lago behind one interface
5. **Shipwright** — autonomous AI agent factory

_LLM Gateway → LiteLLM, Svix outbound webhooks, uvx in agent containers,
and harnesses in agent containers have landed._

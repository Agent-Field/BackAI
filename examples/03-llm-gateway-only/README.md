# Example 03 — LLM Gateway Only

Run an OpenAI-compatible gateway in 60 seconds. Multi-provider routing
(OpenRouter → OpenAI → Anthropic), per-call cost ledger, in-memory
cache. No multi-tenancy, no auth complexity, no sandbox — just the
gateway.

This is the smallest possible AF Stack deployment. Point any OpenAI SDK
at `http://localhost:8080/api/v1/llm` and every call is logged, costed,
and cacheable.

---

## 60-second quickstart

```bash
cd examples/03-llm-gateway-only
cp .env.example .env
# paste your OpenRouter key into .env (get one free at https://openrouter.ai/keys)

docker compose up -d

# wait ~30s for postgres + runtime to come up, then:
./scripts/test-call.sh
```

You should see three things printed:

1. A raw `curl` response with a chat completion.
2. The same call via the Python OpenAI SDK.
3. A `period_total_usd` figure from `/api/v1/cost` showing the spend
   from those two calls (~$0.0002 with Qwen 2.5 72B).

Open `http://localhost:3000/operate/cost` to watch the same number live.

---

## Python — OpenAI SDK

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/api/v1/llm",
    api_key="not-needed-mt-off",  # multi-tenancy is off in this example
)

resp = client.chat.completions.create(
    model="qwen/qwen-2.5-72b-instruct",
    messages=[
        {"role": "user", "content": "What is an LLM gateway?"},
    ],
    max_tokens=80,
)

print(resp.choices[0].message.content)
print(resp.usage.prompt_tokens, resp.usage.completion_tokens)
```

Install: `pip install openai`. Run: `python scripts/openai-demo.py "your prompt"`.

---

## JavaScript — OpenAI SDK

```js
import OpenAI from "openai"

const client = new OpenAI({
  baseURL: "http://localhost:8080/api/v1/llm",
  apiKey: "not-needed-mt-off",
})

const resp = await client.chat.completions.create({
  model: "qwen/qwen-2.5-72b-instruct",
  messages: [{ role: "user", content: "What is an LLM gateway?" }],
  max_tokens: 80,
})

console.log(resp.choices[0].message.content)
```

Install: `npm i openai`.

---

## Raw curl

```bash
curl -s http://localhost:8080/api/v1/llm/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen/qwen-2.5-72b-instruct",
    "messages": [{"role": "user", "content": "Hi"}],
    "max_tokens": 40
  }'
```

The response shape matches OpenAI exactly: `choices[0].message.content`,
`usage.prompt_tokens`, `usage.completion_tokens`.

---

## What each service does

| Service | Purpose | Port |
|---|---|---|
| `postgres` | Stores cost events + provider keys. pgvector image so you can upgrade to Example 01 later without rebuilding. | 5432 |
| `agentfield` | Control plane. Runtime calls it for `/health` and identity. Idle in this example. | 8081 |
| `runtime` | The Go binary serving `/api/v1/llm/*` and `/api/v1/cost`. Single shared port. | 8080 |
| `dashboard` | Read-only operator view of cost, models used, latency. | 3000 |

No MinIO. No docker.sock mount. No jobs queue. No vault.

---

## Switching providers

The runtime picks the first provider whose key is set, in this order:
**OpenRouter → OpenAI → Anthropic.**

To swap to OpenAI direct, either:

**Option A — edit `.env`:**

```bash
OPENROUTER_API_KEY=          # leave blank
OPENAI_API_KEY=sk-...        # paste OpenAI key
```

**Option B — use the override file:**

```bash
docker compose -f docker-compose.yml -f compose-override-example.yml up
```

Then call with an OpenAI-native model: `model: "gpt-4o-mini"`.

For Anthropic, set `ANTHROPIC_API_KEY` and call with
`model: "claude-haiku-4-5-20251001"`. The full price catalogue lives in
`services/runtime/internal/pricing/pricing.go`.

---

## Where the cost shows up

Every chat / embedding / image call fires `HookLLMPostCall`, which the
cost recorder writes to `suite_cost_events` in Postgres. Three places
surface that data:

1. **`GET /api/v1/cost`** — period totals + by-model / by-day breakdown.
2. **`GET /api/v1/cost/events?limit=20`** — raw event list.
3. **Dashboard at `/operate/cost`** — live cost panel.

Cached calls (Phase 7.3 in-memory cache) are recorded with
`cost_usd=0` and `cached=true`, so you can see your hit rate.

---

## Next step — turn on multi-tenancy

This example skips per-tenant API keys, budgets, and rate-limits. When
you're ready to issue keys to your customers and bill them, switch to
**[Example 01 — Notable](../01-notable/)** which is the same gateway
with multi-tenancy on, the budget guard wired, and the customer
self-serve console enabled.

---

## Files in this directory

```
docker-compose.yml             # 4-service stack
compose-override-example.yml   # swap to OpenAI direct
config.yaml                    # runtime modules: gateway + cache on
.env.example                   # OPENROUTER_API_KEY + AF_STACK_AUTH_SECRET
scripts/
  test-call.sh                 # curl + python + cost demo
  openai-demo.py               # OpenAI SDK example
  screenshot.sh                # capture cost.png for this README
```

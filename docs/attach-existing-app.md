# Attach An Existing App

BackAI can be the whole app template, or it can sit behind a product you
already have. Use this path when your mobile app, web app, or API server
is staying in its own repo and you only want the AI backend.

## What You Get

- OpenAI-compatible `/api/v1/llm/*` endpoint.
- Tenant API keys and request authentication.
- Cost events with model, token, tenant, request id, provider, cache
  state, and estimated USD cost.
- Admin dashboard for spend, tenants, keys, runs, budgets, and backend
  operations.
- Optional AgentField agents, memory, jobs, storage, billing hooks,
  sandboxes, and workload modules.

## What You Do Not Need

- You do not need to use `apps/customer-app/`.
- You do not need to move your product database into BackAI.
- You do not need to call AgentField directly from your app.

Your app calls BackAI. BackAI calls the provider layer and AgentField
where needed.

## Minimal Flow

1. Deploy BackAI with runtime, dashboard, LiteLLM, AgentField, and
   Postgres.
2. Create a tenant and API key from the admin dashboard or admin API.
3. Store that key in your backend secret manager.
4. Point your existing OpenAI SDK client at the BackAI base URL.
5. Pass an `X-Request-ID` from your app when you want to deep-link one
   customer action to the admin cost ledger.

```ts
import OpenAI from "openai"

const client = new OpenAI({
  baseURL: "https://backai.example.com/api/v1/llm",
  apiKey: process.env.BACKAI_API_KEY,
})

const response = await client.chat.completions.create(
  {
    model: "qwen/qwen-2.5-72b-instruct",
    messages: [{ role: "user", content: "Draft a support reply." }],
  },
  {
    headers: {
      "X-Request-ID": requestId,
    },
  },
)
```

## Deployment Shape

For an existing app, keep these services:

| Service | Required | Why |
| --- | --- | --- |
| `runtime` | Yes | Public API, LLM gateway, auth, keys, cost ledger. |
| `dashboard` | Yes | Operator console and tenant/key/cost administration. |
| `postgres` | Yes | Runtime state, cost events, AgentField storage. |
| `agentfield` | Yes | Agent/runtime substrate and run traces. |
| `litellm` | Yes for real providers | Provider routing and model compatibility. |
| `customer-app` | Optional | Only needed if you want the bundled SupportDesk app. |
| `minio` or S3 | Optional | Needed for storage, artifacts, and some sandbox flows. |

The `examples/03-llm-gateway-only/` example is the smallest local shape
for this mode. The full root `docker-compose.yml` is still the better
first-run demo because it shows customer action to admin evidence.

## Production Notes

- Do not ship browser clients with tenant API keys. Your existing
  backend should call BackAI server-side.
- Keep one tenant per customer or workspace so cost, budgets, and audit
  records stay meaningful.
- Use `X-Request-ID` consistently. It is the bridge between your app's
  action log and BackAI's cost ledger.
- Leave `AF_STACK_DEMO_MODE=auto` only for demos. Set
  `AF_STACK_DEMO_MODE=false` in production once provider keys are
  configured.
- If you enable sandboxes in production, prefer `e2b` or another managed
  adapter over mounting the Docker socket.

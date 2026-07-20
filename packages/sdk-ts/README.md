# @af-stack/sdk

The Suite SDK for AF Stack — TypeScript. The cross-language twin of the
Python SDK (`packages/sdk-py`); the two are kept at parity by
`packages/sdk-parity.json`.

```ts
import { suite, ctx } from "@af-stack/sdk"

// Call an agent from app code
const result = await suite.agents.call("supportdesk.echo", { payload: { message: "hi" } })

// Read tenant context (set by middleware)
const tenantId = ctx.tenantId
```

## Explicit client

`suite.*` reads `AF_STACK_URL` / `AF_STACK_API_KEY` from the environment. For
an explicit, configurable client, construct `BackAI` — the direct counterpart
to `af_stack.BackAI`:

```ts
import { BackAI } from "@af-stack/sdk"

const client = new BackAI({
  baseUrl: "https://api.example.com",
  apiKey: "sk-...",
  timeout: 30_000, // ms (default)
  maxRetries: 2, // default
})

const res = await client.agents.call("supportdesk.echo", { payload: { message: "hi" } })
```

It exposes the same governed namespaces as the singleton. Config binds
ambiently for the duration of each delegated call, so per-call `opts` overrides
still win.

### Transport rules

- **Timeouts.** Explicit clients apply a 30s per-request timeout; override per
  client (`timeout`) or per call (`opts.timeout`, ms).
- **Retries.** Explicit clients retry transient failures (`429`, `5xx`) with
  exponential backoff + full jitter, honouring a `Retry-After` header (numeric
  seconds or HTTP-date, capped at 20s). Only inherently-idempotent methods
  (`GET`/`HEAD`/`OPTIONS`) retry automatically.
- **Mutation safety.** A non-idempotent mutation is **never** auto-retried
  unless the caller passes an `idempotencyKey` option, which also sends the
  `Idempotency-Key` header so the runtime can dedupe a replay.

### Pagination

`paginate` auto-iterates offset- and cursor-style list endpoints (mirrors
`af_stack.paginate`):

```ts
import { BackAI, paginate } from "@af-stack/sdk"

const client = new BackAI()
for await (const job of paginate<Job>(async (cursor) => {
  const offset = (cursor as number) ?? 0
  const page = await client.jobs.list({ offset, limit: 50 })
  return [page.jobs, page.hasMore ? offset + page.jobs.length : null]
})) {
  console.log(job.id)
}
```

## Runtime-version policy

The SDK targets a runtime-version range of **`>=0.0.0,<1.0.0`**
(`SUPPORTED_RUNTIME`). An explicit client lazily fetches `GET /api/v1/version`
**once** on its first call and `console.warn`s on a **major** mismatch; it
never fails, and a `404` (older runtimes without the endpoint) is tolerated as
"unknown". `checkRuntimeCompat(version)` exposes the pure policy for tests.
This matches the Python SDK's policy exactly.

## Entry points

| Import                  | Surface                                                                         |
| ----------------------- | ------------------------------------------------------------------------------- |
| `@af-stack/sdk`         | Default Node entry — all operational namespaces, `admin`, `BackAI`, pagination. |
| `@af-stack/sdk/browser` | Browser-safe subset — **excludes** the privileged `admin` and `Worker` surface. |
| `@af-stack/sdk/server`  | Privileged superset — everything plus the pull-based `Worker`.                  |

## Background workers

The pull-based `Worker` executes language-neutral job definitions against the
runtime's worker protocol (`/api/v1/jobs/worker/*`). It authenticates with a
tenant key carrying the `jobs:work` scope and is **server-only** (installs
process signal handlers, long-polls a lease loop), so it lives at
`@af-stack/sdk/server`:

```ts
import { Worker } from "@af-stack/sdk/server"

const worker = new Worker("http://localhost:8080", process.env.AF_STACK_API_KEY!)

worker.register("resize-image", async (payload, ctx) => {
  ctx.log("resizing", { fields: { url: payload.url } })
  if (ctx.isCanceled()) return
  return { thumbnail: await resize(payload.url) }
})

await worker.run() // resolves on SIGTERM/SIGINT after a graceful drain
```

## Two SDKs

- **AgentField SDK** — _defines_ agents
- **Suite SDK** (this package) — _calls_ agents and uses suite infrastructure

See the main repo: https://github.com/Agent-Field/backai

## License

Apache 2.0

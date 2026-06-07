#!/usr/bin/env node
/**
 * inject-code-samples.mjs
 *
 * Reads docs-site/public/openapi.json, attaches the OpenAPI vendor
 * extension `x-codeSamples` (the spec extension Scalar + ReDoc both
 * understand) to ~20 high-traffic endpoints, and writes the file back.
 *
 * Why a post-processor instead of teaching the Go OpenAPI builder?
 *   * Samples can be edited without recompiling the runtime.
 *   * The runtime stays a server — code samples are a docs concern.
 *   * Easier to keep code samples in sync with the test scripts and
 *     SDK tests that already exercise these endpoints (curl, Python,
 *     TypeScript) without bloating the Go source.
 *
 * Inputs:
 *   argv[2] (optional)  path to openapi.json. Defaults to
 *                       ../public/openapi.json relative to this file.
 *
 * The samples below mirror:
 *   scripts/test-llm-call.sh, test-sandbox.sh, test-notifications.sh,
 *   test-webhooks.sh, test-mcp.sh, test-billing.sh
 *   packages/sdk-py/tests/test_*.py
 *   packages/sdk-ts/tests/*.test.ts
 *
 * Adding new samples
 * ------------------
 * Add an entry to SAMPLES keyed on `${method.toUpperCase()} ${path}`.
 * Each value is an array of { lang, label, source } per OpenAPI's
 * x-codeSamples convention (Redocly + Scalar both render it).
 *
 * The script is intentionally noisy when a key in SAMPLES doesn't match
 * any path in the spec — that means the runtime moved a route and the
 * sample is now stale; surface it loudly so it gets fixed.
 */

import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const SPEC_PATH = process.argv[2]
  ? resolve(process.argv[2])
  : resolve(HERE, "..", "public", "openapi.json");

const RUNTIME_BASE = "http://localhost:38080";
const API = "/api/v1";

// ---------------------------------------------------------------------------
// Reusable language helpers
//
// `curl` samples are copy-pasteable smoke tests against the dev runtime.
// `python` uses the af_stack SDK pattern from packages/sdk-py.
// `typescript` uses the @af-stack/sdk-ts pattern.

const curl = (s) => ({ lang: "Shell", label: "curl", source: s });
const py = (s) => ({ lang: "Python", label: "af-stack (python)", source: s });
const ts = (s) => ({ lang: "TypeScript", label: "af-stack (ts)", source: s });

// ---------------------------------------------------------------------------
// SAMPLES — keyed by `METHOD path` (matches OpenAPI verbatim).

const SAMPLES = {
  // ── LLM gateway ─────────────────────────────────────────────────────────
  [`POST ${API}/llm/chat/completions`]: [
    curl(
      `curl -X POST ${RUNTIME_BASE}${API}/llm/chat/completions \\
  -H "Authorization: Bearer $AF_STACK_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "qwen/qwen-2.5-72b-instruct",
    "messages": [{"role":"user","content":"Hello"}],
    "temperature": 0.7
  }'`,
    ),
    py(
      `from af_stack import llm

resp = await llm.chat(
    "qwen/qwen-2.5-72b-instruct",
    [{"role": "user", "content": "Hello"}],
    temperature=0.7,
)
print(resp["choices"][0]["message"]["content"])`,
    ),
    ts(
      `import { chat } from "@af-stack/sdk-ts"

const resp = await chat(
  "qwen/qwen-2.5-72b-instruct",
  [{ role: "user", content: "Hello" }],
  { temperature: 0.7 },
)
console.log(resp.choices[0].message.content)`,
    ),
  ],

  [`POST ${API}/llm/embeddings`]: [
    curl(
      `curl -X POST ${RUNTIME_BASE}${API}/llm/embeddings \\
  -H "Authorization: Bearer $AF_STACK_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"text-embedding-3-small","input":"hello world"}'`,
    ),
    py(
      `from af_stack import llm

vec = await llm.embed("text-embedding-3-small", "hello world")
print(vec["data"][0]["embedding"][:5])`,
    ),
    ts(
      `import { embed } from "@af-stack/sdk-ts"

const vec = await embed("text-embedding-3-small", "hello world")
console.log(vec.data[0].embedding.slice(0, 5))`,
    ),
  ],

  [`GET ${API}/llm/models`]: [
    curl(
      `curl ${RUNTIME_BASE}${API}/llm/models \\
  -H "Authorization: Bearer $AF_STACK_API_KEY"`,
    ),
    py(
      `from af_stack import llm

catalog = await llm.models()
for m in catalog.models:
    print(m.id, m.prompt_usd_per_1m)`,
    ),
    ts(
      `import { llmModels } from "@af-stack/sdk-ts"

const catalog = await llmModels()
for (const m of catalog.models) console.log(m.id, m.prompt_usd_per_1m)`,
    ),
  ],

  [`GET ${API}/llm/cache/stats`]: [
    curl(
      `curl ${RUNTIME_BASE}${API}/llm/cache/stats \\
  -H "Authorization: Bearer $AF_STACK_API_KEY"`,
    ),
    py(
      `from af_stack import llm

stats = await llm.cache_stats()
print(stats.hit_rate, stats.savings_usd)`,
    ),
    ts(
      `import { llmCacheStats } from "@af-stack/sdk-ts"

const stats = await llmCacheStats()
console.log(stats.hit_rate, stats.savings_usd)`,
    ),
  ],

  // ── Sandbox ─────────────────────────────────────────────────────────────
  [`POST ${API}/sandbox/run`]: [
    curl(
      `curl -X POST ${RUNTIME_BASE}${API}/sandbox/run \\
  -H "Authorization: Bearer $AF_STACK_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "image": "alpine",
    "command": ["sh","-c","echo hello world"],
    "timeout_s": 30
  }'`,
    ),
    py(
      `from af_stack import sandbox

row = await sandbox.run("python:3.12-slim", ["python","-c","print(1)"])
print(row.status, row.exit_code, row.stdout)`,
    ),
    ts(
      `import { sandbox } from "@af-stack/sdk-ts"

const row = await sandbox.run("python:3.12-slim", ["python","-c","print(1)"])
console.log(row.status, row.exitCode, row.stdout)`,
    ),
  ],

  [`GET ${API}/sandbox/pool`]: [
    curl(
      `curl ${RUNTIME_BASE}${API}/sandbox/pool \\
  -H "Authorization: Bearer $AF_STACK_API_KEY"`,
    ),
    py(
      `from af_stack import sandbox

stats = await sandbox.pool()
print(stats.total_runs_today, stats.in_flight)`,
    ),
    ts(
      `import { sandboxPool } from "@af-stack/sdk-ts"

const stats = await sandboxPool()
console.log(stats.totalRunsToday, stats.inFlight)`,
    ),
  ],

  [`GET ${API}/sandbox/runs`]: [
    curl(
      `curl "${RUNTIME_BASE}${API}/sandbox/runs?limit=10" \\
  -H "Authorization: Bearer $AF_STACK_API_KEY"`,
    ),
    py(
      `from af_stack import sandbox

rows = await sandbox.runs(limit=10)
for r in rows:
    print(r.id, r.status, r.image)`,
    ),
    ts(
      `import { sandboxRuns } from "@af-stack/sdk-ts"

const rows = await sandboxRuns({ limit: 10 })
for (const r of rows) console.log(r.id, r.status, r.image)`,
    ),
  ],

  // ── Notifications ───────────────────────────────────────────────────────
  [`POST ${API}/notifications`]: [
    curl(
      `curl -X POST ${RUNTIME_BASE}${API}/notifications \\
  -H "Authorization: Bearer $AF_STACK_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "kind": "email",
    "template": "welcome",
    "to": "user@example.com",
    "subject": "Welcome",
    "data": {"name":"Alice"}
  }'`,
    ),
    py(
      `from af_stack import notifications

n = await notifications.send(
    to="user@example.com",
    kind="email",
    template="welcome",
    subject="Welcome",
    data={"name": "Alice"},
)
print(n.id, n.status)`,
    ),
    ts(
      `import { notifications } from "@af-stack/sdk-ts"

const n = await notifications.send({
  to: "user@example.com",
  kind: "email",
  template: "welcome",
  subject: "Welcome",
  data: { name: "Alice" },
})
console.log(n.id, n.status)`,
    ),
  ],

  [`GET ${API}/notifications`]: [
    curl(
      `curl "${RUNTIME_BASE}${API}/notifications?limit=10" \\
  -H "Authorization: Bearer $AF_STACK_API_KEY"`,
    ),
    py(
      `from af_stack import notifications

page = await notifications.list(limit=10)
for n in page.notifications:
    print(n.id, n.status, n.to)`,
    ),
    ts(
      `import { notifications } from "@af-stack/sdk-ts"

const page = await notifications.list({ limit: 10 })
for (const n of page.notifications) console.log(n.id, n.status, n.to)`,
    ),
  ],

  [`GET ${API}/notifications/stats`]: [
    curl(
      `curl ${RUNTIME_BASE}${API}/notifications/stats \\
  -H "Authorization: Bearer $AF_STACK_API_KEY"`,
    ),
    py(
      `from af_stack import notifications

stats = await notifications.stats()
print(stats.sent_today, stats.failed_today)`,
    ),
    ts(
      `import { notifications } from "@af-stack/sdk-ts"

const stats = await notifications.stats()
console.log(stats.sent_today, stats.failed_today)`,
    ),
  ],

  // ── Webhooks (outbound + inbound) ───────────────────────────────────────
  [`POST ${API}/webhooks/send`]: [
    curl(
      `curl -X POST ${RUNTIME_BASE}${API}/webhooks/send \\
  -H "Authorization: Bearer $AF_STACK_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "url": "https://httpbin.org/post",
    "event_type": "user.created",
    "body": {"id":"u_123","email":"a@b"}
  }'`,
    ),
    py(
      `from af_stack import webhooks

d = await webhooks.send(
    url="https://httpbin.org/post",
    event_type="user.created",
    body={"id": "u_123", "email": "a@b"},
)
print(d.id, d.status)`,
    ),
    ts(
      `import { webhooks } from "@af-stack/sdk-ts"

const d = await webhooks.send({
  url: "https://httpbin.org/post",
  event_type: "user.created",
  body: { id: "u_123", email: "a@b" },
})
console.log(d.id, d.status)`,
    ),
  ],

  [`POST ${API}/webhooks/endpoints`]: [
    curl(
      `curl -X POST ${RUNTIME_BASE}${API}/webhooks/endpoints \\
  -H "Authorization: Bearer $AF_STACK_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "slug": "stripe-prod",
    "provider": "custom",
    "forward_to": "http://localhost:9999/sink",
    "signature_algorithm": "sha256",
    "signature_header": "X-Signature"
  }'`,
    ),
    py(
      `from af_stack import webhooks

ep = await webhooks.create_endpoint(
    slug="stripe-prod",
    provider="custom",
    forward_to="http://localhost:9999/sink",
    signature_algorithm="sha256",
    signature_header="X-Signature",
)
print(ep.id, ep.slug)`,
    ),
    ts(
      `import { webhooks } from "@af-stack/sdk-ts"

const ep = await webhooks.createEndpoint({
  slug: "stripe-prod",
  provider: "custom",
  forward_to: "http://localhost:9999/sink",
  signature_algorithm: "sha256",
  signature_header: "X-Signature",
})
console.log(ep.id, ep.slug)`,
    ),
  ],

  [`GET ${API}/webhooks/endpoints`]: [
    curl(
      `curl ${RUNTIME_BASE}${API}/webhooks/endpoints \\
  -H "Authorization: Bearer $AF_STACK_API_KEY"`,
    ),
    py(
      `from af_stack import webhooks

eps = await webhooks.list_endpoints()
for ep in eps.endpoints:
    print(ep.id, ep.slug, ep.provider)`,
    ),
    ts(
      `import { webhooks } from "@af-stack/sdk-ts"

const eps = await webhooks.listEndpoints()
for (const ep of eps.endpoints) console.log(ep.id, ep.slug, ep.provider)`,
    ),
  ],

  [`GET ${API}/webhooks/deliveries`]: [
    curl(
      `curl "${RUNTIME_BASE}${API}/webhooks/deliveries?limit=20" \\
  -H "Authorization: Bearer $AF_STACK_API_KEY"`,
    ),
    py(
      `from af_stack import webhooks

page = await webhooks.list_deliveries(limit=20)
for d in page.deliveries:
    print(d.id, d.status, d.direction)`,
    ),
    ts(
      `import { webhooks } from "@af-stack/sdk-ts"

const page = await webhooks.listDeliveries({ limit: 20 })
for (const d of page.deliveries) console.log(d.id, d.status, d.direction)`,
    ),
  ],

  // ── Billing ─────────────────────────────────────────────────────────────
  [`GET ${API}/billing/meters`]: [
    curl(
      `curl ${RUNTIME_BASE}${API}/billing/meters \\
  -H "Authorization: Bearer $AF_STACK_API_KEY"`,
    ),
    py(
      `from af_stack import billing

meters = await billing.meters()
for m in meters.meters:
    print(m.event_name, m.aggregation)`,
    ),
    ts(
      `import { billing } from "@af-stack/sdk-ts"

const meters = await billing.meters()
for (const m of meters.meters) console.log(m.event_name, m.aggregation)`,
    ),
  ],

  [`GET ${API}/billing/customers`]: [
    curl(
      `curl ${RUNTIME_BASE}${API}/billing/customers \\
  -H "Authorization: Bearer $AF_STACK_API_KEY"`,
    ),
    py(
      `from af_stack import billing

custs = await billing.customers()
for c in custs.customers:
    print(c.tenant_id, c.stripe_customer_id)`,
    ),
    ts(
      `import { billing } from "@af-stack/sdk-ts"

const custs = await billing.customers()
for (const c of custs.customers) console.log(c.tenant_id, c.stripe_customer_id)`,
    ),
  ],

  // ── MCP servers ─────────────────────────────────────────────────────────
  [`GET ${API}/mcp/servers`]: [
    curl(
      `curl ${RUNTIME_BASE}${API}/mcp/servers \\
  -H "Authorization: Bearer $AF_STACK_API_KEY"`,
    ),
    py(
      `from af_stack import tools

servers = await tools.mcp.list_servers()
for s in servers.servers:
    print(s.name, s.status)`,
    ),
    ts(
      `import { tools } from "@af-stack/sdk-ts"

const servers = await tools.mcp.listServers()
for (const s of servers.servers) console.log(s.name, s.status)`,
    ),
  ],

  [`POST ${API}/mcp/servers`]: [
    curl(
      `curl -X POST ${RUNTIME_BASE}${API}/mcp/servers \\
  -H "Authorization: Bearer $AF_STACK_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "name": "fs",
    "transport": "stdio",
    "command": ["mcp-server-filesystem","/data"],
    "description": "local filesystem MCP"
  }'`,
    ),
    py(
      `from af_stack import tools

await tools.mcp.add_server(
    name="fs",
    transport="stdio",
    command=["mcp-server-filesystem", "/data"],
    description="local filesystem MCP",
)`,
    ),
    ts(
      `import { tools } from "@af-stack/sdk-ts"

await tools.mcp.addServer({
  name: "fs",
  transport: "stdio",
  command: ["mcp-server-filesystem", "/data"],
  description: "local filesystem MCP",
})`,
    ),
  ],

  [`PUT ${API}/mcp/servers/{name}/enabled`]: [
    curl(
      `curl -X PUT ${RUNTIME_BASE}${API}/mcp/servers/fs/enabled \\
  -H "Authorization: Bearer $AF_STACK_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"enabled": false}'`,
    ),
    py(
      `from af_stack import tools

await tools.mcp.set_enabled("fs", enabled=False)`,
    ),
    ts(
      `import { tools } from "@af-stack/sdk-ts"

await tools.mcp.setEnabled("fs", { enabled: false })`,
    ),
  ],

  // ── Agents (run a reasoner) ─────────────────────────────────────────────
  // The runtime exposes a single dispatch route /api/v1/agents/{call}
  // (e.g. {call} = "sample.echo"). The async sibling is /agents/async/{call}.
  [`POST ${API}/agents/{call}`]: [
    curl(
      `curl -X POST ${RUNTIME_BASE}${API}/agents/sample.echo \\
  -H "Authorization: Bearer $AF_STACK_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"input":{"payload":{"message":"hi"}}}'`,
    ),
    py(
      `from af_stack import agents

run = await agents.invoke("sample.echo", payload={"message": "hi"})
print(run.status, run.output)`,
    ),
    ts(
      `import { agents } from "@af-stack/sdk-ts"

const run = await agents.invoke("sample.echo", { payload: { message: "hi" } })
console.log(run.status, run.output)`,
    ),
  ],

  [`POST ${API}/agents/async/{call}`]: [
    curl(
      `curl -X POST ${RUNTIME_BASE}${API}/agents/async/sample.echo \\
  -H "Authorization: Bearer $AF_STACK_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"input":{"payload":{"message":"hi"}}}'`,
    ),
    py(
      `from af_stack import agents

job = await agents.invoke_async("sample.echo", payload={"message": "hi"})
print(job.run_id, job.status)`,
    ),
    ts(
      `import { agents } from "@af-stack/sdk-ts"

const job = await agents.invokeAsync("sample.echo", { payload: { message: "hi" } })
console.log(job.run_id, job.status)`,
    ),
  ],
};

// ---------------------------------------------------------------------------
// Apply samples

function main() {
  if (!existsSync(SPEC_PATH)) {
    console.error(`inject-code-samples: spec not found at ${SPEC_PATH}`);
    process.exit(1);
  }

  const raw = readFileSync(SPEC_PATH, "utf8");
  let spec;
  try {
    spec = JSON.parse(raw);
  } catch (err) {
    console.error(`inject-code-samples: spec is not valid JSON: ${err.message}`);
    process.exit(1);
  }

  if (!spec.paths || typeof spec.paths !== "object") {
    console.error("inject-code-samples: spec has no paths object");
    process.exit(1);
  }

  let attached = 0;
  let stale = 0;

  for (const [key, samples] of Object.entries(SAMPLES)) {
    const spaceAt = key.indexOf(" ");
    const method = key.slice(0, spaceAt).toLowerCase();
    const path = key.slice(spaceAt + 1);

    const pathObj = spec.paths[path];
    if (!pathObj || !pathObj[method]) {
      console.warn(
        `inject-code-samples: STALE — no ${method.toUpperCase()} ${path} in spec (sample dropped)`,
      );
      stale++;
      continue;
    }
    pathObj[method]["x-codeSamples"] = samples;
    attached++;
  }

  writeFileSync(SPEC_PATH, JSON.stringify(spec, null, 2) + "\n");
  console.log(
    `inject-code-samples: attached samples to ${attached} endpoints (${stale} stale).`,
  );
}

main();

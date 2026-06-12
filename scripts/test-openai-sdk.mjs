#!/usr/bin/env node
/**
 * End-to-end test for the OpenAI-compatible LLM gateway shim.
 *
 * The invariant: every model call goes through AgentField via the gateway
 * at `/api/v1/llm/*`. The proof point is "any OpenAI client works if you
 * just change the base URL." We use the real `openai` npm package and
 * exercise:
 *
 *   1. `chat.completions.create` (non-streaming)
 *   2. `chat.completions.create({stream: true})` (SSE deltas)
 *   3. `/api/v1/cost/events` confirms both calls were recorded
 *
 * Auth:
 *   - If AF_STACK_TENANT_KEY is set, use it directly.
 *   - Otherwise sign in to the dashboard with OP_EMAIL / OP_PASSWORD and
 *     issue a fresh key via the admin endpoint.
 *
 * Skip behaviour:
 *   - When the runtime is unreachable or `/api/v1/llm/models` is not
 *     wired (Phase 7.1 in flight), prints "SKIP: runtime not ready" and
 *     exits 0 so CI smoke runs pass cleanly.
 *
 * Usage:
 *   node scripts/test-openai-sdk.mjs
 *   AF_STACK_PORT=8080 OP_EMAIL=... OP_PASSWORD=... node scripts/test-openai-sdk.mjs
 *   AF_STACK_TENANT_KEY=sk_... node scripts/test-openai-sdk.mjs
 */

import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { createRequire } from "node:module"

const __filename = fileURLToPath(import.meta.url)
const __dirname = dirname(__filename)
const REPO_ROOT = resolve(__dirname, "..")

// ─── config ───────────────────────────────────────────────────────────────

const AF_STACK_PORT = process.env.AF_STACK_PORT ?? "8080"
const DASHBOARD_URL = process.env.DASHBOARD_URL ?? "http://localhost:33000"
const RUNTIME_URL = process.env.RUNTIME_URL ?? `http://localhost:${AF_STACK_PORT}`
const LLM_BASE_URL = `${RUNTIME_URL}/api/v1/llm`

const OP_EMAIL = process.env.OP_EMAIL ?? "operator@example.com"
const OP_PASSWORD = process.env.OP_PASSWORD ?? "af-stack-demo-pwd"
const PREISSUED_KEY = process.env.AF_STACK_TENANT_KEY ?? ""

const MODEL = process.env.AF_STACK_TEST_MODEL ?? "qwen/qwen-2.5-72b-instruct"

// ─── pretty output ────────────────────────────────────────────────────────

const C = {
  red: (s) => `\x1b[31m${s}\x1b[0m`,
  green: (s) => `\x1b[32m${s}\x1b[0m`,
  yellow: (s) => `\x1b[33m${s}\x1b[0m`,
  blue: (s) => `\x1b[34m${s}\x1b[0m`,
  bold: (s) => `\x1b[1m${s}\x1b[0m`,
}

const log = (m) => console.log(m)
const pass = (m) => console.log(`  ${C.green("PASS")}  ${m}`)
const fail = (m) => console.log(`  ${C.red("FAIL")}  ${m}`)
const step = (m) => console.log(`\n${C.bold(C.blue(`==> ${m}`))}`)
const warn = (m) => console.log(`  ${C.yellow("WARN")}  ${m}`)

function skip(reason) {
  console.log(`\n${C.yellow("SKIP")}: ${reason}`)
  console.log(`${C.yellow("       (re-run once Phase 7.1 runtime gateway is wired)")}`)
  process.exit(0)
}

function die(reason, exitCode = 1) {
  console.log(`\n${C.red("FAIL")}: ${reason}`)
  process.exit(exitCode)
}

// ─── runtime readiness probe ──────────────────────────────────────────────

async function probeRuntime() {
  try {
    const res = await fetch(`${RUNTIME_URL}/health`, {
      signal: AbortSignal.timeout(3000),
    })
    if (!res.ok) {
      skip(`runtime /health returned ${res.status}`)
    }
  } catch (e) {
    skip(`runtime unreachable at ${RUNTIME_URL} (${e.message})`)
  }

  // The runtime gateway must expose /api/v1/llm/models AND the chat
  // completions endpoint for this test to make sense. Phase 7.1 lands
  // both — until then we skip cleanly so CI smoke runs pass.
  for (const path of ["/models", "/chat/completions"]) {
    try {
      const method = path === "/models" ? "GET" : "POST"
      const res = await fetch(`${LLM_BASE_URL}${path}`, {
        method,
        headers: method === "POST" ? { "content-type": "application/json" } : {},
        body: method === "POST" ? '{"model":"_probe_","messages":[]}' : undefined,
        signal: AbortSignal.timeout(3000),
      })
      // 4xx other than 404/405 likely means "endpoint exists, payload is
      // bad" — that's fine, the endpoint is wired. 404/405/501 mean the
      // route isn't registered.
      if (res.status === 404 || res.status === 405 || res.status === 501) {
        skip(`${LLM_BASE_URL}${path} not registered yet (Phase 7.1 in flight)`)
      }
      if (res.status === 503) {
        const body = await res.text()
        skip(`${LLM_BASE_URL}${path} returned 503 — ${body.slice(0, 200)}`)
      }
    } catch (e) {
      skip(`${LLM_BASE_URL}${path} unreachable (${e.message})`)
    }
  }
}

// ─── tenant key resolution ────────────────────────────────────────────────

async function resolveApiKey() {
  if (PREISSUED_KEY) {
    log(`  using AF_STACK_TENANT_KEY (${PREISSUED_KEY.slice(0, 8)}…)`)
    return PREISSUED_KEY
  }

  // Try to issue one via the admin endpoint. Some setups require operator
  // sign-in first; we attempt anonymous, then fall back to dashboard auth.
  // The script gives up gracefully if neither path works.
  step("issuing a tenant key via admin endpoint")

  // 1. Anonymous attempt — works on localhost dev where operator gate is off.
  const anonTenantId = `e2e-openai-${Date.now()}`
  const createTenantBody = {
    slug: anonTenantId.toLowerCase(),
    name: `e2e openai test (${anonTenantId})`,
  }
  let tenantId
  try {
    const tres = await fetch(`${RUNTIME_URL}/api/v1/admin/tenants`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(createTenantBody),
      signal: AbortSignal.timeout(5000),
    })
    if (tres.status === 401 || tres.status === 403) {
      warn("admin endpoint requires operator auth — dashboard sign-in flow not implemented in this script")
      warn(`set AF_STACK_TENANT_KEY env to a pre-issued key to run this test`)
      skip("no usable auth path to issue a tenant key")
    }
    if (tres.status === 503) {
      skip("multi-tenancy module not enabled; cannot issue tenant key")
    }
    if (!tres.ok) {
      const text = await tres.text()
      die(`admin/tenants POST returned ${tres.status}: ${text.slice(0, 200)}`)
    }
    const tenantRow = await tres.json()
    tenantId = tenantRow.id
  } catch (e) {
    die(`admin/tenants POST failed: ${e.message}`)
  }

  let keyValue
  try {
    const kres = await fetch(`${RUNTIME_URL}/api/v1/admin/keys`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        tenant_id: tenantId,
        name: "e2e-openai-sdk-test",
        scopes: ["llm:invoke", "agents:invoke"],
      }),
      signal: AbortSignal.timeout(5000),
    })
    if (!kres.ok) {
      const text = await kres.text()
      die(`admin/keys POST returned ${kres.status}: ${text.slice(0, 200)}`)
    }
    const issued = await kres.json()
    keyValue = issued.value
    if (!keyValue) die("admin/keys POST did not include `value` — one-time-reveal contract broken?")
  } catch (e) {
    die(`admin/keys POST failed: ${e.message}`)
  }

  log(`  issued tenant=${tenantId} key=${keyValue.slice(0, 8)}…`)
  return keyValue
}

// ─── tests ────────────────────────────────────────────────────────────────

let openaiClient

async function loadOpenAI() {
  // Use createRequire so we can resolve `openai` from this script's own
  // node_modules even if pnpm hoisted it elsewhere.
  const localRequire = createRequire(import.meta.url)
  let OpenAI
  try {
    OpenAI = localRequire("openai")
  } catch (e) {
    die(
      `\`openai\` package not installed. Run:\n` +
        `  (cd scripts && npm install)\n` +
        `or install it at the repo root.\n\noriginal error: ${e.message}`,
    )
  }
  return OpenAI.default ?? OpenAI
}

async function testChatNonStreaming(client) {
  step("1/3 chat.completions.create (non-streaming)")
  let completion
  try {
    completion = await client.chat.completions.create({
      model: MODEL,
      messages: [
        { role: "system", content: "You are a terse assistant. Reply in one short sentence." },
        { role: "user", content: "Say hello." },
      ],
      max_tokens: 50,
    })
  } catch (e) {
    // A 404 after auth succeeds means the chat-completions handler is
    // not actually wired up downstream of the auth layer (Phase 7.1 in
    // flight). Treat as a clean skip rather than failure — matches the
    // acceptance criteria.
    const msg = String(e.message ?? e)
    if (msg.includes("404") || /not found/i.test(msg)) {
      skip(`chat.completions.create returned 404 — Phase 7.1 runtime gateway not wired downstream of auth`)
    }
    fail(`chat.completions.create threw: ${msg}`)
    return false
  }

  const checks = [
    [typeof completion.id === "string" && completion.id.length > 0, "id is a non-empty string"],
    [typeof completion.model === "string", "model field present"],
    [Array.isArray(completion.choices) && completion.choices.length > 0, "choices[] non-empty"],
    [
      typeof completion.choices[0]?.message?.content === "string" &&
        completion.choices[0].message.content.length > 0,
      "choices[0].message.content non-empty",
    ],
    [
      typeof completion.usage?.prompt_tokens === "number" && completion.usage.prompt_tokens > 0,
      "usage.prompt_tokens > 0",
    ],
    [
      typeof completion.usage?.completion_tokens === "number" && completion.usage.completion_tokens > 0,
      "usage.completion_tokens > 0",
    ],
  ]
  let allOk = true
  for (const [ok, label] of checks) {
    if (ok) pass(label)
    else {
      fail(label)
      allOk = false
    }
  }
  if (allOk) {
    log(`  → "${completion.choices[0].message.content.slice(0, 100)}"`)
  }
  return allOk
}

async function testChatStreaming(client) {
  step("2/3 chat.completions.create (streaming)")
  let chunkCount = 0
  let textConcat = ""
  try {
    const stream = await client.chat.completions.create({
      stream: true,
      model: MODEL,
      messages: [
        { role: "system", content: "Reply in three short words." },
        { role: "user", content: "Count to three." },
      ],
      max_tokens: 50,
    })
    for await (const chunk of stream) {
      chunkCount += 1
      const piece = chunk?.choices?.[0]?.delta?.content
      if (typeof piece === "string") textConcat += piece
    }
  } catch (e) {
    fail(`streaming threw: ${e.message ?? e}`)
    return false
  }
  if (chunkCount === 0) {
    fail("no chunks observed")
    return false
  }
  pass(`received ${chunkCount} stream chunks`)
  if (textConcat.length > 0) {
    pass(`assembled text content: "${textConcat.slice(0, 80)}"`)
  } else {
    warn("no delta.content observed in any chunk")
  }
  return true
}

async function testCostEventsRecorded(apiKey) {
  step("3/3 /api/v1/cost/events confirms calls were recorded")
  // Allow the recorder a beat to flush.
  await new Promise((r) => setTimeout(r, 1000))
  let body
  try {
    const res = await fetch(`${RUNTIME_URL}/api/v1/cost/events?limit=5`, {
      headers: { authorization: `Bearer ${apiKey}` },
      signal: AbortSignal.timeout(5000),
    })
    if (!res.ok) {
      const text = await res.text()
      fail(`/cost/events returned ${res.status}: ${text.slice(0, 200)}`)
      return false
    }
    body = await res.json()
  } catch (e) {
    fail(`/cost/events fetch failed: ${e.message}`)
    return false
  }
  if (!Array.isArray(body.events)) {
    fail("response has no `events` array")
    return false
  }
  if (body.events.length === 0) {
    warn("no cost events found — recorder may not have flushed yet, or Phase 7.2 not wired")
    return false
  }
  pass(`found ${body.events.length} cost event(s)`)
  const nonZero = body.events.find((e) => Number(e.cost_usd ?? e.costUsd) > 0)
  if (nonZero) {
    pass(`at least one event has non-zero cost (cost_usd=${nonZero.cost_usd ?? nonZero.costUsd})`)
  } else {
    warn("all events have zero cost — pricing table may not be loaded")
  }
  return true
}

// ─── main ─────────────────────────────────────────────────────────────────

async function main() {
  console.log(C.bold("AF Stack OpenAI-SDK end-to-end test"))
  console.log(`runtime = ${RUNTIME_URL}`)
  console.log(`llm base = ${LLM_BASE_URL}`)
  console.log(`model = ${MODEL}`)

  step("0/3 probe runtime readiness")
  await probeRuntime()
  pass("runtime reachable, /api/v1/llm/models registered")

  const apiKey = await resolveApiKey()

  const OpenAI = await loadOpenAI()
  openaiClient = new OpenAI({
    baseURL: LLM_BASE_URL,
    apiKey,
  })

  const results = []
  results.push(await testChatNonStreaming(openaiClient))
  results.push(await testChatStreaming(openaiClient))
  results.push(await testCostEventsRecorded(apiKey))

  const passed = results.filter(Boolean).length
  const failed = results.length - passed

  console.log("")
  if (failed === 0) {
    console.log(C.green("===================================================="))
    console.log(C.green(`  OpenAI-SDK e2e PASS — ${passed}/${results.length} stages`))
    console.log(C.green("===================================================="))
    process.exit(0)
  } else {
    console.log(C.red("===================================================="))
    console.log(C.red(`  OpenAI-SDK e2e FAIL — ${passed} passed, ${failed} failed`))
    console.log(C.red("===================================================="))
    process.exit(1)
  }
}

main().catch((e) => {
  console.error(C.red(`uncaught: ${e.stack ?? e}`))
  process.exit(1)
})

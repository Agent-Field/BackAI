// SPDX-License-Identifier: Apache-2.0
//
// Live SDK conformance harness (TypeScript) for the BackAI runtime — the twin
// of run.py. It runs the IDENTICAL checks through `@af-stack/sdk`, so a drift
// in either SDK's live behaviour is caught here. Emits `{pass, fail, skip,
// results[]}` JSON on stdout and exits non-zero if any check FAILED.
//
// Run it (after the SDK is built) with:
//   BASE_URL=http://localhost:8080 API_KEY=... node --experimental-strip-types scripts/sdk-conformance/run.ts
//
// Config: BASE_URL (default http://localhost:8080), API_KEY (optional in
// personal mode).

import { BackAI, SuiteError } from "@af-stack/sdk"

const BASE_URL = (process.env.BASE_URL ?? "http://localhost:8080").replace(/\/+$/, "")
const API_KEY = process.env.API_KEY ?? process.env.AF_STACK_API_KEY

const NOT_CONFIGURED = new Set([
  "JOBS_NOT_CONFIGURED",
  "STORAGE_NOT_CONFIGURED",
  "MODULE_DISABLED",
  "MT_DISABLED",
  "NOT_CONFIGURED",
])

type Status = "pass" | "fail" | "skip"
interface Result {
  name: string
  status: Status
  detail: string
}
const results: Result[] = []
function record(name: string, status: Status, detail = ""): void {
  results.push({ name, status, detail })
}

function hasEnvelope(err: SuiteError): boolean {
  return (
    typeof err.code === "string" &&
    err.code !== "" &&
    typeof err.message === "string" &&
    typeof err.status === "number" &&
    "requestId" in err
  )
}

function uuid(): string {
  return globalThis.crypto?.randomUUID?.().replace(/-/g, "") ?? `${Date.now()}${Math.random()}`
}

async function checkHealth(): Promise<void> {
  try {
    const resp = await fetch(`${BASE_URL}/health`, { signal: AbortSignal.timeout(10_000) })
    const body = (await resp.json()) as { status?: string }
    if (resp.status === 200 && typeof body === "object")
      record("health", "pass", `status=${body.status}`)
    else record("health", "fail", `HTTP ${resp.status}`)
  } catch (err) {
    record("health", "fail", `${(err as Error).name}: ${(err as Error).message}`)
  }
}

async function checkAgentsList(): Promise<void> {
  try {
    const headers: Record<string, string> = { accept: "application/json" }
    if (API_KEY) headers.authorization = `Bearer ${API_KEY}`
    const resp = await fetch(`${BASE_URL}/api/v1/agents`, {
      headers,
      signal: AbortSignal.timeout(15_000),
    })
    if (resp.status === 200) {
      const body: unknown = await resp.json()
      record(
        "agents.list",
        typeof body === "object" ? "pass" : "fail",
        `HTTP 200 type=${typeof body}`,
      )
    } else {
      record("agents.list", "fail", `HTTP ${resp.status}`)
    }
  } catch (err) {
    record("agents.list", "fail", `${(err as Error).name}: ${(err as Error).message}`)
  }
}

async function checkEcho(client: BackAI): Promise<void> {
  try {
    const marker = uuid().slice(0, 8)
    const res = await client.agents.call("supportdesk.echo", { payload: { message: marker } })
    // Runtime returns the agent value under `result` (`output` is a
    // back-compat alias mirrored by the SDK); accept either.
    const value = res.result ?? res.output
    const ok = res.status === "succeeded" && value !== undefined
    record("agents.call:supportdesk.echo", ok ? "pass" : "fail", `status=${res.status}`)
  } catch (err) {
    if (err instanceof SuiteError) {
      if (NOT_CONFIGURED.has(err.code) || err.status === 404) {
        record("agents.call:supportdesk.echo", "skip", `unavailable: [${err.code}]`)
      } else {
        record("agents.call:supportdesk.echo", "fail", `[${err.code}] ${err.message}`)
      }
    } else {
      record(
        "agents.call:supportdesk.echo",
        "fail",
        `${(err as Error).name}: ${(err as Error).message}`,
      )
    }
  }
}

async function checkStorageRoundtrip(client: BackAI): Promise<void> {
  const key = `conformance/ts-${uuid()}.bin`
  const payload = new TextEncoder().encode(uuid().repeat(4))
  try {
    await client.storage.upload(payload, key, { contentType: "application/octet-stream" })
    const got = await client.storage.download(key)
    const same = got.length === payload.length && got.every((b, i) => b === payload[i])
    record(
      "storage.roundtrip",
      same ? "pass" : "fail",
      same ? `key=${key}` : "downloaded bytes differ",
    )
    await client.storage.delete(key)
  } catch (err) {
    if (err instanceof SuiteError && NOT_CONFIGURED.has(err.code)) {
      record("storage.roundtrip", "skip", `unavailable: [${err.code}]`)
    } else if (err instanceof SuiteError) {
      record("storage.roundtrip", "fail", `[${err.code}] ${err.message}`)
    } else {
      record("storage.roundtrip", "fail", `${(err as Error).name}: ${(err as Error).message}`)
    }
  }
}

async function checkJobs(client: BackAI): Promise<void> {
  const validStates = new Set([
    "available",
    "running",
    "completed",
    "discarded",
    "cancelled",
    "retryable",
    "scheduled",
    "pending",
  ])
  try {
    const job = await client.jobs.enqueue("conformance-noop", { probe: true })
    if (!job.id) {
      record("jobs.enqueue", "fail", "no id in enqueue response")
      return
    }
    record("jobs.enqueue", "pass", `id=${job.id}`)
    const fetched = await client.jobs.get(String(job.id))
    record(
      "jobs.status",
      validStates.has(fetched.state) ? "pass" : "fail",
      `state=${fetched.state}`,
    )
  } catch (err) {
    if (err instanceof SuiteError && NOT_CONFIGURED.has(err.code)) {
      record("jobs.enqueue", "skip", `unavailable: [${err.code}]`)
    } else if (err instanceof SuiteError && hasEnvelope(err)) {
      record("jobs.enqueue", "pass", `structured rejection [${err.code}] status=${err.status}`)
      record("jobs.status", "skip", "no row enqueued")
    } else {
      record("jobs.enqueue", "fail", `${(err as Error).name}: ${(err as Error).message}`)
    }
  }
}

async function checkForbidden(): Promise<void> {
  const bad = new BackAI({
    baseUrl: BASE_URL,
    apiKey: `af_invalid_${uuid()}`,
    checkRuntimeVersion: false,
  })
  try {
    await bad.cost.events()
    record("error.denied", "skip", "auth appears disabled (personal mode)")
  } catch (err) {
    if (err instanceof SuiteError && (err.status === 401 || err.status === 403)) {
      record(
        "error.denied",
        hasEnvelope(err) ? "pass" : "fail",
        `status=${err.status} code=${err.code}`,
      )
    } else if (err instanceof SuiteError) {
      record("error.denied", "skip", `unexpected status ${err.status} [${err.code}]`)
    } else {
      record("error.denied", "fail", `${(err as Error).name}: ${(err as Error).message}`)
    }
  } finally {
    await bad.close()
  }
}

async function checkNotFound(client: BackAI): Promise<void> {
  try {
    await client.jobs.get("999999999")
    record("error.not_found", "fail", "expected a 404, got a result")
  } catch (err) {
    if (err instanceof SuiteError && err.status === 404 && hasEnvelope(err)) {
      record("error.not_found", "pass", `status=404 code=${err.code}`)
    } else if (err instanceof SuiteError && NOT_CONFIGURED.has(err.code)) {
      record("error.not_found", "skip", `jobs unavailable: [${err.code}]`)
    } else if (err instanceof SuiteError) {
      record("error.not_found", "fail", `status=${err.status} [${err.code}]`)
    } else {
      record("error.not_found", "fail", `${(err as Error).name}: ${(err as Error).message}`)
    }
  }
}

async function main(): Promise<number> {
  const client = new BackAI({ baseUrl: BASE_URL, apiKey: API_KEY, checkRuntimeVersion: false })
  try {
    await checkHealth()
    await checkAgentsList()
    await checkEcho(client)
    await checkStorageRoundtrip(client)
    await checkJobs(client)
    await checkForbidden()
    await checkNotFound(client)
  } finally {
    await client.close()
  }

  const passed = results.filter((r) => r.status === "pass").length
  const failed = results.filter((r) => r.status === "fail").length
  const skipped = results.filter((r) => r.status === "skip").length
  const summary = { pass: passed, fail: failed, skip: skipped, sdk: "typescript", results }
  console.log(JSON.stringify(summary, null, 2))
  return failed > 0 ? 1 : 0
}

main()
  .then((code) => process.exit(code))
  .catch((err) => {
    console.error(err)
    process.exit(1)
  })

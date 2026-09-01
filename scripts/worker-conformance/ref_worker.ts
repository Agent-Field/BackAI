// SPDX-License-Identifier: Apache-2.0
//
// Reference pull-worker (TypeScript) for the worker-conformance suite — the
// twin of ref_worker.py. Registers a handler for every vector `kind` in
// spec.json (keyed by `behavior`) and runs the @af-stack/sdk Worker lease
// loop. run.sh starts this, enqueues the vectors, and asserts terminal states.
//
// Run (after the SDK is built):
//   BASE_URL=... API_KEY=... node --experimental-strip-types scripts/worker-conformance/ref_worker.ts
//
// Env: BASE_URL, API_KEY (jobs:work-scoped tenant key), SPEC (path to spec.json).

import { readFileSync } from "node:fs"
import { fileURLToPath } from "node:url"
import { dirname, join } from "node:path"
import { JobContext, PermanentError, Worker, type Handler } from "@af-stack/sdk/server"

const here = dirname(fileURLToPath(import.meta.url))
const BASE_URL = (process.env.BASE_URL ?? "http://localhost:8080").replace(/\/+$/, "")
const API_KEY = process.env.API_KEY ?? process.env.AF_STACK_API_KEY ?? ""
const SPEC = process.env.SPEC ?? join(here, "spec.json")

interface Vector {
  kind: string
  behavior: string
  payload: Record<string, unknown>
  max_attempts: number
  expect_state: string
}
interface Spec {
  worker?: { lease_ttl?: number; heartbeat_interval?: number; poll_wait?: number }
  vectors: Vector[]
}

function stableStringify(v: unknown): string {
  if (v === null || typeof v !== "object") return JSON.stringify(v)
  if (Array.isArray(v)) return `[${v.map(stableStringify).join(",")}]`
  const obj = v as Record<string, unknown>
  const keys = Object.keys(obj).sort()
  return `{${keys.map((k) => `${JSON.stringify(k)}:${stableStringify(obj[k])}`).join(",")}}`
}

function deepEqual(a: unknown, b: unknown): boolean {
  return stableStringify(a) === stableStringify(b)
}

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms))
}

function makeHandler(behavior: string, expected: Record<string, unknown>): Handler {
  switch (behavior) {
    case "complete":
      return async (_payload, ctx: JobContext) => {
        await ctx.log("conf: complete")
        return { ok: true }
      }
    case "retry_then_complete":
      return (_payload, ctx: JobContext) => {
        if (ctx.attempt < 2) throw new Error(`retryable failure on attempt ${ctx.attempt}`)
        return { ok: true, attempt: ctx.attempt }
      }
    case "permanent":
      return () => {
        throw new PermanentError("permanent failure — do not retry")
      }
    case "roundtrip":
      return (payload) => {
        if (!deepEqual(payload, expected)) {
          throw new PermanentError(
            `payload roundtrip mismatch: got ${JSON.stringify(payload)} want ${JSON.stringify(expected)}`,
          )
        }
        return { ok: true, echoed: payload }
      }
    case "slow_complete":
      return async (payload, ctx: JobContext) => {
        const sleepMs = Number(payload.sleep_ms ?? 0)
        const deadline = Date.now() + sleepMs
        while (Date.now() < deadline) {
          if (ctx.isCanceled()) return { ok: false, canceled: true }
          await sleep(100)
        }
        return { ok: true, slept_ms: sleepMs }
      }
    default:
      throw new Error(`unknown behavior ${behavior}`)
  }
}

async function main(): Promise<number> {
  if (!API_KEY) {
    console.error("ref_worker.ts: API_KEY is required")
    return 2
  }
  const spec = JSON.parse(readFileSync(SPEC, "utf-8")) as Spec
  const wcfg = spec.worker ?? {}
  const worker = new Worker(BASE_URL, API_KEY, {
    leaseTtl: wcfg.lease_ttl ?? 6,
    heartbeatInterval: wcfg.heartbeat_interval ?? 2,
    pollWait: wcfg.poll_wait ?? 5,
    workerId: "conf-ref-ts",
  })
  for (const vec of spec.vectors) {
    worker.register(vec.kind, makeHandler(vec.behavior, vec.payload ?? {}))
  }
  console.error(
    `ref_worker.ts: leasing kinds ${JSON.stringify(worker.kinds())} against ${BASE_URL}`,
  )
  await worker.run() // blocks; drains on SIGTERM/SIGINT
  return 0
}

main()
  .then((code) => process.exit(code))
  .catch((err) => {
    console.error(err)
    process.exit(1)
  })

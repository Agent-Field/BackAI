// SPDX-License-Identifier: Apache-2.0

// Fetch-mocked tests for the pull-based Worker (mirror of
// af_stack/tests/test_worker.py). No network + no runtime needed. Heartbeats
// use a huge interval so the background timer never fires during a fast
// handler.

import { describe, expect, it, vi } from "vitest"
import { JobContext, PermanentError, Worker } from "../src/server.js"

const BASE = "http://localhost:8080"
const WPREFIX = "/api/v1/jobs/worker"

interface RecordedCall {
  path: string
  body: Record<string, unknown>
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  })
}

function makeFetch(routes: Record<string, (body: Record<string, unknown>) => Response>): {
  fn: typeof fetch
  calls: RecordedCall[]
} {
  const calls: RecordedCall[] = []
  const fn = vi.fn(async (url: string | URL | Request, init?: RequestInit) => {
    const body = init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : {}
    const path = new URL(String(url)).pathname
    calls.push({ path, body })
    const handler = routes[path]
    if (handler === undefined) return jsonResponse({})
    return handler(body)
  })
  return { fn: fn as unknown as typeof fetch, calls }
}

function makeWorker(fetchFn: typeof fetch, extra: Record<string, unknown> = {}): Worker {
  return new Worker(BASE, "test-key", {
    fetch: fetchFn,
    heartbeatInterval: 3600,
    pollWait: 0,
    ...extra,
  })
}

function attempt(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    job_id: "7",
    attempt: 1,
    kind: "resize",
    payload: { x: 1 },
    tenant_id: "t_abc",
    deadline: null,
    lease_expires_at: null,
    ...overrides,
  }
}

function lastCall(calls: RecordedCall[], path: string): RecordedCall {
  const match = [...calls].reverse().find((c) => c.path === `${WPREFIX}${path}`)
  if (match === undefined)
    throw new Error(`no call to ${path}; got ${calls.map((c) => c.path).join(", ")}`)
  return match
}

describe("Worker construction", () => {
  it("requires baseUrl and apiKey", () => {
    expect(() => new Worker("", "k", { fetch: makeFetch({}).fn })).toThrow()
    expect(() => new Worker(BASE, "", { fetch: makeFetch({}).fn })).toThrow()
  })

  it("run() requires a registered handler", async () => {
    const { fn } = makeFetch({})
    await expect(makeWorker(fn).run({ installSignalHandlers: false })).rejects.toThrow(
      /no handlers/i,
    )
  })
})

describe("Worker.leaseOnce", () => {
  it("sends kinds + worker_id + lease_ttl_seconds", async () => {
    const { fn, calls } = makeFetch({
      [`${WPREFIX}/lease`]: () => jsonResponse({ job: attempt() }),
    })
    const w = makeWorker(fn)
    w.register("resize", () => ({}))
    const got = await w.leaseOnce()
    expect(got).not.toBeNull()
    expect(got?.job_id).toBe("7")
    const body = lastCall(calls, "/lease").body
    expect(body.kinds).toEqual(["resize"])
    expect(body.worker_id).toBe(w.workerId)
    expect(body.lease_ttl_seconds).toBe(w.leaseTtl)
  })

  it("returns null when no attempt is available", async () => {
    const { fn } = makeFetch({ [`${WPREFIX}/lease`]: () => jsonResponse({ job: null }) })
    const w = makeWorker(fn)
    w.register("resize", () => ({}))
    expect(await w.leaseOnce()).toBeNull()
  })
})

describe("Worker.process", () => {
  it("completes with the handler result and roundtrips the payload", async () => {
    const { fn, calls } = makeFetch({ [`${WPREFIX}/complete`]: () => jsonResponse({ ok: true }) })
    const w = makeWorker(fn)
    const seen: Record<string, unknown> = {}
    w.register("resize", (payload, ctx: JobContext) => {
      seen.payload = payload
      seen.tenant = ctx.tenantId
      seen.jobId = ctx.jobId
      return { r: (payload.x as number) + 1 }
    })
    await w.process(attempt() as never)
    expect(seen.payload).toEqual({ x: 1 })
    expect(seen.tenant).toBe("t_abc")
    const body = lastCall(calls, "/complete").body
    expect(body.result).toEqual({ r: 2 })
    expect(body.worker_id).toBe(w.workerId)
    expect(body.job_id).toBe("7")
  })

  it("fails retryably on a thrown error", async () => {
    const { fn, calls } = makeFetch({ [`${WPREFIX}/fail`]: () => jsonResponse({ ok: true }) })
    const w = makeWorker(fn)
    w.register("resize", () => {
      throw new Error("boom")
    })
    await w.process(attempt() as never)
    const body = lastCall(calls, "/fail").body
    expect(body.retryable).toBe(true)
    expect(body.error).toBe("boom")
  })

  it("fails permanently on PermanentError (dead-letter)", async () => {
    const { fn, calls } = makeFetch({ [`${WPREFIX}/fail`]: () => jsonResponse({ ok: true }) })
    const w = makeWorker(fn)
    w.register("resize", () => {
      throw new PermanentError("nope")
    })
    await w.process(attempt() as never)
    const body = lastCall(calls, "/fail").body
    expect(body.retryable).toBe(false)
    expect(body.error).toBe("nope")
  })

  it("skips reporting when the job was cancelled mid-handler", async () => {
    const { fn, calls } = makeFetch({
      [`${WPREFIX}/heartbeat`]: () => jsonResponse({ canceled: true }),
    })
    const w = makeWorker(fn)
    w.register("resize", async (_payload, ctx: JobContext) => {
      await w.sendHeartbeat(ctx)
      expect(ctx.isCanceled()).toBe(true)
      return { ignored: true }
    })
    await w.process(attempt() as never)
    // No /complete and no /fail should have been sent.
    expect(calls.some((c) => c.path.endsWith("/complete"))).toBe(false)
    expect(calls.some((c) => c.path.endsWith("/fail"))).toBe(false)
  })

  it("fails retryably when leased a kind with no handler", async () => {
    const { fn, calls } = makeFetch({ [`${WPREFIX}/fail`]: () => jsonResponse({ ok: true }) })
    const w = makeWorker(fn)
    w.register("resize", () => ({}))
    await w.process(attempt({ kind: "unknown" }) as never)
    const body = lastCall(calls, "/fail").body
    expect(body.retryable).toBe(true)
  })
})

describe("Worker heartbeat + logs", () => {
  it("surfaces cancellation from a heartbeat", async () => {
    const { fn } = makeFetch({
      [`${WPREFIX}/heartbeat`]: () => jsonResponse({ canceled: true, lease_expires_at: null }),
    })
    const w = makeWorker(fn)
    const ctx = new JobContext({ worker: w, tenantId: "t", jobId: "7", attempt: 1, deadline: null })
    expect(await w.sendHeartbeat(ctx)).toBe(true)
    expect(ctx.isCanceled()).toBe(true)
  })

  it("ctx.log posts a structured line", async () => {
    const { fn, calls } = makeFetch({ [`${WPREFIX}/logs`]: () => jsonResponse({ accepted: 1 }) })
    const w = makeWorker(fn)
    const ctx = new JobContext({ worker: w, tenantId: "t", jobId: "7", attempt: 2, deadline: null })
    await ctx.log("resizing", { level: "warn", fields: { url: "http://x" } })
    const body = lastCall(calls, "/logs").body
    expect(body.job_id).toBe("7")
    expect(body.attempt).toBe(2)
    const line = (body.lines as Array<Record<string, unknown>>)[0]
    expect(line.message).toBe("resizing")
    expect(line.level).toBe("warn")
    expect(line.fields).toEqual({ url: "http://x" })
  })
})

describe("Worker.run", () => {
  it("drains after a handler requests stop", async () => {
    let leaseCount = 0
    const { fn } = makeFetch({
      [`${WPREFIX}/lease`]: () => {
        leaseCount += 1
        return leaseCount === 1 ? jsonResponse({ job: attempt() }) : jsonResponse({ job: null })
      },
      [`${WPREFIX}/complete`]: () => jsonResponse({ ok: true }),
    })
    const w = makeWorker(fn)
    const ran: number[] = []
    w.register("resize", () => {
      ran.push(1)
      w.stop() // graceful drain after this job
      return {}
    })
    await w.run({ installSignalHandlers: false })
    expect(ran).toEqual([1])
  })
})

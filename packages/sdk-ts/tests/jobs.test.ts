import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { jobs, suite, SuiteError } from "../src/index.js"

// ---------- fetch mock plumbing (same shape as agents.test.ts) ----------

let fetchMock: ReturnType<typeof vi.fn>
let responseQueue: Response[]

function jsonResponse(body: unknown, init: ResponseInit = {}): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "content-type": "application/json" },
    ...init,
  })
}

function errorResponse(body: unknown, status: number): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  })
}

function enqueueResponse(response: Response): void {
  responseQueue.push(response)
}

interface MockCall {
  url: string
  init: RequestInit
}

function nthCall(idx: number): MockCall {
  const args = fetchMock.mock.calls[idx]
  if (args === undefined) {
    throw new Error(`fetch was not called ${idx + 1} time(s); recorded ${fetchMock.mock.calls.length}`)
  }
  return { url: String(args[0]), init: (args[1] as RequestInit | undefined) ?? {} }
}

beforeEach(() => {
  responseQueue = []
  fetchMock = vi.fn(async () => {
    const next = responseQueue.shift()
    if (next === undefined) return jsonResponse({})
    return next
  })
  ;(globalThis as { fetch: typeof fetch }).fetch = fetchMock as unknown as typeof fetch
  process.env.AF_STACK_URL = "http://test.local"
  process.env.AF_STACK_API_KEY = "test-key"
})

afterEach(() => {
  vi.restoreAllMocks()
  delete process.env.AF_STACK_URL
  delete process.env.AF_STACK_API_KEY
})

function jobRow(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "job_123",
    name: "send-welcome-email",
    args: { to: "user@example.com" },
    state: "available",
    tenant_id: "t_xyz",
    attempt: 0,
    max_attempts: 5,
    scheduled_at: "2026-06-06T12:00:00Z",
    enqueued_at: "2026-06-06T12:00:00Z",
    attempted_at: null,
    finalized_at: null,
    errors: null,
    ...overrides,
  }
}

// ---------- tests ----------

describe("jobs.enqueue", () => {
  it("POSTs to /jobs and parses the persisted row to camelCase", async () => {
    enqueueResponse(jsonResponse(jobRow()))
    const job = await jobs.enqueue("send-welcome-email", { to: "user@example.com" })

    expect(job.id).toBe("job_123")
    expect(job.name).toBe("send-welcome-email")
    expect(job.state).toBe("available")
    expect(job.tenantId).toBe("t_xyz")
    expect(job.maxAttempts).toBe(5)
    expect(job.scheduledAt).toBe("2026-06-06T12:00:00Z")
    expect(job.args).toEqual({ to: "user@example.com" })

    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/jobs")
    expect(c.init.method).toBe("POST")
    const body = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(body.name).toBe("send-welcome-email")
    expect(body.args).toEqual({ to: "user@example.com" })
  })

  it("serialises Date for scheduledAt and forwards maxAttempts as snake_case", async () => {
    enqueueResponse(jsonResponse(jobRow({ state: "scheduled" })))
    const when = new Date("2026-06-06T12:00:00.000Z")
    await jobs.enqueue("nightly-report", { day: "2026-06-06" }, {
      scheduledAt: when,
      maxAttempts: 3,
    })
    const body = JSON.parse(nthCall(0).init.body as string) as Record<string, unknown>
    expect(body.scheduled_at).toBe("2026-06-06T12:00:00.000Z")
    expect(body.max_attempts).toBe(3)
  })

  it("accepts a pre-serialised ISO string for scheduledAt", async () => {
    enqueueResponse(jsonResponse(jobRow({ state: "scheduled" })))
    await jobs.enqueue("nightly-report", {}, {
      scheduledAt: "2026-06-06T12:00:00Z",
    })
    const body = JSON.parse(nthCall(0).init.body as string) as Record<string, unknown>
    expect(body.scheduled_at).toBe("2026-06-06T12:00:00Z")
  })

  it("throws on empty name", async () => {
    await expect(jobs.enqueue("", {})).rejects.toThrow(/non-empty/i)
  })

  it("throws on out-of-range maxAttempts", async () => {
    await expect(jobs.enqueue("x", {}, { maxAttempts: 0 })).rejects.toThrow(
      /between 1 and 50/i,
    )
    await expect(jobs.enqueue("x", {}, { maxAttempts: 999 })).rejects.toThrow(
      /between 1 and 50/i,
    )
  })

  it("propagates structured errors as SuiteError", async () => {
    enqueueResponse(
      errorResponse(
        {
          error: {
            code: "JOB_NOT_REGISTERED",
            message: "no such job",
            request_id: "req_x",
          },
        },
        404,
      ),
    )
    const err = await jobs.enqueue("unknown-job", {}).catch((e) => e as unknown)
    expect(err).toBeInstanceOf(SuiteError)
    expect((err as SuiteError).code).toBe("JOB_NOT_REGISTERED")
    expect((err as SuiteError).status).toBe(404)
  })
})

describe("jobs.get", () => {
  it("GETs /jobs/{id}", async () => {
    enqueueResponse(jsonResponse(jobRow({ state: "running", attempt: 1 })))
    const job = await jobs.get("job_123")
    expect(job.state).toBe("running")
    expect(job.attempt).toBe(1)
    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/jobs/job_123")
    expect(c.init.method).toBe("GET")
  })

  it("throws on empty id", async () => {
    await expect(jobs.get("")).rejects.toThrow(/non-empty/i)
  })
})

describe("jobs.retry", () => {
  it("POSTs to /jobs/{id}/retry", async () => {
    enqueueResponse(jsonResponse(jobRow({ state: "available", attempt: 2 })))
    const job = await jobs.retry("job_123")
    expect(job.state).toBe("available")
    expect(job.attempt).toBe(2)
    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/jobs/job_123/retry")
    expect(c.init.method).toBe("POST")
  })
})

describe("jobs.list", () => {
  it("forwards filters as query params and parses paginated response", async () => {
    enqueueResponse(
      jsonResponse({
        jobs: [jobRow({ id: "job_1" }), jobRow({ id: "job_2" })],
        total: 42,
        has_more: true,
      }),
    )
    const result = await jobs.list({
      name: "send-welcome-email",
      state: "running",
      tenant: "t_xyz",
      limit: 10,
      offset: 0,
    })

    expect(result.total).toBe(42)
    expect(result.hasMore).toBe(true)
    expect(result.jobs.map((j) => j.id)).toEqual(["job_1", "job_2"])

    const c = nthCall(0)
    const url = new URL(c.url)
    expect(url.pathname).toBe("/api/v1/jobs")
    expect(url.searchParams.get("name")).toBe("send-welcome-email")
    expect(url.searchParams.get("state")).toBe("running")
    expect(url.searchParams.get("tenant")).toBe("t_xyz")
    expect(url.searchParams.get("limit")).toBe("10")
    expect(url.searchParams.get("offset")).toBe("0")
  })

  it("omits unset filters", async () => {
    enqueueResponse(
      jsonResponse({ jobs: [], total: 0, has_more: false }),
    )
    await jobs.list()
    const url = new URL(nthCall(0).url)
    expect(url.searchParams.has("name")).toBe(false)
    expect(url.searchParams.has("state")).toBe(false)
    expect(url.searchParams.has("tenant")).toBe(false)
    expect(url.searchParams.get("limit")).toBe("50")
    expect(url.searchParams.get("offset")).toBe("0")
  })
})

describe("namespace export", () => {
  it("suite.jobs.* matches the module helpers", () => {
    expect(suite.jobs).toBe(jobs)
    expect(suite.jobs.enqueue).toBe(jobs.enqueue)
    expect(suite.jobs.get).toBe(jobs.get)
    expect(suite.jobs.retry).toBe(jobs.retry)
    expect(suite.jobs.list).toBe(jobs.list)
  })
})

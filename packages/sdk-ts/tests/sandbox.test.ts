// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  sandbox,
  runSandbox,
  listSandboxRuns,
  getSandboxRun,
  stopSandboxRun,
  sandboxPool,
  suite,
  SuiteError,
} from "../src/index.js"

// ---------- fetch mock plumbing (shape matches jobs.test.ts) ----------

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
    throw new Error(
      `fetch was not called ${idx + 1} time(s); recorded ${fetchMock.mock.calls.length}`,
    )
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

function runRow(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "sbx_123",
    tenant_id: "t_xyz",
    workspace_id: null,
    adapter: "docker",
    image: "python:3.12-slim",
    command: ["python", "-c", "print('hi')"],
    status: "done",
    exit_code: 0,
    duration_s: 1.42,
    cpu_seconds: 0.61,
    memory_peak_mb: 32.5,
    network_bytes_in: 0,
    network_bytes_out: 0,
    cost_usd: 0.0003,
    stdout_url: "https://example.com/stdout",
    stderr_url: "https://example.com/stderr",
    artifacts_url: null,
    started_at: "2026-06-06T12:00:00Z",
    ended_at: "2026-06-06T12:00:01Z",
    created_at: "2026-06-06T12:00:00Z",
    ...overrides,
  }
}

function capabilities(): Record<string, unknown> {
  return {
    max_timeout_s: 86400,
    supports_gpu: false,
    supports_network: true,
    supports_mounts: true,
    cold_start_ms: 250,
    adapter: "docker",
  }
}

// ---------- tests ----------

describe("sandbox.run", () => {
  it("POSTs to /sandbox/run with defaults and parses the run row to camelCase", async () => {
    enqueueResponse(jsonResponse(runRow({ status: "queued", exit_code: null })))
    const result = await sandbox.run("python:3.12-slim", ["python", "-c", "print(1)"])

    expect(result.id).toBe("sbx_123")
    expect(result.status).toBe("queued")
    expect(result.adapter).toBe("docker")
    expect(result.tenantId).toBe("t_xyz")
    expect(result.workspaceId).toBeNull()
    expect(result.command).toEqual(["python", "-c", "print('hi')"])
    expect(result.startedAt).toBe("2026-06-06T12:00:00Z")
    expect(result.createdAt).toBe("2026-06-06T12:00:00Z")

    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/sandbox/run")
    expect(c.init.method).toBe("POST")
    const body = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(body.image).toBe("python:3.12-slim")
    expect(body.command).toEqual(["python", "-c", "print(1)"])
    // Wire defaults from SandboxRunInputSchema.
    expect(body.timeout_s).toBe(300)
    expect(body.cpu).toBe(2)
    expect(body.memory_gb).toBe(4)
    expect(body.network).toBe("open")
    // Optional fields must NOT appear when caller didn't supply them.
    expect("files" in body).toBe(false)
    expect("env" in body).toBe(false)
    expect("allow_egress" in body).toBe(false)
    expect("workspace_id" in body).toBe(false)
  })

  it("overrides resource caps and network policy", async () => {
    enqueueResponse(jsonResponse(runRow()))
    await sandbox.run("python:3.12-slim", ["python", "-c", "1"], {
      timeoutS: 60,
      cpu: 8,
      memoryGb: 16,
      network: "isolated",
      allowEgress: ["api.example.com"],
      workspaceId: "ws_abc",
    })

    const body = JSON.parse(nthCall(0).init.body as string) as Record<string, unknown>
    expect(body.timeout_s).toBe(60)
    expect(body.cpu).toBe(8)
    expect(body.memory_gb).toBe(16)
    expect(body.network).toBe("isolated")
    expect(body.allow_egress).toEqual(["api.example.com"])
    expect(body.workspace_id).toBe("ws_abc")
  })

  it("forwards files and env as plain objects", async () => {
    enqueueResponse(jsonResponse(runRow()))
    await sandbox.run("python:3.12-slim", ["python", "main.py"], {
      files: { "main.py": "print('hi')", "data.txt": "hello" },
      env: { PYTHONUNBUFFERED: "1", MODE: "test" },
    })

    const body = JSON.parse(nthCall(0).init.body as string) as Record<string, unknown>
    expect(body.files).toEqual({ "main.py": "print('hi')", "data.txt": "hello" })
    expect(body.env).toEqual({ PYTHONUNBUFFERED: "1", MODE: "test" })
  })

  it("validates inputs", async () => {
    await expect(sandbox.run("", ["python"])).rejects.toThrow(/non-empty/i)
    await expect(sandbox.run("python:3.12-slim", [])).rejects.toThrow(/non-empty/i)
    await expect(
      sandbox.run("python:3.12-slim", ["python"], { timeoutS: 0 }),
    ).rejects.toThrow(/timeoutS/i)
    await expect(
      sandbox.run("python:3.12-slim", ["python"], { timeoutS: 999_999 }),
    ).rejects.toThrow(/timeoutS/i)
    await expect(
      sandbox.run("python:3.12-slim", ["python"], { cpu: 0 }),
    ).rejects.toThrow(/cpu/i)
    await expect(
      sandbox.run("python:3.12-slim", ["python"], { cpu: 999 }),
    ).rejects.toThrow(/cpu/i)
    await expect(
      sandbox.run("python:3.12-slim", ["python"], { memoryGb: 0 }),
    ).rejects.toThrow(/memoryGb/i)
    await expect(
      sandbox.run("python:3.12-slim", ["python"], { memoryGb: 999 }),
    ).rejects.toThrow(/memoryGb/i)
    await expect(
      sandbox.run("python:3.12-slim", ["python"], { network: "bogus" }),
    ).rejects.toThrow(/network/i)
  })

  it("propagates structured errors as SuiteError", async () => {
    enqueueResponse(
      errorResponse(
        {
          error: {
            code: "SANDBOX_QUOTA_EXCEEDED",
            message: "tenant out of sandbox quota",
            request_id: "req_x",
          },
        },
        429,
      ),
    )
    const err = await sandbox.run("python:3.12-slim", ["python", "-c", "1"]).catch(
      (e) => e as unknown,
    )
    expect(err).toBeInstanceOf(SuiteError)
    expect((err as SuiteError).code).toBe("SANDBOX_QUOTA_EXCEEDED")
    expect((err as SuiteError).status).toBe(429)
  })
})

describe("sandbox.list", () => {
  it("forwards filters as query params and parses paginated response", async () => {
    enqueueResponse(
      jsonResponse({
        runs: [runRow({ id: "sbx_1" }), runRow({ id: "sbx_2" })],
        total: 17,
        has_more: true,
      }),
    )
    const result = await sandbox.list({
      tenant: "t_xyz",
      status: "running",
      limit: 10,
      offset: 0,
    })

    expect(result.total).toBe(17)
    expect(result.hasMore).toBe(true)
    expect(result.runs.map((r) => r.id)).toEqual(["sbx_1", "sbx_2"])

    const url = new URL(nthCall(0).url)
    expect(url.pathname).toBe("/api/v1/sandbox/runs")
    expect(url.searchParams.get("tenant")).toBe("t_xyz")
    expect(url.searchParams.get("status")).toBe("running")
    expect(url.searchParams.get("limit")).toBe("10")
    expect(url.searchParams.get("offset")).toBe("0")
  })

  it("omits unset filters and handles empty list", async () => {
    enqueueResponse(jsonResponse({ runs: [], total: 0, has_more: false }))
    const result = await sandbox.list()
    expect(result.runs).toEqual([])
    expect(result.total).toBe(0)
    expect(result.hasMore).toBe(false)

    const url = new URL(nthCall(0).url)
    expect(url.searchParams.has("tenant")).toBe(false)
    expect(url.searchParams.has("status")).toBe(false)
    expect(url.searchParams.get("limit")).toBe("50")
    expect(url.searchParams.get("offset")).toBe("0")
  })
})

describe("sandbox.get", () => {
  it("GETs /sandbox/runs/{id} and parses the row", async () => {
    enqueueResponse(jsonResponse(runRow({ status: "failed", exit_code: 137 })))
    const result = await sandbox.get("sbx_123")
    expect(result.status).toBe("failed")
    expect(result.exitCode).toBe(137)

    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/sandbox/runs/sbx_123")
    expect(c.init.method).toBe("GET")
  })

  it("throws on empty id", async () => {
    await expect(sandbox.get("")).rejects.toThrow(/non-empty/i)
  })
})

describe("sandbox.stop", () => {
  it("DELETEs /sandbox/runs/{id} and returns true", async () => {
    enqueueResponse(jsonResponse({ stopped: true }))
    const ok = await sandbox.stop("sbx_123")
    expect(ok).toBe(true)

    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/sandbox/runs/sbx_123")
    expect(c.init.method).toBe("DELETE")
  })

  it("throws on empty id", async () => {
    await expect(sandbox.stop("")).rejects.toThrow(/non-empty/i)
  })
})

describe("sandbox.pool", () => {
  it("GETs /sandbox/pool and parses stats with capabilities", async () => {
    enqueueResponse(
      jsonResponse({
        adapter: "docker",
        warm: 3,
        active: 1,
        queued: 0,
        total_runs_today: 42,
        cpu_seconds_today: 318.4,
        cost_usd_today: 0.51,
        capabilities: capabilities(),
      }),
    )
    const stats = await sandbox.pool()
    expect(stats.adapter).toBe("docker")
    expect(stats.warm).toBe(3)
    expect(stats.active).toBe(1)
    expect(stats.totalRunsToday).toBe(42)
    expect(stats.cpuSecondsToday).toBeCloseTo(318.4)
    expect(stats.costUsdToday).toBeCloseTo(0.51)
    expect(stats.capabilities.maxTimeoutS).toBe(86400)
    expect(stats.capabilities.supportsGpu).toBe(false)
    expect(stats.capabilities.adapter).toBe("docker")

    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/sandbox/pool")
    expect(c.init.method).toBe("GET")
  })
})

describe("namespace export", () => {
  it("suite.sandbox.* matches the module helpers", () => {
    expect(suite.sandbox).toBe(sandbox)
    expect(suite.sandbox.run).toBe(sandbox.run)
    expect(suite.sandbox.list).toBe(sandbox.list)
    expect(suite.sandbox.get).toBe(sandbox.get)
    expect(suite.sandbox.stop).toBe(sandbox.stop)
    expect(suite.sandbox.pool).toBe(sandbox.pool)
  })

  it("named re-exports are wired to the module helpers", () => {
    expect(runSandbox).toBe(sandbox.run)
    expect(listSandboxRuns).toBe(sandbox.list)
    expect(getSandboxRun).toBe(sandbox.get)
    expect(stopSandboxRun).toBe(sandbox.stop)
    expect(sandboxPool).toBe(sandbox.pool)
  })
})
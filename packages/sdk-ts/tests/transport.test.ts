// SPDX-License-Identifier: Apache-2.0

// Transport parity tests: retry-on-transient with Retry-After, the
// mutation-safety rule (never auto-retry a mutation unless an idempotencyKey
// is supplied), the Idempotency-Key header, timeout wiring, and the
// runtime-version compatibility policy. Mirrors the Python transport rules.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  checkRuntimeCompat,
  request,
  retryDelayMs,
  SuiteError,
  SUPPORTED_RUNTIME_MAJOR,
} from "../src/_http.js"

let fetchMock: ReturnType<typeof vi.fn>
let queue: Response[]
let inits: RequestInit[]

function jsonResponse(body: unknown, init: ResponseInit = {}): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "content-type": "application/json" },
    ...init,
  })
}

function errorResponse(status: number, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify({ error: { code: "TRANSIENT", message: "try later" } }), {
    status,
    headers: { "content-type": "application/json", ...headers },
  })
}

beforeEach(() => {
  queue = []
  inits = []
  fetchMock = vi.fn(async (_url: string, init: RequestInit) => {
    inits.push(init)
    const next = queue.shift()
    return next ?? jsonResponse({})
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

function headerOf(init: RequestInit, name: string): string | undefined {
  return (init.headers as Record<string, string> | undefined)?.[name]
}

describe("transport retries", () => {
  it("retries an idempotent GET on 429 honouring Retry-After", async () => {
    queue = [errorResponse(429, { "retry-after": "0" }), jsonResponse({ ok: true })]
    const body = await request<{ ok: boolean }>("GET", "/x", null, { maxRetries: 2 })
    expect(body.ok).toBe(true)
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it("retries a GET on 5xx and eventually surfaces the error", async () => {
    queue = [
      errorResponse(503, { "retry-after": "0" }),
      errorResponse(503, { "retry-after": "0" }),
      errorResponse(503, { "retry-after": "0" }),
    ]
    const err = await request("GET", "/x", null, { maxRetries: 2 }).catch((e) => e as unknown)
    expect(err).toBeInstanceOf(SuiteError)
    expect((err as SuiteError).status).toBe(503)
    expect(fetchMock).toHaveBeenCalledTimes(3) // initial + 2 retries
  })

  it("NEVER auto-retries a mutation without an idempotencyKey", async () => {
    queue = [errorResponse(429, { "retry-after": "0" }), jsonResponse({ ok: true })]
    const err = await request("POST", "/x", { a: 1 }, { maxRetries: 2 }).catch((e) => e as unknown)
    expect(err).toBeInstanceOf(SuiteError)
    expect((err as SuiteError).status).toBe(429)
    expect(fetchMock).toHaveBeenCalledTimes(1) // no retry
  })

  it("retries a mutation WHEN an idempotencyKey is supplied, sending the header", async () => {
    queue = [errorResponse(429, { "retry-after": "0" }), jsonResponse({ ok: true })]
    const body = await request<{ ok: boolean }>(
      "POST",
      "/x",
      { a: 1 },
      {
        maxRetries: 2,
        idempotencyKey: "key-123",
      },
    )
    expect(body.ok).toBe(true)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(headerOf(inits[0], "idempotency-key")).toBe("key-123")
    expect(headerOf(inits[1], "idempotency-key")).toBe("key-123")
  })

  it("does not retry at all when maxRetries is 0 (the singleton default)", async () => {
    queue = [errorResponse(503, { "retry-after": "0" }), jsonResponse({ ok: true })]
    const err = await request("GET", "/x", null, {}).catch((e) => e as unknown)
    expect(err).toBeInstanceOf(SuiteError)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it("wires a timeout into an AbortSignal", async () => {
    queue = [jsonResponse({ ok: true })]
    await request("GET", "/x", null, { timeout: 5000 })
    expect(inits[0].signal).toBeInstanceOf(AbortSignal)
  })
})

describe("retryDelayMs", () => {
  it("honours numeric Retry-After seconds", () => {
    const r = errorResponse(429, { "retry-after": "2" })
    expect(retryDelayMs(r, 0)).toBe(2000)
  })

  it("caps Retry-After at 20s", () => {
    const r = errorResponse(429, { "retry-after": "999" })
    expect(retryDelayMs(r, 0)).toBe(20_000)
  })

  it("honours an HTTP-date Retry-After", () => {
    const when = new Date(Date.now() + 3000).toUTCString()
    const r = errorResponse(429, { "retry-after": when })
    const d = retryDelayMs(r, 0)
    expect(d).toBeGreaterThan(1000)
    expect(d).toBeLessThanOrEqual(20_000)
  })

  it("falls back to jittered exponential backoff within bounds", () => {
    const r = errorResponse(503)
    for (let attempt = 0; attempt < 3; attempt++) {
      const d = retryDelayMs(r, attempt)
      expect(d).toBeGreaterThanOrEqual(0)
      expect(d).toBeLessThanOrEqual(Math.min(500 * 2 ** attempt, 20_000))
    }
  })
})

describe("checkRuntimeCompat", () => {
  it("returns null for a matching major", () => {
    expect(checkRuntimeCompat(`${SUPPORTED_RUNTIME_MAJOR}.5.2`)).toBeNull()
    expect(checkRuntimeCompat(`v${SUPPORTED_RUNTIME_MAJOR}.0.0`)).toBeNull()
  })

  it("warns on a major mismatch", () => {
    const msg = checkRuntimeCompat(`${SUPPORTED_RUNTIME_MAJOR + 1}.0.0`)
    expect(msg).not.toBeNull()
    expect(msg).toContain("upgrade the SDK")
  })

  it("tolerates unknown / unparseable / missing versions", () => {
    expect(checkRuntimeCompat(null)).toBeNull()
    expect(checkRuntimeCompat("")).toBeNull()
    expect(checkRuntimeCompat("not-a-version")).toBeNull()
  })
})

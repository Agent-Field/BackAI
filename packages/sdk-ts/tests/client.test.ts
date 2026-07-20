// SPDX-License-Identifier: Apache-2.0

// The explicit BackAI client: config binds ambiently to every governed
// namespace (baseUrl / apiKey / retries), the mutation-safety rule holds at
// the client level, and the lazy runtime-version probe warns on a major
// mismatch.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { BackAI, SuiteError } from "../src/index.js"

interface Call {
  url: string
  init: RequestInit
}

let fetchMock: ReturnType<typeof vi.fn>
let calls: Call[]
let handler: (url: string, init: RequestInit) => Response

function jsonResponse(body: unknown, init: ResponseInit = {}): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "content-type": "application/json" },
    ...init,
  })
}

function errorResponse(status: number, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify({ error: { code: "TRANSIENT", message: "later" } }), {
    status,
    headers: { "content-type": "application/json", ...headers },
  })
}

function headerOf(init: RequestInit, name: string): string | undefined {
  return (init.headers as Record<string, string> | undefined)?.[name]
}

beforeEach(() => {
  calls = []
  handler = () => jsonResponse({})
  fetchMock = vi.fn(async (url: string, init: RequestInit) => {
    calls.push({ url: String(url), init })
    return handler(String(url), init)
  })
  ;(globalThis as { fetch: typeof fetch }).fetch = fetchMock as unknown as typeof fetch
  // Env deliberately differs from the client config, to prove the client wins.
  process.env.AF_STACK_URL = "http://env.local"
  process.env.AF_STACK_API_KEY = "env-key"
})

afterEach(() => {
  vi.restoreAllMocks()
  delete process.env.AF_STACK_URL
  delete process.env.AF_STACK_API_KEY
})

describe("BackAI config binding", () => {
  it("routes every namespace to the client's baseUrl + apiKey (not env)", async () => {
    handler = (url) => {
      if (url.includes("/auth/whoami")) {
        return jsonResponse({ authenticated: true, tenant_id: "t", user_id: "u", api_key_id: "k" })
      }
      if (url.includes("/jobs")) return jsonResponse({ jobs: [], total: 0, has_more: false })
      if (url.includes("/storage"))
        return jsonResponse({ objects: [], prefix: "", next_token: null })
      return jsonResponse({})
    }
    const client = new BackAI({
      baseUrl: "http://client.local",
      apiKey: "client-key",
      checkRuntimeVersion: false,
    })
    await client.auth.whoami()
    await client.jobs.list()
    await client.storage.list()

    expect(calls.length).toBe(3)
    for (const c of calls) {
      expect(c.url.startsWith("http://client.local/api/v1/")).toBe(true)
      expect(headerOf(c.init, "authorization")).toBe("Bearer client-key")
    }
  })

  it("applies the default retry budget to an idempotent GET", async () => {
    const responses = [
      errorResponse(429, { "retry-after": "0" }),
      jsonResponse({ jobs: [], total: 0, has_more: false }),
    ]
    handler = () => responses.shift() ?? jsonResponse({})
    const client = new BackAI({
      baseUrl: "http://c.local",
      apiKey: "k",
      checkRuntimeVersion: false,
    })
    const list = await client.jobs.list()
    expect(list.jobs).toEqual([])
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(client.maxRetries).toBe(2)
  })

  it("NEVER retries a mutation even though the client has a retry budget", async () => {
    const responses = [errorResponse(429, { "retry-after": "0" }), jsonResponse({})]
    handler = () => responses.shift() ?? jsonResponse({})
    const client = new BackAI({
      baseUrl: "http://c.local",
      apiKey: "k",
      checkRuntimeVersion: false,
    })
    const err = await client.jobs.enqueue("x", {}).catch((e) => e as unknown)
    expect(err).toBeInstanceOf(SuiteError)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it("retries a mutation when an idempotencyKey is supplied", async () => {
    const jobRow = {
      id: "1",
      name: "x",
      args: {},
      state: "available",
      tenant_id: null,
      attempt: 0,
      max_attempts: 5,
      scheduled_at: "2026-01-01T00:00:00Z",
      enqueued_at: "2026-01-01T00:00:00Z",
      attempted_at: null,
      finalized_at: null,
      errors: null,
    }
    const responses = [errorResponse(429, { "retry-after": "0" }), jsonResponse(jobRow)]
    handler = () => responses.shift() ?? jsonResponse({})
    const client = new BackAI({
      baseUrl: "http://c.local",
      apiKey: "k",
      checkRuntimeVersion: false,
    })
    const job = await client.jobs.enqueue("x", {}, { idempotencyKey: "idem-1" })
    expect(job.id).toBe("1")
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(headerOf(calls[0].init, "idempotency-key")).toBe("idem-1")
  })
})

describe("BackAI runtime-version probe", () => {
  it("warns once on a major version mismatch", async () => {
    handler = (url) =>
      url.endsWith("/api/v1/version") ? jsonResponse({ version: "9.0.0" }) : jsonResponse({})
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {})
    const client = new BackAI({ baseUrl: "http://c.local", apiKey: "k" })
    expect(await client.runtimeVersion()).toBe("9.0.0")
    await client.ensureVersionChecked()
    expect(warn).toHaveBeenCalledTimes(1)
    expect(String(warn.mock.calls[0]?.[0])).toContain("upgrade the SDK")
    // Idempotent: a second call does not warn again.
    await client.ensureVersionChecked()
    expect(warn).toHaveBeenCalledTimes(1)
  })

  it("tolerates a 404 /version (older runtimes) with no warning", async () => {
    handler = (url) =>
      url.endsWith("/api/v1/version")
        ? new Response(JSON.stringify({ error: { code: "NOT_FOUND", message: "nope" } }), {
            status: 404,
            headers: { "content-type": "application/json" },
          })
        : jsonResponse({})
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {})
    const client = new BackAI({ baseUrl: "http://c.local", apiKey: "k" })
    expect(await client.runtimeVersion()).toBeNull()
    await client.ensureVersionChecked()
    expect(warn).not.toHaveBeenCalled()
  })
})

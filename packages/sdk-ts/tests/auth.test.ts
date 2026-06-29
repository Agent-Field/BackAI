// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { auth, authWhoami, suite } from "../src/index.js"

let fetchMock: ReturnType<typeof vi.fn>
let responseQueue: Response[]

function jsonResponse(body: unknown, init: ResponseInit = {}): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "content-type": "application/json" },
    ...init,
  })
}

function enqueue(response: Response): void {
  responseQueue.push(response)
}

function nthCall(idx: number): { url: string; init: RequestInit } {
  const args = fetchMock.mock.calls[idx]
  if (args === undefined) throw new Error(`no fetch call at idx ${idx}`)
  return { url: String(args[0]), init: (args[1] as RequestInit | undefined) ?? {} }
}

beforeEach(() => {
  responseQueue = []
  fetchMock = vi.fn(async () => responseQueue.shift() ?? jsonResponse({}))
  ;(globalThis as { fetch: typeof fetch }).fetch = fetchMock as unknown as typeof fetch
  process.env.AF_STACK_URL = "http://test.local"
  process.env.AF_STACK_API_KEY = "test-key"
})

afterEach(() => {
  vi.restoreAllMocks()
  delete process.env.AF_STACK_URL
  delete process.env.AF_STACK_API_KEY
})

describe("auth.whoami", () => {
  it("GETs /auth/whoami and parses the identity to camelCase", async () => {
    enqueue(
      jsonResponse({
        authenticated: true,
        tenant_id: "t1",
        user_id: "u1",
        api_key_id: "k1",
      }),
    )
    const me = await authWhoami()
    expect(nthCall(0).url).toContain("/auth/whoami")
    expect(nthCall(0).init.method ?? "GET").toBe("GET")
    expect(me).toEqual({
      authenticated: true,
      tenantId: "t1",
      userId: "u1",
      apiKeyId: "k1",
    })
  })

  it("reports authenticated=false for an unauthenticated caller", async () => {
    enqueue(
      jsonResponse({
        authenticated: false,
        tenant_id: "",
        user_id: "",
        api_key_id: "",
      }),
    )
    const me = await authWhoami()
    expect(me.authenticated).toBe(false)
    expect(me.tenantId).toBe("")
  })

  it("is exposed on the suite namespace", () => {
    expect(suite.auth).toBe(auth)
    expect(suite.auth.whoami).toBe(authWhoami)
  })
})

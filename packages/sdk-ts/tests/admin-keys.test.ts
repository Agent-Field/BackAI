// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { adminKeys, APIKeySchema, IssuedAPIKeySchema, suite } from "../src/index.js"

let fetchMock: ReturnType<typeof vi.fn>
let responseQueue: Response[]

function jsonResponse(body: unknown, init: ResponseInit = {}): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "content-type": "application/json" },
    ...init,
  })
}

interface MockCall { url: string; init: RequestInit }
function nthCall(idx: number): MockCall {
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

function row(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "k-1",
    tenant_id: "t-1",
    prefix: "abc12345",
    name: "ci-bot",
    scopes: ["read"],
    created_by: null,
    created_at: "2026-06-06T12:00:00Z",
    last_used_at: null,
    expires_at: null,
    revoked_at: null,
    ...overrides,
  }
}

describe("adminKeys.list", () => {
  it("GETs /admin/keys and parses to camelCase", async () => {
    responseQueue.push(jsonResponse({ keys: [row()] }))
    const result = await adminKeys.list()
    expect(result.keys[0].id).toBe("k-1")
    expect(result.keys[0].tenantId).toBe("t-1")
    expect(result.keys[0].createdAt).toBe("2026-06-06T12:00:00Z")
  })

  it("forwards tenant filter", async () => {
    responseQueue.push(jsonResponse({ keys: [] }))
    await adminKeys.list({ tenant: "t-1" })
    const url = new URL(nthCall(0).url)
    expect(url.searchParams.get("tenant")).toBe("t-1")
  })
})

describe("adminKeys.issue", () => {
  it("returns the plaintext value ONCE on issue (one-time-reveal)", async () => {
    // The runtime returns the IssuedAPIKey shape (APIKey + value) only on
    // POST /api/v1/admin/keys. Subsequent reads expose APIKey (no value).
    const issued = row({ value: "af_abc12345_supersecret" })
    responseQueue.push(jsonResponse(issued))
    const result = await adminKeys.issue({ tenantId: "t-1", scopes: ["read"] })

    expect(result.value).toBe("af_abc12345_supersecret")
    expect(result.id).toBe("k-1")
    expect(result.tenantId).toBe("t-1")

    // Body sent over the wire was snake_case.
    const body = JSON.parse(nthCall(0).init.body as string) as Record<string, unknown>
    expect(body.tenant_id).toBe("t-1")
    expect(body.scopes).toEqual(["read"])
  })

  it("validates schema: IssuedAPIKey has value, APIKey does not", () => {
    const rawApiKey = {
      id: "k-1",
      tenantId: "t-1",
      prefix: "abc",
      name: null,
      scopes: [],
      createdBy: null,
      createdAt: "2026-06-06T12:00:00Z",
      lastUsedAt: null,
      expiresAt: null,
      revokedAt: null,
    }
    // APIKey schema must NOT carry value.
    expect(APIKeySchema.safeParse(rawApiKey).success).toBe(true)
    // IssuedAPIKey schema must REQUIRE value.
    expect(IssuedAPIKeySchema.safeParse(rawApiKey).success).toBe(false)
    expect(IssuedAPIKeySchema.safeParse({ ...rawApiKey, value: "af_x" }).success).toBe(true)
  })

  it("rejects empty tenantId", async () => {
    await expect(adminKeys.issue({ tenantId: "" })).rejects.toThrow(/tenantId/)
  })

  it("defaults scopes to empty array", async () => {
    const issued = row({ value: "af_x", scopes: [] })
    responseQueue.push(jsonResponse(issued))
    await adminKeys.issue({ tenantId: "t-1" })
    const body = JSON.parse(nthCall(0).init.body as string) as Record<string, unknown>
    expect(body.scopes).toEqual([])
  })
})

describe("adminKeys.revoke", () => {
  it("DELETEs /admin/keys/{id} and returns true", async () => {
    responseQueue.push(jsonResponse({ revoked: true }))
    const ok = await adminKeys.revoke("k-1")
    expect(ok).toBe(true)
  })

  it("rejects empty id", async () => {
    await expect(adminKeys.revoke("")).rejects.toThrow()
  })
})

describe("namespace exports", () => {
  it("suite.admin.keys matches the module helpers", () => {
    expect(suite.admin.keys).toBe(adminKeys)
  })
})
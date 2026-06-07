// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { adminUsers, suite } from "../src/index.js"

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

function userRow(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "u-1",
    email: "alice@example.co",
    name: "Alice",
    avatar_url: null,
    created_at: "2026-06-06T12:00:00Z",
    deleted_at: null,
    ...overrides,
  }
}

describe("adminUsers.list", () => {
  it("GETs /admin/users without filters", async () => {
    responseQueue.push(jsonResponse({ users: [userRow(), userRow({ id: "u-2", email: "bob@example.co" })] }))
    const result = await adminUsers.list()
    expect(result.users.map((u) => u.id)).toEqual(["u-1", "u-2"])
    expect(result.users[0].email).toBe("alice@example.co")
    expect(result.users[0].avatarUrl).toBeNull()

    const c = nthCall(0)
    const url = new URL(c.url)
    expect(url.pathname).toBe("/api/v1/admin/users")
    expect(url.searchParams.has("tenant")).toBe(false)
    expect(url.searchParams.has("search")).toBe(false)
  })

  it("forwards tenant + search filters as query params", async () => {
    responseQueue.push(jsonResponse({ users: [] }))
    await adminUsers.list({ tenant: "t-1", search: "alice" })
    const url = new URL(nthCall(0).url)
    expect(url.searchParams.get("tenant")).toBe("t-1")
    expect(url.searchParams.get("search")).toBe("alice")
  })
})

describe("namespace exports", () => {
  it("suite.admin.users matches the module helpers", () => {
    expect(suite.admin.users).toBe(adminUsers)
  })
})
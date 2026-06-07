import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { adminMemberships, suite, SuiteError } from "../src/index.js"

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
    tenant_id: "t-1",
    user_id: "u-1",
    role: "owner",
    invited_at: "2026-06-06T12:00:00Z",
    accepted_at: "2026-06-06T12:00:01Z",
    ...overrides,
  }
}

describe("adminMemberships.list", () => {
  it("GETs /admin/memberships and parses to camelCase", async () => {
    responseQueue.push(jsonResponse({ memberships: [row()] }))
    const result = await adminMemberships.list()
    expect(result.memberships[0].tenantId).toBe("t-1")
    expect(result.memberships[0].userId).toBe("u-1")
    expect(result.memberships[0].role).toBe("owner")
  })

  it("forwards filters", async () => {
    responseQueue.push(jsonResponse({ memberships: [] }))
    await adminMemberships.list({ tenant: "t-1", user: "u-1" })
    const url = new URL(nthCall(0).url)
    expect(url.searchParams.get("tenant")).toBe("t-1")
    expect(url.searchParams.get("user")).toBe("u-1")
  })
})

describe("adminMemberships.add", () => {
  it("POSTs snake_case body", async () => {
    responseQueue.push(jsonResponse(row({ role: "admin" })))
    const m = await adminMemberships.add("t-1", "u-1", "admin")
    expect(m.role).toBe("admin")
    const c = nthCall(0)
    expect(c.init.method).toBe("POST")
    const body = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(body.tenant_id).toBe("t-1")
    expect(body.user_id).toBe("u-1")
    expect(body.role).toBe("admin")
  })

  it("validates inputs", async () => {
    await expect(adminMemberships.add("", "u-1", "member")).rejects.toThrow()
    await expect(adminMemberships.add("t-1", "", "member")).rejects.toThrow()
    await expect(adminMemberships.add("t-1", "u-1", "")).rejects.toThrow()
  })
})

describe("adminMemberships.remove", () => {
  it("DELETEs the composite path and returns true", async () => {
    responseQueue.push(jsonResponse({ deleted: true }))
    const ok = await adminMemberships.remove("t-1", "u-1")
    expect(ok).toBe(true)
    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/admin/memberships/t-1/u-1")
    expect(c.init.method).toBe("DELETE")
  })

  it("surfaces 404 NOT_FOUND as SuiteError", async () => {
    responseQueue.push(
      errorResponse(
        { error: { code: "NOT_FOUND", message: "no such membership" } },
        404,
      ),
    )
    const err = await adminMemberships.remove("t-1", "u-1").catch((e) => e as unknown)
    expect(err).toBeInstanceOf(SuiteError)
    expect((err as SuiteError).code).toBe("NOT_FOUND")
  })
})

describe("namespace exports", () => {
  it("suite.admin.memberships matches the module helpers", () => {
    expect(suite.admin.memberships).toBe(adminMemberships)
  })
})

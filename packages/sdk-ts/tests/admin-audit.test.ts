import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { adminAudit, suite } from "../src/index.js"

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

function entry(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "a-1",
    tenant_id: "t-1",
    user_id: null,
    api_key_id: "k-1",
    action: "tenant.created",
    resource_type: "tenant",
    resource_id: "t-1",
    metadata: { slug: "acme" },
    occurred_at: "2026-06-06T12:00:00Z",
    ...overrides,
  }
}

describe("adminAudit.list", () => {
  it("GETs /admin/audit and parses paginated response", async () => {
    responseQueue.push(jsonResponse({
      entries: [entry(), entry({ id: "a-2", action: "key.issued" })],
      total: 2,
      has_more: false,
    }))
    const result = await adminAudit.list()
    expect(result.total).toBe(2)
    expect(result.hasMore).toBe(false)
    expect(result.entries.map((e) => e.id)).toEqual(["a-1", "a-2"])
    expect(result.entries[0].tenantId).toBe("t-1")
    expect(result.entries[0].apiKeyId).toBe("k-1")
    expect(result.entries[0].metadata).toEqual({ slug: "acme" })
  })

  it("forwards all filters as query params", async () => {
    responseQueue.push(jsonResponse({ entries: [], total: 0, has_more: false }))
    await adminAudit.list({
      tenant: "t-1",
      action: "tenant.created",
      from: "2026-06-01T00:00:00Z",
      to: "2026-06-30T23:59:59Z",
      limit: 10,
      offset: 20,
    })
    const url = new URL(nthCall(0).url)
    expect(url.searchParams.get("tenant")).toBe("t-1")
    expect(url.searchParams.get("action")).toBe("tenant.created")
    expect(url.searchParams.get("from")).toBe("2026-06-01T00:00:00Z")
    expect(url.searchParams.get("to")).toBe("2026-06-30T23:59:59Z")
    expect(url.searchParams.get("limit")).toBe("10")
    expect(url.searchParams.get("offset")).toBe("20")
  })

  it("serialises Date for from/to", async () => {
    responseQueue.push(jsonResponse({ entries: [], total: 0, has_more: false }))
    const when = new Date("2026-06-06T12:00:00.000Z")
    await adminAudit.list({ from: when, to: when })
    const url = new URL(nthCall(0).url)
    expect(url.searchParams.get("from")).toBe("2026-06-06T12:00:00.000Z")
    expect(url.searchParams.get("to")).toBe("2026-06-06T12:00:00.000Z")
  })

  it("validates pagination bounds", async () => {
    await expect(adminAudit.list({ limit: 0 })).rejects.toThrow(/limit/i)
    await expect(adminAudit.list({ offset: -1 })).rejects.toThrow(/offset/i)
  })
})

describe("namespace exports", () => {
  it("suite.admin.audit matches the module helpers", () => {
    expect(suite.admin.audit).toBe(adminAudit)
  })
})

// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { adminTenants, suite, SuiteError } from "../src/index.js"

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

function tenantRow(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "t-1",
    slug: "acme",
    name: "Acme",
    plan: "free",
    settings: {},
    quota: {},
    created_at: "2026-06-06T12:00:00Z",
    deleted_at: null,
    ...overrides,
  }
}

describe("adminTenants.list", () => {
  it("GETs /admin/tenants and parses to camelCase", async () => {
    enqueueResponse(jsonResponse({ tenants: [tenantRow(), tenantRow({ id: "t-2", slug: "beta" })] }))
    const result = await adminTenants.list()
    expect(result.tenants.map((t) => t.id)).toEqual(["t-1", "t-2"])
    expect(result.tenants[0].slug).toBe("acme")
    expect(result.tenants[0].createdAt).toBe("2026-06-06T12:00:00Z")
    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/admin/tenants")
    expect(c.init.method).toBe("GET")
  })
})

describe("adminTenants.get", () => {
  it("GETs /admin/tenants/{id} and parses tenant detail", async () => {
    enqueueResponse(jsonResponse({
      tenant: tenantRow(),
      members: [
        {
          user: {
            id: "u-1",
            email: "owner@acme.co",
            name: "Owner",
            avatar_url: null,
            created_at: "2026-06-06T12:00:00Z",
            deleted_at: null,
          },
          role: "owner",
        },
      ],
      api_keys: [],
      usage: {
        requests_30d: 12,
        cost_usd_30d: 1.25,
        storage_bytes: 1024,
        secrets_count: 3,
      },
    }))
    const result = await adminTenants.get("t-1")
    expect(result.tenant.id).toBe("t-1")
    expect(result.members[0].user.email).toBe("owner@acme.co")
    expect(result.usage.requests30d).toBe(12)
    expect(result.usage.costUsd30d).toBe(1.25)
  })

  it("throws on empty id", async () => {
    await expect(adminTenants.get("")).rejects.toThrow(/non-empty/i)
  })
})

describe("adminTenants.create", () => {
  it("POSTs snake_case body and returns the new tenant", async () => {
    enqueueResponse(jsonResponse(tenantRow()))
    const result = await adminTenants.create({ slug: "acme", name: "Acme" })
    expect(result.id).toBe("t-1")
    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/admin/tenants")
    expect(c.init.method).toBe("POST")
    const body = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(body.slug).toBe("acme")
    expect(body.name).toBe("Acme")
  })

  it("validates required fields client-side", async () => {
    await expect(adminTenants.create({ slug: "", name: "x" })).rejects.toThrow(/slug/i)
    await expect(adminTenants.create({ slug: "x", name: "" })).rejects.toThrow(/name/i)
  })
})

describe("adminTenants.update", () => {
  it("PATCHes /admin/tenants/{id} with snake_case body", async () => {
    enqueueResponse(jsonResponse(tenantRow({ name: "Acme Renamed" })))
    const result = await adminTenants.update("t-1", { name: "Acme Renamed" })
    expect(result.name).toBe("Acme Renamed")
    const c = nthCall(0)
    expect(c.init.method).toBe("PATCH")
    const body = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(body.name).toBe("Acme Renamed")
  })
})

describe("adminTenants.delete", () => {
  it("DELETEs /admin/tenants/{id} and returns true", async () => {
    enqueueResponse(jsonResponse({ deleted: true }))
    const ok = await adminTenants.delete("t-1")
    expect(ok).toBe(true)
  })
})

describe("MT_DISABLED gate", () => {
  it("surfaces 503 + MT_DISABLED envelope as a clear SuiteError", async () => {
    enqueueResponse(
      errorResponse(
        {
          error: {
            code: "MT_DISABLED",
            message: "multi-tenancy module not enabled",
          },
        },
        503,
      ),
    )
    const err = await adminTenants.list().catch((e) => e as unknown)
    expect(err).toBeInstanceOf(SuiteError)
    expect((err as SuiteError).code).toBe("MT_DISABLED")
    expect((err as SuiteError).status).toBe(503)
  })
})

describe("namespace exports", () => {
  it("suite.admin.tenants matches the module helpers", () => {
    expect(suite.admin.tenants).toBe(adminTenants)
    expect(suite.admin.tenants.list).toBe(adminTenants.list)
  })
})
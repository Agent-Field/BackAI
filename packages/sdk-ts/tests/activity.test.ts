// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  activity,
  listActivity,
  logActivity,
  suite,
} from "../src/index.js"

let fetchMock: ReturnType<typeof vi.fn>
let responseQueue: Response[]

function jsonResponse(body: unknown, init: ResponseInit = {}): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "content-type": "application/json" },
    ...init,
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
  if (args === undefined) throw new Error(`fetch call ${idx} missing`)
  return { url: String(args[0]), init: (args[1] as RequestInit | undefined) ?? {} }
}

function row(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "act_1",
    tenant_id: "00000000-0000-0000-0000-000000000000",
    user_id: "11111111-1111-1111-1111-111111111111",
    api_key_id: null,
    actor_type: "user",
    action: "document.uploaded",
    resource_type: "document",
    resource_id: "doc_123",
    metadata: { fileType: "pdf" },
    ip: "203.0.113.1",
    user_agent: "vitest",
    occurred_at: "2026-06-07T12:00:00Z",
    ...overrides,
  }
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

describe("activity.log", () => {
  it("POSTs /activity and preserves caller metadata", async () => {
    enqueueResponse(jsonResponse(row()))
    const entry = await logActivity("document.uploaded", {
      userId: "11111111-1111-1111-1111-111111111111",
      actorType: "user",
      resourceType: "document",
      resourceId: "doc_123",
      metadata: { fileType: "pdf" },
      occurredAt: new Date("2026-06-07T12:00:00Z"),
    })
    expect(entry.actorType).toBe("user")
    expect(entry.resourceType).toBe("document")
    expect(entry.metadata).toEqual({ fileType: "pdf" })

    const c = nthCall(0)
    expect(c.init.method).toBe("POST")
    expect(new URL(c.url).pathname).toBe("/api/v1/activity")
    const body = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(body.action).toBe("document.uploaded")
    expect(body.user_id).toBe("11111111-1111-1111-1111-111111111111")
    expect(body.actor_type).toBe("user")
    expect(body.resource_type).toBe("document")
    expect(body.resource_id).toBe("doc_123")
    expect(body.metadata).toEqual({ fileType: "pdf" })
    expect(body.occurred_at).toBe("2026-06-07T12:00:00.000Z")
  })

  it("validates action, actor type, and resource relation", async () => {
    await expect(activity.log("")).rejects.toThrow(/non-empty/i)
    await expect(activity.log("x", { actorType: "bot" as never })).rejects.toThrow(/actorType/)
    await expect(activity.log("x", { resourceId: "r1" })).rejects.toThrow(/resourceType/)
  })
})

describe("activity.list", () => {
  it("GETs /activity with filters and parses the page", async () => {
    enqueueResponse(jsonResponse({ entries: [row()], total: 7, has_more: true }))
    const page = await listActivity({
      userId: "11111111-1111-1111-1111-111111111111",
      action: "document.uploaded",
      resourceType: "document",
      resourceId: "doc_123",
      from: "2026-06-07T00:00:00Z",
      limit: 10,
      offset: 20,
    })
    expect(page.total).toBe(7)
    expect(page.hasMore).toBe(true)
    expect(page.entries[0]?.userAgent).toBe("vitest")

    const url = new URL(nthCall(0).url)
    expect(url.pathname).toBe("/api/v1/activity")
    expect(url.searchParams.get("user_id")).toBe("11111111-1111-1111-1111-111111111111")
    expect(url.searchParams.get("action")).toBe("document.uploaded")
    expect(url.searchParams.get("resource_type")).toBe("document")
    expect(url.searchParams.get("resource_id")).toBe("doc_123")
    expect(url.searchParams.get("from")).toBe("2026-06-07T00:00:00Z")
    expect(url.searchParams.get("limit")).toBe("10")
    expect(url.searchParams.get("offset")).toBe("20")
  })

  it("validates paging and is exposed on suite.activity", async () => {
    await expect(activity.list({ limit: 0 })).rejects.toThrow(/limit/)
    await expect(activity.list({ offset: -1 })).rejects.toThrow(/offset/)
    expect(suite.activity).toBe(activity)
  })
})

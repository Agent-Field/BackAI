// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { approvals, suite } from "../src/index.js"

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

function nthCall(idx: number): { url: string; init: RequestInit } {
  const args = fetchMock.mock.calls[idx]
  if (args === undefined) throw new Error(`fetch call ${idx} missing`)
  return { url: String(args[0]), init: (args[1] as RequestInit | undefined) ?? {} }
}

function approvalRow(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "appr_1",
    tenant_id: "tenant_1",
    requested_by: "user_1",
    kind: "deploy_to_prod",
    payload: { service: "api" },
    status: "pending",
    decided_by: null,
    decided_at: null,
    decision_note: null,
    created_at: "2026-06-07T12:00:00Z",
    updated_at: "2026-06-07T12:00:00Z",
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

describe("approvals", () => {
  it("requests an approval", async () => {
    enqueueResponse(jsonResponse(approvalRow(), { status: 202 }))
    const result = await approvals.request({
      kind: "deploy_to_prod",
      payload: { service: "api" },
    })
    expect(result.kind).toBe("deploy_to_prod")
    expect(result.tenantId).toBe("tenant_1")
    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/approvals")
    expect(c.init.method).toBe("POST")
    expect(JSON.parse(c.init.body as string).payload.service).toBe("api")
  })

  it("lists, gets, and decides approvals", async () => {
    enqueueResponse(jsonResponse({ approvals: [approvalRow()], total: 1, has_more: false }))
    const list = await approvals.list({ status: "pending", kind: "deploy_to_prod", limit: 10 })
    expect(list.approvals[0]?.requestedBy).toBe("user_1")
    const url = new URL(nthCall(0).url)
    expect(url.pathname).toBe("/api/v1/approvals")
    expect(url.searchParams.get("status")).toBe("pending")

    enqueueResponse(jsonResponse(approvalRow()))
    await approvals.get("appr_1")
    expect(nthCall(1).url).toBe("http://test.local/api/v1/approvals/appr_1")

    enqueueResponse(jsonResponse(approvalRow({
      status: "approved",
      decided_by: "user_2",
      decision_note: "ok",
    })))
    const decided = await approvals.decide("appr_1", {
      status: "approved",
      decisionNote: "ok",
    })
    expect(decided.status).toBe("approved")
    const body = JSON.parse(nthCall(2).init.body as string) as Record<string, unknown>
    expect(body.decision_note).toBe("ok")
  })

  it("suite.approvals matches the module helpers", () => {
    expect(suite.approvals).toBe(approvals)
    expect(suite.approvals.request).toBe(approvals.request)
  })
})

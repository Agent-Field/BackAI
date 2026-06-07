// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { adminBudgets, suite, SuiteError } from "../src/index.js"

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

function budgetRow(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    tenant_id: "t-acme",
    monthly_usd: 100,
    alert_threshold_pct: 80,
    spent_this_period_usd: 12.5,
    remaining_usd: 87.5,
    resets_at: "2026-07-01T00:00:00Z",
    ...overrides,
  }
}

describe("adminBudgets.list", () => {
  it("GETs /admin/budgets and parses to camelCase", async () => {
    enqueueResponse(jsonResponse({
      budgets: [budgetRow(), budgetRow({ tenant_id: "t-globex", monthly_usd: 50 })],
    }))
    const result = await adminBudgets.list()
    expect(result.budgets.map((b) => b.tenantId)).toEqual(["t-acme", "t-globex"])
    expect(result.budgets[0].monthlyUsd).toBe(100)
    expect(result.budgets[0].alertThresholdPct).toBe(80)
    expect(result.budgets[0].spentThisPeriodUsd).toBe(12.5)
    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/admin/budgets")
    expect(c.init.method).toBe("GET")
  })
})

describe("adminBudgets.get", () => {
  it("GETs /admin/budgets/{id} and returns a single budget", async () => {
    enqueueResponse(jsonResponse(budgetRow()))
    const result = await adminBudgets.get("t-acme")
    expect(result.tenantId).toBe("t-acme")
    expect(result.remainingUsd).toBe(87.5)
    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/admin/budgets/t-acme")
  })

  it("throws on empty id", async () => {
    await expect(adminBudgets.get("")).rejects.toThrow(/non-empty/i)
  })
})

describe("adminBudgets.set", () => {
  it("PUTs snake_case body and returns the new budget", async () => {
    enqueueResponse(jsonResponse(budgetRow({ monthly_usd: 200 })))
    const result = await adminBudgets.set({
      tenantId: "t-acme",
      monthlyUsd: 200,
      alertThresholdPct: 90,
    })
    expect(result.monthlyUsd).toBe(200)
    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/admin/budgets")
    expect(c.init.method).toBe("PUT")
    const body = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(body.tenant_id).toBe("t-acme")
    expect(body.monthly_usd).toBe(200)
    expect(body.alert_threshold_pct).toBe(90)
  })

  it("validates required fields client-side", async () => {
    await expect(
      adminBudgets.set({ tenantId: "", monthlyUsd: 10 }),
    ).rejects.toThrow(/tenantId/)
    await expect(
      adminBudgets.set({ tenantId: "t", monthlyUsd: 0 }),
    ).rejects.toThrow(/positive/)
    await expect(
      adminBudgets.set({ tenantId: "t", monthlyUsd: -5 }),
    ).rejects.toThrow(/positive/)
  })
})

describe("BUDGET_EXCEEDED gate", () => {
  it("surfaces 402 envelope as SuiteError with code BUDGET_EXCEEDED", async () => {
    enqueueResponse(
      errorResponse(
        {
          error: {
            code: "BUDGET_EXCEEDED",
            message: "tenant has exhausted its monthly budget",
          },
        },
        402,
      ),
    )
    const err = await adminBudgets
      .set({ tenantId: "t-acme", monthlyUsd: 10 })
      .catch((e) => e as unknown)
    expect(err).toBeInstanceOf(SuiteError)
    expect((err as SuiteError).code).toBe("BUDGET_EXCEEDED")
    expect((err as SuiteError).status).toBe(402)
  })
})

describe("namespace exports", () => {
  it("suite.admin.budgets matches the module helpers", () => {
    expect(suite.admin.budgets).toBe(adminBudgets)
    expect(suite.admin.budgets.set).toBe(adminBudgets.set)
  })
})
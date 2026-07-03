// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  billing,
  billingCheckout,
  billingCustomer,
  billingCustomers,
  billingEntitlements,
  billingHasBudget,
  billingMeter,
  billingMeters,
  billingPlans,
  billingPortalLink,
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

function enqueue(response: Response): void {
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

function customerRow(
  overrides: Record<string, unknown> = {},
): Record<string, unknown> {
  return {
    tenant_id: "00000000-0000-0000-0000-000000000001",
    stripe_customer_id: "cus_stub_acme",
    email: "billing@acme.test",
    plan: "pro",
    trial_ends_at: null,
    current_period_end: "2026-07-01T00:00:00Z",
    subscription_status: "active",
    created_at: "2026-05-01T00:00:00Z",
    updated_at: "2026-06-01T00:00:00Z",
    ...overrides,
  }
}

function meterRow(
  overrides: Record<string, unknown> = {},
): Record<string, unknown> {
  return {
    meter: "sandbox_seconds",
    tenant_id: "00000000-0000-0000-0000-000000000001",
    period_start: "2026-06-01T00:00:00Z",
    period_end: "2026-07-01T00:00:00Z",
    quantity: 1234.0,
    cost_usd: 0.0617,
    stripe_meter_id: null,
    last_synced_at: null,
    ...overrides,
  }
}

describe("billing.customers", () => {
  it("GETs /billing/customers and parses to camelCase", async () => {
    enqueue(
      jsonResponse({
        customers: [
          customerRow(),
          customerRow({
            tenant_id: "00000000-0000-0000-0000-000000000002",
            plan: "free",
            subscription_status: null,
          }),
        ],
      }),
    )
    const result = await billingCustomers()
    expect(result.customers).toHaveLength(2)
    expect(result.customers[0].plan).toBe("pro")
    expect(result.customers[0].stripeCustomerId).toBe("cus_stub_acme")
    expect(result.customers[1].plan).toBe("free")
    expect(result.customers[1].subscriptionStatus).toBeNull()
    expect(nthCall(0).url).toBe(
      "http://test.local/api/v1/billing/customers",
    )
  })
})

describe("billing.customer", () => {
  it("GETs /billing/customers/{tenantId}", async () => {
    enqueue(jsonResponse(customerRow()))
    const result = await billingCustomer("00000000-0000-0000-0000-000000000001")
    expect(result.tenantId).toBe("00000000-0000-0000-0000-000000000001")
    expect(result.plan).toBe("pro")
  })

  it("rejects empty tenant", async () => {
    await expect(billingCustomer("")).rejects.toThrow(/tenantId/)
  })
})

describe("billing.meters", () => {
  it("GETs /billing/meters with parsed list", async () => {
    enqueue(
      jsonResponse({
        meters: [
          meterRow(),
          meterRow({ meter: "llm_tokens", quantity: 4321.0, cost_usd: null }),
        ],
        total_cost_usd: 0.0617,
      }),
    )
    const result = await billingMeters()
    expect(result.totalCostUsd).toBe(0.0617)
    expect(result.meters).toHaveLength(2)
    expect(result.meters[0].meter).toBe("sandbox_seconds")
    expect(result.meters[1].costUsd).toBeNull()
  })

  it("sends query params", async () => {
    enqueue(jsonResponse({ meters: [], total_cost_usd: 0 }))
    await billingMeters({
      tenant: "00000000-0000-0000-0000-000000000001",
      periodStart: "2026-06-01T00:00:00Z",
      bucket: "day",
    })
    const url = new URL(nthCall(0).url)
    expect(url.pathname).toBe("/api/v1/billing/meters")
    expect(url.searchParams.get("tenant")).toBe(
      "00000000-0000-0000-0000-000000000001",
    )
    expect(url.searchParams.get("period_start")).toBe("2026-06-01T00:00:00Z")
    expect(url.searchParams.get("bucket")).toBe("day")
  })
})

describe("billing.portalLink", () => {
  it("POSTs to /billing/customers/{tenantId}/portal", async () => {
    enqueue(
      jsonResponse({
        url: "https://example.com/portal-stub?customer=cus_stub_acme",
        expires_at: "2026-06-02T00:00:00Z",
      }),
    )
    const result = await billingPortalLink(
      "00000000-0000-0000-0000-000000000001",
      { returnUrl: "https://app.example.com/back" },
    )
    expect(result.url.startsWith("https://example.com/portal-stub")).toBe(true)
    expect(result.expiresAt).toBe("2026-06-02T00:00:00Z")
    const call = nthCall(0)
    expect(call.init.method).toBe("POST")
    expect(call.url).toBe(
      "http://test.local/api/v1/billing/customers/00000000-0000-0000-0000-000000000001/portal",
    )
    const body = JSON.parse(String(call.init.body))
    expect(body.return_url).toBe("https://app.example.com/back")
  })

  it("rejects empty tenant", async () => {
    await expect(billingPortalLink("")).rejects.toThrow(/tenantId/)
  })
})

describe("billing.meter", () => {
  it("validates inputs", async () => {
    await expect(billingMeter("", 1)).rejects.toThrow(/non-empty/)
    await expect(billingMeter("sandbox_seconds", -1)).rejects.toThrow(/qty/)
  })

  it("POSTs the increment to /billing/meter", async () => {
    enqueue(new Response(null, { status: 204 }))
    await billingMeter("sandbox_seconds", 1.5, { tenantId: "t1" })
    const call = nthCall(0)
    expect(call.url).toContain("/billing/meter")
    expect(call.init.method).toBe("POST")
    expect(JSON.parse(String(call.init.body))).toEqual({
      name: "sandbox_seconds",
      qty: 1.5,
      tenant_id: "t1",
    })
  })
})

describe("billing.hasBudget", () => {
  it("is permissive for unknown / enterprise plans", async () => {
    enqueue(jsonResponse(customerRow({ plan: "enterprise" })))
    const ok = await billingHasBudget(
      "00000000-0000-0000-0000-000000000001",
      100,
    )
    expect(ok).toBe(true)
  })

  it("blocks when over cap", async () => {
    enqueue(jsonResponse(customerRow({ plan: "free" })))
    enqueue(
      jsonResponse({
        meters: [meterRow({ cost_usd: 9.5 })],
        total_cost_usd: 9.5,
      }),
    )
    const ok = await billingHasBudget(
      "00000000-0000-0000-0000-000000000001",
      1.0,
    )
    expect(ok).toBe(false)
  })

  it("passes when within cap", async () => {
    enqueue(jsonResponse(customerRow({ plan: "free" })))
    enqueue(
      jsonResponse({
        meters: [meterRow({ cost_usd: 5 })],
        total_cost_usd: 5,
      }),
    )
    const ok = await billingHasBudget(
      "00000000-0000-0000-0000-000000000001",
      1.0,
    )
    expect(ok).toBe(true)
  })

  it("is permissive without a tenant id", async () => {
    const ok = await billingHasBudget("", 100)
    expect(ok).toBe(true)
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

function planRow(
  overrides: Record<string, unknown> = {},
): Record<string, unknown> {
  return {
    id: "pro",
    name: "Pro",
    stripe_price_id: "price_1QStubPro",
    price_usd_month: 29.0,
    llm_budget_usd: 50.0,
    entitlements: { max_agents: 10, priority_support: true },
    is_default: false,
    created_at: "2026-07-01T00:00:00Z",
    updated_at: "2026-07-01T00:00:00Z",
    ...overrides,
  }
}

describe("billing.plans", () => {
  it("GETs /billing/plans and parses the catalog", async () => {
    enqueue(
      jsonResponse({
        plans: [
          planRow({
            id: "free",
            name: "Free",
            stripe_price_id: null,
            price_usd_month: 0,
            llm_budget_usd: null,
            entitlements: {},
            is_default: true,
          }),
          planRow(),
        ],
      }),
    )
    const result = await billingPlans()
    expect(nthCall(0).url).toBe("http://test.local/api/v1/billing/plans")
    expect(result.plans).toHaveLength(2)
    const [free, pro] = result.plans
    expect(free.id).toBe("free")
    expect(free.isDefault).toBe(true)
    expect(free.stripePriceId).toBeNull()
    expect(free.llmBudgetUsd).toBeNull()
    expect(free.entitlements).toEqual({})
    expect(pro.priceUsdMonth).toBe(29.0)
    expect(pro.llmBudgetUsd).toBe(50.0)
    expect(pro.stripePriceId).toBe("price_1QStubPro")
  })

  it("preserves entitlement keys verbatim (no camelization)", async () => {
    enqueue(jsonResponse({ plans: [planRow()] }))
    const result = await billingPlans()
    expect(result.plans[0].entitlements).toEqual({
      max_agents: 10,
      priority_support: true,
    })
  })
})

describe("billing.checkout", () => {
  it("POSTs the minimal payload and parses the session", async () => {
    enqueue(
      jsonResponse({
        url: "https://checkout.stripe.com/c/pay/cs_test_123",
        applied_directly: false,
      }),
    )
    const result = await billingCheckout("pro", "https://app.example.com/thanks")
    const call = nthCall(0)
    expect(call.url).toBe("http://test.local/api/v1/billing/checkout")
    expect(call.init.method).toBe("POST")
    // Optional fields stay off the wire when not given.
    expect(JSON.parse(String(call.init.body))).toEqual({
      plan_id: "pro",
      success_url: "https://app.example.com/thanks",
    })
    expect(result.url).toBe("https://checkout.stripe.com/c/pay/cs_test_123")
    expect(result.appliedDirectly).toBe(false)
  })

  it("sends cancelUrl and tenantId when given", async () => {
    enqueue(
      jsonResponse({
        url: "https://checkout.stripe.com/c/pay/cs_test_456",
        applied_directly: false,
      }),
    )
    await billingCheckout("pro", "https://app.example.com/thanks", {
      cancelUrl: "https://app.example.com/pricing",
      tenantId: "00000000-0000-0000-0000-000000000001",
    })
    expect(JSON.parse(String(nthCall(0).init.body))).toEqual({
      plan_id: "pro",
      success_url: "https://app.example.com/thanks",
      cancel_url: "https://app.example.com/pricing",
      tenant_id: "00000000-0000-0000-0000-000000000001",
    })
  })

  it("reports appliedDirectly in stub/dev mode", async () => {
    enqueue(jsonResponse({ url: "", applied_directly: true }))
    const result = await billingCheckout("pro", "https://app.example.com/thanks")
    expect(result.appliedDirectly).toBe(true)
    expect(result.url).toBe("")
  })

  it("rejects empty inputs before any HTTP call", async () => {
    await expect(
      billingCheckout("", "https://app.example.com/thanks"),
    ).rejects.toThrow(/planId/)
    await expect(billingCheckout("pro", "")).rejects.toThrow(/successUrl/)
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

describe("billing.entitlements", () => {
  it("GETs /billing/entitlements without tenant when omitted", async () => {
    enqueue(
      jsonResponse({
        tenant_id: "00000000-0000-0000-0000-000000000001",
        plan: planRow(),
        entitlements: { max_agents: 10, priority_support: true },
        usage: { sandbox_seconds: 1234.0, llm_tokens: 4321.0 },
      }),
    )
    const result = await billingEntitlements()
    const url = new URL(nthCall(0).url)
    expect(url.pathname).toBe("/api/v1/billing/entitlements")
    expect(url.searchParams.has("tenant")).toBe(false)
    expect(result.tenantId).toBe("00000000-0000-0000-0000-000000000001")
    expect(result.plan.id).toBe("pro")
    expect(result.entitlements).toEqual({
      max_agents: 10,
      priority_support: true,
    })
    // Usage map keys are meter names — preserved verbatim.
    expect(result.usage).toEqual({ sandbox_seconds: 1234.0, llm_tokens: 4321.0 })
  })

  it("sends the tenant query param when given", async () => {
    enqueue(
      jsonResponse({
        tenant_id: "00000000-0000-0000-0000-000000000002",
        plan: planRow({ id: "free", name: "Free", is_default: true }),
        entitlements: {},
        usage: {},
      }),
    )
    const result = await billingEntitlements({
      tenant: "00000000-0000-0000-0000-000000000002",
    })
    const url = new URL(nthCall(0).url)
    expect(url.searchParams.get("tenant")).toBe(
      "00000000-0000-0000-0000-000000000002",
    )
    expect(result.entitlements).toEqual({})
    expect(result.usage).toEqual({})
  })
})

describe("namespace exports", () => {
  it("suite.billing matches the module helpers", () => {
    expect(suite.billing).toBe(billing)
    expect(suite.billing.customers).toBe(billing.customers)
    expect(suite.billing.portalLink).toBe(billing.portalLink)
    expect(suite.billing.plans).toBe(billing.plans)
    expect(suite.billing.checkout).toBe(billing.checkout)
    expect(suite.billing.entitlements).toBe(billing.entitlements)
  })
})
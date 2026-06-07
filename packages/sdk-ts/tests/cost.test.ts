import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { cost, costEvents, suite } from "../src/index.js"

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

function eventRow(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "ce-1",
    tenant_id: "t-acme",
    api_key_id: "key-abc",
    model: "qwen/qwen-2.5-72b-instruct",
    provider: "openrouter",
    agent: "sample.summarize",
    prompt_tokens: 100,
    completion_tokens: 25,
    total_tokens: 125,
    cost_usd: 0.0001,
    cached: false,
    latency_ms: 410,
    occurred_at: "2026-06-06T12:00:00Z",
    ...overrides,
  }
}

describe("cost.events", () => {
  it("GETs /cost/events and parses to camelCase", async () => {
    enqueueResponse(jsonResponse({
      events: [eventRow(), eventRow({ id: "ce-2", cost_usd: 0.0002 })],
      total: 42,
      has_more: true,
    }))
    const result = await costEvents()
    expect(result.total).toBe(42)
    expect(result.hasMore).toBe(true)
    expect(result.events.map((e) => e.id)).toEqual(["ce-1", "ce-2"])
    expect(result.events[0].costUsd).toBe(0.0001)
    expect(result.events[0].promptTokens).toBe(100)
    expect(result.events[0].apiKeyId).toBe("key-abc")
  })

  it("sends filters as query params", async () => {
    enqueueResponse(jsonResponse({ events: [], total: 0, has_more: false }))
    await costEvents({
      tenant: "t-acme",
      model: "qwen/qwen-2.5-72b-instruct",
      limit: 10,
      offset: 5,
    })
    const c = nthCall(0)
    const url = new URL(c.url)
    expect(url.pathname).toBe("/api/v1/cost/events")
    expect(url.searchParams.get("tenant")).toBe("t-acme")
    expect(url.searchParams.get("model")).toBe("qwen/qwen-2.5-72b-instruct")
    expect(url.searchParams.get("limit")).toBe("10")
    expect(url.searchParams.get("offset")).toBe("5")
  })

  it("serialises Date filters", async () => {
    enqueueResponse(jsonResponse({ events: [], total: 0, has_more: false }))
    await costEvents({ from: new Date("2026-06-01T00:00:00Z") })
    const c = nthCall(0)
    const url = new URL(c.url)
    expect(url.searchParams.get("from")).toBe("2026-06-01T00:00:00.000Z")
  })

  it("rejects invalid pagination", async () => {
    await expect(costEvents({ limit: 0 })).rejects.toThrow(/limit/)
    await expect(costEvents({ offset: -1 })).rejects.toThrow(/offset/)
  })
})

describe("namespace exports", () => {
  it("suite.cost matches the module helpers", () => {
    expect(suite.cost).toBe(cost)
    expect(suite.cost.events).toBe(cost.events)
  })
})

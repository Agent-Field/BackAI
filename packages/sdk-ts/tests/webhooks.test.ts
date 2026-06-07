import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  webhooks,
  sendWebhook,
  listWebhookDeliveries,
  getWebhookDelivery,
  retryWebhookDelivery,
  suite,
  SuiteError,
} from "../src/index.js"

// ---------- fetch mock plumbing (shape matches sandbox.test.ts) ----------

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

interface MockCall {
  url: string
  init: RequestInit
}

function nthCall(idx: number): MockCall {
  const args = fetchMock.mock.calls[idx]
  if (args === undefined) {
    throw new Error(
      `fetch was not called ${idx + 1} time(s); recorded ${fetchMock.mock.calls.length}`,
    )
  }
  return { url: String(args[0]), init: (args[1] as RequestInit | undefined) ?? {} }
}

beforeEach(() => {
  responseQueue = []
  fetchMock = vi.fn(async () => {
    const next = responseQueue.shift()
    if (next === undefined) return jsonResponse({})
    return next
  })
  ;(globalThis as { fetch: typeof fetch }).fetch = fetchMock as unknown as typeof fetch
  process.env.AF_STACK_URL = "http://test.local"
  process.env.AF_STACK_API_KEY = "test-key"
})

afterEach(() => {
  vi.restoreAllMocks()
  delete process.env.AF_STACK_URL
  delete process.env.AF_STACK_API_KEY
})

function deliveryRow(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "wd_123",
    endpoint_id: null,
    direction: "outbound",
    destination: "https://example.com/hooks",
    event_type: "user.created",
    status: "queued",
    attempts: 0,
    response_status: null,
    response_ms: null,
    body_preview: '{"name":"alice"}',
    body_storage_url: null,
    last_error: null,
    scheduled_at: "2026-06-06T12:00:00Z",
    delivered_at: null,
    created_at: "2026-06-06T12:00:00Z",
    ...overrides,
  }
}

// ---------- send ----------

describe("webhooks.send", () => {
  it("POSTs the payload with defaults", async () => {
    enqueueResponse(jsonResponse(deliveryRow()))
    const delivery = await sendWebhook(
      "https://example.com/hooks",
      "user.created",
      { name: "alice" },
    )

    expect(delivery.id).toBe("wd_123")
    expect(delivery.direction).toBe("outbound")
    expect(delivery.status).toBe("queued")

    const call = nthCall(0)
    expect(call.url).toBe("http://test.local/api/v1/webhooks/send")
    expect(call.init.method).toBe("POST")
    const body = JSON.parse(String(call.init.body))
    expect(body.url).toBe("https://example.com/hooks")
    expect(body.event_type).toBe("user.created")
    expect(body.body).toEqual({ name: "alice" })
    expect(body.immediate).toBe(false)
    expect("headers" in body).toBe(false)
  })

  it("forwards headers and immediate flag", async () => {
    enqueueResponse(
      jsonResponse(
        deliveryRow({
          status: "succeeded",
          attempts: 1,
          response_status: 200,
          response_ms: 42,
        }),
      ),
    )
    const delivery = await sendWebhook(
      "https://example.com/hooks",
      "user.created",
      { id: 1 },
      { headers: { "X-Signature": "abc" }, immediate: true },
    )

    expect(delivery.status).toBe("succeeded")
    expect(delivery.responseStatus).toBe(200)

    const call = nthCall(0)
    const body = JSON.parse(String(call.init.body))
    expect(body.headers).toEqual({ "X-Signature": "abc" })
    expect(body.immediate).toBe(true)
  })

  it("validates url + event_type", async () => {
    await expect(sendWebhook("", "user.created", {})).rejects.toThrow(/url/)
    await expect(
      sendWebhook("https://example.com/hooks", "", {}),
    ).rejects.toThrow(/event_type/)
  })
})

// ---------- list ----------

describe("webhooks.list", () => {
  it("returns paginated deliveries", async () => {
    enqueueResponse(
      jsonResponse({
        deliveries: [deliveryRow(), deliveryRow({ id: "wd_124", direction: "inbound" })],
        total: 2,
        has_more: false,
      }),
    )
    const page = await listWebhookDeliveries()
    expect(page.total).toBe(2)
    expect(page.hasMore).toBe(false)
    expect(page.deliveries).toHaveLength(2)
    expect(page.deliveries[1].direction).toBe("inbound")
  })

  it("passes filters as query params", async () => {
    enqueueResponse(
      jsonResponse({ deliveries: [], total: 0, has_more: false }),
    )
    await listWebhookDeliveries({
      direction: "outbound",
      status: "failed",
      endpointId: "ep_1",
      tenant: "t_xyz",
      limit: 10,
      offset: 20,
    })

    const call = nthCall(0)
    expect(call.url).toContain("/webhooks/deliveries?")
    expect(call.url).toContain("direction=outbound")
    expect(call.url).toContain("status=failed")
    expect(call.url).toContain("endpoint_id=ep_1")
    expect(call.url).toContain("tenant=t_xyz")
    expect(call.url).toContain("limit=10")
    expect(call.url).toContain("offset=20")
  })

  it("rejects invalid direction", async () => {
    await expect(
      listWebhookDeliveries({ direction: "sideways" }),
    ).rejects.toThrow(/direction/)
  })

  it("rejects invalid paging", async () => {
    await expect(listWebhookDeliveries({ limit: 0 })).rejects.toThrow(/limit/)
    await expect(listWebhookDeliveries({ offset: -1 })).rejects.toThrow(/offset/)
  })
})

// ---------- get ----------

describe("webhooks.get", () => {
  it("returns a single row", async () => {
    enqueueResponse(jsonResponse(deliveryRow()))
    const row = await getWebhookDelivery("wd_123")
    expect(row.id).toBe("wd_123")
    const call = nthCall(0)
    expect(call.url).toBe("http://test.local/api/v1/webhooks/deliveries/wd_123")
  })

  it("validates id", async () => {
    await expect(getWebhookDelivery("")).rejects.toThrow(/id/)
  })

  it("raises a SuiteError on 404", async () => {
    enqueueResponse(
      errorResponse(
        { error: { code: "NOT_FOUND", message: "delivery missing" } },
        404,
      ),
    )
    await expect(getWebhookDelivery("missing")).rejects.toBeInstanceOf(SuiteError)
  })
})

// ---------- retry ----------

describe("webhooks.retry", () => {
  it("returns the refreshed row", async () => {
    enqueueResponse(
      jsonResponse(
        deliveryRow({ status: "queued", attempts: 0, last_error: null }),
      ),
    )
    const row = await retryWebhookDelivery("wd_123")
    expect(row.status).toBe("queued")
    expect(row.attempts).toBe(0)
    expect(row.lastError).toBe(null)

    const call = nthCall(0)
    expect(call.url).toBe(
      "http://test.local/api/v1/webhooks/deliveries/wd_123/retry",
    )
    expect(call.init.method).toBe("POST")
  })

  it("validates id", async () => {
    await expect(retryWebhookDelivery("")).rejects.toThrow(/id/)
  })
})

// ---------- suite namespace ----------

describe("suite.webhooks", () => {
  it("is the same namespace as the named export", () => {
    expect(suite.webhooks).toBe(webhooks)
  })
})

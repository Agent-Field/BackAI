import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  notifications,
  sendNotification,
  sendEmail,
  listNotifications,
  getNotification,
  notificationStats,
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

function row(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "ntf_123",
    tenant_id: "00000000-0000-0000-0000-000000000000",
    kind: "email",
    adapter: "log",
    template: "default",
    to: "user@example.com",
    from: "noreply@af-stack.local",
    subject: "Hello",
    data: { body: "hi" },
    status: "queued",
    provider_message_id: null,
    attempts: 0,
    last_error: null,
    scheduled_at: "2026-06-06T12:00:00Z",
    sent_at: null,
    created_at: "2026-06-06T12:00:00Z",
    ...overrides,
  }
}

// ---------- tests ----------

describe("notifications.send", () => {
  it("POSTs to /notifications and parses the row to camelCase", async () => {
    enqueueResponse(jsonResponse(row()))
    const result = await notifications.send({ to: "user@example.com" })

    expect(result.id).toBe("ntf_123")
    expect(result.kind).toBe("email")
    expect(result.template).toBe("default")
    expect(result.tenantId).toBe("00000000-0000-0000-0000-000000000000")
    expect(result.providerMessageId).toBeNull()

    const call = nthCall(0)
    expect(call.url).toBe("http://test.local/api/v1/notifications")
    expect(call.init.method).toBe("POST")
    const body = JSON.parse(call.init.body as string) as Record<string, unknown>
    expect(body.to).toBe("user@example.com")
    expect(body.kind).toBe("email")
    expect(body.template).toBe("default")
    expect(body.subject).toBeUndefined()
    expect(body.data).toBeUndefined()
  })

  it("forwards subject, from, data and scheduled_at when provided", async () => {
    enqueueResponse(jsonResponse(row()))
    await notifications.send({
      to: "user@example.com",
      template: "welcome",
      subject: "Welcome!",
      from: "hello@af.dev",
      data: { name: "Ada" },
      scheduledAt: "2026-06-07T10:00:00Z",
    })

    const body = JSON.parse(nthCall(0).init.body as string) as Record<string, unknown>
    expect(body.subject).toBe("Welcome!")
    expect(body.from).toBe("hello@af.dev")
    expect(body.data).toEqual({ name: "Ada" })
    expect(body.scheduled_at).toBe("2026-06-07T10:00:00Z")
  })

  it("validates inputs before fetching", async () => {
    await expect(notifications.send({ to: "" })).rejects.toThrow(/non-empty/)
    await expect(
      notifications.send({ to: "user@example.com", template: "" }),
    ).rejects.toThrow(/template/)
    await expect(
      notifications.send({ to: "user@example.com", kind: "webhook" as unknown as "email" }),
    ).rejects.toThrow(/kind/)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it("throws SuiteError on a 503", async () => {
    enqueueResponse(
      errorResponse(
        {
          error: {
            code: "NOTIFICATIONS_NOT_CONFIGURED",
            message: "notifications module is not configured",
          },
        },
        503,
      ),
    )
    await expect(notifications.send({ to: "x@x.com" })).rejects.toBeInstanceOf(
      SuiteError,
    )
  })
})

describe("notifications.email", () => {
  it("folds body into data.body", async () => {
    enqueueResponse(jsonResponse(row()))
    await notifications.email("user@example.com", "Subject", {
      body: "welcome to AF Stack",
    })
    const body = JSON.parse(nthCall(0).init.body as string) as Record<string, unknown>
    expect(body.kind).toBe("email")
    expect(body.subject).toBe("Subject")
    expect(body.data).toEqual({ body: "welcome to AF Stack" })
  })

  it("requires a non-empty subject", async () => {
    await expect(
      notifications.email("user@example.com", ""),
    ).rejects.toThrow(/subject/)
  })
})

describe("notifications.list", () => {
  it("sends filters as query params and camelizes the list", async () => {
    enqueueResponse(
      jsonResponse({
        notifications: [row({ id: "a" }), row({ id: "b" })],
        total: 2,
        has_more: false,
      }),
    )
    const result = await notifications.list({
      tenant: "t_x",
      status: "sent",
      kind: "email",
      limit: 10,
      offset: 20,
    })

    expect(result.notifications.map((n) => n.id)).toEqual(["a", "b"])
    expect(result.hasMore).toBe(false)
    expect(nthCall(0).url).toContain("tenant=t_x")
    expect(nthCall(0).url).toContain("status=sent")
    expect(nthCall(0).url).toContain("kind=email")
    expect(nthCall(0).url).toContain("limit=10")
    expect(nthCall(0).url).toContain("offset=20")
  })

  it("rejects invalid limit/offset before fetching", async () => {
    await expect(notifications.list({ limit: 0 })).rejects.toThrow(/limit/)
    await expect(notifications.list({ offset: -1 })).rejects.toThrow(/offset/)
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

describe("notifications.get", () => {
  it("fetches by id and camelizes the row", async () => {
    enqueueResponse(jsonResponse(row({ status: "sent" })))
    const result = await notifications.get("ntf_123")
    expect(result.status).toBe("sent")
    expect(nthCall(0).url).toBe("http://test.local/api/v1/notifications/ntf_123")
  })

  it("rejects empty id", async () => {
    await expect(notifications.get("")).rejects.toThrow(/non-empty/)
  })
})

describe("notifications.stats", () => {
  it("returns the KPI aggregates", async () => {
    enqueueResponse(
      jsonResponse({
        by_status: { sent: 12, failed: 1, queued: 3 },
        by_adapter: [{ adapter: "log", count: 16 }],
        sent_today: 12,
        failed_today: 1,
      }),
    )
    const result = await notifications.stats()
    expect(result.byStatus).toEqual({ sent: 12, failed: 1, queued: 3 })
    expect(result.byAdapter).toHaveLength(1)
    expect(result.byAdapter[0]).toEqual({ adapter: "log", count: 16 })
    expect(result.sentToday).toBe(12)
    expect(result.failedToday).toBe(1)
  })
})

describe("namespace", () => {
  it("suite.notifications mirrors the module exports", () => {
    expect(suite.notifications).toBe(notifications)
    expect(suite.notifications.send).toBe(notifications.send)
    expect(suite.notifications.email).toBe(notifications.email)
    expect(suite.notifications.list).toBe(notifications.list)
    expect(suite.notifications.get).toBe(notifications.get)
    expect(suite.notifications.stats).toBe(notifications.stats)
  })

  it("exposes the top-level send/email/list/get/stats helpers", () => {
    expect(sendNotification).toBe(notifications.send)
    expect(sendEmail).toBe(notifications.email)
    expect(listNotifications).toBe(notifications.list)
    expect(getNotification).toBe(notifications.get)
    expect(notificationStats).toBe(notifications.stats)
  })
})

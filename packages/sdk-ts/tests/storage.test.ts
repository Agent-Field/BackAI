import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { storage, suite, SuiteError } from "../src/index.js"

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

function binaryResponse(bytes: Uint8Array): Response {
  return new Response(bytes, {
    status: 200,
    headers: { "content-type": "application/octet-stream" },
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

function objRow(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    key: "reports/q2.pdf",
    size: 11,
    content_type: "application/pdf",
    last_modified: "2026-06-06T12:00:00Z",
    etag: "abc123",
    ...overrides,
  }
}

// ---------- upload ----------

describe("storage.upload", () => {
  it("POSTs multipart form-data with key + file and parses object metadata", async () => {
    enqueue(jsonResponse(objRow()))
    const payload = new TextEncoder().encode("hello world")
    const obj = await storage.upload(payload, "reports/q2.pdf", {
      contentType: "application/pdf",
    })

    expect(obj.key).toBe("reports/q2.pdf")
    expect(obj.size).toBe(11)
    expect(obj.contentType).toBe("application/pdf")
    expect(obj.lastModified).toBe("2026-06-06T12:00:00Z")
    expect(obj.etag).toBe("abc123")

    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/storage/upload")
    expect(c.init.method).toBe("POST")
    expect(c.init.body).toBeInstanceOf(FormData)
    const form = c.init.body as FormData
    expect(form.get("key")).toBe("reports/q2.pdf")
    const filePart = form.get("file")
    expect(filePart).toBeInstanceOf(Blob)

    // We deliberately don't set content-type — the runtime adds it with the
    // multipart boundary. Authorization must still flow through.
    const headers = c.init.headers as Record<string, string>
    expect(headers.authorization).toBe("Bearer test-key")
    expect(headers["content-type"]).toBeUndefined()
  })

  it("accepts a string body and sends as text/plain blob", async () => {
    enqueue(jsonResponse(objRow({ content_type: "text/plain", size: 5 })))
    const obj = await storage.upload("hello", "notes/hi.txt", {
      contentType: "text/plain",
    })
    expect(obj.size).toBe(5)
    const form = nthCall(0).init.body as FormData
    const part = form.get("file") as Blob
    expect(part.type).toBe("text/plain")
  })

  it("throws on empty key", async () => {
    await expect(storage.upload(new Uint8Array(), "")).rejects.toThrow(/non-empty/i)
  })

  it("surfaces structured errors as SuiteError", async () => {
    enqueue(
      errorResponse(
        {
          error: {
            code: "STORAGE_FULL",
            message: "tenant quota exceeded",
            request_id: "req_x",
          },
        },
        413,
      ),
    )
    const err = await storage
      .upload(new Uint8Array([1, 2, 3]), "k")
      .catch((e) => e as unknown)
    expect(err).toBeInstanceOf(SuiteError)
    expect((err as SuiteError).code).toBe("STORAGE_FULL")
    expect((err as SuiteError).status).toBe(413)
  })
})

// ---------- download ----------

describe("storage.download", () => {
  it("GETs /storage/{key} and returns bytes", async () => {
    const payload = new Uint8Array([0, 1, 2, 250, 255])
    enqueue(binaryResponse(payload))
    const bytes = await storage.download("reports/q2.pdf")
    expect(Array.from(bytes)).toEqual([0, 1, 2, 250, 255])
    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/storage/reports%2Fq2.pdf")
    expect(c.init.method).toBe("GET")
    const headers = c.init.headers as Record<string, string>
    expect(headers.accept).toBe("application/octet-stream")
  })

  it("throws on empty key", async () => {
    await expect(storage.download("")).rejects.toThrow(/non-empty/i)
  })
})

// ---------- signedURL ----------

describe("storage.signedURL", () => {
  it("GETs /storage/signed-url with key and ttl query params", async () => {
    enqueue(
      jsonResponse({
        key: "exports/run-1.csv",
        url: "https://signed.example.com/abc?sig=xyz",
        expires_at: "2026-06-06T13:00:00Z",
      }),
    )
    const signed = await storage.signedURL("exports/run-1.csv", 7200)
    expect(signed.key).toBe("exports/run-1.csv")
    expect(signed.url).toContain("signed.example.com")
    expect(signed.expiresAt).toBe("2026-06-06T13:00:00Z")

    const url = new URL(nthCall(0).url)
    expect(url.pathname).toBe("/api/v1/storage/signed-url")
    expect(url.searchParams.get("key")).toBe("exports/run-1.csv")
    expect(url.searchParams.get("ttl")).toBe("7200")
  })

  it("defaults ttl to 3600", async () => {
    enqueue(
      jsonResponse({
        key: "k",
        url: "https://signed.example.com/x",
        expires_at: "2026-06-06T13:00:00Z",
      }),
    )
    await storage.signedURL("k")
    expect(new URL(nthCall(0).url).searchParams.get("ttl")).toBe("3600")
  })

  it("throws on non-positive ttl", async () => {
    await expect(storage.signedURL("k", 0)).rejects.toThrow(/positive/i)
    await expect(storage.signedURL("k", -1)).rejects.toThrow(/positive/i)
  })
})

// ---------- delete ----------

describe("storage.delete", () => {
  it("DELETEs /storage/{key} and returns true on success", async () => {
    enqueue(jsonResponse({ deleted: true }))
    const ok = await storage.delete("junk.txt")
    expect(ok).toBe(true)
    const c = nthCall(0)
    expect(c.init.method).toBe("DELETE")
    expect(c.url).toBe("http://test.local/api/v1/storage/junk.txt")
  })

  it("returns false when runtime reports no-op", async () => {
    enqueue(jsonResponse({ deleted: false }))
    expect(await storage.delete("missing.txt")).toBe(false)
  })
})

// ---------- list ----------

describe("storage.list", () => {
  it("GETs /storage and parses paginated response", async () => {
    enqueue(
      jsonResponse({
        objects: [objRow({ key: "reports/q1.pdf" }), objRow({ key: "reports/q2.pdf" })],
        prefix: "reports/",
        next_token: "page_2",
      }),
    )
    const listing = await storage.list({ prefix: "reports/", limit: 10 })
    expect(listing.prefix).toBe("reports/")
    expect(listing.nextToken).toBe("page_2")
    expect(listing.objects.map((o) => o.key)).toEqual([
      "reports/q1.pdf",
      "reports/q2.pdf",
    ])

    const url = new URL(nthCall(0).url)
    expect(url.pathname).toBe("/api/v1/storage")
    expect(url.searchParams.get("prefix")).toBe("reports/")
    expect(url.searchParams.get("limit")).toBe("10")
  })

  it("omits empty prefix and defaults limit to 100", async () => {
    enqueue(
      jsonResponse({ objects: [], prefix: "", next_token: null }),
    )
    await storage.list()
    const url = new URL(nthCall(0).url)
    expect(url.searchParams.has("prefix")).toBe(false)
    expect(url.searchParams.get("limit")).toBe("100")
  })
})

describe("namespace export", () => {
  it("suite.storage matches module", () => {
    expect(suite.storage).toBe(storage)
    expect(suite.storage.upload).toBe(storage.upload)
    expect(suite.storage.signedURL).toBe(storage.signedURL)
    expect(suite.storage.delete).toBe(storage.delete)
  })
})

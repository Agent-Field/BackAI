import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  memory,
  getMemory,
  putMemory,
  deleteMemory,
  listMemory,
  searchMemory,
  suite,
  SuiteError,
} from "../src/index.js"

// ---------- fetch mock plumbing (shape matches jobs.test.ts) ----------

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

function entryRow(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    scope: "tenant",
    scope_id: "t-acme",
    key: "user:42:prefs",
    value: { theme: "dark", lang: "en" },
    metadata: { source: "settings-form" },
    has_embedding: false,
    created_at: "2026-06-06T12:00:00Z",
    updated_at: "2026-06-06T12:00:00Z",
    ...overrides,
  }
}

// ---------- tests ----------

describe("memory.get", () => {
  it("GETs /memory/get with scope, key, scope_id query params", async () => {
    enqueueResponse(jsonResponse(entryRow()))
    const entry = await memory.get("tenant", "user:42:prefs", "t-acme")

    expect(entry.scope).toBe("tenant")
    expect(entry.scopeId).toBe("t-acme")
    expect(entry.key).toBe("user:42:prefs")
    expect(entry.value).toEqual({ theme: "dark", lang: "en" })
    expect(entry.metadata).toEqual({ source: "settings-form" })
    expect(entry.hasEmbedding).toBe(false)
    expect(entry.createdAt).toBe("2026-06-06T12:00:00Z")

    const url = new URL(nthCall(0).url)
    expect(url.pathname).toBe("/api/v1/memory/get")
    expect(url.searchParams.get("scope")).toBe("tenant")
    expect(url.searchParams.get("key")).toBe("user:42:prefs")
    expect(url.searchParams.get("scope_id")).toBe("t-acme")
  })

  it("omits scope_id when not supplied", async () => {
    enqueueResponse(jsonResponse(entryRow({ scope: "global", scope_id: null })))
    await memory.get("global", "feature_flag")
    const url = new URL(nthCall(0).url)
    expect(url.searchParams.get("scope")).toBe("global")
    expect(url.searchParams.get("key")).toBe("feature_flag")
    expect(url.searchParams.has("scope_id")).toBe(false)
  })

  it("throws on invalid scope or empty key", async () => {
    await expect(memory.get("bogus", "k")).rejects.toThrow(/scope/)
    await expect(memory.get("tenant", "")).rejects.toThrow(/non-empty/i)
  })

  it("propagates structured errors as SuiteError", async () => {
    enqueueResponse(
      errorResponse(
        {
          error: {
            code: "MEMORY_NOT_FOUND",
            message: "no such key",
            request_id: "req_x",
          },
        },
        404,
      ),
    )
    const err = await memory.get("tenant", "missing", "t-acme").catch((e) => e as unknown)
    expect(err).toBeInstanceOf(SuiteError)
    expect((err as SuiteError).code).toBe("MEMORY_NOT_FOUND")
    expect((err as SuiteError).status).toBe(404)
  })
})

describe("memory.put", () => {
  it("PUTs /memory with embed=true forwarded in the request body", async () => {
    enqueueResponse(jsonResponse(entryRow({ has_embedding: true })))
    const entry = await memory.put(
      "tenant",
      "user:42:prefs",
      { theme: "dark", lang: "en" },
      {
        scopeId: "t-acme",
        metadata: { source: "settings-form" },
        embed: true,
      },
    )
    expect(entry.hasEmbedding).toBe(true)

    const c = nthCall(0)
    expect(c.init.method).toBe("PUT")
    expect(new URL(c.url).pathname).toBe("/api/v1/memory")
    const body = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(body.scope).toBe("tenant")
    expect(body.scope_id).toBe("t-acme")
    expect(body.key).toBe("user:42:prefs")
    expect(body.value).toEqual({ theme: "dark", lang: "en" })
    expect(body.metadata).toEqual({ source: "settings-form" })
    expect(body.embed).toBe(true)
  })

  it("defaults embed=false and omits unset optionals", async () => {
    enqueueResponse(jsonResponse(entryRow()))
    await memory.put("global", "feature_flag", "enabled")
    const body = JSON.parse(nthCall(0).init.body as string) as Record<string, unknown>
    expect(body.scope).toBe("global")
    expect(body.key).toBe("feature_flag")
    expect(body.value).toBe("enabled")
    expect(body.embed).toBe(false)
    expect(body).not.toHaveProperty("scope_id")
    expect(body).not.toHaveProperty("metadata")
  })

  it("throws on invalid scope or empty key", async () => {
    await expect(memory.put("bogus", "k", "v")).rejects.toThrow(/scope/)
    await expect(memory.put("tenant", "", "v")).rejects.toThrow(/non-empty/i)
  })
})

describe("memory.delete", () => {
  it("DELETEs /memory with scope, key, scope_id query params", async () => {
    enqueueResponse(jsonResponse({ deleted: true }))
    const ok = await memory.delete("tenant", "user:42:prefs", "t-acme")
    expect(ok).toBe(true)
    const c = nthCall(0)
    expect(c.init.method).toBe("DELETE")
    const url = new URL(c.url)
    expect(url.pathname).toBe("/api/v1/memory")
    expect(url.searchParams.get("scope")).toBe("tenant")
    expect(url.searchParams.get("key")).toBe("user:42:prefs")
    expect(url.searchParams.get("scope_id")).toBe("t-acme")
  })
})

describe("memory.list", () => {
  it("forwards filters as query params and parses paginated response", async () => {
    enqueueResponse(
      jsonResponse({
        entries: [
          entryRow({ key: "user:1:prefs" }),
          entryRow({ key: "user:2:prefs" }),
        ],
        total: 12,
        has_more: true,
      }),
    )
    const result = await memory.list({
      scope: "tenant",
      scopeId: "t-acme",
      prefix: "user:",
      limit: 25,
      offset: 50,
    })

    expect(result.total).toBe(12)
    expect(result.hasMore).toBe(true)
    expect(result.entries.map((e) => e.key)).toEqual([
      "user:1:prefs",
      "user:2:prefs",
    ])

    const url = new URL(nthCall(0).url)
    expect(url.pathname).toBe("/api/v1/memory")
    expect(url.searchParams.get("scope")).toBe("tenant")
    expect(url.searchParams.get("scope_id")).toBe("t-acme")
    expect(url.searchParams.get("prefix")).toBe("user:")
    expect(url.searchParams.get("limit")).toBe("25")
    expect(url.searchParams.get("offset")).toBe("50")
  })

  it("omits unset filters", async () => {
    enqueueResponse(jsonResponse({ entries: [], total: 0, has_more: false }))
    await memory.list()
    const url = new URL(nthCall(0).url)
    expect(url.searchParams.has("scope")).toBe(false)
    expect(url.searchParams.has("scope_id")).toBe(false)
    expect(url.searchParams.has("prefix")).toBe(false)
    expect(url.searchParams.get("limit")).toBe("100")
    expect(url.searchParams.get("offset")).toBe("0")
  })

  it("rejects invalid pagination", async () => {
    await expect(memory.list({ limit: 0 })).rejects.toThrow(/limit/)
    await expect(memory.list({ offset: -1 })).rejects.toThrow(/offset/)
  })
})

describe("memory.search", () => {
  it("POSTs /memory/search and returns hits in similarity order", async () => {
    enqueueResponse(
      jsonResponse({
        hits: [
          {
            entry: entryRow({ key: "doc:alpha", has_embedding: true }),
            similarity: 0.92,
            distance: 0.08,
          },
          {
            entry: entryRow({ key: "doc:beta", has_embedding: true }),
            similarity: 0.71,
            distance: 0.29,
          },
        ],
        duration_ms: 17,
      }),
    )
    const result = await memory.search("dark mode preferences", {
      scope: "tenant",
      scopeId: "t-acme",
      topK: 5,
      threshold: 0.5,
    })
    expect(result.durationMs).toBe(17)
    expect(result.hits.map((h) => h.entry.key)).toEqual([
      "doc:alpha",
      "doc:beta",
    ])
    const sims = result.hits.map((h) => h.similarity)
    expect(sims).toEqual([...sims].sort((a, b) => b - a))

    const c = nthCall(0)
    expect(c.init.method).toBe("POST")
    expect(new URL(c.url).pathname).toBe("/api/v1/memory/search")
    const body = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(body.query).toBe("dark mode preferences")
    expect(body.scope).toBe("tenant")
    expect(body.scope_id).toBe("t-acme")
    expect(body.top_k).toBe(5)
    expect(body.threshold).toBe(0.5)
  })

  it("omits unset filters and defaults topK=10", async () => {
    enqueueResponse(jsonResponse({ hits: [], duration_ms: 1 }))
    await memory.search("hello")
    const body = JSON.parse(nthCall(0).init.body as string) as Record<string, unknown>
    expect(body.query).toBe("hello")
    expect(body.top_k).toBe(10)
    expect(body).not.toHaveProperty("scope")
    expect(body).not.toHaveProperty("scope_id")
    expect(body).not.toHaveProperty("threshold")
  })

  it("validates query, topK, and threshold ranges", async () => {
    await expect(memory.search("")).rejects.toThrow(/non-empty/i)
    await expect(memory.search("q", { topK: 0 })).rejects.toThrow(/between 1 and 50/i)
    await expect(memory.search("q", { topK: 999 })).rejects.toThrow(/between 1 and 50/i)
    await expect(memory.search("q", { threshold: -0.1 })).rejects.toThrow(
      /between 0 and 1/i,
    )
    await expect(memory.search("q", { threshold: 1.5 })).rejects.toThrow(
      /between 0 and 1/i,
    )
  })
})

describe("named exports", () => {
  it("exports getMemory, putMemory, deleteMemory, listMemory, searchMemory aliases", () => {
    expect(getMemory).toBe(memory.get)
    expect(putMemory).toBe(memory.put)
    expect(deleteMemory).toBe(memory.delete)
    expect(listMemory).toBe(memory.list)
    expect(searchMemory).toBe(memory.search)
  })
})

describe("namespace export", () => {
  it("suite.memory.* matches the module helpers", () => {
    expect(suite.memory).toBe(memory)
    expect(suite.memory.get).toBe(memory.get)
    expect(suite.memory.put).toBe(memory.put)
    expect(suite.memory.delete).toBe(memory.delete)
    expect(suite.memory.list).toBe(memory.list)
    expect(suite.memory.search).toBe(memory.search)
  })
})

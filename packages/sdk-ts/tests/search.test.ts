// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  deleteSearchDocument,
  search,
  searchIndex,
  suite,
  upsertSearchDocument,
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

function doc(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    tenant_id: "00000000-0000-0000-0000-000000000000",
    namespace: "notes",
    key: "note_1",
    title: "Launch notes",
    body: "Ship the realtime and search backend primitives.",
    metadata: { type: "note" },
    has_embedding: true,
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

describe("search", () => {
  it("POSTs /search with query, mode, namespace, filters, and parses hits", async () => {
    enqueueResponse(
      jsonResponse({
        hits: [
          {
            document: doc(),
            score: 0.031,
            fts_rank: 0.21,
            similarity: 0.88,
          },
        ],
        mode: "hybrid",
        duration_ms: 8,
      }),
    )

    const result = await search("launch search", {
      namespace: "notes",
      metadataFilter: { type: "note" },
      limit: 5,
    })

    expect(result.mode).toBe("hybrid")
    expect(result.durationMs).toBe(8)
    expect(result.hits[0]?.document.tenantId).toBe("00000000-0000-0000-0000-000000000000")
    expect(result.hits[0]?.document.hasEmbedding).toBe(true)
    expect(result.hits[0]?.ftsRank).toBe(0.21)
    expect(result.hits[0]?.similarity).toBe(0.88)

    const c = nthCall(0)
    expect(c.init.method).toBe("POST")
    expect(new URL(c.url).pathname).toBe("/api/v1/search")
    const body = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(body.query).toBe("launch search")
    expect(body.mode).toBe("hybrid")
    expect(body.namespace).toBe("notes")
    expect(body.metadata_filter).toEqual({ type: "note" })
    expect(body.limit).toBe(5)
    expect(body.offset).toBe(0)
  })

  it("accepts shorthand mode as second argument", async () => {
    enqueueResponse(jsonResponse({ hits: [], mode: "fts", duration_ms: 1 }))
    await search("postgres", "fts")
    const body = JSON.parse(nthCall(0).init.body as string) as Record<string, unknown>
    expect(body.mode).toBe("fts")
  })

  it("validates query, mode, limit, and offset", async () => {
    await expect(search("")).rejects.toThrow(/non-empty/i)
    await expect(search("q", "bogus" as never)).rejects.toThrow(/mode/)
    await expect(search("q", { limit: 0 })).rejects.toThrow(/limit/)
    await expect(search("q", { offset: -1 })).rejects.toThrow(/offset/)
  })
})

describe("search document indexing", () => {
  it("PUTs /search/documents and parses the indexed document", async () => {
    enqueueResponse(jsonResponse(doc()))
    const indexed = await upsertSearchDocument("note_1", {
      namespace: "notes",
      title: "Launch notes",
      body: "Ship the realtime and search backend primitives.",
      metadata: { type: "note" },
      embed: true,
    })
    expect(indexed.namespace).toBe("notes")
    expect(indexed.metadata).toEqual({ type: "note" })

    const c = nthCall(0)
    expect(c.init.method).toBe("PUT")
    expect(new URL(c.url).pathname).toBe("/api/v1/search/documents")
    const body = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(body.key).toBe("note_1")
    expect(body.namespace).toBe("notes")
    expect(body.embed).toBe(true)
  })

  it("DELETEs the encoded namespace/key path", async () => {
    enqueueResponse(jsonResponse({ deleted: true }))
    const ok = await deleteSearchDocument("note:1", { namespace: "customer notes" })
    expect(ok).toBe(true)
    const c = nthCall(0)
    expect(c.init.method).toBe("DELETE")
    expect(new URL(c.url).pathname).toBe("/api/v1/search/documents/customer%20notes/note%3A1")
  })

  it("exposes the namespace object and suite alias", async () => {
    enqueueResponse(jsonResponse(doc()))
    await searchIndex.upsert("note_1", { title: "Launch notes" })
    enqueueResponse(jsonResponse({ hits: [], mode: "hybrid", duration_ms: 0 }))
    await suite.search("launch")
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})

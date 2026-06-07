// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  agents,
  approve,
  call,
  callAsync,
  cancel,
  deny,
  pendingApprovals,
  status,
  stream,
  SuiteError,
} from "../src/index.js"
import { withCtx } from "../src/ctx.js"

// ---------- fetch mock plumbing ----------
// We replace globalThis.fetch with a vi.fn that pulls from a per-test
// response queue. Call history lives on `fetchMock.mock.calls`, which
// vitest manages — that way `mockReset()`-style behaviour in beforeEach
// works without losing the implementation.

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

function sseResponse(chunks: string[]): Response {
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      const encoder = new TextEncoder()
      for (const c of chunks) controller.enqueue(encoder.encode(c))
      controller.close()
    },
  })
  return new Response(stream, {
    status: 200,
    headers: { "content-type": "text/event-stream" },
  })
}

function enqueue(response: Response): void {
  responseQueue.push(response)
}

interface MockCall {
  url: string
  init: RequestInit
}

function nthCall(idx: number): MockCall {
  const args = fetchMock.mock.calls[idx]
  if (args === undefined) {
    throw new Error(`fetch was not called ${idx + 1} time(s); recorded ${fetchMock.mock.calls.length}`)
  }
  return {
    url: String(args[0]),
    init: (args[1] as RequestInit | undefined) ?? {},
  }
}

beforeEach(() => {
  responseQueue = []
  fetchMock = vi.fn(async () => {
    const next = responseQueue.shift()
    if (next === undefined) return jsonResponse({}) // safe default
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

function headersOf(init: RequestInit): Record<string, string> {
  const h = init.headers
  if (h === undefined) return {}
  if (h instanceof Headers) {
    const out: Record<string, string> = {}
    h.forEach((v, k) => {
      out[k.toLowerCase()] = v
    })
    return out
  }
  if (Array.isArray(h)) {
    const out: Record<string, string> = {}
    for (const [k, v] of h) out[k.toLowerCase()] = v
    return out
  }
  const out: Record<string, string> = {}
  for (const [k, v] of Object.entries(h as Record<string, string>)) {
    out[k.toLowerCase()] = v
  }
  return out
}

// ---------- tests ----------

describe("agents.call", () => {
  it("POSTs to /agents/{ns.func} forwarding input verbatim and camelCasing envelope", async () => {
    enqueue(
      jsonResponse({
        execution_id: "exec_123",
        status: "succeeded",
        output: { result_text: "hi" },
        duration_ms: 42,
      }),
    )

    // Agent input is opaque — keys are part of the agent's own contract,
    // so the SDK must forward them verbatim. Envelope fields (execution_id,
    // duration_ms, etc.) are SDK-defined and translated to camelCase.
    const result = await call("notable-ai.summarize", { page_id: "p-1", max_tokens: 100 })

    expect(result.executionId).toBe("exec_123")
    expect(result.status).toBe("succeeded")
    expect(result.durationMs).toBe(42)
    expect(result.output).toEqual({ result_text: "hi" })

    expect(fetchMock.mock.calls).toHaveLength(1)
    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/agents/notable-ai.summarize")
    expect(c.init.method).toBe("POST")
    const body = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(body.input).toEqual({ page_id: "p-1", max_tokens: 100 })
    const hdrs = headersOf(c.init)
    expect(hdrs.authorization).toBe("Bearer test-key")
    expect(hdrs["content-type"]).toBe("application/json")
    expect(hdrs["x-request-id"]).toBeDefined()
  })

  it("forwards timeoutS and idempotencyKey as snake_case", async () => {
    enqueue(jsonResponse({ execution_id: "e", status: "succeeded" }))
    await call("a.b", { x: 1 }, { timeoutS: 30, idempotencyKey: "abc" })
    const body = JSON.parse(nthCall(0).init.body as string) as Record<string, unknown>
    expect(body.timeout_s).toBe(30)
    expect(body.idempotency_key).toBe("abc")
  })

  it("uses ctx.requestId when no override is given", async () => {
    enqueue(jsonResponse({ execution_id: "e", status: "succeeded" }))
    await withCtx({ requestId: "r-from-ctx" }, async () => {
      await call("a.b", {})
    })
    expect(headersOf(nthCall(0).init)["x-request-id"]).toBe("r-from-ctx")
  })

  it("throws SuiteError on non-2xx response", async () => {
    enqueue(
      errorResponse(
        {
          error: {
            code: "VALIDATION_FAILED",
            message: "input.x missing",
            request_id: "req_zzz",
            details: { field: "x" },
          },
        },
        422,
      ),
    )
    const err = await call("a.b", {}).catch((e) => e as unknown)
    expect(err).toBeInstanceOf(SuiteError)
    expect((err as SuiteError).code).toBe("VALIDATION_FAILED")
    expect((err as SuiteError).status).toBe(422)
    expect((err as SuiteError).requestId).toBe("req_zzz")
  })

  it("propagates traceparent header", async () => {
    enqueue(jsonResponse({ execution_id: "e", status: "succeeded" }))
    await call("a.b", {}, { traceparent: "00-trace-span-01" })
    expect(headersOf(nthCall(0).init).traceparent).toBe("00-trace-span-01")
  })
})

describe("agents.callAsync", () => {
  it("POSTs to /agents/async/{ns.func} and parses async result", async () => {
    enqueue(jsonResponse({ execution_id: "exec_async", status: "queued" }))
    const result = await callAsync("a.b", { x: 1 }, { webhookUrl: "https://h/cb" })
    expect(result.executionId).toBe("exec_async")
    expect(result.status).toBe("queued")
    expect(nthCall(0).url).toBe("http://test.local/api/v1/agents/async/a.b")
    const body = JSON.parse(nthCall(0).init.body as string) as Record<string, unknown>
    expect(body.webhook_url).toBe("https://h/cb")
  })
})

describe("agents.status / cancel", () => {
  it("GETs /executions/{id}", async () => {
    enqueue(
      jsonResponse({
        execution_id: "exec_42",
        status: "running",
        started_at: "2026-01-01T00:00:00Z",
      }),
    )
    const s = await status("exec_42")
    expect(s.executionId).toBe("exec_42")
    expect(s.status).toBe("running")
    expect(s.startedAt).toBe("2026-01-01T00:00:00Z")
    expect(nthCall(0).url).toBe("http://test.local/api/v1/executions/exec_42")
    expect(nthCall(0).init.method).toBe("GET")
  })

  it("DELETEs /executions/{id} with reason in body", async () => {
    enqueue(new Response(null, { status: 204 }))
    await cancel("exec_42", "user cancelled")
    expect(nthCall(0).init.method).toBe("DELETE")
    expect(nthCall(0).url).toBe("http://test.local/api/v1/executions/exec_42")
    const body = JSON.parse(nthCall(0).init.body as string) as Record<string, unknown>
    expect(body.reason).toBe("user cancelled")
  })
})

describe("agents.approve / deny / pendingApprovals", () => {
  it("approve POSTs to /executions/{id}/approve", async () => {
    enqueue(new Response(null, { status: 204 }))
    await approve("exec_42", { byUserId: "u-1", note: "lgtm" })
    expect(nthCall(0).url).toBe("http://test.local/api/v1/executions/exec_42/approve")
    const body = JSON.parse(nthCall(0).init.body as string) as Record<string, unknown>
    expect(body.by_user_id).toBe("u-1")
    expect(body.note).toBe("lgtm")
  })

  it("deny POSTs to /executions/{id}/deny", async () => {
    enqueue(new Response(null, { status: 204 }))
    await deny("exec_42", { reason: "nope" })
    expect(nthCall(0).url).toBe("http://test.local/api/v1/executions/exec_42/deny")
    const body = JSON.parse(nthCall(0).init.body as string) as Record<string, unknown>
    expect(body.reason).toBe("nope")
  })

  it("pendingApprovals GETs /approvals/pending and unwraps items", async () => {
    enqueue(
      jsonResponse({
        items: [
          { execution_id: "e1", agent: "ns.fn", reason: "policy" },
          { execution_id: "e2", agent: "ns.fn2" },
        ],
      }),
    )
    const items = await pendingApprovals()
    expect(items).toHaveLength(2)
    expect(items[0].executionId).toBe("e1")
    expect(items[1].agent).toBe("ns.fn2")
  })
})

describe("agents.stream", () => {
  it("yields parsed SSE events", async () => {
    enqueue(
      sseResponse([
        'event: token\ndata: {"text":"hello"}\n\n',
        'event: token\ndata: {"text":" world"}\n\n',
        'event: done\ndata: {"execution_id":"exec_s"}\n\n',
      ]),
    )
    const events = []
    for await (const evt of stream("a.b", { x: 1 })) events.push(evt)
    expect(events).toHaveLength(3)
    expect(events[0].event).toBe("token")
    expect(events[0].data).toEqual({ text: "hello" })
    expect(events[2].event).toBe("done")
    expect((events[2].data as { execution_id: string }).execution_id).toBe("exec_s")

    expect(nthCall(0).url).toBe("http://test.local/api/v1/agents/stream/a.b")
    expect(headersOf(nthCall(0).init).accept).toBe("text/event-stream")
  })

  it("handles records split across multiple chunks", async () => {
    enqueue(
      sseResponse([
        'event: token\ndata: {"te',
        'xt":"hi"}\n\nevent: done\ndata: {}\n\n',
      ]),
    )
    const events = []
    for await (const evt of stream("a.b", {})) events.push(evt)
    expect(events).toHaveLength(2)
    expect(events[0].data).toEqual({ text: "hi" })
  })
})

describe("namespace export", () => {
  it("agents.* matches the top-level helpers", () => {
    expect(agents.call).toBe(call)
    expect(agents.callAsync).toBe(callAsync)
    expect(agents.stream).toBe(stream)
    expect(agents.status).toBe(status)
    expect(agents.cancel).toBe(cancel)
    expect(agents.approve).toBe(approve)
    expect(agents.deny).toBe(deny)
    expect(agents.pendingApprovals).toBe(pendingApprovals)
  })
})
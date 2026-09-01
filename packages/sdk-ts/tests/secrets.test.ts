// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { secrets, suite, SuiteError } from "../src/index.js"

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

function enqueue(response: Response): void {
  responseQueue.push(response)
}

interface MockCall {
  url: string
  init: RequestInit
}
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

describe("secrets.get", () => {
  it("POSTs to /vault/secrets/{key}/reveal and returns plaintext", async () => {
    enqueue(jsonResponse({ key: "OPENAI_API_KEY", value: "sk-very-secret" }))
    const v = await secrets.get("OPENAI_API_KEY")
    expect(v).toBe("sk-very-secret")
    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/vault/secrets/OPENAI_API_KEY/reveal")
    expect(c.init.method).toBe("POST")
  })

  it("URL-encodes keys containing slashes", async () => {
    enqueue(jsonResponse({ key: "team/deploy-token", value: "v" }))
    const v = await secrets.get("team/deploy-token")
    expect(v).toBe("v")
    expect(nthCall(0).url).toBe("http://test.local/api/v1/vault/secrets/team%2Fdeploy-token/reveal")
  })

  it("throws SuiteError on unauthorised reveal", async () => {
    enqueue(
      errorResponse(
        {
          error: {
            code: "FORBIDDEN",
            message: "not authorised",
            request_id: "req_x",
          },
        },
        403,
      ),
    )
    const err = await secrets.get("SOME_KEY").catch((e) => e as unknown)
    expect(err).toBeInstanceOf(SuiteError)
    expect((err as SuiteError).code).toBe("FORBIDDEN")
    expect((err as SuiteError).status).toBe(403)
  })

  it("throws on empty key", async () => {
    await expect(secrets.get("")).rejects.toThrow(/non-empty/i)
  })

  it("throws on malformed body (missing value)", async () => {
    enqueue(jsonResponse({ key: "K" }))
    await expect(secrets.get("K")).rejects.toThrow()
  })
})

describe("admin verbs absent from main SDK", () => {
  it("only exposes get", () => {
    expect(Object.keys(secrets).sort()).toEqual(["get"])
  })
})

describe("namespace export", () => {
  it("suite.secrets matches module", () => {
    expect(suite.secrets).toBe(secrets)
    expect(suite.secrets.get).toBe(secrets.get)
  })
})

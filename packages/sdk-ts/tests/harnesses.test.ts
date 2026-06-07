import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  harnesses,
  listHarnesses,
  getHarness,
  probeHarness,
  suite,
} from "../src/index.js"

// ---------- fetch mock plumbing (shape matches notifications.test.ts) ----

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
    provider: "claude-code",
    is_installed: true,
    binary_path: "/usr/local/bin/claude",
    version: "claude 1.2.3",
    status: "ready",
    last_error: null,
    required_env: ["ANTHROPIC_API_KEY"],
    install_doc_url: "https://docs.claude.com/claude-code",
    ...overrides,
  }
}

// ---------- tests ----------

describe("harnesses.list", () => {
  it("GETs /harnesses and parses to camelCase", async () => {
    enqueueResponse(
      jsonResponse({
        harnesses: [
          row(),
          row({
            provider: "codex",
            status: "needs_auth",
            required_env: ["OPENAI_API_KEY"],
            binary_path: "/usr/bin/codex",
            version: "codex 0.5.0",
            last_error: "required env not set: OPENAI_API_KEY",
            install_doc_url: "https://github.com/openai/codex",
          }),
          row({
            provider: "gemini",
            is_installed: false,
            status: "missing",
            binary_path: null,
            version: null,
            required_env: ["GEMINI_API_KEY"],
            install_doc_url: "https://github.com/google-gemini/gemini-cli",
          }),
          row({
            provider: "opencode",
            status: "ready",
            binary_path: "/usr/bin/opencode",
            version: "opencode 2.0.0",
            required_env: [],
            install_doc_url: "https://opencode.ai",
          }),
        ],
      }),
    )
    const result = await listHarnesses()
    expect(result.harnesses).toHaveLength(4)
    expect(result.harnesses[0]?.provider).toBe("claude-code")
    expect(result.harnesses[0]?.isInstalled).toBe(true)
    expect(result.harnesses[0]?.binaryPath).toBe("/usr/local/bin/claude")
    expect(result.harnesses[0]?.installDocUrl).toBe(
      "https://docs.claude.com/claude-code",
    )
    expect(result.harnesses[2]?.status).toBe("missing")
    expect(result.harnesses[2]?.binaryPath).toBeNull()

    const call = nthCall(0)
    expect(call.url).toBe("http://test.local/api/v1/harnesses")
    expect(call.init.method).toBe("GET")
  })

  it("handles an empty harnesses list", async () => {
    enqueueResponse(jsonResponse({ harnesses: [] }))
    const result = await listHarnesses()
    expect(result.harnesses).toEqual([])
  })
})

describe("harnesses.get", () => {
  it("GETs /harnesses/{provider}", async () => {
    enqueueResponse(jsonResponse(row()))
    const h = await getHarness("claude-code")
    expect(h.provider).toBe("claude-code")
    expect(h.version).toBe("claude 1.2.3")
    expect(h.requiredEnv).toEqual(["ANTHROPIC_API_KEY"])

    const call = nthCall(0)
    expect(call.url).toBe("http://test.local/api/v1/harnesses/claude-code")
    expect(call.init.method).toBe("GET")
  })

  it("rejects unknown providers before calling fetch", async () => {
    await expect(getHarness("nonexistent")).rejects.toThrow(/must be one of/)
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

describe("harnesses.probe", () => {
  it("POSTs to /harnesses/{provider}/probe and returns the fresh row", async () => {
    enqueueResponse(
      jsonResponse(
        row({
          status: "needs_auth",
          last_error: "required env not set: ANTHROPIC_API_KEY",
        }),
      ),
    )
    const h = await probeHarness("claude-code")
    expect(h.status).toBe("needs_auth")
    expect(h.lastError).toBe("required env not set: ANTHROPIC_API_KEY")

    const call = nthCall(0)
    expect(call.url).toBe(
      "http://test.local/api/v1/harnesses/claude-code/probe",
    )
    expect(call.init.method).toBe("POST")
  })

  it("rejects unknown providers", async () => {
    await expect(probeHarness("bogus")).rejects.toThrow(/must be one of/)
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

describe("suite namespace", () => {
  it("exposes suite.harnesses with list/get/probe", () => {
    expect(suite.harnesses).toBe(harnesses)
    expect(typeof suite.harnesses.list).toBe("function")
    expect(typeof suite.harnesses.get).toBe("function")
    expect(typeof suite.harnesses.probe).toBe("function")
  })
})

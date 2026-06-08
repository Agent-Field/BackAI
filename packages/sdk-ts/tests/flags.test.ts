// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  flags,
  isFeatureFlagEnabled,
  listFeatureFlags,
  setFeatureFlag,
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

function enqueueResponse(response: Response): void {
  responseQueue.push(response)
}

function nthCall(idx: number): { url: string; init: RequestInit } {
  const args = fetchMock.mock.calls[idx]
  if (args === undefined) throw new Error(`fetch call ${idx} missing`)
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

describe("flags", () => {
  it("lists runtime feature flags", async () => {
    enqueueResponse(
      jsonResponse({
        flags: [
          {
            key: "verbose-run-logs",
            label: "Verbose run logs",
            description: "Include tool input/output payloads.",
            enabled: false,
            source: "default",
            metadata: {},
            updated_at: null,
          },
        ],
      }),
    )
    const result = await listFeatureFlags()
    expect(result.flags[0]?.key).toBe("verbose-run-logs")
    expect(result.flags[0]?.updatedAt).toBeNull()
    expect(new URL(nthCall(0).url).pathname).toBe("/api/v1/config/flags")
  })

  it("sets a runtime feature flag", async () => {
    enqueueResponse(
      jsonResponse({
        key: "verbose-run-logs",
        label: "Verbose run logs",
        description: "Include tool input/output payloads.",
        enabled: true,
        source: "db",
        metadata: { scope: "operator" },
        updated_at: "2026-06-07T12:00:00Z",
      }),
    )
    const flag = await setFeatureFlag("verbose-run-logs", true, {
      metadata: { scope: "operator" },
    })
    expect(flag.enabled).toBe(true)
    const c = nthCall(0)
    expect(c.init.method).toBe("PUT")
    expect(new URL(c.url).pathname).toBe("/api/v1/config/flags/verbose-run-logs")
    expect(JSON.parse(c.init.body as string)).toEqual({
      enabled: true,
      metadata: { scope: "operator" },
    })
  })

  it("checks enabled state and exposes suite namespace", async () => {
    enqueueResponse(
      jsonResponse({
        flags: [
          {
            key: "command-palette-recents",
            label: "Command palette recents",
            description: "",
            enabled: true,
            source: "default",
            metadata: {},
            updated_at: null,
          },
        ],
      }),
    )
    await expect(isFeatureFlagEnabled("command-palette-recents")).resolves.toBe(true)
    expect(suite.flags).toBe(flags)
  })
})

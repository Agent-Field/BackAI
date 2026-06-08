// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  oauth,
  oauthAuthorizeUrl,
  oauthConnected,
  oauthDisconnect,
  oauthToken,
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

function bodyOf(call: MockCall): Record<string, unknown> {
  return JSON.parse(String(call.init.body ?? "{}")) as Record<string, unknown>
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

describe("oauth.authorizeUrl", () => {
  it("POSTs scopes and return_to and returns the authorization URL", async () => {
    enqueueResponse(
      jsonResponse({
        provider: "github",
        authorization_url: "https://github.com/login/oauth/authorize?state=s",
        redirect_uri: "http://test.local/oauth/callback/github",
        scopes: ["repo"],
      }),
    )

    const url = await oauth.authorizeUrl("github", {
      scopes: ["repo"],
      returnTo: "http://localhost:3000/integrations",
    })

    expect(url).toBe("https://github.com/login/oauth/authorize?state=s")
    const call = nthCall(0)
    expect(call.url).toBe("http://test.local/api/v1/oauth/github/authorize")
    expect(call.init.method).toBe("POST")
    expect(bodyOf(call)).toEqual({
      scopes: ["repo"],
      return_to: "http://localhost:3000/integrations",
    })
  })

  it("validates provider", async () => {
    await expect(oauth.authorizeUrl("")).rejects.toThrow(/non-empty/)
  })
})

describe("oauth.connected", () => {
  it("parses connection metadata without token bytes", async () => {
    enqueueResponse(
      jsonResponse({
        connections: [
          {
            provider: "google",
            scopes: ["https://www.googleapis.com/auth/drive.readonly"],
            connected_at: "2026-06-07T10:00:00Z",
            expires_at: "2026-06-07T11:00:00Z",
          },
        ],
      }),
    )

    const list = await oauth.connected()

    expect(list.connections).toHaveLength(1)
    expect(list.connections[0]?.provider).toBe("google")
    expect(nthCall(0).url).toBe("http://test.local/api/v1/oauth/connections")
  })
})

describe("oauth.token", () => {
  it("POSTs with the internal header and returns only the access token", async () => {
    enqueueResponse(
      jsonResponse({
        provider: "github",
        user_id: "11111111-1111-1111-1111-111111111111",
        access_token: "gho_secret",
        scopes: ["repo"],
      }),
    )

    const token = await oauth.token("github", { userId: "11111111-1111-1111-1111-111111111111" })

    expect(token).toBe("gho_secret")
    const call = nthCall(0)
    expect(call.url).toBe("http://test.local/api/v1/oauth/token")
    expect(call.init.method).toBe("POST")
    expect((call.init.headers as Record<string, string>)["x-af-stack-internal"]).toBe("1")
    expect(bodyOf(call)).toEqual({
      provider: "github",
      user_id: "11111111-1111-1111-1111-111111111111",
    })
  })
})

describe("oauth.disconnect", () => {
  it("DELETEs the provider connection and optional user id", async () => {
    enqueueResponse(jsonResponse({ disconnected: true }))

    await expect(oauth.disconnect("github", { userId: "user-1" })).resolves.toBe(true)

    const call = nthCall(0)
    expect(call.url).toBe("http://test.local/api/v1/oauth/github?user_id=user-1")
    expect(call.init.method).toBe("DELETE")
  })
})

describe("oauth exports", () => {
  it("is available directly and under suite.oauth", () => {
    expect(suite.oauth).toBe(oauth)
    expect(oauthAuthorizeUrl).toBe(oauth.authorizeUrl)
    expect(oauthConnected).toBe(oauth.connected)
    expect(oauthToken).toBe(oauth.token)
    expect(oauthDisconnect).toBe(oauth.disconnect)
  })
})

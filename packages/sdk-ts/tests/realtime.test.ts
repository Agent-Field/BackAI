// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it } from "vitest"

import { subscribeRealtime } from "../src/index.js"

class MockWebSocket {
  static instances: MockWebSocket[] = []
  readonly url: string
  closed = false

  constructor(url: string) {
    this.url = url
    MockWebSocket.instances.push(this)
  }

  close(): void {
    this.closed = true
  }
}

beforeEach(() => {
  MockWebSocket.instances = []
  process.env.AF_STACK_URL = "https://api.example.com"
  process.env.AF_STACK_API_KEY = "af_test_secret"
})

afterEach(() => {
  delete process.env.AF_STACK_URL
  delete process.env.AF_STACK_API_KEY
})

describe("realtime.subscribe", () => {
  it("builds a tenant-authenticated websocket URL", () => {
    const socket = subscribeRealtime(
      "public.messages",
      { room_id: "r1", archived: false },
      { WebSocket: MockWebSocket as unknown as typeof WebSocket },
    ) as unknown as MockWebSocket

    const url = new URL(socket.url)
    expect(url.protocol).toBe("wss:")
    expect(url.host).toBe("api.example.com")
    expect(url.pathname).toBe("/api/v1/realtime")
    expect(url.searchParams.get("table")).toBe("public.messages")
    expect(url.searchParams.get("api_key")).toBe("af_test_secret")
    expect(JSON.parse(url.searchParams.get("filter") ?? "{}")).toEqual({
      room_id: "r1",
      archived: false,
    })
  })

  it("closes on abort", () => {
    const controller = new AbortController()
    const socket = subscribeRealtime(
      "events",
      {},
      {
        baseUrl: "http://localhost:8080",
        apiKey: "",
        signal: controller.signal,
        WebSocket: MockWebSocket as unknown as typeof WebSocket,
      },
    ) as unknown as MockWebSocket

    expect(new URL(socket.url).protocol).toBe("ws:")
    expect(socket.closed).toBe(false)
    controller.abort()
    expect(socket.closed).toBe(true)
  })

  it("rejects empty table names", () => {
    expect(() =>
      subscribeRealtime("", {}, { WebSocket: MockWebSocket as unknown as typeof WebSocket }),
    ).toThrow("table is required")
  })
})

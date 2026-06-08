// SPDX-License-Identifier: Apache-2.0

import { afterEach, describe, expect, it } from "vitest"
import { runs, subscribeRunEvents, subscribeRuns, suite } from "../src/index.js"

class FakeWebSocket {
  static urls: string[] = []
  public closed = false

  constructor(url: string) {
    FakeWebSocket.urls.push(url)
  }

  close(): void {
    this.closed = true
  }
}

afterEach(() => {
  FakeWebSocket.urls = []
  delete process.env.AF_STACK_URL
  delete process.env.AF_STACK_API_KEY
})

describe("runs.subscribeById (legacy per-run)", () => {
  it("opens the per-run event WebSocket endpoint", () => {
    process.env.AF_STACK_URL = "https://suite.example"
    process.env.AF_STACK_API_KEY = "tk_run"

    runs.subscribeById("run_123", { WebSocket: FakeWebSocket as unknown as typeof WebSocket })

    expect(FakeWebSocket.urls).toHaveLength(1)
    const url = new URL(FakeWebSocket.urls[0])
    expect(url.protocol).toBe("wss:")
    expect(url.host).toBe("suite.example")
    expect(url.pathname).toBe("/api/v1/runs/run_123/events")
    expect(url.searchParams.get("api_key")).toBe("tk_run")
  })

  it("closes immediately when the abort signal is already aborted", () => {
    const ctrl = new AbortController()
    ctrl.abort()
    const socket = runs.subscribeById("run_123", {
      WebSocket: FakeWebSocket as unknown as typeof WebSocket,
      signal: ctrl.signal,
    }) as unknown as FakeWebSocket
    expect(socket.closed).toBe(true)
  })

  it("rejects an empty run id", () => {
    expect(() => runs.subscribeById(" ", {
      WebSocket: FakeWebSocket as unknown as typeof WebSocket,
    })).toThrow(/runId/)
  })

  it("exports the legacy per-run alias", () => {
    expect(subscribeRunEvents).toBe(runs.subscribeById)
  })
})

describe("runs.subscribe (#15 filter-based)", () => {
  it("opens the realtime/runs endpoint with no filter when none is supplied", () => {
    process.env.AF_STACK_URL = "http://localhost:8080"

    runs.subscribe({}, { WebSocket: FakeWebSocket as unknown as typeof WebSocket })

    expect(FakeWebSocket.urls).toHaveLength(1)
    const url = new URL(FakeWebSocket.urls[0])
    expect(url.pathname).toBe("/api/v1/realtime/runs")
    expect(url.protocol).toBe("ws:")
    expect([...url.searchParams.keys()]).toEqual([])
  })

  it("attaches every populated filter as a query parameter", () => {
    process.env.AF_STACK_URL = "https://suite.example"
    process.env.AF_STACK_API_KEY = "tk_subscribe"

    runs.subscribe(
      {
        tenant_id: "t-1",
        user_id: "u-1",
        agent: "notable.summarize",
        run_id: "run-1",
        execution_id: "exec-1",
      },
      { WebSocket: FakeWebSocket as unknown as typeof WebSocket },
    )

    expect(FakeWebSocket.urls).toHaveLength(1)
    const url = new URL(FakeWebSocket.urls[0])
    expect(url.protocol).toBe("wss:")
    expect(url.pathname).toBe("/api/v1/realtime/runs")
    expect(url.searchParams.get("tenant_id")).toBe("t-1")
    expect(url.searchParams.get("user_id")).toBe("u-1")
    expect(url.searchParams.get("agent")).toBe("notable.summarize")
    expect(url.searchParams.get("run_id")).toBe("run-1")
    expect(url.searchParams.get("execution_id")).toBe("exec-1")
    expect(url.searchParams.get("api_key")).toBe("tk_subscribe")
  })

  it("closes immediately when the abort signal is already aborted", () => {
    const ctrl = new AbortController()
    ctrl.abort()
    const socket = runs.subscribe(
      { run_id: "run_123" },
      { WebSocket: FakeWebSocket as unknown as typeof WebSocket, signal: ctrl.signal },
    ) as unknown as FakeWebSocket
    expect(socket.closed).toBe(true)
  })

  it("exports namespace helpers", () => {
    expect(suite.runs).toBe(runs)
    expect(subscribeRuns).toBe(runs.subscribe)
  })
})

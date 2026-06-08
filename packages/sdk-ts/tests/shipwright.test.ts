// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { shipwright, suite } from "../src/index.js"

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

function taskRow(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "task_123",
    tenant_id: "tenant_1",
    user_id: "user_1",
    title: "Add export",
    description: "Add CSV export",
    repo_url: "https://github.com/acme/app",
    status: "running",
    run_id: "exec_123",
    created_at: "2026-06-07T12:00:00Z",
    updated_at: "2026-06-07T12:00:01Z",
    ...overrides,
  }
}

describe("shipwright.create", () => {
  it("POSTs to /shipwright/tasks and parses the AgentField link response", async () => {
    enqueueResponse(jsonResponse({
      task: taskRow(),
      agent_call: "shipwright.build",
      agentfield_url: "http://localhost:8081",
      details_url: "http://localhost:8081/agent-api/executions/exec_123/details",
    }, { status: 202 }))

    const result = await shipwright.create({
      title: "Add export",
      description: "Add CSV export",
      repoUrl: "https://github.com/acme/app",
      harnessProvider: "codex",
      model: "openrouter/google/gemini-2.5-flash",
    })

    expect(result.task.repoUrl).toBe("https://github.com/acme/app")
    expect(result.task.runId).toBe("exec_123")
    expect(result.agentCall).toBe("shipwright.build")
    expect(result.detailsUrl).toContain("exec_123")

    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/shipwright/tasks")
    expect(c.init.method).toBe("POST")
    const body = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(body.repo_url).toBe("https://github.com/acme/app")
    expect(body.harness_provider).toBe("codex")
  })
})

describe("shipwright.list/get/complete", () => {
  it("lists tasks with filters", async () => {
    enqueueResponse(jsonResponse({ tasks: [taskRow()], total: 1, has_more: false }))
    const result = await shipwright.list({ status: "running", limit: 10, offset: 5 })
    expect(result.tasks[0]?.tenantId).toBe("tenant_1")
    const url = new URL(nthCall(0).url)
    expect(url.pathname).toBe("/api/v1/shipwright/tasks")
    expect(url.searchParams.get("status")).toBe("running")
    expect(url.searchParams.get("limit")).toBe("10")
    expect(url.searchParams.get("offset")).toBe("5")
  })

  it("gets one task with patches", async () => {
    enqueueResponse(jsonResponse({
      task: taskRow({ status: "succeeded" }),
      patches: [{
        task_id: "task_123",
        ref: "refs/heads/shipwright/task_123",
        summary: "Implemented export",
        diff_url: "https://github.com/acme/app/pull/1",
        created_at: "2026-06-07T12:10:00Z",
      }],
    }))
    const result = await shipwright.get("task_123")
    expect(result.patches?.[0]?.diffUrl).toBe("https://github.com/acme/app/pull/1")
    expect(nthCall(0).url).toBe("http://test.local/api/v1/shipwright/tasks/task_123")
  })

  it("records completion metadata", async () => {
    enqueueResponse(jsonResponse({ task: taskRow({ status: "succeeded" }), patches: [] }))
    await shipwright.complete("task_123", {
      status: "succeeded",
      ref: "refs/heads/shipwright/task_123",
      summary: "Done",
    })
    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/shipwright/tasks/task_123/complete")
    expect(c.init.method).toBe("POST")
    const body = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(body.ref).toBe("refs/heads/shipwright/task_123")
  })
})

describe("namespace export", () => {
  it("suite.shipwright matches the module helpers", () => {
    expect(suite.shipwright).toBe(shipwright)
    expect(suite.shipwright.create).toBe(shipwright.create)
  })
})

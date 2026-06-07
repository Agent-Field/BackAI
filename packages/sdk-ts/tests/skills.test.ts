// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  skills,
  listSkills,
  installSkill,
  uninstallSkill,
  attachSkill,
  suite,
  SuiteError,
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

function row(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "af-skill://acme/security-audit@1.2.0",
    name: "security-audit",
    version: "1.2.0",
    source: "af-skill://acme/security-audit@1.2.0",
    description: "Adversarial security review overlay.",
    harnesses: ["claude-code", "codex"],
    tags: ["security", "review"],
    tenant_id: null,
    installed_at: "2026-06-07T12:00:00Z",
    ...overrides,
  }
}

// ---------- tests ----------

describe("skills.list", () => {
  it("GETs /skills and camelCases the result", async () => {
    enqueueResponse(jsonResponse({ skills: [row()] }))
    const result = await listSkills()
    expect(result.skills).toHaveLength(1)
    expect(result.skills[0].id).toBe("af-skill://acme/security-audit@1.2.0")
    expect(result.skills[0].tenantId).toBeNull()
    expect(result.skills[0].installedAt).toBe("2026-06-07T12:00:00Z")
    expect(result.skills[0].harnesses).toEqual(["claude-code", "codex"])
    const call = nthCall(0)
    expect(call.url).toBe("http://test.local/api/v1/skills")
    expect(call.init.method).toBe("GET")
  })

  it("passes tenant and harness as query params", async () => {
    enqueueResponse(jsonResponse({ skills: [] }))
    await skills.list({ tenant: "t-acme", harness: "claude-code" })
    const call = nthCall(0)
    expect(call.url).toContain("tenant=t-acme")
    expect(call.url).toContain("harness=claude-code")
  })

  it("returns empty list when server returns no rows", async () => {
    enqueueResponse(jsonResponse({ skills: [] }))
    const result = await skills.list()
    expect(result.skills).toEqual([])
  })
})

describe("skills.install", () => {
  it("POSTs the source and returns the row", async () => {
    enqueueResponse(jsonResponse(row()))
    const result = await installSkill({
      source: "af-skill://acme/security-audit@1.2.0",
    })
    expect(result.name).toBe("security-audit")
    expect(result.version).toBe("1.2.0")
    const call = nthCall(0)
    expect(call.url).toBe("http://test.local/api/v1/skills")
    expect(call.init.method).toBe("POST")
    const body = JSON.parse(call.init.body as string) as Record<string, unknown>
    expect(body.source).toBe("af-skill://acme/security-audit@1.2.0")
    expect(body.tenant_id).toBeUndefined()
  })

  it("includes tenant_id (snake_case) when provided", async () => {
    enqueueResponse(jsonResponse(row({ tenant_id: "t-acme" })))
    const result = await installSkill({
      source: "embedded:default",
      tenantId: "t-acme",
    })
    expect(result.tenantId).toBe("t-acme")
    const body = JSON.parse(nthCall(0).init.body as string) as Record<string, unknown>
    expect(body.source).toBe("embedded:default")
    expect(body.tenant_id).toBe("t-acme")
  })

  it("rejects an empty source before fetching", async () => {
    await expect(skills.install({ source: "" })).rejects.toThrow(/source/)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it("surfaces a 503 as a SuiteError", async () => {
    enqueueResponse(
      errorResponse(
        {
          error: {
            code: "SKILLS_NOT_CONFIGURED",
            message: "skills module is not configured",
          },
        },
        503,
      ),
    )
    await expect(
      skills.install({ source: "embedded:default" }),
    ).rejects.toBeInstanceOf(SuiteError)
  })
})

describe("skills.uninstall", () => {
  it("DELETEs the URL-encoded id", async () => {
    enqueueResponse(jsonResponse({ deleted: true }))
    await uninstallSkill("af-skill://acme/security-audit@1.2.0")
    const call = nthCall(0)
    expect(call.init.method).toBe("DELETE")
    expect(call.url).toContain(
      encodeURIComponent("af-skill://acme/security-audit@1.2.0"),
    )
  })

  it("rejects an empty id before fetching", async () => {
    await expect(skills.uninstall("")).rejects.toThrow(/non-empty/)
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

describe("skills.attach", () => {
  it("POSTs the snake_case payload", async () => {
    enqueueResponse(jsonResponse({ attached: true }))
    await attachSkill({
      skillId: "af-skill://acme/security-audit@1.2.0",
      agent: "acme.review",
    })
    const call = nthCall(0)
    expect(call.url).toBe("http://test.local/api/v1/skills/attach")
    expect(call.init.method).toBe("POST")
    const body = JSON.parse(call.init.body as string) as Record<string, unknown>
    expect(body.skill_id).toBe("af-skill://acme/security-audit@1.2.0")
    expect(body.agent).toBe("acme.review")
  })

  it("rejects empty args before fetching", async () => {
    await expect(
      skills.attach({ skillId: "", agent: "acme.review" }),
    ).rejects.toThrow(/skillId/)
    await expect(
      skills.attach({ skillId: "embedded:default", agent: "" }),
    ).rejects.toThrow(/agent/)
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

describe("suite.admin.skills namespace", () => {
  it("re-exports the same functions as the named exports", () => {
    expect(suite.admin.skills).toBe(skills)
    expect(suite.admin.skills.list).toBe(skills.list)
    expect(suite.admin.skills.install).toBe(skills.install)
    expect(suite.admin.skills.uninstall).toBe(skills.uninstall)
    expect(suite.admin.skills.attach).toBe(skills.attach)
  })
})
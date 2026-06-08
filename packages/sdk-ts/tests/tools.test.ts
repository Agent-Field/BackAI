// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  tools,
  listMcpServers,
  addMcpServer,
  removeMcpServer,
  enableMcpServer,
  listMcpTools,
  callMcp,
  listToolAdapters,
  setToolAdapterEnabled,
  callToolAdapter,
  suite,
  SuiteError,
} from "../src/index.js"

// ---------- fetch mock plumbing (shape matches sandbox.test.ts) ----------

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

function serverRow(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    name: "github",
    transport: "stdio",
    command: ["uvx", "mcp-server-github"],
    url: null,
    env: { GITHUB_TOKEN: "secret:github_token" },
    tenant_id: null,
    description: "GitHub MCP server",
    is_enabled: true,
    status: "ready",
    last_error: null,
    tools_count: 12,
    installed_at: "2026-06-06T10:00:00Z",
    last_connected_at: "2026-06-06T12:00:00Z",
    ...overrides,
  }
}

function toolRow(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "github/search_repos",
    server: "github",
    name: "search_repos",
    description: "Search GitHub repositories",
    input_schema: {
      type: "object",
      properties: { q: { type: "string" } },
      required: ["q"],
    },
    ...overrides,
  }
}

function adapterRow(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "http",
    label: "HTTP",
    description: "Make outbound HTTP calls",
    enabled: true,
    configured: true,
    default_enabled: true,
    config: {},
    updated_at: null,
    tools: [
      {
        name: "request",
        description: "Perform one HTTP request",
        input_schema: { type: "object" },
      },
    ],
    ...overrides,
  }
}

// ---------- tests ----------

describe("tools.listMcpServers", () => {
  it("GETs /mcp/servers and parses rows to camelCase", async () => {
    enqueueResponse(
      jsonResponse({
        servers: [
          serverRow(),
          serverRow({
            name: "acme",
            transport: "sse",
            command: [],
            url: "https://mcp.acme.com/sse",
            tools_count: 0,
            last_connected_at: null,
            status: "connecting",
          }),
        ],
      }),
    )
    const result = await tools.listMcpServers()

    expect(result).toHaveLength(2)
    expect(result[0]?.name).toBe("github")
    expect(result[0]?.transport).toBe("stdio")
    expect(result[0]?.isEnabled).toBe(true)
    expect(result[0]?.toolsCount).toBe(12)
    expect(result[0]?.lastConnectedAt).toBe("2026-06-06T12:00:00Z")
    expect(result[1]?.url).toBe("https://mcp.acme.com/sse")

    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/mcp/servers")
    expect(c.init.method).toBe("GET")
  })

  it("handles empty list", async () => {
    enqueueResponse(jsonResponse({ servers: [] }))
    const result = await tools.listMcpServers()
    expect(result).toEqual([])
  })
})

describe("tools built-in adapters", () => {
  it("lists adapters and parses snake_case fields", async () => {
    enqueueResponse(jsonResponse({ adapters: [adapterRow()] }))

    const adapters = await listToolAdapters()

    expect(adapters).toHaveLength(1)
    expect(adapters[0]?.id).toBe("http")
    expect(adapters[0]?.defaultEnabled).toBe(true)
    expect(adapters[0]?.tools[0]?.inputSchema).toEqual({ type: "object" })
    expect(nthCall(0).url).toBe("http://test.local/api/v1/tools/adapters")
  })

  it("enables an adapter with config", async () => {
    enqueueResponse(jsonResponse(adapterRow({ id: "sql", enabled: true })))

    const adapter = await setToolAdapterEnabled("sql", true, {
      config: { max_rows: 25 },
    })

    expect(adapter.id).toBe("sql")
    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/tools/adapters/sql/enabled")
    expect(c.init.method).toBe("PUT")
    expect(JSON.parse(c.init.body as string)).toEqual({
      enabled: true,
      config: { max_rows: 25 },
    })
  })

  it("calls a built-in adapter", async () => {
    enqueueResponse(
      jsonResponse({
        adapter: "http",
        tool: "request",
        status: "succeeded",
        result: { status_code: 200 },
        duration_ms: 12,
      }),
    )

    const result = await callToolAdapter("http", "request", {
      arguments: { url: "https://example.com" },
    })

    expect(result.durationMs).toBe(12)
    expect(result.result).toEqual({ status_code: 200 })
    const body = JSON.parse(nthCall(0).init.body as string) as Record<string, unknown>
    expect(body).toEqual({
      adapter: "http",
      tool: "request",
      arguments: { url: "https://example.com" },
    })
  })

  it("is also available under suite.tools", async () => {
    enqueueResponse(jsonResponse({ adapters: [adapterRow()] }))
    const adapters = await suite.tools.listAdapters()
    expect(adapters[0]?.id).toBe("http")
  })
})

describe("tools.addMcpServer", () => {
  it("POSTs stdio server with command + env", async () => {
    enqueueResponse(jsonResponse(serverRow()))
    const result = await tools.addMcpServer("github", "stdio", {
      command: ["uvx", "mcp-server-github"],
      env: { GITHUB_TOKEN: "secret:github_token" },
      description: "GitHub MCP server",
    })

    expect(result.name).toBe("github")
    expect(result.transport).toBe("stdio")
    expect(result.toolsCount).toBe(12)

    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/mcp/servers")
    expect(c.init.method).toBe("POST")
    const body = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(body.name).toBe("github")
    expect(body.transport).toBe("stdio")
    expect(body.command).toEqual(["uvx", "mcp-server-github"])
    expect(body.env).toEqual({ GITHUB_TOKEN: "secret:github_token" })
    expect(body.description).toBe("GitHub MCP server")
    // Optional fields must NOT appear when caller didn't supply them.
    expect("url" in body).toBe(false)
    expect("tenant_id" in body).toBe(false)
  })

  it("POSTs sse server with url + tenant_id", async () => {
    enqueueResponse(
      jsonResponse(
        serverRow({
          name: "acme",
          transport: "sse",
          command: [],
          url: "https://mcp.acme.com/sse",
        }),
      ),
    )
    await tools.addMcpServer("acme", "sse", {
      url: "https://mcp.acme.com/sse",
      tenantId: "t_xyz",
    })

    const body = JSON.parse(nthCall(0).init.body as string) as Record<string, unknown>
    expect(body.transport).toBe("sse")
    expect(body.url).toBe("https://mcp.acme.com/sse")
    expect(body.tenant_id).toBe("t_xyz")
  })

  it("validates inputs", async () => {
    await expect(tools.addMcpServer("", "stdio", { command: ["x"] })).rejects.toThrow(
      /non-empty/i,
    )
    await expect(
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      tools.addMcpServer("github", "bogus" as any, { command: ["x"] }),
    ).rejects.toThrow(/transport/i)
    await expect(tools.addMcpServer("github", "stdio")).rejects.toThrow(/command/i)
    await expect(tools.addMcpServer("acme", "sse")).rejects.toThrow(/url/i)
  })

  it("propagates structured errors as SuiteError", async () => {
    enqueueResponse(
      errorResponse(
        {
          error: {
            code: "MCP_DUPLICATE_NAME",
            message: "server name already exists",
            request_id: "req_x",
          },
        },
        409,
      ),
    )
    const err = await tools
      .addMcpServer("github", "stdio", { command: ["uvx", "mcp-server-github"] })
      .catch((e) => e as unknown)
    expect(err).toBeInstanceOf(SuiteError)
    expect((err as SuiteError).code).toBe("MCP_DUPLICATE_NAME")
    expect((err as SuiteError).status).toBe(409)
  })
})

describe("tools.removeMcpServer", () => {
  it("DELETEs /mcp/servers/{name}", async () => {
    enqueueResponse(jsonResponse({ deleted: true }))
    await tools.removeMcpServer("github")

    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/mcp/servers/github")
    expect(c.init.method).toBe("DELETE")
  })

  it("throws on empty name", async () => {
    await expect(tools.removeMcpServer("")).rejects.toThrow(/non-empty/i)
  })
})

describe("tools.enableMcpServer", () => {
  it("PUTs {enabled: bool} and parses returned row", async () => {
    enqueueResponse(jsonResponse(serverRow({ is_enabled: false, status: "disabled" })))
    const server = await tools.enableMcpServer("github", false)

    expect(server.isEnabled).toBe(false)
    expect(server.status).toBe("disabled")

    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/mcp/servers/github/enabled")
    expect(c.init.method).toBe("PUT")
    const body = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(body).toEqual({ enabled: false })
  })

  it("throws on empty name", async () => {
    await expect(tools.enableMcpServer("", true)).rejects.toThrow(/non-empty/i)
  })
})

describe("tools.listMcpTools", () => {
  it("GETs /mcp/tools with no filter", async () => {
    enqueueResponse(
      jsonResponse({
        tools: [toolRow(), toolRow({ id: "github/star_repo", name: "star_repo" })],
      }),
    )
    const result = await tools.listMcpTools()
    expect(result.map((t) => t.name)).toEqual(["search_repos", "star_repo"])
    expect(result[0]?.inputSchema).toEqual({
      type: "object",
      properties: { q: { type: "string" } },
      required: ["q"],
    })

    const url = new URL(nthCall(0).url)
    expect(url.pathname).toBe("/api/v1/mcp/tools")
    expect(url.searchParams.has("server")).toBe(false)
  })

  it("scopes by server when provided", async () => {
    enqueueResponse(jsonResponse({ tools: [toolRow()] }))
    await tools.listMcpTools({ server: "github" })

    const url = new URL(nthCall(0).url)
    expect(url.pathname).toBe("/api/v1/mcp/tools")
    expect(url.searchParams.get("server")).toBe("github")
  })

  it("rejects empty server filter", async () => {
    await expect(tools.listMcpTools({ server: "" })).rejects.toThrow(/non-empty/i)
  })
})

describe("tools.callMcp", () => {
  it("POSTs the tool call and parses MCPCallResult", async () => {
    enqueueResponse(
      jsonResponse({
        content: [
          { type: "text", text: "Found 3 repos" },
          { type: "json", json: { count: 3 } },
        ],
        is_error: false,
        duration_ms: 412,
      }),
    )
    const result = await tools.callMcp("github", "search_repos", { q: "agentfield" })

    expect(result.isError).toBe(false)
    expect(result.durationMs).toBe(412)
    expect(result.content).toHaveLength(2)
    expect(result.content[0]).toEqual({ type: "text", text: "Found 3 repos" })

    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/mcp/call")
    expect(c.init.method).toBe("POST")
    const body = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(body).toEqual({
      server: "github",
      tool: "search_repos",
      arguments: { q: "agentfield" },
    })
  })

  it("omits arguments when not provided", async () => {
    enqueueResponse(
      jsonResponse({ content: [], is_error: false, duration_ms: 12 }),
    )
    await tools.callMcp("github", "list_repos")
    const body = JSON.parse(nthCall(0).init.body as string) as Record<string, unknown>
    expect(body).toEqual({ server: "github", tool: "list_repos" })
  })

  it("validates inputs", async () => {
    await expect(tools.callMcp("", "search_repos")).rejects.toThrow(/non-empty/i)
    await expect(tools.callMcp("github", "")).rejects.toThrow(/non-empty/i)
  })

  it("surfaces is_error=true in the parsed result", async () => {
    enqueueResponse(
      jsonResponse({
        content: [{ type: "text", text: "Rate limited" }],
        is_error: true,
        duration_ms: 200,
      }),
    )
    const result = await tools.callMcp("github", "search_repos", { q: "x" })
    expect(result.isError).toBe(true)
    expect(result.content[0]?.text).toBe("Rate limited")
  })
})

describe("namespace export", () => {
  it("suite.tools.* matches the module helpers", () => {
    expect(suite.tools).toBe(tools)
    expect(suite.tools.listMcpServers).toBe(tools.listMcpServers)
    expect(suite.tools.addMcpServer).toBe(tools.addMcpServer)
    expect(suite.tools.removeMcpServer).toBe(tools.removeMcpServer)
    expect(suite.tools.enableMcpServer).toBe(tools.enableMcpServer)
    expect(suite.tools.listMcpTools).toBe(tools.listMcpTools)
    expect(suite.tools.callMcp).toBe(tools.callMcp)
  })

  it("named re-exports are wired to the module helpers", () => {
    expect(listMcpServers).toBe(tools.listMcpServers)
    expect(addMcpServer).toBe(tools.addMcpServer)
    expect(removeMcpServer).toBe(tools.removeMcpServer)
    expect(enableMcpServer).toBe(tools.enableMcpServer)
    expect(listMcpTools).toBe(tools.listMcpTools)
    expect(callMcp).toBe(tools.callMcp)
  })
})

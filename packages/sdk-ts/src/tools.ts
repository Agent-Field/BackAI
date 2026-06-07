// suite.tools.* — Model Context Protocol (MCP) server + tool operations.
//
// Endpoint paths and JSON shapes are the canonical contract from
// `apps/dashboard/src/lib/api.ts` (the Phase 11.1 MCP section). Schemas
// below mirror the zod schemas there; if the dashboard contract moves,
// move these together.
//
// Public API stays camelCase; the wire stays snake_case. We translate at
// the boundary — `command`, `env`, `arguments`, and `content` are
// opaque caller-defined payloads that pass through verbatim.

import { z } from "zod"
import { request, type HttpOptions } from "./_http.js"

// ---------- shared schemas (mirror lib/api.ts, camelCase) ----------

export const MCPTransportSchema = z.enum(["stdio", "sse"])
export type MCPTransport = z.infer<typeof MCPTransportSchema>

export const MCPServerStatusSchema = z.enum([
  "connecting",
  "ready",
  "errored",
  "disabled",
])
export type MCPServerStatus = z.infer<typeof MCPServerStatusSchema>

export const MCPServerSchema = z.object({
  name: z.string(),
  transport: MCPTransportSchema,
  command: z.array(z.string()),
  url: z.string().nullable(),
  env: z.record(z.string(), z.string()),
  tenantId: z.string().nullable(),
  description: z.string(),
  isEnabled: z.boolean(),
  status: MCPServerStatusSchema,
  lastError: z.string().nullable(),
  toolsCount: z.number(),
  installedAt: z.string(),
  lastConnectedAt: z.string().nullable(),
})
export type MCPServer = z.infer<typeof MCPServerSchema>

export const MCPServerListSchema = z.object({
  servers: z.array(MCPServerSchema),
})
export type MCPServerList = z.infer<typeof MCPServerListSchema>

export const MCPToolSchema = z.object({
  id: z.string(),
  server: z.string(),
  name: z.string(),
  description: z.string().nullable(),
  inputSchema: z.record(z.string(), z.unknown()),
})
export type MCPTool = z.infer<typeof MCPToolSchema>

export const MCPToolListSchema = z.object({
  tools: z.array(MCPToolSchema),
})
export type MCPToolList = z.infer<typeof MCPToolListSchema>

export const MCPCallResultSchema = z.object({
  content: z.array(z.record(z.string(), z.unknown())),
  isError: z.boolean(),
  durationMs: z.number(),
})
export type MCPCallResult = z.infer<typeof MCPCallResultSchema>

// ---------- option shapes ----------

export interface AddMCPServerOptions extends HttpOptions {
  /** stdio: ["uvx", "mcp-server-github"]. Required when transport=stdio. */
  command?: string[]
  /** sse: "https://mcp.acme.com/sse". Required when transport=sse. */
  url?: string
  /**
   * Environment passed to stdio servers. Values can reference vault keys
   * via the `"secret:<key>"` prefix — never raw secret strings.
   */
  env?: Record<string, string>
  description?: string
  /** Per-tenant scoping. Omit = available to every tenant. */
  tenantId?: string
}

export interface ListMCPToolsOptions extends HttpOptions {
  /** Scope the listing to one server. */
  server?: string
}

const ALLOWED_TRANSPORTS: readonly MCPTransport[] = ["stdio", "sse"]

// ---------- public API ----------

/** List every configured MCP server visible to the caller. */
export async function listMcpServers(
  opts: HttpOptions = {},
): Promise<MCPServer[]> {
  const raw = await request<unknown>("GET", "/mcp/servers", null, opts)
  const parsed = MCPServerListSchema.parse(camelizeServerList(raw))
  return parsed.servers
}

/**
 * Register a new MCP server.
 *
 * For `transport: "stdio"` you must supply `command`; for
 * `transport: "sse"` you must supply `url`. The runtime will attempt to
 * connect immediately and the returned `status` will reflect the first
 * probe outcome (`ready` / `errored`).
 */
export async function addMcpServer(
  name: string,
  transport: MCPTransport,
  opts: AddMCPServerOptions = {},
): Promise<MCPServer> {
  if (typeof name !== "string" || name.length === 0) {
    throw new Error("mcp server name must be a non-empty string")
  }
  if (!(ALLOWED_TRANSPORTS as readonly string[]).includes(transport)) {
    throw new Error(
      `transport must be one of: ${ALLOWED_TRANSPORTS.join(", ")}; got ${JSON.stringify(transport)}`,
    )
  }
  const { command, url, env, description, tenantId, ...http } = opts

  if (transport === "stdio") {
    if (!Array.isArray(command) || command.length === 0) {
      throw new Error("stdio transport requires a non-empty command array")
    }
    for (const arg of command) {
      if (typeof arg !== "string") {
        throw new Error("command entries must all be strings")
      }
    }
  }
  if (transport === "sse") {
    if (typeof url !== "string" || url.length === 0) {
      throw new Error("sse transport requires a url")
    }
  }

  const body: Record<string, unknown> = { name, transport }
  if (command !== undefined) body.command = [...command]
  if (url !== undefined) body.url = url
  if (env !== undefined) body.env = { ...env }
  if (description !== undefined) body.description = description
  if (tenantId !== undefined) body.tenant_id = tenantId

  const raw = await request<unknown>("POST", "/mcp/servers", body, http)
  return MCPServerSchema.parse(camelizeServer(raw))
}

/**
 * Remove an MCP server. The runtime closes the underlying connection
 * and the row is deleted.
 */
export async function removeMcpServer(
  name: string,
  opts: HttpOptions = {},
): Promise<void> {
  if (typeof name !== "string" || name.length === 0) {
    throw new Error("mcp server name must be a non-empty string")
  }
  await request<unknown>(
    "DELETE",
    `/mcp/servers/${encodeURIComponent(name)}`,
    null,
    opts,
  )
}

/**
 * Enable or disable an MCP server. Disabling closes the connection but
 * preserves the configuration.
 */
export async function enableMcpServer(
  name: string,
  enabled: boolean,
  opts: HttpOptions = {},
): Promise<MCPServer> {
  if (typeof name !== "string" || name.length === 0) {
    throw new Error("mcp server name must be a non-empty string")
  }
  const raw = await request<unknown>(
    "PUT",
    `/mcp/servers/${encodeURIComponent(name)}/enabled`,
    { enabled: Boolean(enabled) },
    opts,
  )
  return MCPServerSchema.parse(camelizeServer(raw))
}

/** List MCP tools, optionally scoped to a single server. */
export async function listMcpTools(
  opts: ListMCPToolsOptions = {},
): Promise<MCPTool[]> {
  const { server, ...http } = opts
  if (server !== undefined && server.length === 0) {
    throw new Error("server filter must be a non-empty string")
  }
  const qs = new URLSearchParams()
  if (server !== undefined) qs.set("server", server)
  const qsStr = qs.toString()
  const path = qsStr.length > 0 ? `/mcp/tools?${qsStr}` : "/mcp/tools"
  const raw = await request<unknown>("GET", path, null, http)
  const parsed = MCPToolListSchema.parse(camelizeToolList(raw))
  return parsed.tools
}

/**
 * Invoke an MCP tool and return its result. The returned `content`
 * array carries the MCP protocol's typed parts (text / image / json) —
 * caller code should check `isError` before consuming.
 */
export async function callMcp(
  server: string,
  tool: string,
  args?: Record<string, unknown>,
  opts: HttpOptions = {},
): Promise<MCPCallResult> {
  if (typeof server !== "string" || server.length === 0) {
    throw new Error("mcp server name must be a non-empty string")
  }
  if (typeof tool !== "string" || tool.length === 0) {
    throw new Error("mcp tool name must be a non-empty string")
  }
  const body: Record<string, unknown> = { server, tool }
  if (args !== undefined) body.arguments = { ...args }
  const raw = await request<unknown>("POST", "/mcp/call", body, opts)
  return MCPCallResultSchema.parse(camelizeCallResult(raw))
}

// ---------- helpers ----------

function camelizeServer(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    name: src.name,
    transport: src.transport,
    command: src.command ?? [],
    url: src.url ?? null,
    env: src.env ?? {},
    tenantId: src.tenant_id ?? src.tenantId ?? null,
    description: src.description ?? "",
    isEnabled: src.is_enabled ?? src.isEnabled,
    status: src.status,
    lastError: src.last_error ?? src.lastError ?? null,
    toolsCount: src.tools_count ?? src.toolsCount,
    installedAt: src.installed_at ?? src.installedAt,
    lastConnectedAt: src.last_connected_at ?? src.lastConnectedAt ?? null,
  }
}

function camelizeServerList(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  const servers = Array.isArray(src.servers)
    ? src.servers.map(camelizeServer)
    : []
  return { servers }
}

function camelizeTool(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    id: src.id,
    server: src.server,
    name: src.name,
    description: src.description ?? null,
    // `input_schema` is forwarded as an opaque JSON Schema object — we
    // only rename the wrapping key, not any inner field names.
    inputSchema: src.input_schema ?? src.inputSchema ?? {},
  }
}

function camelizeToolList(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  const tools = Array.isArray(src.tools) ? src.tools.map(camelizeTool) : []
  return { tools }
}

function camelizeCallResult(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    // `content` parts are opaque MCP payloads — pass through unchanged.
    content: Array.isArray(src.content) ? src.content : [],
    isError: src.is_error ?? src.isError ?? false,
    durationMs: src.duration_ms ?? src.durationMs ?? 0,
  }
}

/** Namespace object — the shape `suite.tools` is built from. */
export const tools = {
  listMcpServers,
  addMcpServer,
  removeMcpServer,
  enableMcpServer,
  listMcpTools,
  callMcp,
} as const

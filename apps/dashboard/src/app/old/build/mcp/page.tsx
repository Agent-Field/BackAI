// MCP (Model Context Protocol) configuration page.
//
// Server-renders the list of registered MCP servers and their tools via
// `Promise.allSettled` so a failing tools probe doesn't take the whole
// page down. The client component handles add/remove/enable mutations
// and the inline tool-call inspector.

import { PageHeader } from "@/components/layout/page-header"
import {
  api,
  type MCPServer,
  type MCPTool,
} from "@/lib/api"

import { McpView } from "./_components/mcp-view"

export const dynamic = "force-dynamic"

type InitialState = {
  servers: MCPServer[]
  tools: MCPTool[]
  serversError: string | null
  toolsError: string | null
}

async function loadInitialState(): Promise<InitialState> {
  const [serversResult, toolsResult] = await Promise.allSettled([
    api.mcp.servers(),
    api.mcp.tools(),
  ])

  let servers: MCPServer[] = []
  let serversError: string | null = null
  if (serversResult.status === "fulfilled") {
    servers = serversResult.value.servers
  } else {
    const reason = serversResult.reason
    serversError =
      reason instanceof Error ? reason.message : "MCP module unreachable"
  }

  let tools: MCPTool[] = []
  let toolsError: string | null = null
  if (toolsResult.status === "fulfilled") {
    tools = toolsResult.value.tools
  } else {
    const reason = toolsResult.reason
    toolsError =
      reason instanceof Error ? reason.message : "MCP tools probe failed"
  }

  return { servers, tools, serversError, toolsError }
}

export default async function McpPage() {
  const initial = await loadInitialState()

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="MCP"
        description="Model Context Protocol servers and the tools they expose to agents."
      />
      <McpView
        initialServers={initial.servers}
        initialTools={initial.tools}
        serversError={initial.serversError}
        toolsError={initial.toolsError}
      />
    </div>
  )
}

// SPDX-License-Identifier: Apache-2.0

// MCP page — Build → MCP. Catalog of Model Context Protocol servers
// configured on the runtime and the tools they expose, backed by
// GET /api/v1/mcp/servers. Server component: fetched once per render
// (force-dynamic + no-store via lib/api.ts). When the runtime isn't
// reachable we render a clean empty-state shell rather than crashing.

import { Plug } from "lucide-react"

import { api, ApiError, type MCPServerList } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { PageHeader } from "@/components/layout/page-header"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

export const dynamic = "force-dynamic"

async function loadServers(): Promise<{
  data: MCPServerList
  error: string | null
}> {
  try {
    const data = await api.mcp.servers()
    return { data, error: null }
  } catch (e) {
    const message =
      e instanceof ApiError
        ? `${e.code}: ${e.message}`
        : e instanceof Error
          ? e.message
          : "Failed to load MCP servers"
    return { data: { servers: [] }, error: message }
  }
}

export default async function Page() {
  const { data, error } = await loadServers()
  const servers = data.servers

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="MCP"
        description="Model Context Protocol servers and the tools they expose. Backed by /api/v1/mcp/servers."
      />

      {error && servers.length === 0 ? (
        <div className="bg-card text-muted-foreground rounded-xl border border-dashed p-6 text-sm">
          <p className="text-foreground mb-1 font-medium">
            MCP servers aren&apos;t available
          </p>
          <p>
            The runtime returned:{" "}
            <code className="bg-muted rounded px-1 font-mono text-xs">
              {error}
            </code>
            .
          </p>
        </div>
      ) : servers.length === 0 ? (
        <div className="text-muted-foreground bg-card flex items-center gap-2 rounded-xl border p-6 text-xs">
          <Plug className="size-3.5" />
          No MCP servers configured.
        </div>
      ) : (
        <div className="bg-card rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Server
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Transport
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Status
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Tools
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Enabled
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {servers.map((server) => (
                <TableRow key={server.name} className="hover:bg-muted/30">
                  <TableCell>
                    <code className="font-mono text-xs">{server.name}</code>
                    {server.description ? (
                      <p className="text-muted-foreground mt-0.5 text-xs">
                        {server.description}
                      </p>
                    ) : null}
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs">
                    {server.transport}
                  </TableCell>
                  <TableCell>
                    <StatusBadge status={server.status} />
                  </TableCell>
                  <TableCell className="tabular-nums text-xs">
                    {server.tools_count}
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={server.is_enabled ? "secondary" : "outline"}
                      className="text-xs"
                    >
                      {server.is_enabled ? "Enabled" : "Disabled"}
                    </Badge>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}

function StatusBadge({
  status,
}: {
  status: MCPServerList["servers"][number]["status"]
}) {
  if (status === "errored") {
    return (
      <Badge variant="destructive" className="text-xs">
        {status}
      </Badge>
    )
  }
  if (status === "disabled") {
    return (
      <Badge variant="outline" className="text-xs">
        {status}
      </Badge>
    )
  }
  if (status === "ready") {
    return (
      <Badge variant="default" className="text-xs">
        {status}
      </Badge>
    )
  }
  return (
    <Badge variant="secondary" className="text-xs">
      {status}
    </Badge>
  )
}

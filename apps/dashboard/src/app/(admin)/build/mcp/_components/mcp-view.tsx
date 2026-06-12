"use client"

// McpView — two-pane controller for the MCP tab.
//
// Left pane:  Server table with status badge + enable switch.
// Right pane: When a server is selected, lists its tools with collapsible
//             JSON schema previews. When nothing is selected, shows the
//             aggregate tool count + an "Add server" CTA.
//
// Mutations (add / remove / enable / call) live in sibling components so
// this file stays focused on selection state and table rendering.

import * as React from "react"
import {
  AlertTriangle,
  ChevronDown,
  ChevronRight,
  MoreHorizontal,
  Plus,
  Terminal,
  Trash2,
  Wrench,
} from "lucide-react"
import { toast } from "sonner"

import {
  api,
  ApiError,
  type MCPServer,
  type MCPTool,
} from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Switch } from "@/components/ui/switch"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { cn } from "@/lib/utils"

import { AddServerDialog } from "./add-server-dialog"
import { CallToolSheet } from "./call-tool-sheet"

type McpViewProps = {
  initialServers: MCPServer[]
  initialTools: MCPTool[]
  serversError: string | null
  toolsError: string | null
}

export function McpView({
  initialServers,
  initialTools,
  serversError,
  toolsError,
}: McpViewProps) {
  const [servers, setServers] = React.useState<MCPServer[]>(initialServers)
  const [tools, setTools] = React.useState<MCPTool[]>(initialTools)
  const [selected, setSelected] = React.useState<string | null>(null)
  const [openAdd, setOpenAdd] = React.useState(false)
  const [openCall, setOpenCall] = React.useState(false)

  const refresh = React.useCallback(async () => {
    try {
      const [serverList, toolList] = await Promise.all([
        api.mcp.servers(),
        api.mcp.tools(),
      ])
      setServers(serverList.servers)
      setTools(toolList.tools)
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Refresh failed"
      toast.error("Could not refresh MCP state", { description: message })
    }
  }, [])

  const selectedServer =
    selected !== null ? servers.find((s) => s.name === selected) ?? null : null
  const selectedTools = React.useMemo(
    () => (selected !== null ? tools.filter((t) => t.server === selected) : []),
    [tools, selected],
  )

  if (serversError !== null && servers.length === 0) {
    return (
      <Empty className="border-dashed">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <AlertTriangle />
          </EmptyMedia>
          <EmptyTitle>MCP module unreachable</EmptyTitle>
          <EmptyDescription>{serversError}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <span className="text-muted-foreground text-xs">
          {servers.length === 0
            ? "No MCP servers configured"
            : `${servers.length} server${servers.length === 1 ? "" : "s"} · ${tools.length} tool${
                tools.length === 1 ? "" : "s"
              }`}
        </span>
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            variant="outline"
            onClick={() => setOpenCall(true)}
            disabled={tools.length === 0}
          >
            <Terminal />
            Call tool
          </Button>
          <Button size="sm" onClick={() => setOpenAdd(true)}>
            <Plus />
            Add server
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-5">
        <div className="lg:col-span-3">
          <ServerTable
            servers={servers}
            selected={selected}
            onSelect={setSelected}
            onChanged={refresh}
            onCreate={() => setOpenAdd(true)}
          />
        </div>
        <div className="lg:col-span-2">
          <ToolsPane
            server={selectedServer}
            tools={selectedTools}
            totalTools={tools.length}
            toolsError={toolsError}
            onAddServer={() => setOpenAdd(true)}
          />
        </div>
      </div>

      <AddServerDialog
        open={openAdd}
        onOpenChange={setOpenAdd}
        onSaved={refresh}
      />
      <CallToolSheet
        open={openCall}
        onOpenChange={setOpenCall}
        servers={servers}
        tools={tools}
        initialServer={selected ?? undefined}
      />
    </div>
  )
}

// ─── Server table (left pane) ─────────────────────────────────────────────

type ServerTableProps = {
  servers: MCPServer[]
  selected: string | null
  onSelect: (name: string | null) => void
  onChanged: () => Promise<void>
  onCreate: () => void
}

function ServerTable({
  servers,
  selected,
  onSelect,
  onChanged,
  onCreate,
}: ServerTableProps) {
  const [pendingEnable, setPendingEnable] = React.useState<string | null>(null)
  const [pendingDelete, setPendingDelete] = React.useState<string | null>(null)

  const handleToggle = async (server: MCPServer, next: boolean) => {
    setPendingEnable(server.name)
    try {
      await api.mcp.enableServer(server.name, next)
      toast.success(next ? "Server enabled" : "Server disabled", {
        description: server.name,
      })
      await onChanged()
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Toggle failed"
      toast.error("Could not update server", { description: message })
    } finally {
      setPendingEnable(null)
    }
  }

  const handleRemove = async (server: MCPServer) => {
    setPendingDelete(server.name)
    try {
      await api.mcp.removeServer(server.name)
      toast.success("Server removed", { description: server.name })
      if (selected === server.name) onSelect(null)
      await onChanged()
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Remove failed"
      toast.error("Could not remove server", { description: message })
    } finally {
      setPendingDelete(null)
    }
  }

  if (servers.length === 0) {
    return (
      <div className="rounded-xl border bg-card">
        <Empty className="border-0">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Wrench />
            </EmptyMedia>
            <EmptyTitle>No MCP servers configured</EmptyTitle>
            <EmptyDescription>
              Register an MCP server to give agents access to external tools.
              Servers can be stdio child processes or remote SSE endpoints.
            </EmptyDescription>
          </EmptyHeader>
          <div className="mt-4 flex justify-center">
            <Button size="sm" onClick={onCreate}>
              <Plus />
              Add server
            </Button>
          </div>
        </Empty>
      </div>
    )
  }

  return (
    <div className="rounded-xl border bg-card">
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
              Name
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
              Last connected
            </TableHead>
            <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
              Enabled
            </TableHead>
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          {servers.map((s) => {
            const isSelected = selected === s.name
            return (
              <TableRow
                key={s.name}
                onClick={() => onSelect(isSelected ? null : s.name)}
                className={cn(
                  "cursor-pointer",
                  isSelected ? "bg-muted/40" : "hover:bg-muted/30",
                )}
                aria-selected={isSelected}
              >
                <TableCell>
                  <span className="font-mono text-xs font-medium">
                    {s.name}
                  </span>
                </TableCell>
                <TableCell>
                  <TransportBadge transport={s.transport} />
                </TableCell>
                <TableCell>
                  <StatusBadge status={s.status} lastError={s.last_error} />
                </TableCell>
                <TableCell className="text-muted-foreground text-xs tabular-nums">
                  {s.tools_count}
                </TableCell>
                <TableCell className="text-muted-foreground text-xs">
                  {s.last_connected_at ?? "—"}
                </TableCell>
                <TableCell onClick={(e) => e.stopPropagation()}>
                  <Switch
                    checked={s.is_enabled}
                    onCheckedChange={(v) => void handleToggle(s, v)}
                    disabled={pendingEnable === s.name}
                    aria-label={`${s.is_enabled ? "Disable" : "Enable"} ${s.name}`}
                  />
                </TableCell>
                <TableCell
                  className="text-right"
                  onClick={(e) => e.stopPropagation()}
                >
                  <DropdownMenu>
                    <DropdownMenuTrigger
                      render={
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label="Server actions"
                          disabled={pendingDelete === s.name}
                        >
                          <MoreHorizontal />
                        </Button>
                      }
                    />
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem
                        variant="destructive"
                        onClick={() => void handleRemove(s)}
                      >
                        <Trash2 />
                        Remove
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </div>
  )
}

function TransportBadge({ transport }: { transport: MCPServer["transport"] }) {
  return (
    <Badge variant="outline" className="font-mono text-[10px] uppercase">
      {transport}
    </Badge>
  )
}

function StatusBadge({
  status,
  lastError,
}: {
  status: MCPServer["status"]
  lastError: string | null
}) {
  const variantByStatus: Record<
    MCPServer["status"],
    React.ComponentProps<typeof Badge>["variant"]
  > = {
    connecting: "secondary",
    ready: "default",
    errored: "destructive",
    disabled: "outline",
  }
  const labelByStatus: Record<MCPServer["status"], string> = {
    connecting: "Connecting",
    ready: "Ready",
    errored: "Errored",
    disabled: "Disabled",
  }
  const badge = (
    <Badge variant={variantByStatus[status]}>{labelByStatus[status]}</Badge>
  )
  if (status === "errored" && lastError !== null && lastError !== "") {
    return (
      <span title={lastError} className="cursor-help">
        {badge}
      </span>
    )
  }
  return badge
}

// ─── Tools pane (right side) ──────────────────────────────────────────────

type ToolsPaneProps = {
  server: MCPServer | null
  tools: MCPTool[]
  totalTools: number
  toolsError: string | null
  onAddServer: () => void
}

function ToolsPane({
  server,
  tools,
  totalTools,
  toolsError,
  onAddServer,
}: ToolsPaneProps) {
  if (server === null) {
    return (
      <div className="rounded-xl border bg-card p-4">
        <div className="flex flex-col gap-3">
          <h2 className="text-sm font-semibold">All tools</h2>
          <p className="text-muted-foreground text-xs">
            {toolsError !== null
              ? `Tool list probe failed: ${toolsError}`
              : `${totalTools} tool${totalTools === 1 ? "" : "s"} across every configured server. Select a server on the left to see its tools.`}
          </p>
          <div className="flex">
            <Button size="sm" onClick={onAddServer}>
              <Plus />
              Add server
            </Button>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="rounded-xl border bg-card">
      <div className="border-b px-4 py-3">
        <div className="flex items-center gap-2">
          <h2 className="font-mono text-sm font-semibold">{server.name}</h2>
          <TransportBadge transport={server.transport} />
          <StatusBadge status={server.status} lastError={server.last_error} />
        </div>
        {server.description ? (
          <p className="text-muted-foreground mt-1 text-xs">
            {server.description}
          </p>
        ) : null}
        {server.status === "errored" && server.last_error ? (
          <pre className="bg-destructive/10 text-destructive mt-3 overflow-x-auto rounded-md p-2 font-mono text-[11px]">
            {server.last_error}
          </pre>
        ) : null}
      </div>
      <div className="p-4">
        {tools.length === 0 ? (
          <Empty className="border-0">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <Wrench />
              </EmptyMedia>
              <EmptyTitle>No tools available</EmptyTitle>
              <EmptyDescription>
                {server.status === "ready"
                  ? "This server is connected but reported no tools."
                  : "Tools become visible once the server reaches the ready state."}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <ul className="flex flex-col gap-2">
            {tools.map((t) => (
              <li key={t.id}>
                <ToolRow tool={t} />
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}

function ToolRow({ tool }: { tool: MCPTool }) {
  const [open, setOpen] = React.useState(false)
  return (
    <Collapsible
      open={open}
      onOpenChange={setOpen}
      className="rounded-md border bg-background"
    >
      <CollapsibleTrigger
        render={
          <button
            type="button"
            className="hover:bg-muted/40 flex w-full items-start gap-2 rounded-md px-3 py-2 text-left"
          >
            {open ? (
              <ChevronDown className="text-muted-foreground mt-0.5 size-3.5" />
            ) : (
              <ChevronRight className="text-muted-foreground mt-0.5 size-3.5" />
            )}
            <div className="flex flex-col gap-0.5">
              <span className="font-mono text-xs font-medium">{tool.name}</span>
              {tool.description ? (
                <span className="text-muted-foreground text-xs">
                  {tool.description}
                </span>
              ) : null}
            </div>
          </button>
        }
      />
      <CollapsibleContent className="px-3 pb-3">
        <pre className="bg-muted max-h-80 overflow-auto rounded-md p-2 font-mono text-[11px] leading-snug">
          {JSON.stringify(tool.input_schema, null, 2)}
        </pre>
      </CollapsibleContent>
    </Collapsible>
  )
}

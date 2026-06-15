// SPDX-License-Identifier: Apache-2.0

// Agents page — Build → Agents. Catalog of agents registered with the
// runtime, backed by GET /api/v1/agents. Server component: fetched once per
// render (force-dynamic + no-store via lib/api.ts). When the runtime isn't
// reachable we render a clean empty-state shell rather than crashing.

import { Boxes, Tags, Workflow } from "lucide-react"

import { api, ApiError, type AgentList } from "@/lib/api"
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

async function loadAgents(): Promise<{ data: AgentList; error: string | null }> {
  try {
    const data = await api.agents()
    return { data, error: null }
  } catch (e) {
    const message =
      e instanceof ApiError
        ? `${e.code}: ${e.message}`
        : e instanceof Error
          ? e.message
          : "Failed to load agents"
    return { data: { agents: [] }, error: message }
  }
}

export default async function Page() {
  const { data, error } = await loadAgents()
  const agents = data.agents

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Agents"
        description="Agents registered with the runtime — schemas, tags, and attached reasoners. Backed by /api/v1/agents."
      />

      {error && agents.length === 0 ? (
        <div className="bg-card text-muted-foreground rounded-xl border border-dashed p-6 text-sm">
          <p className="text-foreground mb-1 font-medium">
            Agent catalog isn&apos;t available
          </p>
          <p>
            The runtime returned:{" "}
            <code className="bg-muted rounded px-1 font-mono text-xs">
              {error}
            </code>
            .
          </p>
        </div>
      ) : agents.length === 0 ? (
        <div className="text-muted-foreground bg-card flex items-center gap-2 rounded-xl border p-6 text-xs">
          <Boxes className="size-3.5" />
          No agents registered yet. Define one under{" "}
          <code className="font-mono">apps/backend/agents/</code> and it&apos;ll
          appear here.
        </div>
      ) : (
        <div className="bg-card rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Agent
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Version
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Reasoners
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Tags
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {agents.map((agent) => (
                <TableRow key={agent.node_id} className="hover:bg-muted/30">
                  <TableCell>
                    <code className="font-mono text-xs">{agent.node_id}</code>
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs tabular-nums">
                    {agent.version ?? "—"}
                  </TableCell>
                  <TableCell>
                    {agent.reasoners && agent.reasoners.length > 0 ? (
                      <div className="flex flex-wrap items-center gap-1">
                        <Workflow className="text-muted-foreground size-3.5" />
                        {agent.reasoners.map((r) => (
                          <Badge key={r} variant="secondary" className="text-xs">
                            {r}
                          </Badge>
                        ))}
                      </div>
                    ) : (
                      <span className="text-muted-foreground text-xs">—</span>
                    )}
                  </TableCell>
                  <TableCell>
                    {agent.tags && agent.tags.length > 0 ? (
                      <div className="flex flex-wrap items-center gap-1">
                        <Tags className="text-muted-foreground size-3.5" />
                        {agent.tags.map((t) => (
                          <Badge key={t} variant="outline" className="text-xs">
                            {t}
                          </Badge>
                        ))}
                      </div>
                    ) : (
                      <span className="text-muted-foreground text-xs">—</span>
                    )}
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

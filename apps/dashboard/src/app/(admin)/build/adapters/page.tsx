// SPDX-License-Identifier: Apache-2.0

// Adapters page — Build → Adapters. Active and available backend adapter
// choices, backed by GET /api/v1/tools/adapters. Server component: fetched
// once per render (force-dynamic + no-store via lib/api.ts). When the runtime
// isn't reachable we render a clean empty-state shell rather than crashing.

import { Plug } from "lucide-react"

import { api, ApiError, type ToolAdapterList } from "@/lib/api"
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

async function loadAdapters(): Promise<{
  data: ToolAdapterList
  error: string | null
}> {
  try {
    const data = await api.tools.adapters()
    return { data, error: null }
  } catch (e) {
    const message =
      e instanceof ApiError
        ? `${e.code}: ${e.message}`
        : e instanceof Error
          ? e.message
          : "Failed to load adapters"
    return { data: { adapters: [] }, error: message }
  }
}

export default async function Page() {
  const { data, error } = await loadAdapters()
  const adapters = data.adapters

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Adapters"
        description="Active and available backend adapter choices. Backed by /api/v1/tools/adapters."
      />

      {error && adapters.length === 0 ? (
        <div className="bg-card text-muted-foreground rounded-xl border border-dashed p-6 text-sm">
          <p className="text-foreground mb-1 font-medium">
            Adapters aren&apos;t available
          </p>
          <p>
            The runtime returned:{" "}
            <code className="bg-muted rounded px-1 font-mono text-xs">
              {error}
            </code>
            .
          </p>
        </div>
      ) : adapters.length === 0 ? (
        <div className="text-muted-foreground bg-card flex items-center gap-2 rounded-xl border p-6 text-xs">
          <Plug className="size-3.5" />
          No tool adapters reported.
        </div>
      ) : (
        <div className="bg-card rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Adapter
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  ID
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Enabled
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Configured
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Tools
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {adapters.map((adapter) => (
                <TableRow key={adapter.id} className="hover:bg-muted/30">
                  <TableCell>
                    <div className="font-medium">{adapter.label}</div>
                    <div className="text-muted-foreground text-xs">
                      {adapter.description}
                    </div>
                  </TableCell>
                  <TableCell>
                    <code className="font-mono text-xs">{adapter.id}</code>
                  </TableCell>
                  <TableCell>
                    {adapter.enabled ? (
                      <Badge variant="secondary" className="text-xs">
                        enabled
                      </Badge>
                    ) : (
                      <Badge variant="outline" className="text-xs">
                        disabled
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell>
                    {adapter.configured ? (
                      <Badge variant="secondary" className="text-xs">
                        configured
                      </Badge>
                    ) : (
                      <Badge variant="outline" className="text-xs">
                        unconfigured
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs tabular-nums">
                    {adapter.tools.length}
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

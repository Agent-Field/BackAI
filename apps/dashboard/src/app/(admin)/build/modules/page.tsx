// SPDX-License-Identifier: Apache-2.0

// Modules page — Build → Modules. Read-only view of the suite modules
// enabled in config.yaml, backed by GET /api/v1/modules. Editing happens in
// the file (git-tracked), not here. Server component: fetched once per render
// (force-dynamic + no-store via lib/api.ts). When the runtime isn't reachable
// we render a clean empty-state shell rather than crashing.

import { Layers, Network } from "lucide-react"

import { api, ApiError, type ModulesState } from "@/lib/api"
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

async function loadModules(): Promise<{
  data: ModulesState
  error: string | null
}> {
  try {
    const data = await api.modulesState()
    return { data, error: null }
  } catch (e) {
    const message =
      e instanceof ApiError
        ? `${e.code}: ${e.message}`
        : e instanceof Error
          ? e.message
          : "Failed to load modules"
    return {
      data: {
        modules: [],
        workload_modules: [],
        multi_tenancy_enabled: false,
      },
      error: message,
    }
  }
}

export default async function Page() {
  const { data, error } = await loadModules()
  const modules = data.modules
  const enabledCount = modules.filter((m) => m.enabled).length

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Modules"
        description="Read-only view of the suite modules enabled in config.yaml. Editing happens in the file (git-tracked)."
      />

      {error && modules.length === 0 ? (
        <div className="bg-card text-muted-foreground rounded-xl border border-dashed p-6 text-sm">
          <p className="text-foreground mb-1 font-medium">
            Module state isn&apos;t available
          </p>
          <p>
            The runtime returned:{" "}
            <code className="bg-muted rounded px-1 font-mono text-xs">
              {error}
            </code>
            .
          </p>
        </div>
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-2">
            <Badge
              variant={data.multi_tenancy_enabled ? "secondary" : "outline"}
              className="text-xs"
            >
              Multi-tenancy: {data.multi_tenancy_enabled ? "enabled" : "disabled"}
            </Badge>
            <Badge variant="outline" className="text-xs">
              {enabledCount} enabled module{enabledCount === 1 ? "" : "s"}
            </Badge>
          </div>

          {modules.length === 0 ? (
            <div className="text-muted-foreground bg-card flex items-center gap-2 rounded-xl border p-6 text-xs">
              <Layers className="size-3.5" />
              No suite modules reported.
            </div>
          ) : (
            <div className="bg-card rounded-xl border">
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                      Module
                    </TableHead>
                    <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                      ID
                    </TableHead>
                    <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                      Enabled
                    </TableHead>
                    <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                      Adapter
                    </TableHead>
                    <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                      Version
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {modules.map((module) => (
                    <TableRow key={module.id} className="hover:bg-muted/30">
                      <TableCell className="text-sm">{module.name}</TableCell>
                      <TableCell>
                        <code className="font-mono text-xs">{module.id}</code>
                      </TableCell>
                      <TableCell>
                        {module.enabled ? (
                          <Badge variant="secondary" className="text-xs">
                            Enabled
                          </Badge>
                        ) : (
                          <Badge variant="outline" className="text-xs">
                            Disabled
                          </Badge>
                        )}
                      </TableCell>
                      <TableCell className="text-muted-foreground text-xs">
                        {module.adapter ?? "—"}
                      </TableCell>
                      <TableCell className="text-muted-foreground text-xs tabular-nums">
                        {module.version ?? "—"}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}

          <div className="flex flex-col gap-2">
            <p className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
              Workload modules
            </p>
            {data.workload_modules.length > 0 ? (
              <div className="flex flex-wrap items-center gap-1">
                <Network className="text-muted-foreground size-3.5" />
                {data.workload_modules.map((wm) => (
                  <Badge key={wm} variant="outline" className="text-xs">
                    {wm}
                  </Badge>
                ))}
              </div>
            ) : (
              <p className="text-muted-foreground text-xs">
                No workload modules reported.
              </p>
            )}
          </div>
        </>
      )}

      <p className="text-muted-foreground text-xs">
        Modules are configured in <code className="font-mono">config.yaml</code>
        .
      </p>
    </div>
  )
}

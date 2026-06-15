// SPDX-License-Identifier: Apache-2.0

// Integrations page — Build → Integrations. The capabilities you give
// agents: harnesses (CLI tools agents shell out to) and native tools
// (HTTP, SQL, etc.). Backed by GET /api/v1/harnesses and
// GET /api/v1/tools/native. Server component: fetched once per render
// (force-dynamic + no-store via lib/api.ts). Each surface loads
// independently so one failing endpoint doesn't blank the other.

import { Boxes, Cpu, Wrench } from "lucide-react"

import {
  api,
  ApiError,
  type HarnessList,
  type NativeToolList,
} from "@/lib/api"
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

async function loadHarnesses(): Promise<{
  data: HarnessList
  error: string | null
}> {
  try {
    const data = await api.harnesses.list()
    return { data, error: null }
  } catch (e) {
    const message =
      e instanceof ApiError
        ? `${e.code}: ${e.message}`
        : e instanceof Error
          ? e.message
          : "Failed to load harnesses"
    return { data: { harnesses: [] }, error: message }
  }
}

async function loadNativeTools(): Promise<{
  data: NativeToolList
  error: string | null
}> {
  try {
    const data = await api.tools.listNative()
    return { data, error: null }
  } catch (e) {
    const message =
      e instanceof ApiError
        ? `${e.code}: ${e.message}`
        : e instanceof Error
          ? e.message
          : "Failed to load native tools"
    return { data: { tools: [] }, error: message }
  }
}

export default async function Page() {
  const [harnessRes, toolsRes] = await Promise.all([
    loadHarnesses(),
    loadNativeTools(),
  ])
  const harnesses = harnessRes.data.harnesses
  const tools = toolsRes.data.tools

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Integrations"
        description="Capabilities you give agents — harnesses, native tools, MCP servers, and skills. Backed by /api/v1/harnesses and /api/v1/tools/native."
      />

      <section className="flex flex-col gap-3">
        <h2 className="text-lg font-semibold tracking-tight">Harnesses</h2>

        {harnessRes.error && harnesses.length === 0 ? (
          <div className="bg-card text-muted-foreground rounded-xl border border-dashed p-6 text-sm">
            <p className="text-foreground mb-1 font-medium">
              Harnesses aren&apos;t available
            </p>
            <p>
              The runtime returned:{" "}
              <code className="bg-muted rounded px-1 font-mono text-xs">
                {harnessRes.error}
              </code>
              .
            </p>
          </div>
        ) : harnesses.length === 0 ? (
          <div className="text-muted-foreground bg-card flex items-center gap-2 rounded-xl border p-6 text-xs">
            <Cpu className="size-3.5" />
            No harnesses detected.
          </div>
        ) : (
          <div className="bg-card rounded-xl border">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                    Provider
                  </TableHead>
                  <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                    Status
                  </TableHead>
                  <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                    Version
                  </TableHead>
                  <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                    Required env
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {harnesses.map((harness) => (
                  <TableRow
                    key={harness.provider}
                    className="hover:bg-muted/30"
                  >
                    <TableCell>
                      <code className="font-mono text-xs">
                        {harness.provider}
                      </code>
                    </TableCell>
                    <TableCell>
                      <HarnessStatusBadge status={harness.status} />
                    </TableCell>
                    <TableCell className="text-muted-foreground text-xs tabular-nums">
                      {harness.version ?? "—"}
                    </TableCell>
                    <TableCell>
                      {harness.required_env.length > 0 ? (
                        <div className="flex flex-wrap items-center gap-1">
                          {harness.required_env.map((env) => (
                            <Badge
                              key={env}
                              variant="outline"
                              className="font-mono text-xs"
                            >
                              {env}
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
      </section>

      <section className="flex flex-col gap-3">
        <h2 className="text-lg font-semibold tracking-tight">Native tools</h2>

        {toolsRes.error && tools.length === 0 ? (
          <div className="bg-card text-muted-foreground rounded-xl border border-dashed p-6 text-sm">
            <p className="text-foreground mb-1 font-medium">
              Native tools aren&apos;t available
            </p>
            <p>
              The runtime returned:{" "}
              <code className="bg-muted rounded px-1 font-mono text-xs">
                {toolsRes.error}
              </code>
              .
            </p>
          </div>
        ) : tools.length === 0 ? (
          <div className="text-muted-foreground bg-card flex items-center gap-2 rounded-xl border p-6 text-xs">
            <Wrench className="size-3.5" />
            No native tools registered.
          </div>
        ) : (
          <div className="bg-card rounded-xl border">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                    Tool
                  </TableHead>
                  <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                    Adapter
                  </TableHead>
                  <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                    Verbs
                  </TableHead>
                  <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                    Enabled
                  </TableHead>
                  <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                    Configured
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {tools.map((tool) => (
                  <TableRow key={tool.tool} className="hover:bg-muted/30">
                    <TableCell>
                      <span className="text-sm font-medium">{tool.tool}</span>
                      {tool.description ? (
                        <p className="text-muted-foreground mt-0.5 text-xs">
                          {tool.description}
                        </p>
                      ) : null}
                    </TableCell>
                    <TableCell>
                      <code className="font-mono text-xs">
                        {tool.adapter_id}
                      </code>
                    </TableCell>
                    <TableCell className="tabular-nums text-xs">
                      {tool.verbs.length}
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant={tool.enabled ? "secondary" : "outline"}
                        className="text-xs"
                      >
                        {tool.enabled ? "Enabled" : "Disabled"}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant={tool.configured ? "secondary" : "outline"}
                        className="text-xs"
                      >
                        {tool.configured ? "Configured" : "Unconfigured"}
                      </Badge>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </section>

      <div className="text-muted-foreground flex items-center gap-2 text-xs">
        <Boxes className="size-3.5" />
        MCP servers and Skills have dedicated tabs under Build.
      </div>
    </div>
  )
}

function HarnessStatusBadge({
  status,
}: {
  status: HarnessList["harnesses"][number]["status"]
}) {
  if (status === "ready") {
    return (
      <Badge variant="secondary" className="text-xs">
        {status}
      </Badge>
    )
  }
  if (status === "needs_auth") {
    return (
      <Badge variant="outline" className="text-xs">
        {status}
      </Badge>
    )
  }
  return (
    <Badge variant="destructive" className="text-xs">
      {status}
    </Badge>
  )
}

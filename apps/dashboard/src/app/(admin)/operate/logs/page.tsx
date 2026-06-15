// SPDX-License-Identifier: Apache-2.0

// Logs page — Operate → Logs. Live tail of recent log lines emitted by the
// runtime, backed by GET /api/v1/logs. Server component: fetched once per
// render (force-dynamic + no-store via lib/api.ts). The runtime keeps an
// in-memory, single-process ring buffer; when it isn't reachable we render a
// clean empty-state shell rather than crashing.

import { ScrollText } from "lucide-react"

import { api, ApiError, type LogLine } from "@/lib/api"
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

async function loadLogs(): Promise<{
  lines: LogLine[]
  error: string | null
}> {
  try {
    const data = await api.logs({ limit: 200 })
    return { lines: data.lines, error: null }
  } catch (e) {
    const message =
      e instanceof ApiError
        ? `${e.code}: ${e.message}`
        : e instanceof Error
          ? e.message
          : "Failed to load logs"
    return { lines: [], error: message }
  }
}

export default async function Page() {
  const { lines, error } = await loadLogs()

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Logs"
        description="Live tail of recent log lines across the runtime. Backed by /api/v1/logs."
      />

      {error && lines.length === 0 ? (
        <div className="bg-card text-muted-foreground rounded-xl border border-dashed p-6 text-sm">
          <p className="text-foreground mb-1 font-medium">
            Logs aren&apos;t available
          </p>
          <p>
            The runtime returned:{" "}
            <code className="bg-muted rounded px-1 font-mono text-xs">
              {error}
            </code>
            .
          </p>
        </div>
      ) : lines.length === 0 ? (
        <div className="text-muted-foreground bg-card flex items-center gap-2 rounded-xl border p-6 text-xs">
          <ScrollText className="size-3.5" />
          No log lines in the in-memory buffer yet.
        </div>
      ) : (
        <div className="bg-card rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Time
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Level
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Service
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Message
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Agent
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {lines.map((line, i) => (
                <TableRow
                  key={`${line.ts}-${i}`}
                  className="hover:bg-muted/30"
                >
                  <TableCell className="text-muted-foreground text-xs tabular-nums whitespace-nowrap">
                    {line.ts}
                  </TableCell>
                  <TableCell>
                    <LevelBadge level={line.level} />
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs">
                    {line.service}
                  </TableCell>
                  <TableCell className="max-w-md truncate font-mono text-xs break-words whitespace-pre-wrap">
                    {line.msg}
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs">
                    {line.agent ?? "—"}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <div className="text-muted-foreground flex items-center gap-2 text-xs">
        <ScrollText className="size-3.5" />
        In-memory, single-process ring buffer. For multi-process deployments,
        ship logs to a real aggregator (Loki, Vector, etc.).
      </div>
    </div>
  )
}

function LevelBadge({ level }: { level: LogLine["level"] }) {
  switch (level) {
    case "error":
      return (
        <Badge variant="destructive" className="text-xs">
          error
        </Badge>
      )
    case "warn":
      return (
        <Badge variant="outline" className="text-xs">
          warn
        </Badge>
      )
    case "info":
      return (
        <Badge variant="secondary" className="text-xs">
          info
        </Badge>
      )
    case "debug":
    default:
      return (
        <Badge variant="secondary" className="text-muted-foreground text-xs">
          debug
        </Badge>
      )
  }
}

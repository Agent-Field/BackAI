// SPDX-License-Identifier: Apache-2.0

// Database page — Build → Database. Tables, views, and RLS coverage, backed
// by GET /api/v1/db/tables. Server component: fetched once per render
// (force-dynamic + no-store via lib/api.ts). When the runtime isn't reachable
// we render a clean empty-state shell rather than crashing.

import { Database } from "lucide-react"

import { api, ApiError, type DBTableList } from "@/lib/api"
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

async function loadTables(): Promise<{
  data: DBTableList
  error: string | null
}> {
  try {
    const data = await api.db.tables()
    return { data, error: null }
  } catch (e) {
    const message =
      e instanceof ApiError
        ? `${e.code}: ${e.message}`
        : e instanceof Error
          ? e.message
          : "Failed to load tables"
    return { data: { tables: [] }, error: message }
  }
}

export default async function Page() {
  const { data, error } = await loadTables()
  const tables = data.tables

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Database"
        description="Tables, views, and RLS coverage. Backed by /api/v1/db/tables."
      />

      {error && tables.length === 0 ? (
        <div className="bg-card text-muted-foreground rounded-xl border border-dashed p-6 text-sm">
          <p className="text-foreground mb-1 font-medium">
            Database catalog isn&apos;t available
          </p>
          <p>
            The runtime returned:{" "}
            <code className="bg-muted rounded px-1 font-mono text-xs">
              {error}
            </code>
            .
          </p>
        </div>
      ) : tables.length === 0 ? (
        <div className="text-muted-foreground bg-card flex items-center gap-2 rounded-xl border p-6 text-xs">
          <Database className="size-3.5" />
          No tables found.
        </div>
      ) : (
        <div className="bg-card rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Table
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Schema
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Kind
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Rows
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Size
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  RLS
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {tables.map((table) => (
                <TableRow
                  key={`${table.schema}.${table.name}`}
                  className="hover:bg-muted/30"
                >
                  <TableCell>
                    <code className="font-mono text-xs">{table.name}</code>
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs">
                    {table.schema}
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline" className="text-xs">
                      {table.kind}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs tabular-nums">
                    {formatNumber(table.estimated_rows)}
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs tabular-nums">
                    {formatBytes(table.size_bytes)}
                  </TableCell>
                  <TableCell>
                    {table.has_rls ? (
                      <Badge variant="secondary" className="text-xs">
                        RLS
                      </Badge>
                    ) : (
                      <Badge variant="outline" className="text-xs">
                        none
                      </Badge>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <p className="text-muted-foreground text-xs">
        The SQL runner and per-table RLS policy view are planned follow-ups.
      </p>
    </div>
  )
}

function formatNumber(value: number): string {
  return new Intl.NumberFormat("en-US").format(value)
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  const value = bytes / Math.pow(1024, i)
  const fixed = value < 10 ? value.toFixed(2) : value.toFixed(1)
  return `${fixed} ${units[i]}`
}

"use client"

// DataTab — paginated TanStack browser for a single table's rows.
//
// Backs onto `api.db.rows({schema, table, limit, offset})`. Columns are
// derived from the runtime's `result.columns` so the studio works for
// arbitrary tables without front-end metadata.

import * as React from "react"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table"
import { Database, RotateCw } from "lucide-react"

import { api, ApiError, type SQLRunResult } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import {
  Pagination,
  PaginationContent,
  PaginationItem,
  PaginationNext,
  PaginationPrevious,
} from "@/components/ui/pagination"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { cn } from "@/lib/utils"

import { stringifyValue, truncate } from "../_lib/format"
import type { SelectedTable } from "./database-studio"

const PAGE_SIZES = [25, 50, 100, 200] as const
type PageSize = (typeof PAGE_SIZES)[number]

type DataTabProps = {
  table: SelectedTable
}

type Row = unknown[]

export function DataTab({ table }: DataTabProps) {
  const [pageSize, setPageSize] = React.useState<PageSize>(100)
  const [offset, setOffset] = React.useState(0)
  const [result, setResult] = React.useState<SQLRunResult | null>(null)
  const [loading, setLoading] = React.useState(true)
  const [error, setError] = React.useState<string | null>(null)

  // Reset page when the table or page size changes.
  React.useEffect(() => {
    setOffset(0)
  }, [table.schema, table.name, pageSize])

  const fetchRows = React.useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const r = await api.db.rows({
        schema: table.schema,
        table: table.name,
        limit: pageSize,
        offset,
      })
      setResult(r)
    } catch (e) {
      setError(
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to load rows",
      )
      setResult(null)
    } finally {
      setLoading(false)
    }
  }, [table.schema, table.name, pageSize, offset])

  React.useEffect(() => {
    void fetchRows()
  }, [fetchRows])

  const columns = React.useMemo<ColumnDef<Row>[]>(() => {
    const cols = result?.columns ?? []
    return cols.map((name, idx) => ({
      id: name,
      header: name,
      cell: ({ row }) => <CellValue value={row.original[idx]} />,
    }))
  }, [result?.columns])

  const tableInstance = useReactTable({
    data: result?.rows ?? [],
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  const rowCount = result?.row_count ?? 0
  const hasMore = (result?.rows.length ?? 0) === pageSize
  const startIdx = rowCount === 0 ? 0 : offset + 1
  const endIdx = offset + rowCount

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <span className="font-mono text-sm">
            {table.schema}.<span className="font-medium">{table.name}</span>
          </span>
          {result ? (
            <Badge variant="outline" className="tabular-nums">
              Showing {startIdx}–{endIdx}
              {result.truncated ? "+" : ""}
            </Badge>
          ) : null}
          {result?.truncated ? (
            <Badge variant="secondary">truncated</Badge>
          ) : null}
        </div>

        <div className="flex items-center gap-2">
          <Select
            value={String(pageSize)}
            onValueChange={(v) => setPageSize(Number(v) as PageSize)}
          >
            <SelectTrigger size="sm" className="w-28">
              <SelectValue placeholder="Page size" />
            </SelectTrigger>
            <SelectContent>
              {PAGE_SIZES.map((n) => (
                <SelectItem key={n} value={String(n)}>
                  {n} / page
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button
            variant="outline"
            size="sm"
            onClick={() => void fetchRows()}
            disabled={loading}
          >
            <RotateCw className={cn(loading && "animate-spin")} />
            Refresh
          </Button>
        </div>
      </div>

      <div className="bg-card max-w-full overflow-x-auto rounded-xl border">
        <ScrollArea className="max-h-[560px]">
          <Table>
            <TableHeader>
              {tableInstance.getHeaderGroups().map((hg) => (
                <TableRow key={hg.id} className="hover:bg-transparent">
                  {hg.headers.map((header) => (
                    <TableHead
                      key={header.id}
                      className="text-muted-foreground text-xs font-medium uppercase tracking-wide whitespace-nowrap"
                    >
                      {header.isPlaceholder
                        ? null
                        : flexRender(
                            header.column.columnDef.header,
                            header.getContext(),
                          )}
                    </TableHead>
                  ))}
                </TableRow>
              ))}
            </TableHeader>
            <TableBody>
              {loading && !result ? (
                <SkeletonRows
                  columns={Math.max(columns.length, 4)}
                  rows={8}
                />
              ) : tableInstance.getRowModel().rows.length === 0 ? (
                <TableRow className="hover:bg-transparent">
                  <TableCell
                    colSpan={Math.max(columns.length, 1)}
                    className="p-0"
                  >
                    <DataEmpty error={error} table={table} />
                  </TableCell>
                </TableRow>
              ) : (
                tableInstance.getRowModel().rows.map((row) => (
                  <TableRow key={row.id} className="hover:bg-muted/30">
                    {row.getVisibleCells().map((cell) => (
                      <TableCell
                        key={cell.id}
                        className="font-mono text-xs whitespace-nowrap"
                      >
                        {flexRender(
                          cell.column.columnDef.cell,
                          cell.getContext(),
                        )}
                      </TableCell>
                    ))}
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </ScrollArea>
      </div>

      {tableInstance.getRowModel().rows.length > 0 ? (
        <div className="flex items-center justify-between gap-2 px-1">
          <p className="text-muted-foreground text-xs tabular-nums">
            Page {Math.floor(offset / pageSize) + 1}
            {hasMore ? "" : " · last page"} · {result?.duration_ms}ms
          </p>
          <Pagination className="m-0 w-fit justify-end">
            <PaginationContent>
              <PaginationItem>
                <PaginationPrevious
                  href="#"
                  className={cn(
                    offset === 0 && "pointer-events-none opacity-50",
                  )}
                  onClick={(e) => {
                    e.preventDefault()
                    if (offset === 0) return
                    setOffset(Math.max(0, offset - pageSize))
                  }}
                />
              </PaginationItem>
              <PaginationItem>
                <PaginationNext
                  href="#"
                  className={cn(
                    !hasMore && "pointer-events-none opacity-50",
                  )}
                  onClick={(e) => {
                    e.preventDefault()
                    if (!hasMore) return
                    setOffset(offset + pageSize)
                  }}
                />
              </PaginationItem>
            </PaginationContent>
          </Pagination>
        </div>
      ) : null}
    </div>
  )
}

function SkeletonRows({ columns, rows }: { columns: number; rows: number }) {
  return (
    <>
      {Array.from({ length: rows }).map((_, i) => (
        <TableRow key={i} className="hover:bg-transparent">
          {Array.from({ length: columns }).map((__, j) => (
            <TableCell key={j}>
              <Skeleton className="h-4 w-24" />
            </TableCell>
          ))}
        </TableRow>
      ))}
    </>
  )
}

function DataEmpty({
  error,
  table,
}: {
  error: string | null
  table: SelectedTable
}) {
  return (
    <Empty className="border-0">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <Database />
        </EmptyMedia>
        <EmptyTitle>{error ? "Couldn't load rows" : "No rows"}</EmptyTitle>
        <EmptyDescription>
          {error ? (
            <span className="font-mono text-xs">{error}</span>
          ) : (
            <>
              <span className="font-mono">
                {table.schema}.{table.name}
              </span>{" "}
              is empty. Insert rows from your application or use the SQL Runner.
            </>
          )}
        </EmptyDescription>
      </EmptyHeader>
    </Empty>
  )
}

function CellValue({ value }: { value: unknown }) {
  if (value === null) {
    return <span className="text-muted-foreground italic">null</span>
  }
  if (value === undefined) {
    return <span className="text-muted-foreground">—</span>
  }
  if (typeof value === "boolean") {
    return (
      <Badge variant={value ? "secondary" : "outline"} className="font-mono">
        {String(value)}
      </Badge>
    )
  }
  const text = stringifyValue(value)
  return <span title={text}>{truncate(text, 80)}</span>
}

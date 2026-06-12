"use client"

// StructureTab — renders columns and indexes for the selected table.
//
// Both child tables are TanStack-driven for consistency with the rest of
// the dashboard. RLS state is surfaced as a badge row above the cards.

import * as React from "react"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table"
import { AlertCircle, ShieldCheck, ShieldOff } from "lucide-react"

import type { DBColumn, DBIndex, DBTableDetail } from "@/lib/api"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

type StructureTabProps = {
  detail: DBTableDetail | null
  loading: boolean
  error: string | null
}

export function StructureTab({ detail, loading, error }: StructureTabProps) {
  if (error) {
    return (
      <Alert variant="destructive">
        <AlertCircle />
        <AlertTitle>Couldn&apos;t load table structure</AlertTitle>
        <AlertDescription>
          <span className="font-mono text-xs">{error}</span>
        </AlertDescription>
      </Alert>
    )
  }

  if (loading && !detail) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-7 w-40" />
        <Skeleton className="h-48 w-full" />
        <Skeleton className="h-32 w-full" />
      </div>
    )
  }

  if (!detail) return null

  return (
    <div className="flex flex-col gap-4">
      <RLSBadgeRow detail={detail} />
      <ColumnsCard columns={detail.columns} />
      <IndexesCard indexes={detail.indexes} />
    </div>
  )
}

function RLSBadgeRow({ detail }: { detail: DBTableDetail }) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      {detail.rls_enabled ? (
        detail.rls_forced ? (
          <Badge variant="secondary">
            <ShieldCheck />
            RLS forced
          </Badge>
        ) : (
          <Badge>
            <ShieldCheck />
            RLS enabled
          </Badge>
        )
      ) : (
        <Badge variant="outline">
          <ShieldOff />
          RLS off
        </Badge>
      )}
      <Badge variant="outline" className="tabular-nums">
        {detail.columns.length} columns
      </Badge>
      <Badge variant="outline" className="tabular-nums">
        {detail.indexes.length} indexes
      </Badge>
      <Badge variant="outline" className="tabular-nums">
        {detail.policies.length} policies
      </Badge>
    </div>
  )
}

function ColumnsCard({ columns }: { columns: DBColumn[] }) {
  const colDefs = React.useMemo<ColumnDef<DBColumn>[]>(
    () => [
      {
        id: "name",
        header: "Name",
        cell: ({ row }) => (
          <span className="font-mono text-xs">{row.original.name}</span>
        ),
      },
      {
        id: "data_type",
        header: "Type",
        cell: ({ row }) => (
          <Badge variant="outline" className="font-mono">
            {row.original.data_type}
          </Badge>
        ),
      },
      {
        id: "is_nullable",
        header: "Nullable",
        cell: ({ row }) =>
          row.original.is_nullable ? (
            <Badge variant="ghost">nullable</Badge>
          ) : (
            <Badge variant="secondary">not null</Badge>
          ),
      },
      {
        id: "default",
        header: "Default",
        cell: ({ row }) => (
          <span className="text-muted-foreground font-mono text-xs">
            {row.original.default_value ?? "—"}
          </span>
        ),
      },
      {
        id: "flags",
        header: "Flags",
        cell: ({ row }) => (
          <div className="flex flex-wrap items-center gap-1">
            {row.original.is_primary_key ? (
              <Badge variant="secondary">PK</Badge>
            ) : null}
            {row.original.is_unique && !row.original.is_primary_key ? (
              <Badge variant="outline">unique</Badge>
            ) : null}
            {!row.original.is_primary_key && !row.original.is_unique ? (
              <span className="text-muted-foreground text-xs">—</span>
            ) : null}
          </div>
        ),
      },
    ],
    [],
  )

  const tableInstance = useReactTable({
    data: columns,
    columns: colDefs,
    getCoreRowModel: getCoreRowModel(),
  })

  return (
    <Card className="gap-0">
      <CardHeader className="border-b pb-4">
        <CardTitle>Columns</CardTitle>
        <CardDescription>
          Column names, types, nullability, defaults, and key/unique flags.
        </CardDescription>
      </CardHeader>
      <CardContent className="p-0">
        {columns.length === 0 ? (
          <Empty className="border-0">
            <EmptyHeader>
              <EmptyTitle>No columns</EmptyTitle>
              <EmptyDescription>
                The runtime returned an empty column list for this table.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <Table>
            <TableHeader>
              {tableInstance.getHeaderGroups().map((hg) => (
                <TableRow key={hg.id} className="hover:bg-transparent">
                  {hg.headers.map((header) => (
                    <TableHead
                      key={header.id}
                      className="text-muted-foreground text-xs font-medium uppercase tracking-wide"
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
              {tableInstance.getRowModel().rows.map((row) => (
                <TableRow key={row.id} className="hover:bg-muted/30">
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id}>
                      {flexRender(
                        cell.column.columnDef.cell,
                        cell.getContext(),
                      )}
                    </TableCell>
                  ))}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

function IndexesCard({ indexes }: { indexes: DBIndex[] }) {
  const colDefs = React.useMemo<ColumnDef<DBIndex>[]>(
    () => [
      {
        id: "name",
        header: "Name",
        cell: ({ row }) => (
          <span className="font-mono text-xs">{row.original.name}</span>
        ),
      },
      {
        id: "definition",
        header: "Definition",
        cell: ({ row }) => (
          <code
            className="text-muted-foreground block max-w-md truncate font-mono text-xs"
            title={row.original.definition}
          >
            {row.original.definition}
          </code>
        ),
      },
      {
        id: "flags",
        header: "Flags",
        cell: ({ row }) => (
          <div className="flex flex-wrap items-center gap-1">
            {row.original.is_primary ? (
              <Badge variant="secondary">primary</Badge>
            ) : null}
            {row.original.is_unique && !row.original.is_primary ? (
              <Badge variant="outline">unique</Badge>
            ) : null}
            {!row.original.is_primary && !row.original.is_unique ? (
              <span className="text-muted-foreground text-xs">—</span>
            ) : null}
          </div>
        ),
      },
    ],
    [],
  )

  const tableInstance = useReactTable({
    data: indexes,
    columns: colDefs,
    getCoreRowModel: getCoreRowModel(),
  })

  return (
    <Card className="gap-0">
      <CardHeader className="border-b pb-4">
        <CardTitle>Indexes</CardTitle>
        <CardDescription>
          Index names with the underlying SQL definition.
        </CardDescription>
      </CardHeader>
      <CardContent className="p-0">
        {indexes.length === 0 ? (
          <Empty className="border-0">
            <EmptyHeader>
              <EmptyTitle>No indexes</EmptyTitle>
              <EmptyDescription>
                This table has no user-defined indexes.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <Table>
            <TableHeader>
              {tableInstance.getHeaderGroups().map((hg) => (
                <TableRow key={hg.id} className="hover:bg-transparent">
                  {hg.headers.map((header) => (
                    <TableHead
                      key={header.id}
                      className="text-muted-foreground text-xs font-medium uppercase tracking-wide"
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
              {tableInstance.getRowModel().rows.map((row) => (
                <TableRow key={row.id} className="hover:bg-muted/30">
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id}>
                      {flexRender(
                        cell.column.columnDef.cell,
                        cell.getContext(),
                      )}
                    </TableCell>
                  ))}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

"use client"

// AuditView — paginated audit log across tenants.
//
// Filters: tenant select, free-text action search (debounced), date range
// (from/to). Pagination uses limit/offset to match the Phase 4 runs view.
//
// Metadata is rendered inside a Collapsible so dense JSON payloads don't
// dominate the table row height.

import * as React from "react"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table"
import { ChevronRight, FileBadge, RotateCw } from "lucide-react"

import {
  api,
  ApiError,
  type AuditEntry,
  type AuditList,
  type Tenant,
} from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import {
  Pagination,
  PaginationContent,
  PaginationItem,
  PaginationNext,
  PaginationPrevious,
} from "@/components/ui/pagination"
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

import { formatRelative } from "../../../operate/_lib/format"

const PAGE_SIZE = 25
const TENANT_ALL = "all"

// datetime-local → ISO. Returns undefined if blank or invalid.
function localToISO(value: string): string | undefined {
  if (!value) return undefined
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return undefined
  return d.toISOString()
}

export function AuditView() {
  const [tenants, setTenants] = React.useState<Tenant[]>([])
  const [tenantFilter, setTenantFilter] = React.useState<string>(TENANT_ALL)
  const [action, setAction] = React.useState("")
  const [debouncedAction, setDebouncedAction] = React.useState("")
  const [from, setFrom] = React.useState("")
  const [to, setTo] = React.useState("")
  const [offset, setOffset] = React.useState(0)
  const [data, setData] = React.useState<AuditList | null>(null)
  const [loading, setLoading] = React.useState(true)
  const [error, setError] = React.useState<string | null>(null)

  React.useEffect(() => {
    const t = setTimeout(() => setDebouncedAction(action.trim()), 300)
    return () => clearTimeout(t)
  }, [action])

  // Reset page when filters change.
  React.useEffect(() => {
    setOffset(0)
  }, [tenantFilter, debouncedAction, from, to])

  React.useEffect(() => {
    void api.admin.tenants
      .list()
      .then((r) => setTenants(r.tenants))
      .catch(() => setTenants([]))
  }, [])

  const tenantNameById = React.useMemo(() => {
    const map = new Map<string, string>()
    for (const t of tenants) map.set(t.id, t.name)
    return map
  }, [tenants])

  const fetchEntries = React.useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const list = await api.admin.audit.list({
        tenant: tenantFilter === TENANT_ALL ? undefined : tenantFilter,
        action: debouncedAction || undefined,
        from: localToISO(from),
        to: localToISO(to),
        limit: PAGE_SIZE,
        offset,
      })
      setData(list)
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to load audit log"
      setError(message)
      setData({ entries: [], total: 0, has_more: false })
    } finally {
      setLoading(false)
    }
  }, [tenantFilter, debouncedAction, from, to, offset])

  React.useEffect(() => {
    void fetchEntries()
  }, [fetchEntries])

  const columns = React.useMemo<ColumnDef<AuditEntry>[]>(
    () => [
      {
        id: "occurred_at",
        header: "Occurred",
        cell: ({ row }) => (
          <span className="text-muted-foreground font-mono text-xs">
            {formatRelative(row.original.occurred_at)}
          </span>
        ),
      },
      {
        id: "action",
        header: "Action",
        cell: ({ row }) => (
          <Badge variant="outline" className="font-mono text-[10px]">
            {row.original.action}
          </Badge>
        ),
      },
      {
        id: "tenant",
        header: "Tenant",
        cell: ({ row }) => (
          <span className="text-muted-foreground text-xs">
            {row.original.tenant_id
              ? (tenantNameById.get(row.original.tenant_id) ??
                row.original.tenant_id)
              : "—"}
          </span>
        ),
      },
      {
        id: "user",
        header: "Actor",
        cell: ({ row }) => (
          <span className="text-muted-foreground font-mono text-xs">
            {row.original.user_id
              ? row.original.user_id
              : row.original.api_key_id
                ? `key:${row.original.api_key_id}`
                : "—"}
          </span>
        ),
      },
      {
        id: "resource",
        header: "Resource",
        cell: ({ row }) => (
          <span className="font-mono text-xs">
            {row.original.resource_type
              ? `${row.original.resource_type}${
                  row.original.resource_id
                    ? `:${row.original.resource_id}`
                    : ""
                }`
              : "—"}
          </span>
        ),
      },
      {
        id: "metadata",
        header: "Metadata",
        cell: ({ row }) => <MetadataCell entry={row.original} />,
      },
    ],
    [tenantNameById],
  )

  const table = useReactTable({
    data: data?.entries ?? [],
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  const total = data?.total ?? 0
  const hasMore = data?.has_more ?? false
  const page = Math.floor(offset / PAGE_SIZE) + 1
  const totalPages = total > 0 ? Math.ceil(total / PAGE_SIZE) : 1
  const startIdx = total === 0 ? 0 : offset + 1
  const endIdx = Math.min(offset + PAGE_SIZE, total)

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <Select
          value={tenantFilter}
          onValueChange={(v) => setTenantFilter(v ?? TENANT_ALL)}
        >
          <SelectTrigger className="w-48">
            <SelectValue placeholder="All tenants" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={TENANT_ALL}>All tenants</SelectItem>
            {tenants.map((t) => (
              <SelectItem key={t.id} value={t.id}>
                {t.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Input
          placeholder="Filter by action…"
          value={action}
          onChange={(e) => setAction(e.target.value)}
          className="w-56"
        />
        <Input
          type="datetime-local"
          value={from}
          onChange={(e) => setFrom(e.target.value)}
          aria-label="From"
          className="w-56"
        />
        <Input
          type="datetime-local"
          value={to}
          onChange={(e) => setTo(e.target.value)}
          aria-label="To"
          className="w-56"
        />
        <div className="ml-auto">
          <Button
            variant="outline"
            size="sm"
            onClick={() => void fetchEntries()}
            disabled={loading}
          >
            <RotateCw className={cn(loading && "animate-spin")} />
            Refresh
          </Button>
        </div>
      </div>

      <div className="rounded-xl border bg-card">
        <Table>
          <TableHeader>
            {table.getHeaderGroups().map((hg) => (
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
            {loading && (data?.entries.length ?? 0) === 0 ? (
              <SkeletonRows columns={columns.length} />
            ) : table.getRowModel().rows.length === 0 ? (
              <TableRow className="hover:bg-transparent">
                <TableCell colSpan={columns.length} className="p-0">
                  <AuditEmpty error={error} />
                </TableCell>
              </TableRow>
            ) : (
              table.getRowModel().rows.map((row) => (
                <TableRow key={row.id} className="hover:bg-muted/30">
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id} className="align-top">
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </TableCell>
                  ))}
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>

      {table.getRowModel().rows.length > 0 ? (
        <div className="flex items-center justify-between gap-2 px-1">
          <p className="text-muted-foreground text-xs">
            Showing {startIdx}–{endIdx} of {total} ·{" "}
            <span className="tabular-nums">
              page {page} of {totalPages}
            </span>
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
                    setOffset(Math.max(0, offset - PAGE_SIZE))
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
                    setOffset(offset + PAGE_SIZE)
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

function MetadataCell({ entry }: { entry: AuditEntry }) {
  const keys = Object.keys(entry.metadata ?? {})
  if (keys.length === 0) {
    return <span className="text-muted-foreground text-xs">—</span>
  }
  return (
    <Collapsible className="w-full">
      <CollapsibleTrigger
        render={
          <Button
            variant="ghost"
            size="sm"
            className="h-6 px-1.5 text-[11px] text-muted-foreground"
          >
            <ChevronRight
              data-icon="inline-start"
              className="size-3 transition-transform group-data-[open]/collapsible:rotate-90"
            />
            {keys.length} field{keys.length === 1 ? "" : "s"}
          </Button>
        }
      />
      <CollapsibleContent>
        <pre className="bg-muted/40 mt-1 max-h-48 overflow-auto rounded-md p-2 font-mono text-[10px]">
          {JSON.stringify(entry.metadata, null, 2)}
        </pre>
      </CollapsibleContent>
    </Collapsible>
  )
}

function SkeletonRows({ columns }: { columns: number }) {
  return (
    <>
      {Array.from({ length: 5 }).map((_, i) => (
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

function AuditEmpty({ error }: { error: string | null }) {
  return (
    <Empty className="border-0">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <FileBadge />
        </EmptyMedia>
        <EmptyTitle>No audit entries match these filters</EmptyTitle>
        <EmptyDescription>
          {error ? (
            <>
              The runtime returned:{" "}
              <code className="font-mono">{error}</code>.
            </>
          ) : (
            "Audit entries are written when tenants, users, keys, or resources change. Try widening the filters."
          )}
        </EmptyDescription>
      </EmptyHeader>
    </Empty>
  )
}


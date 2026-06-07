"use client"

// CustomerBillingView — per-tenant usage and cost rollup.
//
// Pulls the tenant list and fans out to api.admin.tenants.get() in parallel
// to materialise the usage block per row. CSV export builds the file in
// memory and writes it to the clipboard — no server round-trip.
//
// TODO(phase-10): real Stripe linkage (invoices, subscriptions, payment
// methods). For now this is the in-house usage + cost rollup only.

import * as React from "react"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table"
import { ClipboardCopy, ReceiptText, RotateCw } from "lucide-react"
import { toast } from "sonner"

import { api, ApiError, type Tenant } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
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
import { cn } from "@/lib/utils"

function planBadgeVariant(
  plan: string,
): "default" | "secondary" | "outline" {
  switch (plan) {
    case "enterprise":
      return "default"
    case "pro":
      return "secondary"
    default:
      return "outline"
  }
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.min(
    Math.floor(Math.log(bytes) / Math.log(1024)),
    units.length - 1,
  )
  const value = bytes / Math.pow(1024, i)
  return `${value.toFixed(value >= 100 || i === 0 ? 0 : 1)} ${units[i]}`
}

type BillingRow = {
  tenant: Tenant
  requests_30d: number
  cost_usd_30d: number
  storage_bytes: number
  error: string | null
}

function csvEscape(value: string): string {
  if (/[,"\n\r]/.test(value)) {
    return `"${value.replace(/"/g, '""')}"`
  }
  return value
}

function buildCsv(rows: BillingRow[]): string {
  const header = [
    "tenant_id",
    "slug",
    "name",
    "plan",
    "requests_30d",
    "cost_usd_30d",
    "storage_bytes",
  ]
  const lines = [header.join(",")]
  for (const r of rows) {
    lines.push(
      [
        r.tenant.id,
        r.tenant.slug,
        r.tenant.name,
        r.tenant.plan,
        String(r.requests_30d),
        r.cost_usd_30d.toFixed(2),
        String(r.storage_bytes),
      ]
        .map(csvEscape)
        .join(","),
    )
  }
  return lines.join("\n")
}

export function CustomerBillingView() {
  const [rows, setRows] = React.useState<BillingRow[]>([])
  const [loading, setLoading] = React.useState(true)
  const [error, setError] = React.useState<string | null>(null)
  const [exporting, setExporting] = React.useState(false)

  const fetchRows = React.useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const list = await api.admin.tenants.list()
      const details = await Promise.all(
        list.tenants.map((t) =>
          api.admin.tenants
            .get(t.id)
            .then(
              (d): BillingRow => ({
                tenant: t,
                requests_30d: d.usage.requests_30d,
                cost_usd_30d: d.usage.cost_usd_30d,
                storage_bytes: d.usage.storage_bytes,
                error: null,
              }),
            )
            .catch(
              (e: unknown): BillingRow => ({
                tenant: t,
                requests_30d: 0,
                cost_usd_30d: 0,
                storage_bytes: 0,
                error:
                  e instanceof Error ? e.message : "Failed to load usage",
              }),
            ),
        ),
      )
      setRows(details)
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to load tenants"
      setError(message)
      setRows([])
    } finally {
      setLoading(false)
    }
  }, [])

  React.useEffect(() => {
    void fetchRows()
  }, [fetchRows])

  const handleCopyCsv = async () => {
    if (rows.length === 0) {
      toast.error("No rows to export")
      return
    }
    setExporting(true)
    try {
      const csv = buildCsv(rows)
      await navigator.clipboard.writeText(csv)
      toast.success("CSV copied to clipboard", {
        description: `${rows.length} row${rows.length === 1 ? "" : "s"}`,
      })
    } catch {
      toast.error("Could not copy CSV")
    } finally {
      setExporting(false)
    }
  }

  const columns = React.useMemo<ColumnDef<BillingRow>[]>(
    () => [
      {
        id: "tenant",
        header: "Tenant",
        cell: ({ row }) => (
          <div className="flex flex-col">
            <span className="text-sm">{row.original.tenant.name}</span>
            <span className="text-muted-foreground font-mono text-xs">
              {row.original.tenant.slug}
            </span>
          </div>
        ),
      },
      {
        id: "plan",
        header: "Plan",
        cell: ({ row }) => (
          <Badge variant={planBadgeVariant(row.original.tenant.plan)}>
            {row.original.tenant.plan}
          </Badge>
        ),
      },
      {
        id: "requests_30d",
        header: "Requests 30d",
        cell: ({ row }) => (
          <span className="tabular-nums text-xs">
            {row.original.error
              ? "—"
              : row.original.requests_30d.toLocaleString()}
          </span>
        ),
      },
      {
        id: "cost_usd_30d",
        header: "Cost 30d",
        cell: ({ row }) => (
          <span className="tabular-nums text-xs">
            {row.original.error
              ? "—"
              : `$${row.original.cost_usd_30d.toFixed(2)}`}
          </span>
        ),
      },
      {
        id: "storage_bytes",
        header: "Storage",
        cell: ({ row }) => (
          <span className="tabular-nums text-xs">
            {row.original.error ? "—" : formatBytes(row.original.storage_bytes)}
          </span>
        ),
      },
    ],
    [],
  )

  const table = useReactTable({
    data: rows,
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-muted-foreground text-xs">
          {loading
            ? "Loading…"
            : rows.length === 0
              ? "No tenants"
              : `${rows.length} tenant${rows.length === 1 ? "" : "s"} · usage rollup`}
        </span>
        <div className="ml-auto flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={fetchRows}
            disabled={loading}
          >
            <RotateCw className={cn(loading && "animate-spin")} />
            Refresh
          </Button>
          <Button
            size="sm"
            onClick={handleCopyCsv}
            disabled={exporting || rows.length === 0}
          >
            <ClipboardCopy />
            {exporting ? "Copying…" : "Copy CSV"}
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
            {loading && rows.length === 0 ? (
              <SkeletonRows columns={columns.length} />
            ) : rows.length === 0 ? (
              <TableRow className="hover:bg-transparent">
                <TableCell colSpan={columns.length} className="p-0">
                  <BillingEmpty error={error} />
                </TableCell>
              </TableRow>
            ) : (
              table.getRowModel().rows.map((row) => (
                <TableRow key={row.id} className="hover:bg-muted/30">
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id}>
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </TableCell>
                  ))}
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>

      <p className="text-muted-foreground text-[11px]">
        Real billing integration (invoices, subscriptions, payment methods)
        lands in Phase 10. For now this view rolls up runtime usage only.
      </p>
    </div>
  )
}

function SkeletonRows({ columns }: { columns: number }) {
  return (
    <>
      {Array.from({ length: 4 }).map((_, i) => (
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

function BillingEmpty({ error }: { error: string | null }) {
  return (
    <Empty className="border-0">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <ReceiptText />
        </EmptyMedia>
        <EmptyTitle>No tenants to bill yet</EmptyTitle>
        <EmptyDescription>
          {error ? (
            <>
              The runtime returned:{" "}
              <code className="font-mono">{error}</code>.
            </>
          ) : (
            "Create a tenant to start seeing usage and cost here."
          )}
        </EmptyDescription>
      </EmptyHeader>
    </Empty>
  )
}

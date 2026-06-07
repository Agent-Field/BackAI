// SPDX-License-Identifier: Apache-2.0

"use client"

// CustomerBillingView — per-tenant Stripe billing rollup (Phase 10.4).
//
// Pulls api.billing.customers() for the customer list and pairs each row
// with a meter aggregation from api.billing.meters({ tenant }) to surface
// usage + cost for the current billing period. The "Open Stripe Portal"
// button mints a short-lived portal URL via api.billing.portalLink() and
// opens it in a new tab.
//
// shadcn/tailwind rules:
//   - no space-y-*; use flex flex-col + gap-*
//   - Badge for the plan + subscription status
//   - the table sits inside a rounded-xl border bg-card chrome

import * as React from "react"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table"
import {
  ClipboardCopy,
  ExternalLink,
  ReceiptText,
  RotateCw,
} from "lucide-react"
import { toast } from "sonner"

import {
  api,
  ApiError,
  type BillingCustomer,
  type UsageMeterList,
} from "@/lib/api"
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

function statusBadgeVariant(
  status: string | null,
): "default" | "secondary" | "outline" | "destructive" {
  if (status === null || status === "") return "outline"
  switch (status) {
    case "active":
    case "trialing":
      return "default"
    case "past_due":
    case "unpaid":
      return "destructive"
    case "canceled":
    case "incomplete_expired":
      return "outline"
    default:
      return "secondary"
  }
}

type BillingRow = {
  customer: BillingCustomer
  usage: UsageMeterList | null
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
    "stripe_customer_id",
    "email",
    "plan",
    "subscription_status",
    "current_period_end",
    "total_cost_usd",
  ]
  const lines = [header.join(",")]
  for (const r of rows) {
    const total = r.usage?.total_cost_usd ?? 0
    lines.push(
      [
        r.customer.tenant_id,
        r.customer.stripe_customer_id ?? "",
        r.customer.email ?? "",
        r.customer.plan,
        r.customer.subscription_status ?? "",
        r.customer.current_period_end ?? "",
        total.toFixed(4),
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
  const [opening, setOpening] = React.useState<string | null>(null)

  const fetchRows = React.useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const list = await api.billing.customers()
      const details = await Promise.all(
        list.customers.map((customer) =>
          api.billing
            .meters({ tenant: customer.tenant_id })
            .then(
              (usage): BillingRow => ({
                customer,
                usage,
                error: null,
              }),
            )
            .catch(
              (e: unknown): BillingRow => ({
                customer,
                usage: null,
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
            : "Failed to load billing customers"
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

  const handleOpenPortal = async (tenantId: string) => {
    setOpening(tenantId)
    try {
      const link = await api.billing.portalLink(tenantId)
      // Open in a new tab — portal URLs are short-lived (~24h).
      window.open(link.url, "_blank", "noopener,noreferrer")
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to open portal"
      toast.error("Could not open Stripe portal", { description: message })
    } finally {
      setOpening(null)
    }
  }

  const columns = React.useMemo<ColumnDef<BillingRow>[]>(
    () => [
      {
        id: "tenant",
        header: "Tenant",
        cell: ({ row }) => (
          <div className="flex flex-col">
            <span className="font-mono text-xs">
              {row.original.customer.tenant_id}
            </span>
            <span className="text-muted-foreground text-xs">
              {row.original.customer.email ?? "—"}
            </span>
          </div>
        ),
      },
      {
        id: "plan",
        header: "Plan",
        cell: ({ row }) => (
          <Badge variant={planBadgeVariant(row.original.customer.plan)}>
            {row.original.customer.plan}
          </Badge>
        ),
      },
      {
        id: "status",
        header: "Status",
        cell: ({ row }) => {
          const status = row.original.customer.subscription_status
          return (
            <Badge variant={statusBadgeVariant(status)}>{status ?? "—"}</Badge>
          )
        },
      },
      {
        id: "stripe_customer_id",
        header: "Stripe ID",
        cell: ({ row }) => (
          <span className="text-muted-foreground font-mono text-xs">
            {row.original.customer.stripe_customer_id ?? "—"}
          </span>
        ),
      },
      {
        id: "cost",
        header: "Cost (period)",
        cell: ({ row }) => {
          if (row.original.error) {
            return <span className="tabular-nums text-xs">—</span>
          }
          const usd = row.original.usage?.total_cost_usd ?? 0
          const abs = Math.abs(usd)
          const digits = abs === 0 ? 2 : abs < 0.0001 ? 6 : abs < 0.01 ? 4 : 2
          return (
            <span className="tabular-nums text-xs">
              ${usd.toFixed(digits)}
            </span>
          )
        },
      },
      {
        id: "period_end",
        header: "Period ends",
        cell: ({ row }) => {
          const v = row.original.customer.current_period_end
          if (v === null || v === "") return <span className="text-xs">—</span>
          const d = new Date(v)
          return (
            <span className="text-muted-foreground text-xs tabular-nums">
              {Number.isNaN(d.getTime()) ? v : d.toISOString().slice(0, 10)}
            </span>
          )
        },
      },
      {
        id: "actions",
        header: "",
        cell: ({ row }) => {
          const tid = row.original.customer.tenant_id
          const busy = opening === tid
          // Stub mode: stripe_customer_id starts with "cus_stub_" when
          // the runtime is running without STRIPE_SECRET_KEY. The
          // Portal link would just go to example.com, so hide the
          // button and show the dev hint instead.
          const isStub = (
            row.original.customer.stripe_customer_id ?? ""
          ).startsWith("cus_stub_")
          if (isStub) {
            return (
              <div className="flex justify-end">
                <span
                  className="text-muted-foreground text-xs"
                  title="Set STRIPE_SECRET_KEY in your .env and restart the runtime to enable the Stripe Portal."
                >
                  Stripe stub — set <code className="font-mono">STRIPE_SECRET_KEY</code>
                </span>
              </div>
            )
          }
          return (
            <div className="flex justify-end">
              <Button
                variant="outline"
                size="sm"
                onClick={() => void handleOpenPortal(tid)}
                disabled={busy}
              >
                {busy ? "Opening…" : "Open Stripe Portal"}
                <ExternalLink />
              </Button>
            </div>
          )
        },
      },
    ],
    // handleOpenPortal closes over `opening` — re-create columns when it changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [opening],
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
              ? "No customers"
              : `${rows.length} customer${rows.length === 1 ? "" : "s"} · current period`}
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
        Stripe customers + per-tenant usage meters from
        <code className="font-mono"> /api/v1/billing/*</code>. Portal links
        are short-lived (~24h). When the runtime is in stub mode (no
        STRIPE_SECRET_KEY), the portal opens
        <code className="font-mono"> example.com</code> as a placeholder.
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
        <EmptyTitle>No billing customers yet</EmptyTitle>
        <EmptyDescription>
          {error ? (
            <>
              The runtime returned:{" "}
              <code className="font-mono">{error}</code>.
            </>
          ) : (
            "Provision a tenant + open its Stripe portal to seed the first row."
          )}
        </EmptyDescription>
      </EmptyHeader>
    </Empty>
  )
}
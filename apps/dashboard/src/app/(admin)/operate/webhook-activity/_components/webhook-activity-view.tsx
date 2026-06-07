// SPDX-License-Identifier: Apache-2.0

"use client"

// Webhook activity view — interactive client component.
//
// Renders a KPI strip + filterable deliveries table with 5s auto-refresh,
// status badges, a direction toggle (all/inbound/outbound), and a
// per-row detail sheet showing the full payload + retry button.

import { useEffect, useMemo, useState } from "react"
import {
  ArrowDownToLine,
  ArrowUpFromLine,
  Pause,
  Play,
  RefreshCw,
  Webhook,
} from "lucide-react"
import {
  type ColumnDef,
  flexRender,
  getCoreRowModel,
  useReactTable,
} from "@tanstack/react-table"
import { toast } from "sonner"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  api,
  type WebhookDelivery,
  type WebhookDeliveryList,
  type WebhookDirection,
  type WebhookEndpointList,
} from "@/lib/api"

import { formatRelative } from "../../_lib/format"

type Props = {
  initialDeliveries: WebhookDeliveryList
  initialEndpoints: WebhookEndpointList
}

const POLL_MS = 5000

const STATUS_VARIANT: Record<
  WebhookDelivery["status"],
  "default" | "secondary" | "destructive" | "outline"
> = {
  queued: "secondary",
  delivering: "secondary",
  succeeded: "default",
  failed: "destructive",
  dropped_duplicate: "outline",
  dropped_invalid_signature: "destructive",
}

export function WebhookActivityView({
  initialDeliveries,
  initialEndpoints,
}: Props) {
  const [deliveries, setDeliveries] = useState(initialDeliveries)
  const [endpoints] = useState(initialEndpoints)
  const [direction, setDirection] = useState<"all" | WebhookDirection>("all")
  const [status, setStatus] = useState<string>("all")
  const [paused, setPaused] = useState(false)
  const [refreshing, setRefreshing] = useState(false)

  const refresh = async () => {
    setRefreshing(true)
    try {
      const next = await api.webhooks.deliveries({
        limit: 50,
        direction: direction === "all" ? undefined : direction,
        status: status === "all" ? undefined : status,
      })
      setDeliveries(next)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to refresh")
    } finally {
      setRefreshing(false)
    }
  }

  useEffect(() => {
    refresh()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [direction, status])

  useEffect(() => {
    if (paused) return
    const id = setInterval(refresh, POLL_MS)
    return () => clearInterval(id)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [paused, direction, status])

  const counts = useMemo(() => {
    let inbound = 0
    let outbound = 0
    let succeeded = 0
    let failed = 0
    for (const d of deliveries.deliveries) {
      if (d.direction === "inbound") inbound++
      else outbound++
      if (d.status === "succeeded") succeeded++
      else if (
        d.status === "failed" ||
        d.status === "dropped_invalid_signature"
      )
        failed++
    }
    return { inbound, outbound, succeeded, failed }
  }, [deliveries])

  const columns = useMemo<ColumnDef<WebhookDelivery>[]>(
    () => [
      {
        id: "direction",
        header: "",
        cell: ({ row }) =>
          row.original.direction === "inbound" ? (
            <ArrowDownToLine
              className="size-4 text-blue-500"
              aria-label="inbound"
            />
          ) : (
            <ArrowUpFromLine
              className="size-4 text-emerald-500"
              aria-label="outbound"
            />
          ),
      },
      {
        id: "status",
        header: "Status",
        cell: ({ row }) => (
          <Badge variant={STATUS_VARIANT[row.original.status] ?? "secondary"}>
            {row.original.status.replace(/_/g, " ")}
          </Badge>
        ),
      },
      {
        id: "event_type",
        header: "Event",
        cell: ({ row }) => (
          <span className="font-mono text-xs">{row.original.event_type}</span>
        ),
      },
      {
        id: "destination",
        header: "Destination",
        cell: ({ row }) => (
          <span
            className="text-muted-foreground font-mono text-xs"
            title={row.original.destination}
          >
            {truncate(row.original.destination, 40)}
          </span>
        ),
      },
      {
        id: "response_status",
        header: "Response",
        cell: ({ row }) => (
          <span className="text-muted-foreground tabular-nums text-xs">
            {row.original.response_status ?? "—"}
          </span>
        ),
      },
      {
        id: "attempts",
        header: "Attempts",
        cell: ({ row }) => (
          <span className="tabular-nums text-xs">{row.original.attempts}</span>
        ),
      },
      {
        id: "created_at",
        header: "Created",
        cell: ({ row }) => (
          <span className="text-muted-foreground text-xs">
            {formatRelative(row.original.created_at)}
          </span>
        ),
      },
    ],
    [],
  )

  const table = useReactTable({
    data: deliveries.deliveries,
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  return (
    <div className="flex flex-col gap-6">
      <div className="grid grid-cols-1 gap-4 md:grid-cols-4">
        <KpiCard
          icon={<ArrowDownToLine className="size-4" />}
          title="Inbound"
          value={counts.inbound.toString()}
          description={`${endpoints.endpoints.length} endpoint${endpoints.endpoints.length === 1 ? "" : "s"} configured`}
        />
        <KpiCard
          icon={<ArrowUpFromLine className="size-4" />}
          title="Outbound"
          value={counts.outbound.toString()}
          description="Recent dispatches via /webhooks/send"
        />
        <KpiCard
          icon={<Webhook className="size-4" />}
          title="Succeeded"
          value={counts.succeeded.toString()}
          description="Delivered + acked by recipient"
        />
        <KpiCard
          icon={<Webhook className="size-4" />}
          title="Failed"
          value={counts.failed.toString()}
          description="Awaiting retry or dropped at the door"
        />
      </div>

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between gap-4">
            <div>
              <CardTitle>Recent deliveries</CardTitle>
              <CardDescription>
                Auto-refreshing every {POLL_MS / 1000}s. Click any row for the
                full payload.
              </CardDescription>
            </div>
            <div className="flex items-center gap-2">
              <Select
                value={direction}
                onValueChange={(v) =>
                  setDirection(v as "all" | WebhookDirection)
                }
              >
                <SelectTrigger className="min-w-[9rem]">
                  <SelectValue>
                    {direction === "all"
                      ? "All directions"
                      : direction === "inbound"
                        ? "Inbound"
                        : "Outbound"}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All directions</SelectItem>
                  <SelectItem value="inbound">Inbound</SelectItem>
                  <SelectItem value="outbound">Outbound</SelectItem>
                </SelectContent>
              </Select>
              <Select
                value={status}
                onValueChange={(v) => setStatus(v ?? "all")}
              >
                <SelectTrigger className="min-w-[9rem]">
                  <SelectValue>
                    {status === "all" ? "All statuses" : status}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All statuses</SelectItem>
                  <SelectItem value="queued">queued</SelectItem>
                  <SelectItem value="delivering">delivering</SelectItem>
                  <SelectItem value="succeeded">succeeded</SelectItem>
                  <SelectItem value="failed">failed</SelectItem>
                  <SelectItem value="dropped_duplicate">duplicate</SelectItem>
                  <SelectItem value="dropped_invalid_signature">
                    invalid sig
                  </SelectItem>
                </SelectContent>
              </Select>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setPaused((p) => !p)}
              >
                {paused ? (
                  <Play data-icon="inline-start" />
                ) : (
                  <Pause data-icon="inline-start" />
                )}
                {paused ? "Resume" : "Pause"}
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={refresh}
                disabled={refreshing}
              >
                <RefreshCw data-icon="inline-start" />
                Refresh
              </Button>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              {table.getHeaderGroups().map((hg) => (
                <TableRow key={hg.id}>
                  {hg.headers.map((h) => (
                    <TableHead key={h.id}>
                      {h.isPlaceholder
                        ? null
                        : flexRender(h.column.columnDef.header, h.getContext())}
                    </TableHead>
                  ))}
                </TableRow>
              ))}
            </TableHeader>
            <TableBody>
              {table.getRowModel().rows.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={columns.length} className="h-24 text-center">
                    <span className="text-muted-foreground">
                      No deliveries match these filters yet.
                    </span>
                  </TableCell>
                </TableRow>
              ) : (
                table.getRowModel().rows.map((row) => (
                  <TableRow key={row.id}>
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
        </CardContent>
      </Card>
    </div>
  )
}

function KpiCard(props: {
  icon: React.ReactNode
  title: string
  value: string
  description: string
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-sm font-medium text-muted-foreground">
          {props.icon}
          {props.title}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="text-3xl font-semibold tabular-nums">{props.value}</div>
        <p className="text-muted-foreground mt-1 text-xs">{props.description}</p>
      </CardContent>
    </Card>
  )
}

function truncate(s: string, n: number): string {
  return s.length <= n ? s : `${s.slice(0, n)}…`
}
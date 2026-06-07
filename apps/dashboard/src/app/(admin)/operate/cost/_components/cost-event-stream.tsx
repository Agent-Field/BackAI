"use client"

// Live cost event stream.
//
// Polls `api.costEvents({ limit: 50 })` every 5s using a single in-flight
// AbortController so a slow response never piles up. The table is built
// with @tanstack/react-table to match the rest of the dashboard. Empty
// and error states are quiet — this section is meant to be glanced at,
// not stared at.

import * as React from "react"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table"
import { Activity, CircleDot, Pause, Play } from "lucide-react"

import { api, ApiError, type CostEvent } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

import { formatRelative } from "../../_lib/format"
import { formatCurrency } from "./format"

const REFRESH_INTERVAL_MS = 5000
const LIMIT = 50

type CostEventStreamProps = {
  initialEvents?: CostEvent[]
  /** Optional client-side filters. Forwarded as query params. */
  tenant?: string
  model?: string
}

function truncate(value: string, max = 10): string {
  if (value.length <= max) return value
  return `${value.slice(0, max)}…`
}

function tokenSummary(ev: CostEvent): string {
  return `${ev.prompt_tokens.toLocaleString()} + ${ev.completion_tokens.toLocaleString()}`
}

export function CostEventStream({
  initialEvents = [],
  tenant,
  model,
}: CostEventStreamProps) {
  const [events, setEvents] = React.useState<CostEvent[]>(initialEvents)
  const [error, setError] = React.useState<string | null>(null)
  const [loaded, setLoaded] = React.useState<boolean>(
    initialEvents.length > 0,
  )
  const [paused, setPaused] = React.useState<boolean>(false)

  const abortRef = React.useRef<AbortController | null>(null)
  const inFlight = React.useRef<boolean>(false)

  const fetchOnce = React.useCallback(async () => {
    if (inFlight.current) return
    inFlight.current = true
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    try {
      const data = await api.costEvents({
        limit: LIMIT,
        tenant: tenant || undefined,
        model: model || undefined,
      })
      if (controller.signal.aborted) return
      setEvents(data.events)
      setError(null)
      setLoaded(true)
    } catch (e) {
      if (controller.signal.aborted) return
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to load events"
      setError(message)
    } finally {
      inFlight.current = false
    }
  }, [tenant, model])

  // Fire once on mount (only if we don't already have server-rendered
  // events) and then poll on an interval. Tabs in the background pause
  // polling to be a good citizen.
  React.useEffect(() => {
    if (!loaded) void fetchOnce()
    return () => {
      abortRef.current?.abort()
    }
    // We intentionally only want this on mount.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  React.useEffect(() => {
    if (paused) return
    const id = setInterval(() => {
      if (typeof document !== "undefined" && document.hidden) return
      void fetchOnce()
    }, REFRESH_INTERVAL_MS)
    return () => {
      clearInterval(id)
    }
  }, [paused, fetchOnce])

  // Refetch when filters change.
  React.useEffect(() => {
    void fetchOnce()
  }, [tenant, model, fetchOnce])

  const columns = React.useMemo<ColumnDef<CostEvent>[]>(
    () => [
      {
        id: "occurred_at",
        header: "When",
        cell: ({ row }) => (
          <span className="text-muted-foreground text-xs">
            {formatRelative(row.original.occurred_at)}
          </span>
        ),
      },
      {
        id: "tenant",
        header: "Tenant",
        cell: ({ row }) => (
          <span
            className="font-mono text-[11px]"
            title={row.original.tenant_id ?? ""}
          >
            {row.original.tenant_id
              ? truncate(row.original.tenant_id, 12)
              : "—"}
          </span>
        ),
      },
      {
        id: "model",
        header: "Model",
        cell: ({ row }) => (
          <Badge variant="outline" className="font-mono">
            {row.original.model}
          </Badge>
        ),
      },
      {
        id: "tokens",
        header: "Tokens",
        cell: ({ row }) => (
          <span
            className="tabular-nums text-xs"
            title={`${row.original.total_tokens.toLocaleString()} total (prompt + completion)`}
          >
            {tokenSummary(row.original)}
          </span>
        ),
      },
      {
        id: "cost",
        header: "Cost",
        cell: ({ row }) => (
          <span className="font-mono tabular-nums text-xs">
            {formatCurrency(row.original.cost_usd)}
          </span>
        ),
      },
      {
        id: "cached",
        header: "Cached",
        cell: ({ row }) =>
          row.original.cached ? (
            <Badge variant="secondary">Cached</Badge>
          ) : (
            <Badge variant="ghost">Live</Badge>
          ),
      },
      {
        id: "latency",
        header: "Latency",
        cell: ({ row }) => (
          <span className="tabular-nums text-xs">
            {row.original.latency_ms}ms
          </span>
        ),
      },
    ],
    [],
  )

  const table = useReactTable({
    data: events,
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  return (
    <Card className="gap-0">
      <CardHeader className="border-b pb-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-col gap-0.5">
            <CardTitle className="flex items-center gap-2">
              Live events
              {!paused && events.length > 0 ? (
                <CircleDot
                  className="size-3 animate-pulse text-emerald-500"
                  aria-hidden
                />
              ) : null}
            </CardTitle>
            <CardDescription>
              {paused
                ? "Paused. Resume to keep the table fresh every 5s."
                : "Auto-refreshing every 5s. Latest 50 model invocations."}
            </CardDescription>
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => setPaused((p) => !p)}
            >
              {paused ? <Play /> : <Pause />}
              {paused ? "Resume" : "Pause"}
            </Button>
          </div>
        </div>
      </CardHeader>

      {events.length === 0 ? (
        <div className="p-4">
          <Empty className="border-dashed">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <Activity />
              </EmptyMedia>
              <EmptyTitle>
                {error ? "Could not load events" : "No events yet"}
              </EmptyTitle>
              <EmptyDescription>
                {error ? (
                  <span className="font-mono text-xs">{error}</span>
                ) : (
                  "Every billed model call lands here in real time. Run an agent to see events flow in."
                )}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        </div>
      ) : (
        <ScrollArea className="max-h-[420px]">
          <Table>
            <TableHeader>
              {table.getHeaderGroups().map((hg) => (
                <TableRow key={hg.id} className="hover:bg-transparent">
                  {hg.headers.map((h) => (
                    <TableHead
                      key={h.id}
                      className="text-muted-foreground text-xs font-medium uppercase tracking-wide"
                    >
                      {h.isPlaceholder
                        ? null
                        : flexRender(
                            h.column.columnDef.header,
                            h.getContext(),
                          )}
                    </TableHead>
                  ))}
                </TableRow>
              ))}
            </TableHeader>
            <TableBody>
              {table.getRowModel().rows.map((row) => (
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
        </ScrollArea>
      )}
    </Card>
  )
}

/** Helper for CSV export — call from a sibling component when needed. */
export function eventsToCsvRows(events: CostEvent[]): (string | number)[][] {
  const header = [
    "occurred_at",
    "tenant_id",
    "model",
    "provider",
    "prompt_tokens",
    "completion_tokens",
    "total_tokens",
    "cost_usd",
    "cached",
    "latency_ms",
    "agent",
    "api_key_id",
  ]
  const rows: (string | number)[][] = [header]
  for (const e of events) {
    rows.push([
      e.occurred_at,
      e.tenant_id ?? "",
      e.model,
      e.provider,
      e.prompt_tokens,
      e.completion_tokens,
      e.total_tokens,
      e.cost_usd.toFixed(6),
      e.cached ? "true" : "false",
      e.latency_ms,
      e.agent ?? "",
      e.api_key_id ?? "",
    ])
  }
  return rows
}

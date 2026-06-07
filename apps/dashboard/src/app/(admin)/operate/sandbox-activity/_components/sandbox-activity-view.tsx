"use client"

// SandboxActivityView — the client-side wrapper around the sandbox
// activity surface.
//
// Responsibilities:
//   - Keep the pool snapshot + run list fresh (5s polling, single
//     in-flight controller, pause/resume button, tab-hidden gate).
//   - Drive filtering (tenant, status) and pagination via limit/offset.
//   - Hand off detail inspection to <SandboxRunDetailSheet />.

import * as React from "react"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table"
import {
  CircleDot,
  Pause,
  Play,
  RotateCw,
  TerminalSquare,
} from "lucide-react"

import {
  api,
  ApiError,
  type SandboxPoolStats,
  type SandboxRun,
  type SandboxRunList,
} from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
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

import { formatRelative } from "../../_lib/format"
import { formatCurrency } from "../../cost/_components/format"
import { SandboxAdapterCard } from "./sandbox-adapter-card"
import { SandboxKpiStrip } from "./sandbox-kpi-strip"
import { SandboxRunDetailSheet } from "./sandbox-run-detail-sheet"
import { SandboxStatusBadge } from "./sandbox-status-badge"

const PAGE_SIZE = 25
const REFRESH_INTERVAL_MS = 5000

type StatusFilter = "all" | SandboxRun["status"]

const STATUSES: { value: StatusFilter; label: string }[] = [
  { value: "all", label: "All statuses" },
  { value: "queued", label: "Queued" },
  { value: "running", label: "Running" },
  { value: "done", label: "Done" },
  { value: "failed", label: "Failed" },
  { value: "timeout", label: "Timeout" },
  { value: "killed", label: "Killed" },
]

const SAMPLE_RUN_CMD = `curl -X POST http://localhost:8080/api/v1/sandbox/run \\
  -H "Content-Type: application/json" \\
  -d '{
    "image": "python:3.12-slim",
    "command": ["python", "-c", "print(\\"hello sandbox\\")"]
  }'`

type SandboxActivityViewProps = {
  initialPool: SandboxPoolStats | null
  initialRuns: SandboxRunList | null
  initialError: string | null
}

export function SandboxActivityView({
  initialPool,
  initialRuns,
  initialError,
}: SandboxActivityViewProps) {
  const [pool, setPool] = React.useState<SandboxPoolStats | null>(initialPool)
  const [data, setData] = React.useState<SandboxRunList | null>(initialRuns)
  const [tenant, setTenant] = React.useState("")
  const [status, setStatus] = React.useState<StatusFilter>("all")
  const [offset, setOffset] = React.useState(0)
  const [loading, setLoading] = React.useState<boolean>(initialRuns === null)
  const [error, setError] = React.useState<string | null>(initialError)
  const [paused, setPaused] = React.useState(false)
  const [openRun, setOpenRun] = React.useState<SandboxRun | null>(null)

  const [debouncedTenant, setDebouncedTenant] = React.useState("")
  React.useEffect(() => {
    const t = setTimeout(() => setDebouncedTenant(tenant.trim()), 300)
    return () => clearTimeout(t)
  }, [tenant])

  // Reset to first page when filters change.
  React.useEffect(() => {
    setOffset(0)
  }, [debouncedTenant, status])

  const abortRef = React.useRef<AbortController | null>(null)
  const inFlight = React.useRef(false)

  const fetchOnce = React.useCallback(
    async ({ silent }: { silent: boolean } = { silent: false }) => {
      if (inFlight.current) return
      inFlight.current = true
      abortRef.current?.abort()
      const controller = new AbortController()
      abortRef.current = controller
      if (!silent) setLoading(true)
      try {
        const [poolResult, runsResult] = await Promise.allSettled([
          api.sandbox.pool(),
          api.sandbox.list({
            tenant: debouncedTenant || undefined,
            status: status === "all" ? undefined : status,
            limit: PAGE_SIZE,
            offset,
          }),
        ])
        if (controller.signal.aborted) return
        if (poolResult.status === "fulfilled") {
          setPool(poolResult.value)
        }
        if (runsResult.status === "fulfilled") {
          setData(runsResult.value)
          setError(null)
        } else {
          const reason = runsResult.reason
          const message =
            reason instanceof ApiError
              ? `${reason.code}: ${reason.message}`
              : reason instanceof Error
                ? reason.message
                : "Failed to load sandbox runs"
          setError(message)
          setData({ runs: [], total: 0, has_more: false })
        }
      } finally {
        inFlight.current = false
        if (!silent) setLoading(false)
      }
    },
    [debouncedTenant, status, offset],
  )

  // Refetch when filters/pagination change.
  React.useEffect(() => {
    void fetchOnce()
  }, [fetchOnce])

  // Polling loop. Pauses on tab hidden + on explicit pause.
  React.useEffect(() => {
    if (paused) return
    const id = setInterval(() => {
      if (typeof document !== "undefined" && document.hidden) return
      void fetchOnce({ silent: true })
    }, REFRESH_INTERVAL_MS)
    return () => clearInterval(id)
  }, [paused, fetchOnce])

  // Abort any in-flight request on unmount.
  React.useEffect(() => {
    return () => abortRef.current?.abort()
  }, [])

  const columns = React.useMemo<ColumnDef<SandboxRun>[]>(
    () => [
      {
        id: "status",
        header: "Status",
        cell: ({ row }) => <SandboxStatusBadge status={row.original.status} />,
      },
      {
        id: "adapter",
        header: "Adapter",
        cell: ({ row }) => (
          <Badge variant="outline" className="font-mono">
            {row.original.adapter}
          </Badge>
        ),
      },
      {
        id: "image",
        header: "Image",
        cell: ({ row }) => (
          <span
            className="font-mono text-xs"
            title={row.original.image}
          >
            {truncate(row.original.image, 40)}
          </span>
        ),
      },
      {
        id: "command",
        header: "Command",
        cell: ({ row }) => {
          const joined = row.original.command.join(" ")
          return (
            <span
              className="text-muted-foreground font-mono text-xs"
              title={joined}
            >
              {truncate(joined, 60)}
            </span>
          )
        },
      },
      {
        id: "tenant",
        header: "Tenant",
        cell: ({ row }) => {
          const id = row.original.tenant_id
          if (!id) return <span className="text-muted-foreground text-[11px]">—</span>
          const label =
            id === "00000000-0000-0000-0000-000000000000"
              ? "Default"
              : truncate(id, 12)
          return (
            <span
              className="text-muted-foreground font-mono text-[11px]"
              title={id}
            >
              {label}
            </span>
          )
        },
      },
      {
        id: "duration_s",
        header: "Duration",
        cell: ({ row }) => (
          <span className="tabular-nums text-xs">
            {formatSeconds(row.original.duration_s)}
          </span>
        ),
      },
      {
        id: "cpu_seconds",
        header: "CPU·s",
        cell: ({ row }) => (
          <span className="tabular-nums text-xs">
            {formatNumber(row.original.cpu_seconds)}
          </span>
        ),
      },
      {
        id: "cost_usd",
        header: "Cost",
        cell: ({ row }) => (
          <span className="font-mono tabular-nums text-xs">
            {formatCurrency(row.original.cost_usd ?? 0)}
          </span>
        ),
      },
      {
        id: "started_at",
        header: "Started",
        cell: ({ row }) => (
          <span className="text-muted-foreground text-xs">
            {formatRelative(row.original.started_at ?? row.original.created_at)}
          </span>
        ),
      },
    ],
    [],
  )

  const table = useReactTable({
    data: data?.runs ?? [],
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  const total = data?.total ?? 0
  const hasMore = data?.has_more ?? false
  const page = Math.floor(offset / PAGE_SIZE) + 1
  const totalPages = total > 0 ? Math.ceil(total / PAGE_SIZE) : 1
  const startIdx = total === 0 ? 0 : offset + 1
  const endIdx = Math.min(offset + PAGE_SIZE, total)
  const runsForView = table.getRowModel().rows
  const liveRunning = data?.runs.some((r) => r.status === "running") ?? false

  return (
    <div className="flex flex-col gap-6">
      {pool ? <SandboxKpiStrip pool={pool} /> : <KpiSkeleton />}

      {pool ? (
        <SandboxAdapterCard
          adapter={pool.adapter}
          capabilities={pool.capabilities}
        />
      ) : null}

      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-col gap-0.5">
            <div className="flex items-center gap-2">
              <h2 className="font-heading text-base font-medium">
                Recent sandbox runs
              </h2>
              {!paused && liveRunning ? (
                <CircleDot
                  className="size-3 animate-pulse text-emerald-500"
                  aria-hidden
                />
              ) : null}
            </div>
            <p className="text-muted-foreground text-xs">
              {paused
                ? "Paused. Resume to keep this table fresh every 5s."
                : "Auto-refreshing every 5s. Click any row for the full detail."}
            </p>
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
            <Button
              variant="outline"
              size="sm"
              onClick={() => void fetchOnce()}
              disabled={loading}
            >
              <RotateCw className={cn(loading && "animate-spin")} />
              Refresh
            </Button>
          </div>
        </div>

        <FiltersBar
          tenant={tenant}
          status={status}
          onTenantChange={setTenant}
          onStatusChange={setStatus}
        />

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
              {loading && (data?.runs.length ?? 0) === 0 ? (
                <SkeletonRows columns={columns.length} />
              ) : runsForView.length === 0 ? (
                <TableRow className="hover:bg-transparent">
                  <TableCell colSpan={columns.length} className="p-0">
                    <SandboxEmptyState error={error} />
                  </TableCell>
                </TableRow>
              ) : (
                runsForView.map((row) => (
                  <TableRow
                    key={row.id}
                    className="cursor-pointer"
                    onClick={() => setOpenRun(row.original)}
                  >
                    {row.getVisibleCells().map((cell) => (
                      <TableCell key={cell.id}>
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
        </div>

        {runsForView.length > 0 ? (
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

      <SandboxRunDetailSheet
        run={openRun}
        onOpenChange={(open) => {
          if (!open) setOpenRun(null)
        }}
        onStopped={() => void fetchOnce({ silent: true })}
      />
    </div>
  )
}

function FiltersBar({
  tenant,
  status,
  onTenantChange,
  onStatusChange,
}: {
  tenant: string
  status: StatusFilter
  onTenantChange: (v: string) => void
  onStatusChange: (v: StatusFilter) => void
}) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Input
        placeholder="Filter by tenant…"
        value={tenant}
        onChange={(e) => onTenantChange(e.target.value)}
        className="w-56"
      />
      <Select
        value={status}
        onValueChange={(v) => onStatusChange(v as StatusFilter)}
      >
        <SelectTrigger className="w-44">
          <SelectValue placeholder="Status" />
        </SelectTrigger>
        <SelectContent>
          {STATUSES.map((s) => (
            <SelectItem key={s.value} value={s.value}>
              {s.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}

function KpiSkeleton() {
  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4">
      {Array.from({ length: 4 }).map((_, i) => (
        <div
          key={i}
          className="rounded-xl bg-card p-4 ring-1 ring-foreground/10"
        >
          <Skeleton className="h-3 w-20" />
          <Skeleton className="mt-2 h-9 w-32" />
          <Skeleton className="mt-3 h-3 w-40" />
        </div>
      ))}
    </div>
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

function SandboxEmptyState({ error }: { error: string | null }) {
  if (error) {
    return (
      <Empty className="border-0">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <TerminalSquare />
          </EmptyMedia>
          <EmptyTitle>Couldn&apos;t load sandbox runs</EmptyTitle>
          <EmptyDescription>
            The runtime returned:{" "}
            <code className="font-mono">{error}</code>. Check that the sandbox
            module is enabled in <code className="font-mono">config.yaml</code>.
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }
  return (
    <Empty className="border-0">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <TerminalSquare />
        </EmptyMedia>
        <EmptyTitle>No sandbox runs yet</EmptyTitle>
        <EmptyDescription>
          Submit a workload to spin up an isolated sandbox. Runs land here as
          soon as the adapter accepts them.
        </EmptyDescription>
      </EmptyHeader>
      <pre className="bg-muted text-muted-foreground mt-4 max-w-2xl overflow-x-auto rounded-md p-3 text-left font-mono text-xs">
        {SAMPLE_RUN_CMD}
      </pre>
    </Empty>
  )
}

// ─── Helpers ──────────────────────────────────────────────────────────────

function truncate(value: string, max: number): string {
  if (value.length <= max) return value
  return `${value.slice(0, max)}…`
}

function formatSeconds(s: number | null | undefined): string {
  if (s === null || s === undefined) return "—"
  if (s < 1) return `${(s * 1000).toFixed(0)}ms`
  if (s < 60) return `${s.toFixed(1)}s`
  const mins = Math.floor(s / 60)
  const secs = Math.round(s % 60)
  return `${mins}m ${secs}s`
}

function formatNumber(n: number | null | undefined): string {
  if (n === null || n === undefined) return "—"
  return n.toLocaleString(undefined, { maximumFractionDigits: 2 })
}

// SPDX-License-Identifier: Apache-2.0

"use client"

import * as React from "react"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table"
import { ExternalLink, RotateCw, SearchIcon } from "lucide-react"

import {
  api,
  ApiError,
  type Run,
  type RunList,
} from "@/lib/api"
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
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
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

import {
  formatCost,
  formatDuration,
  formatRelative,
} from "../../_lib/format"
import { RunStatusBadge } from "./run-status-badge"

const PAGE_SIZE = 25

type StatusFilter = "all" | Run["status"]

const STATUSES: { value: StatusFilter; label: string }[] = [
  { value: "all", label: "All statuses" },
  { value: "queued", label: "Queued" },
  { value: "running", label: "Running" },
  { value: "succeeded", label: "Succeeded" },
  { value: "failed", label: "Failed" },
  { value: "cancelled", label: "Cancelled" },
]

const SAMPLE_CURL = `curl -X POST http://localhost:8080/api/v1/agents/sample.echo \\
  -H "Content-Type: application/json" \\
  -d '{"input": {"message": "hello"}}'`

export function RunsView() {
  const [agent, setAgent] = React.useState("")
  const [tenant, setTenant] = React.useState("")
  const [status, setStatus] = React.useState<StatusFilter>("all")
  const [offset, setOffset] = React.useState(0)
  const [data, setData] = React.useState<RunList | null>(null)
  const [loading, setLoading] = React.useState(true)
  const [error, setError] = React.useState<string | null>(null)
  const [openRun, setOpenRun] = React.useState<Run | null>(null)

  // Debounce text inputs so we don't fire a request on every keystroke.
  const [debouncedAgent, setDebouncedAgent] = React.useState("")
  const [debouncedTenant, setDebouncedTenant] = React.useState("")
  React.useEffect(() => {
    const t = setTimeout(() => setDebouncedAgent(agent.trim()), 300)
    return () => clearTimeout(t)
  }, [agent])
  React.useEffect(() => {
    const t = setTimeout(() => setDebouncedTenant(tenant.trim()), 300)
    return () => clearTimeout(t)
  }, [tenant])

  // Reset to first page when filters change.
  React.useEffect(() => {
    setOffset(0)
  }, [debouncedAgent, debouncedTenant, status])

  const fetchRuns = React.useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const list = await api.runs({
        agent: debouncedAgent || undefined,
        tenant: debouncedTenant || undefined,
        status: status === "all" ? undefined : status,
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
            : "Failed to load runs"
      setError(message)
      setData({ runs: [], total: 0, has_more: false })
    } finally {
      setLoading(false)
    }
  }, [debouncedAgent, debouncedTenant, status, offset])

  React.useEffect(() => {
    void fetchRuns()
  }, [fetchRuns])

  const columns = React.useMemo<ColumnDef<Run>[]>(
    () => [
      {
        id: "status",
        header: "Status",
        cell: ({ row }) => <RunStatusBadge status={row.original.status} />,
      },
      {
        id: "agent",
        header: "Agent",
        cell: ({ row }) => (
          <span className="font-mono text-xs">{row.original.agent}</span>
        ),
      },
      {
        id: "tenant_id",
        header: "Tenant",
        cell: ({ row }) => {
          const id = row.original.tenant_id
          if (!id) return <span className="text-muted-foreground text-xs">—</span>
          const label =
            id === "00000000-0000-0000-0000-000000000000"
              ? "Default"
              : id.slice(0, 8)
          return (
            <span className="text-muted-foreground font-mono text-xs">
              {label}
            </span>
          )
        },
      },
      {
        id: "started_at",
        header: "Started",
        cell: ({ row }) => (
          <span className="text-muted-foreground text-xs">
            {formatRelative(row.original.started_at)}
          </span>
        ),
      },
      {
        id: "duration_ms",
        header: "Duration",
        cell: ({ row }) => (
          <span className="tabular-nums">
            {formatDuration(row.original.duration_ms)}
          </span>
        ),
      },
      {
        id: "cost_usd",
        header: "Cost",
        cell: ({ row }) => (
          <span className="tabular-nums">
            {formatCost(row.original.cost_usd)}
          </span>
        ),
      },
      {
        // #25: link-out to AgentField's deep view for this run.
        // Small external-link icon — keeps the table uncluttered while
        // giving operators a one-click jump to the DAG / step inspector.
        id: "agentfield_link",
        header: "",
        cell: ({ row }) => (
          <a
            href={api.traceUrl(row.original.id)}
            target="_blank"
            rel="noopener noreferrer"
            onClick={(e) => e.stopPropagation()}
            className="text-muted-foreground hover:text-foreground inline-flex"
            aria-label="View in AgentField"
            title="View in AgentField"
          >
            <ExternalLink className="size-3.5" />
          </a>
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

  const handleRefresh = () => void fetchRuns()

  return (
    <div className="flex flex-col gap-4">
      <FiltersBar
        agent={agent}
        tenant={tenant}
        status={status}
        onAgentChange={setAgent}
        onTenantChange={setTenant}
        onStatusChange={setStatus}
        onRefresh={handleRefresh}
        loading={loading}
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
            ) : table.getRowModel().rows.length === 0 ? (
              <TableRow className="hover:bg-transparent">
                <TableCell colSpan={columns.length} className="p-0">
                  <RunsEmptyState error={error} />
                </TableCell>
              </TableRow>
            ) : (
              table.getRowModel().rows.map((row) => (
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

      <RunDetailSheet
        run={openRun}
        onOpenChange={(open) => {
          if (!open) setOpenRun(null)
        }}
      />
    </div>
  )

  function RunsEmptyState({ error }: { error: string | null }) {
    if (error) {
      return (
        <Empty className="border-0">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <SearchIcon />
            </EmptyMedia>
            <EmptyTitle>Couldn&apos;t load runs</EmptyTitle>
            <EmptyDescription>
              The runtime returned: <code className="font-mono">{error}</code>.
              If the runs endpoint isn&apos;t live yet this is expected — it
              lands as part of Phase 5.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      )
    }
    return (
      <Empty className="border-0">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <SearchIcon />
          </EmptyMedia>
          <EmptyTitle>No runs yet</EmptyTitle>
          <EmptyDescription>
            Invoke an agent to see runs here.
          </EmptyDescription>
        </EmptyHeader>
        <pre className="bg-muted text-muted-foreground mt-4 max-w-2xl overflow-x-auto rounded-md p-3 text-left font-mono text-xs">
          {SAMPLE_CURL}
        </pre>
      </Empty>
    )
  }
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

type FiltersBarProps = {
  agent: string
  tenant: string
  status: StatusFilter
  onAgentChange: (v: string) => void
  onTenantChange: (v: string) => void
  onStatusChange: (v: StatusFilter) => void
  onRefresh: () => void
  loading: boolean
}

function FiltersBar({
  agent,
  tenant,
  status,
  onAgentChange,
  onTenantChange,
  onStatusChange,
  onRefresh,
  loading,
}: FiltersBarProps) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Input
        placeholder="Filter by agent…"
        value={agent}
        onChange={(e) => onAgentChange(e.target.value)}
        className="w-56"
      />
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
      <div className="ml-auto">
        <Button
          variant="outline"
          size="sm"
          onClick={onRefresh}
          disabled={loading}
        >
          <RotateCw className={cn(loading && "animate-spin")} />
          Refresh
        </Button>
      </div>
    </div>
  )
}

function RunDetailSheet({
  run,
  onOpenChange,
}: {
  run: Run | null
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Sheet open={run !== null} onOpenChange={onOpenChange}>
      <SheetContent className="flex w-full flex-col gap-0 p-0 sm:max-w-2xl">
        {run ? <RunDetailBody run={run} /> : null}
      </SheetContent>
    </Sheet>
  )
}

function RunDetailBody({ run }: { run: Run }) {
  const traceUrl = api.traceUrl(run.id)
  return (
    <>
      <SheetHeader className="border-b">
        <div className="flex items-start justify-between gap-3">
          <div className="flex flex-col gap-1">
            <SheetTitle className="flex items-center gap-2">
              <RunStatusBadge status={run.status} />
              <span className="font-mono text-xs text-muted-foreground">
                {run.id}
              </span>
            </SheetTitle>
            <SheetDescription className="font-mono text-xs">
              {run.agent}
            </SheetDescription>
          </div>
          <Button
            variant="outline"
            size="sm"
            render={
              // eslint-disable-next-line react/jsx-no-target-blank -- noopener provided
              <a
                href={traceUrl}
                target="_blank"
                rel="noopener noreferrer"
              >
                View full trace
                <ExternalLink data-icon="inline-end" />
              </a>
            }
          />
        </div>
      </SheetHeader>

      <ScrollArea className="flex-1">
        <div className="flex flex-col gap-6 p-4">
          <section className="grid grid-cols-2 gap-3 text-xs sm:grid-cols-3">
            <Stat label="Started" value={formatRelative(run.started_at)} />
            <Stat label="Duration" value={formatDuration(run.duration_ms)} />
            <Stat label="Cost" value={formatCost(run.cost_usd)} />
            <Stat label="Tenant" value={run.tenant_id || "—"} />
          </section>

          {run.error ? (
            <section className="flex flex-col gap-1.5">
              <SectionLabel>Error</SectionLabel>
              <pre className="bg-destructive/10 text-destructive overflow-x-auto rounded-md p-3 font-mono text-xs">
                {run.error}
              </pre>
            </section>
          ) : null}

          <section className="flex flex-col gap-1.5">
            <SectionLabel>Input</SectionLabel>
            <JsonBlock value={run.input} />
          </section>

          <section className="flex flex-col gap-1.5">
            <SectionLabel>Output</SectionLabel>
            <JsonBlock value={run.output} />
          </section>

          <section className="flex flex-col gap-1.5">
            <SectionLabel>Logs</SectionLabel>
            <div className="bg-muted/40 text-muted-foreground rounded-md p-3 text-xs">
              Logs will appear here once execution logging lands in Phase 5.
            </div>
          </section>
        </div>
      </ScrollArea>
    </>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-muted-foreground text-[10px] font-medium uppercase tracking-wide">
        {label}
      </span>
      <span className="font-mono text-xs">{value}</span>
    </div>
  )
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <span className="text-muted-foreground text-[10px] font-medium uppercase tracking-wide">
      {children}
    </span>
  )
}

function JsonBlock({ value }: { value: unknown }) {
  if (value === undefined || value === null) {
    return (
      <div className="bg-muted/40 text-muted-foreground rounded-md p-3 text-xs">
        —
      </div>
    )
  }
  let formatted: string
  try {
    formatted = JSON.stringify(value, null, 2)
  } catch {
    formatted = String(value)
  }
  return (
    <pre className="bg-muted/40 max-h-72 overflow-auto rounded-md p-3 font-mono text-xs">
      {formatted}
    </pre>
  )
}
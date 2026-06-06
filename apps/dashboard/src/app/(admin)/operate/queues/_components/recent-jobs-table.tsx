"use client"

// Recent background jobs table.
//
// Client component because it owns:
//   - TanStack Table row state
//   - The "show full error" popover/details affordance
//   - The "Retry" no-op button (toast stub until Phase 5)
//
// The data is a static slice from the parent server component (the latest
// queue summary). The table doesn't refetch on its own.

import * as React from "react"
import { useRouter } from "next/navigation"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table"
import { ChevronRight, RotateCw } from "lucide-react"
import { toast } from "sonner"

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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { ListChecks } from "lucide-react"
import { api, ApiError, type QueueSummary } from "@/lib/api"

import { formatRelative } from "../../_lib/format"
import { QueueStatusBadge } from "./queue-status-badge"

type Job = QueueSummary["recent"][number]

type RecentJobsTableProps = {
  jobs: Job[]
}

export function RecentJobsTable({ jobs }: RecentJobsTableProps) {
  const columns = React.useMemo<ColumnDef<Job>[]>(
    () => [
      {
        id: "name",
        header: "Name",
        cell: ({ row }) => (
          <span className="font-mono text-xs">{row.original.name}</span>
        ),
      },
      {
        id: "status",
        header: "Status",
        cell: ({ row }) => <QueueStatusBadge status={row.original.status} />,
      },
      {
        id: "enqueued_at",
        header: "Enqueued",
        cell: ({ row }) => (
          <span className="text-muted-foreground text-xs">
            {formatRelative(row.original.enqueued_at)}
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
        id: "last_error",
        header: "Last error",
        cell: ({ row }) => <ErrorCell error={row.original.last_error} />,
      },
      {
        id: "actions",
        header: "",
        cell: ({ row }) => <RetryCell job={row.original} />,
      },
    ],
    [],
  )

  const table = useReactTable({
    data: jobs,
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  if (jobs.length === 0) {
    return (
      <div className="bg-card rounded-xl border">
        <Empty className="border-0">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ListChecks />
            </EmptyMedia>
            <EmptyTitle>No background jobs yet</EmptyTitle>
            <EmptyDescription>
              Define one in{" "}
              <code className="font-mono">apps/backend/jobs/</code> and it will
              show up here when it runs.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </div>
    )
  }

  return (
    <div className="bg-card rounded-xl border">
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
          {table.getRowModel().rows.map((row) => (
            <TableRow key={row.id} className="hover:bg-muted/30">
              {row.getVisibleCells().map((cell) => (
                <TableCell key={cell.id}>
                  {flexRender(cell.column.columnDef.cell, cell.getContext())}
                </TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function ErrorCell({ error }: { error: string | null }) {
  if (!error) {
    return <span className="text-muted-foreground text-xs">—</span>
  }
  return (
    <Collapsible>
      <CollapsibleTrigger className="group text-destructive inline-flex max-w-md items-start gap-1 text-left text-xs">
        <ChevronRight
          className="mt-0.5 size-3 shrink-0 transition-transform group-data-[panel-open]:rotate-90"
          aria-hidden
        />
        <span className="line-clamp-1 truncate font-mono">{error}</span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <pre className="bg-destructive/10 text-destructive mt-2 max-w-md overflow-x-auto rounded-md p-2 font-mono text-[11px]">
          {error}
        </pre>
      </CollapsibleContent>
    </Collapsible>
  )
}

function RetryCell({ job }: { job: Job }) {
  const router = useRouter()
  const [pending, setPending] = React.useState(false)

  if (job.status !== "failed") return null

  const handleRetry = async () => {
    setPending(true)
    try {
      await api.jobs.retry(job.id)
      toast.success("Job re-queued", {
        description: `${job.name} will be retried by the runtime.`,
      })
      router.refresh()
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Retry failed"
      toast.error("Could not retry job", { description: message })
    } finally {
      setPending(false)
    }
  }

  return (
    <Button
      variant="outline"
      size="sm"
      onClick={handleRetry}
      disabled={pending}
    >
      <RotateCw className={pending ? "animate-spin" : undefined} />
      {pending ? "Retrying…" : "Retry"}
    </Button>
  )
}

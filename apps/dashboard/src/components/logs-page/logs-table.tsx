// SPDX-License-Identifier: Apache-2.0

"use client"

import { Skeleton } from "@/components/ui/skeleton"

import type { LogLine } from "@/lib/api"

import { LOG_ROW_COLUMNS, LogRow } from "./log-row"

// Log stream table — sticky header + expandable rows, same column shell
// across loading / empty / degraded states so structure stays visible
// even at zero data. Newest lines render at the top (sorted upstream).

interface LogsTableProps {
  logs: LogLine[]
  loading: boolean
  healthy: boolean
}

export function LogsTable({ logs, loading, healthy }: LogsTableProps) {
  return (
    <section
      aria-label="Logs table"
      className="flex min-h-0 flex-col rounded-md border bg-card"
    >
      <TableHeader count={logs.length} />
      {!healthy ? (
        <DegradedRow />
      ) : loading && logs.length === 0 ? (
        <SkeletonRows />
      ) : logs.length === 0 ? (
        <EmptyRow />
      ) : (
        <ul role="list" className="flex flex-col">
          {logs.map((line, i) => (
            <li key={`${line.ts}-${line.service}-${i}`}>
              <LogRow line={line} />
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function TableHeader({ count }: { count: number }) {
  return (
    <header
      className={`sticky top-0 z-10 grid items-center gap-stack border-b bg-card px-row-x py-tile-tight text-eyebrow uppercase tracking-wide text-muted-foreground ${LOG_ROW_COLUMNS}`}
    >
      <span>Time</span>
      <span>Level</span>
      <span>Service</span>
      <span>Message</span>
      <span className="text-right tabular-nums">{count}</span>
    </header>
  )
}

function SkeletonRows() {
  return (
    <ul role="list" className="flex flex-col">
      {Array.from({ length: 10 }).map((_, i) => (
        <li
          key={i}
          className={`grid items-center gap-stack border-l-4 border-b border-l-transparent px-row-x py-row-y ${LOG_ROW_COLUMNS}`}
        >
          <Skeleton className="h-3 w-14" />
          <Skeleton className="h-4 w-10" />
          <Skeleton className="h-3 w-16" />
          <Skeleton className="h-3 w-full" />
          <span aria-hidden />
        </li>
      ))}
    </ul>
  )
}

function EmptyRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">
        No log lines match the current filters.
      </p>
      <p className="max-w-md text-meta text-muted-foreground">
        The runtime emits lines here as traffic flows. Widen the level
        filter or clear the service / search inputs — the page polls and
        picks up new lines automatically.
      </p>
    </div>
  )
}

function DegradedRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">Log stream unavailable.</p>
      <p className="max-w-md text-meta text-muted-foreground">
        The runtime did not return a log list. Check the Health page for
        a runtime probe, then retry — polling recovers automatically once
        the runtime responds.
      </p>
    </div>
  )
}

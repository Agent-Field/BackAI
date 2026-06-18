// SPDX-License-Identifier: Apache-2.0

"use client"

import { Skeleton } from "@/components/ui/skeleton"

import type { Run } from "@/lib/api"

import { RUN_ROW_COLUMNS, RunRow } from "./run-row"

// Runs table — sticky header + status-tiered rows. Header stays
// visible per the framework's "structure visible even at zero" rule.
// Loading / empty states render the same column shell.

interface RunsTableProps {
  runs: Run[]
  totalShown: number
  totalAll: number
  loading: boolean
  healthy: boolean
  selectedRunId: string | null
  onSelect: (run: Run) => void
}

export function RunsTable({
  runs,
  totalShown,
  totalAll,
  loading,
  healthy,
  selectedRunId,
  onSelect,
}: RunsTableProps) {
  return (
    <section
      aria-label="Runs table"
      className="flex min-h-0 flex-col rounded-md border bg-card"
    >
      <TableHeader totalShown={totalShown} totalAll={totalAll} />
      {!healthy ? (
        <DegradedRow />
      ) : loading && runs.length === 0 ? (
        <SkeletonRows />
      ) : runs.length === 0 ? (
        <EmptyRow />
      ) : (
        <ul role="list" className="flex flex-col">
          {runs.map((run) => (
            <li key={run.id}>
              <RunRow
                run={run}
                selected={run.id === selectedRunId}
                onSelect={onSelect}
              />
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function TableHeader({
  totalShown,
  totalAll,
}: {
  totalShown: number
  totalAll: number
}) {
  return (
    <header
      className={`sticky top-0 z-10 grid items-center gap-stack border-b bg-card px-row-x py-tile-tight text-eyebrow uppercase tracking-wide text-muted-foreground ${RUN_ROW_COLUMNS}`}
    >
      <span aria-hidden />
      <span>Time</span>
      <span>Agent / ID / Error</span>
      <span>Tenant</span>
      <span className="text-right">Duration</span>
      <span className="text-right">Cost</span>
      <span className="text-right">Status</span>
      <span className="text-right tabular-nums">
        {totalShown === totalAll ? totalAll : `${totalShown}/${totalAll}`}
      </span>
    </header>
  )
}

function SkeletonRows() {
  return (
    <ul role="list" className="flex flex-col">
      {Array.from({ length: 8 }).map((_, i) => (
        <li
          key={i}
          className={`grid items-center gap-stack border-l-4 border-b border-l-transparent px-row-x py-row-y ${RUN_ROW_COLUMNS}`}
        >
          <Skeleton className="size-icon-dot rounded-pill" />
          <Skeleton className="h-3 w-12" />
          <Skeleton className="h-3 w-full" />
          <Skeleton className="h-3 w-16" />
          <Skeleton className="h-3 w-10 justify-self-end" />
          <Skeleton className="h-3 w-12 justify-self-end" />
          <Skeleton className="h-3 w-10 justify-self-end" />
          <span aria-hidden />
        </li>
      ))}
    </ul>
  )
}

function EmptyRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">No runs match the current filters.</p>
      <p className="max-w-md text-meta text-muted-foreground">
        Make an agent call to populate this page. The fastest path: copy
        a dev tenant key from Home and send a chat completion through{" "}
        <code className="font-mono">/api/v1/llm/chat/completions</code>.
      </p>
    </div>
  )
}

function DegradedRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">Runs ledger unavailable.</p>
      <p className="max-w-md text-meta text-muted-foreground">
        The runtime did not return a runs list. Check the Health page for
        a database probe, then retry.
      </p>
    </div>
  )
}

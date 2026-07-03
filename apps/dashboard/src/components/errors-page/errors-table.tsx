// SPDX-License-Identifier: Apache-2.0

"use client"

import { Skeleton } from "@/components/ui/skeleton"

import type { ErrorCapabilities, ErrorGroup } from "@/lib/api"

import { ERROR_ROW_COLUMNS, ErrorRow } from "./error-row"

// Error-groups table — sticky header + status-tiered rows, same column
// shell across loading / empty / degraded states so the structure stays
// visible even at zero data.

interface ErrorsTableProps {
  groups: ErrorGroup[]
  total: number
  loading: boolean
  healthy: boolean
  capabilities: ErrorCapabilities | null
  onMutated: () => Promise<void> | void
}

export function ErrorsTable({
  groups,
  total,
  loading,
  healthy,
  capabilities,
  onMutated,
}: ErrorsTableProps) {
  // No outer border here — the shell nests this table inside a ZoneCard,
  // which already owns the card chrome (mirrors the Tenants list).
  return (
    <section
      aria-label="Error groups table"
      className="flex min-h-0 flex-col"
    >
      <TableHeader shown={groups.length} total={total} />
      {!healthy ? (
        <DegradedRow />
      ) : loading && groups.length === 0 ? (
        <SkeletonRows />
      ) : groups.length === 0 ? (
        <EmptyRow />
      ) : (
        <ul role="list" className="flex flex-col">
          {groups.map((group) => (
            <li key={group.id}>
              <ErrorRow
                group={group}
                capabilities={capabilities}
                onMutated={onMutated}
              />
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function TableHeader({ shown, total }: { shown: number; total: number }) {
  return (
    <header
      className={`sticky top-0 z-10 grid items-center gap-stack border-b bg-card px-row-x py-tile-tight text-eyebrow uppercase tracking-wide text-muted-foreground ${ERROR_ROW_COLUMNS}`}
    >
      <span aria-hidden />
      <span>Error / Culprit</span>
      <span>Service</span>
      <span className="text-right">Events</span>
      <span className="text-right">Users</span>
      <span className="text-right">Last seen</span>
      <span className="text-right tabular-nums">
        {shown === total ? total : `${shown}/${total}`}
      </span>
    </header>
  )
}

function SkeletonRows() {
  return (
    <ul role="list" className="flex flex-col">
      {Array.from({ length: 6 }).map((_, i) => (
        <li
          key={i}
          className={`grid items-center gap-stack border-l-4 border-b border-l-transparent px-row-x py-row-y ${ERROR_ROW_COLUMNS}`}
        >
          <Skeleton className="size-icon-dot rounded-pill" />
          <Skeleton className="h-3 w-full" />
          <Skeleton className="h-3 w-16" />
          <Skeleton className="h-3 w-8 justify-self-end" />
          <Skeleton className="h-3 w-8 justify-self-end" />
          <Skeleton className="h-3 w-12 justify-self-end" />
          <Skeleton className="h-5 w-24 justify-self-end" />
        </li>
      ))}
    </ul>
  )
}

function EmptyRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">
        No error groups in the current window.
      </p>
      <p className="max-w-md text-meta text-muted-foreground">
        Groups appear here when a service logs at error level or an
        agent run fails. Switch the status filter to check muted or
        resolved groups.
      </p>
    </div>
  )
}

function DegradedRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">Error groups unavailable.</p>
      <p className="max-w-md text-meta text-muted-foreground">
        The runtime did not return an error-group list. Check the Health
        page for a runtime probe, then retry — polling recovers
        automatically once the runtime responds.
      </p>
    </div>
  )
}

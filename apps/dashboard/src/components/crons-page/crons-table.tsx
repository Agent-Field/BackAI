// SPDX-License-Identifier: Apache-2.0

"use client"

import { useState } from "react"

import { Skeleton } from "@/components/ui/skeleton"

import type { Cron } from "@/lib/api"

import { CRON_ROW_COLUMNS, CronRow } from "./cron-row"

// Crons table — sticky header + toggle-tiered rows with inline expansion.
// Header stays visible per the framework's "structure visible even at
// zero" rule. Loading / empty / degraded states render the same shell.

interface CronsTableProps {
  crons: Cron[]
  total: number
  loading: boolean
  healthy: boolean
  onMutated: () => Promise<void> | void
}

export function CronsTable({ crons, total, loading, healthy, onMutated }: CronsTableProps) {
  const [expandedId, setExpandedId] = useState<string | null>(null)
  return (
    <section aria-label="Crons table" className="flex min-h-0 flex-col rounded-md border bg-card">
      <TableHeader shown={crons.length} total={total} />
      {!healthy ? (
        <DegradedRow />
      ) : loading && crons.length === 0 ? (
        <SkeletonRows />
      ) : crons.length === 0 ? (
        <EmptyRow />
      ) : (
        <ul role="list" className="flex flex-col">
          {crons.map((cron) => (
            <li key={cron.id}>
              <CronRow
                cron={cron}
                expanded={cron.id === expandedId}
                onToggle={(c) => setExpandedId((prev) => (prev === c.id ? null : c.id))}
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
      className={`sticky top-0 z-10 grid items-center gap-stack border-b bg-card px-row-x py-tile-tight text-eyebrow uppercase tracking-wide text-muted-foreground ${CRON_ROW_COLUMNS}`}
    >
      <span aria-hidden />
      <span>Cron / job</span>
      <span>Schedule</span>
      <span className="text-right">Last run</span>
      <span className="text-right">Next run</span>
      <span className="text-right">State</span>
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
          className={`grid items-center gap-stack border-l-4 border-b border-l-transparent px-row-x py-row-y ${CRON_ROW_COLUMNS}`}
        >
          <Skeleton className="size-icon-dot rounded-pill" />
          <Skeleton className="h-3 w-full" />
          <Skeleton className="h-3 w-20" />
          <Skeleton className="h-3 w-12 justify-self-end" />
          <Skeleton className="h-3 w-12 justify-self-end" />
          <Skeleton className="h-3 w-12 justify-self-end" />
          <span aria-hidden />
        </li>
      ))}
    </ul>
  )
}

function EmptyRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">No crons scheduled.</p>
      <p className="max-w-md text-meta text-muted-foreground">
        Register one via <code className="font-mono">POST /api/v1/crons</code> or the SDK — a cron
        points at a job kind and a crontab expression.
      </p>
    </div>
  )
}

function DegradedRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">Crons unavailable.</p>
      <p className="max-w-md text-meta text-muted-foreground">
        The runtime did not return a crons list. Check the Health page for a database probe, then
        retry.
      </p>
    </div>
  )
}

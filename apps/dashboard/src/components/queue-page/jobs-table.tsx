// SPDX-License-Identifier: Apache-2.0

"use client"

import { useState } from "react"

import { Skeleton } from "@/components/ui/skeleton"

import type { Job } from "@/lib/api"

import { JOB_ROW_COLUMNS, JobRow } from "./job-row"

// Jobs table — sticky header + state-tiered rows with inline expansion.
// Header stays visible per the framework's "structure visible even at
// zero" rule. Loading / empty / degraded states render the same shell.

interface JobsTableProps {
  jobs: Job[]
  total: number
  loading: boolean
  healthy: boolean
  onMutated: () => Promise<void> | void
}

export function JobsTable({
  jobs,
  total,
  loading,
  healthy,
  onMutated,
}: JobsTableProps) {
  const [expandedId, setExpandedId] = useState<string | null>(null)
  return (
    <section
      aria-label="Jobs table"
      className="flex min-h-0 flex-col rounded-md border bg-card"
    >
      <TableHeader shown={jobs.length} total={total} />
      {!healthy ? (
        <DegradedRow />
      ) : loading && jobs.length === 0 ? (
        <SkeletonRows />
      ) : jobs.length === 0 ? (
        <EmptyRow />
      ) : (
        <ul role="list" className="flex flex-col">
          {jobs.map((job) => (
            <li key={job.id}>
              <JobRow
                job={job}
                expanded={job.id === expandedId}
                onToggle={(j) =>
                  setExpandedId((prev) => (prev === j.id ? null : j.id))
                }
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
      className={`sticky top-0 z-10 grid items-center gap-stack border-b bg-card px-row-x py-tile-tight text-eyebrow uppercase tracking-wide text-muted-foreground ${JOB_ROW_COLUMNS}`}
    >
      <span aria-hidden />
      <span>Enqueued</span>
      <span>Job / ID / Error</span>
      <span>Tenant</span>
      <span className="text-right">Attempt</span>
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
      {Array.from({ length: 8 }).map((_, i) => (
        <li
          key={i}
          className={`grid items-center gap-stack border-l-4 border-b border-l-transparent px-row-x py-row-y ${JOB_ROW_COLUMNS}`}
        >
          <Skeleton className="size-icon-dot rounded-pill" />
          <Skeleton className="h-3 w-12" />
          <Skeleton className="h-3 w-full" />
          <Skeleton className="h-3 w-16" />
          <Skeleton className="h-3 w-10 justify-self-end" />
          <Skeleton className="h-3 w-14 justify-self-end" />
          <span aria-hidden />
        </li>
      ))}
    </ul>
  )
}

function EmptyRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">No jobs yet.</p>
      <p className="max-w-md text-meta text-muted-foreground">
        Enqueue one via{" "}
        <code className="font-mono">POST /api/v1/jobs</code> or the SDK.
      </p>
    </div>
  )
}

function DegradedRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">Job queue unavailable.</p>
      <p className="max-w-md text-meta text-muted-foreground">
        The runtime did not return a jobs list. Check the Health page for
        a database probe, then retry.
      </p>
    </div>
  )
}

// SPDX-License-Identifier: Apache-2.0

"use client"

import { RefreshCw } from "lucide-react"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useCallback, useEffect, useMemo, useState } from "react"

import { Button } from "@/components/ui/button"
import { FilterChip, FilterChipGroup } from "@/components/ui/filter-chip"

import { api } from "@/lib/api"
import { isJobStateFilter } from "@/lib/queue-page/derive"
import type { JobStateFilter, QueueSnapshot } from "@/lib/queue-page/types"
import { DEFAULT_JOB_STATE_FILTER } from "@/lib/queue-page/types"
import { polling } from "@/lib/theme"

import { DefinitionsList } from "./definitions-list"
import { JobsTable } from "./jobs-table"
import { QueueKpis } from "./queue-kpis"

// QueueShell owns:
//   - URL-persistent ?state filter (server-side filter on the jobs list)
//   - 5s polling tick — the queue is a live surface like Runs
//   - Refresh after a Retry mutation so the row's state updates

const STATE_OPTIONS: { value: JobStateFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "pending", label: "Pending" },
  { value: "scheduled", label: "Scheduled" },
  { value: "available", label: "Available" },
  { value: "running", label: "Running" },
  { value: "retryable", label: "Retryable" },
  { value: "completed", label: "Completed" },
  { value: "discarded", label: "Discarded" },
  { value: "cancelled", label: "Cancelled" },
]

interface QueueShellProps {
  initialSnapshot: QueueSnapshot
}

export function QueueShell({ initialSnapshot }: QueueShellProps) {
  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()

  const [snapshot, setSnapshot] = useState<QueueSnapshot>(initialSnapshot)
  const [refreshing, setRefreshing] = useState(false)

  const state = useMemo<JobStateFilter>(
    () => parseState(searchParams.get("state")),
    [searchParams],
  )

  const setState = useCallback(
    (next: JobStateFilter) => {
      const params = new URLSearchParams(searchParams.toString())
      if (next === DEFAULT_JOB_STATE_FILTER) params.delete("state")
      else params.set("state", next)
      const qs = params.toString()
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
    },
    [pathname, router, searchParams],
  )

  const refresh = useCallback(
    async (stateFilter: JobStateFilter, manual = false) => {
      if (manual) setRefreshing(true)
      const fetchedAt = new Date().toISOString()
      const [summary, jobs, definitions] = await Promise.all([
        api.queue().catch(() => null),
        api
          .jobs.list({
            limit: 100,
            state: stateFilter === "all" ? undefined : stateFilter,
          })
          .catch(() => null),
        api.jobs.definitions().catch(() => null),
      ])
      setSnapshot({
        summary,
        jobs: jobs?.jobs ?? [],
        total: jobs?.total ?? 0,
        hasMore: jobs?.has_more ?? false,
        definitions: definitions?.definitions ?? [],
        fetchedAt,
        healthy: summary !== null || jobs !== null || definitions !== null,
      })
      if (manual) setRefreshing(false)
    },
    [],
  )

  useEffect(() => {
    void refresh(state)
  }, [state, refresh])

  useEffect(() => {
    let cancelled = false
    const id = setInterval(() => {
      if (!cancelled) void refresh(state)
    }, polling.home)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [state, refresh])

  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <Header
        fetchedAt={snapshot.fetchedAt}
        refreshing={refreshing}
        onRefresh={() => refresh(state, true)}
      />
      <QueueKpis summary={snapshot.summary} />
      <div className="flex flex-col gap-stack">
        <div className="flex flex-wrap items-center gap-stack rounded-md border bg-card/95 px-row-x py-row-y">
          <FilterChipGroup label="State">
            {STATE_OPTIONS.map((opt) => (
              <FilterChip
                key={opt.value}
                label={opt.label}
                active={state === opt.value}
                onSelect={() => setState(opt.value)}
              />
            ))}
          </FilterChipGroup>
        </div>
        <JobsTable
          jobs={snapshot.jobs}
          total={snapshot.total}
          loading={refreshing}
          healthy={snapshot.healthy}
          onMutated={() => refresh(state, false)}
        />
      </div>
      <DefinitionsList
        definitions={snapshot.definitions}
        healthy={snapshot.healthy}
      />
    </div>
  )
}

function Header({
  fetchedAt,
  refreshing,
  onRefresh,
}: {
  fetchedAt: string
  refreshing: boolean
  onRefresh: () => void
}) {
  const ageSec = Math.max(0, Math.round((Date.now() - Date.parse(fetchedAt)) / 1000))
  const ageLabel = ageSec < 5 ? "now" : `${ageSec}s ago`
  return (
    <header className="flex items-baseline justify-between">
      <div className="flex flex-col gap-tile-tight">
        <h1 className="text-xl font-semibold tracking-tight text-foreground">
          Queue
        </h1>
        <p className="text-meta text-muted-foreground">
          Background jobs — what&apos;s waiting, running, and failing, plus
          the registered job kinds.
        </p>
      </div>
      <div className="flex items-center gap-stack text-meta text-muted-foreground">
        <span className="tabular-nums">updated {ageLabel}</span>
        <Button
          type="button"
          size="sm"
          variant="outline"
          onClick={onRefresh}
          disabled={refreshing}
          className="h-7 gap-inline text-meta"
        >
          <RefreshCw
            className={`size-3.5 ${refreshing ? "animate-spin" : ""}`}
            aria-hidden
          />
          Refresh
        </Button>
      </div>
    </header>
  )
}

function parseState(raw: string | null): JobStateFilter {
  if (raw && isJobStateFilter(raw)) return raw
  return DEFAULT_JOB_STATE_FILTER
}

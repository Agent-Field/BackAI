// SPDX-License-Identifier: Apache-2.0

"use client"

import { RefreshCw } from "lucide-react"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useCallback, useEffect, useMemo, useState } from "react"

import { Button } from "@/components/ui/button"

import { api } from "@/lib/api"
import type { Run } from "@/lib/api"
import {
  applyClientFilters,
  isRunStatusFilter,
  isRunTimeRange,
  statusCounts,
} from "@/lib/runs/derive"
import type {
  RunStatusFilter,
  RunTimeRange,
  RunsFilters,
  RunsSnapshot,
} from "@/lib/runs/types"
import { DEFAULT_FILTERS } from "@/lib/runs/types"
import { polling } from "@/lib/theme"

import { FilterBar } from "./filter-bar"
import { RunDrawer } from "./run-drawer"
import { RunsTable } from "./runs-table"

// RunsShell owns:
//   - URL-persistent filter state (?status / ?time / ?search / ?drawer)
//   - 5s polling tick — Runs is the closest thing the dashboard has to a
//     live feed without WebSocket
//   - Client-side filtering for time / search (Gaps 27-28 deferred)
//   - Optimistic refresh after a drawer mutation

interface RunsShellProps {
  initialSnapshot: RunsSnapshot
}

export function RunsShell({ initialSnapshot }: RunsShellProps) {
  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()

  const [snapshot, setSnapshot] = useState<RunsSnapshot>(initialSnapshot)
  const [refreshing, setRefreshing] = useState(false)

  const filters = useMemo<RunsFilters>(
    () => ({
      status: parseStatus(searchParams.get("status")),
      time: parseTime(searchParams.get("time")),
      search: searchParams.get("search") ?? "",
    }),
    [searchParams],
  )

  const drawerId = searchParams.get("drawer")

  const setFilters = useCallback(
    (next: Partial<RunsFilters>) => {
      const params = new URLSearchParams(searchParams.toString())
      const final = { ...filters, ...next }
      if (final.status === DEFAULT_FILTERS.status) params.delete("status")
      else params.set("status", final.status)
      if (final.time === DEFAULT_FILTERS.time) params.delete("time")
      else params.set("time", final.time)
      if (!final.search.trim()) params.delete("search")
      else params.set("search", final.search)
      const qs = params.toString()
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
    },
    [filters, pathname, router, searchParams],
  )

  const openDrawer = useCallback(
    (id: string | null) => {
      const params = new URLSearchParams(searchParams.toString())
      if (id === null) params.delete("drawer")
      else params.set("drawer", id)
      const qs = params.toString()
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
    },
    [pathname, router, searchParams],
  )

  const refresh = useCallback(
    async (status: RunStatusFilter, manual = false) => {
      if (manual) setRefreshing(true)
      const fetchedAt = new Date().toISOString()
      try {
        const next = await api.runs({
          limit: 200,
          status: status === "all" ? undefined : status,
        })
        setSnapshot({
          runs: next.runs,
          total: next.total,
          hasMore: next.has_more,
          fetchedAt,
          healthy: true,
        })
      } catch {
        setSnapshot((prev) => ({ ...prev, fetchedAt, healthy: false }))
      } finally {
        if (manual) setRefreshing(false)
      }
    },
    [],
  )

  useEffect(() => {
    void refresh(filters.status)
  }, [filters.status, refresh])

  useEffect(() => {
    let cancelled = false
    const id = setInterval(() => {
      if (!cancelled) void refresh(filters.status)
    }, polling.home)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [filters.status, refresh])

  const filtered = useMemo(
    () => applyClientFilters(snapshot.runs, filters),
    [snapshot.runs, filters],
  )
  const counts = useMemo(() => statusCounts(snapshot.runs), [snapshot.runs])

  const selectedRun = useMemo<Run | null>(
    () => snapshot.runs.find((r) => r.id === drawerId) ?? null,
    [snapshot.runs, drawerId],
  )

  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <Header
        fetchedAt={snapshot.fetchedAt}
        refreshing={refreshing}
        onRefresh={() => refresh(filters.status, true)}
      />
      <FilterBar filters={filters} counts={counts} onChange={setFilters} />
      <RunsTable
        runs={filtered}
        totalShown={filtered.length}
        totalAll={snapshot.runs.length}
        loading={refreshing}
        healthy={snapshot.healthy}
        selectedRunId={drawerId}
        onSelect={(run) => openDrawer(run.id)}
      />
      <RunDrawer
        run={selectedRun}
        onClose={() => openDrawer(null)}
        onMutated={() => refresh(filters.status, false)}
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
          Runs
        </h1>
        <p className="text-meta text-muted-foreground">
          Every agent and handler invocation, newest first.
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

function parseStatus(raw: string | null): RunStatusFilter {
  if (raw && isRunStatusFilter(raw)) return raw
  return DEFAULT_FILTERS.status
}

function parseTime(raw: string | null): RunTimeRange {
  if (raw && isRunTimeRange(raw)) return raw
  return DEFAULT_FILTERS.time
}

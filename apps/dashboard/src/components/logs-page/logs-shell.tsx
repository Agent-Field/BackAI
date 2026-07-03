// SPDX-License-Identifier: Apache-2.0

"use client"

import { RefreshCw } from "lucide-react"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useCallback, useEffect, useMemo, useState } from "react"

import { Button } from "@/components/ui/button"

import { api } from "@/lib/api"
import { sortNewestFirst } from "@/lib/logs-page/derive"
import type { LogsFilters, LogsSnapshot } from "@/lib/logs-page/types"
import {
  DEFAULT_LOGS_FILTERS,
  isLogLevelFilter,
} from "@/lib/logs-page/types"
import { polling } from "@/lib/theme"

import { LogsFilterBar } from "./logs-filter-bar"
import { LogsTable } from "./logs-table"

// LogsShell owns:
//   - URL-persistent filters (?level / ?service / ?search)
//   - polling tick — the log ring is the closest thing to a live feed
//     without the SSE tail endpoint
//   - a short debounce on filter changes so free-text typing doesn't
//     fire a fetch per keystroke (filters apply server-side)

const FILTER_DEBOUNCE_MS = 300

interface LogsShellProps {
  initialSnapshot: LogsSnapshot
}

export function LogsShell({ initialSnapshot }: LogsShellProps) {
  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()

  const [snapshot, setSnapshot] = useState<LogsSnapshot>(initialSnapshot)
  const [refreshing, setRefreshing] = useState(false)

  const filters = useMemo<LogsFilters>(
    () => ({
      level: parseLevel(searchParams.get("level")),
      service: searchParams.get("service") ?? "",
      search: searchParams.get("search") ?? "",
    }),
    [searchParams],
  )

  const setFilters = useCallback(
    (next: Partial<LogsFilters>) => {
      const params = new URLSearchParams(searchParams.toString())
      const final = { ...filters, ...next }
      if (final.level === DEFAULT_LOGS_FILTERS.level) params.delete("level")
      else params.set("level", final.level)
      if (!final.service.trim()) params.delete("service")
      else params.set("service", final.service)
      if (!final.search.trim()) params.delete("search")
      else params.set("search", final.search)
      const qs = params.toString()
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
    },
    [filters, pathname, router, searchParams],
  )

  const refresh = useCallback(async (next: LogsFilters, manual = false) => {
    if (manual) setRefreshing(true)
    const fetchedAt = new Date().toISOString()
    try {
      const result = await api.logs({
        level: next.level === "all" ? undefined : next.level,
        service: next.service.trim() || undefined,
        search: next.search.trim() || undefined,
        limit: 200,
      })
      setSnapshot({
        logs: sortNewestFirst(result.logs),
        fetchedAt,
        healthy: true,
      })
    } catch {
      setSnapshot((prev) => ({ ...prev, fetchedAt, healthy: false }))
    } finally {
      if (manual) setRefreshing(false)
    }
  }, [])

  // Debounced fetch on filter change — level clicks land after the same
  // short delay; typing in service / search coalesces to one request.
  useEffect(() => {
    const id = setTimeout(() => {
      void refresh(filters)
    }, FILTER_DEBOUNCE_MS)
    return () => clearTimeout(id)
  }, [filters, refresh])

  useEffect(() => {
    let cancelled = false
    const id = setInterval(() => {
      if (!cancelled) void refresh(filters)
    }, polling.home)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [filters, refresh])

  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <Header
        fetchedAt={snapshot.fetchedAt}
        refreshing={refreshing}
        onRefresh={() => refresh(filters, true)}
      />
      <LogsFilterBar filters={filters} onChange={setFilters} />
      <LogsTable
        logs={snapshot.logs}
        loading={refreshing}
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
  const ageSec = Math.max(
    0,
    Math.round((Date.now() - Date.parse(fetchedAt)) / 1000),
  )
  const ageLabel = ageSec < 5 ? "now" : `${ageSec}s ago`
  return (
    <header className="flex items-baseline justify-between">
      <div className="flex flex-col gap-tile-tight">
        <h1 className="text-xl font-semibold tracking-tight text-foreground">
          Logs
        </h1>
        <p className="text-meta text-muted-foreground">
          Runtime log stream, newest first. Served from the in-memory
          ring (volatile) unless an external adapter (
          <code className="font-mono">AF_STACK_LOGS_ADAPTER</code>, e.g.
          Loki) is configured.
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

function parseLevel(raw: string | null) {
  if (raw && isLogLevelFilter(raw)) return raw
  return DEFAULT_LOGS_FILTERS.level
}

// SPDX-License-Identifier: Apache-2.0

"use client"

import { RefreshCw } from "lucide-react"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useCallback, useEffect, useMemo, useState } from "react"

import { Button } from "@/components/ui/button"
import { FilterChip, FilterChipGroup } from "@/components/ui/filter-chip"

import { api } from "@/lib/api"
import { isCronActiveFilter, matchesActiveFilter } from "@/lib/crons-page/derive"
import type { CronActiveFilter, CronsSnapshot } from "@/lib/crons-page/types"
import { DEFAULT_CRON_ACTIVE_FILTER } from "@/lib/crons-page/types"
import { polling } from "@/lib/theme"

import { CronsKpis } from "./crons-kpis"
import { CronsTable } from "./crons-table"

// CronsShell owns:
//   - URL-persistent ?active filter — applied client-side because the
//     /api/v1/crons endpoint returns every definition (no server filter)
//   - 5s polling tick — a cron's next_run_at ages live, and Trigger /
//     Pause mutations need the row to reflect the new state
//   - Refresh after a mutation so last_run / next_run update

const ACTIVE_OPTIONS: { value: CronActiveFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "active", label: "Active" },
  { value: "inactive", label: "Paused" },
]

interface CronsShellProps {
  initialSnapshot: CronsSnapshot
}

export function CronsShell({ initialSnapshot }: CronsShellProps) {
  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()

  const [snapshot, setSnapshot] = useState<CronsSnapshot>(initialSnapshot)
  const [refreshing, setRefreshing] = useState(false)

  const filter = useMemo<CronActiveFilter>(
    () => parseFilter(searchParams.get("active")),
    [searchParams],
  )

  const setFilter = useCallback(
    (next: CronActiveFilter) => {
      const params = new URLSearchParams(searchParams.toString())
      if (next === DEFAULT_CRON_ACTIVE_FILTER) params.delete("active")
      else params.set("active", next)
      const qs = params.toString()
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
    },
    [pathname, router, searchParams],
  )

  const refresh = useCallback(async (manual = false) => {
    if (manual) setRefreshing(true)
    const fetchedAt = new Date().toISOString()
    const list = await api.crons.list().catch(() => null)
    setSnapshot({
      crons: list?.crons ?? [],
      fetchedAt,
      healthy: list !== null,
    })
    if (manual) setRefreshing(false)
  }, [])

  useEffect(() => {
    let cancelled = false
    const id = setInterval(() => {
      if (!cancelled) void refresh()
    }, polling.home)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [refresh])

  const visible = useMemo(
    () => snapshot.crons.filter((cron) => matchesActiveFilter(cron, filter)),
    [snapshot.crons, filter],
  )

  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <Header
        fetchedAt={snapshot.fetchedAt}
        refreshing={refreshing}
        onRefresh={() => refresh(true)}
      />
      <CronsKpis crons={snapshot.crons} healthy={snapshot.healthy} />
      <div className="flex flex-col gap-stack">
        <div className="flex flex-wrap items-center gap-stack rounded-md border bg-card/95 px-row-x py-row-y">
          <FilterChipGroup label="State">
            {ACTIVE_OPTIONS.map((opt) => (
              <FilterChip
                key={opt.value}
                label={opt.label}
                active={filter === opt.value}
                onSelect={() => setFilter(opt.value)}
              />
            ))}
          </FilterChipGroup>
        </div>
        <CronsTable
          crons={visible}
          total={snapshot.crons.length}
          loading={refreshing}
          healthy={snapshot.healthy}
          onMutated={() => refresh(false)}
        />
      </div>
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
  // Tick the "updated Xs ago" label off a 1s interval rather than reading
  // the clock during render — keeps the component render-pure.
  const [ageLabel, setAgeLabel] = useState("now")
  useEffect(() => {
    const id = setInterval(() => {
      const ageSec = Math.max(0, Math.round((Date.now() - Date.parse(fetchedAt)) / 1000))
      setAgeLabel(ageSec < 5 ? "now" : `${ageSec}s ago`)
    }, 1000)
    return () => clearInterval(id)
  }, [fetchedAt])
  return (
    <header className="flex items-baseline justify-between">
      <div className="flex flex-col gap-tile-tight">
        <h1 className="text-xl font-semibold tracking-tight text-foreground">Crons</h1>
        <p className="text-meta text-muted-foreground">
          Scheduled jobs — what fires when, its next tick, and a manual trigger. Each cron enqueues
          a job kind on its crontab schedule.
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
          <RefreshCw className={`size-3.5 ${refreshing ? "animate-spin" : ""}`} aria-hidden />
          Refresh
        </Button>
      </div>
    </header>
  )
}

function parseFilter(raw: string | null): CronActiveFilter {
  if (raw && isCronActiveFilter(raw)) return raw
  return DEFAULT_CRON_ACTIVE_FILTER
}

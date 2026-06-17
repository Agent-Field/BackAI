// SPDX-License-Identifier: Apache-2.0

"use client"

import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useCallback, useEffect, useMemo, useState } from "react"

import { api } from "@/lib/api"
import { isCostRangeKind, rangeFromKind } from "@/lib/cost/range"
import type { CostSnapshot, CostRangeKind } from "@/lib/cost/types"
import { topNTenants } from "@/lib/cost/derive"
import { polling } from "@/lib/theme"

import { AdapterFooter } from "./adapter-footer"
import { DegradedBanner } from "./degraded-banner"
import { RangeChips } from "./range-chips"
import { Zone1AnomalyStrip } from "./zone1-anomaly-strip"
import { Zone2Hierarchy } from "./zone2-hierarchy"
import { Zone3Explorer } from "./zone3-explorer"
import { Zone4Economics } from "./zone4-economics"

// CostShell — owns the range selection, polling tick, and per-tenant
// fan-out for Zone 3. Per the brief Cost is desktop-primary; the shell
// scales widely (max-width follows the dashboard's normal flow).

const POLL_MS = polling.services // 10s — cost data isn't worth more

export function CostShell({
  initialSnapshot,
}: {
  initialSnapshot: CostSnapshot
}) {
  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()

  const [snapshot, setSnapshot] = useState<CostSnapshot>(initialSnapshot)

  const range: CostRangeKind = useMemo(() => {
    const raw = searchParams.get("range")
    return raw && isCostRangeKind(raw) ? raw : initialSnapshot.range.kind
  }, [searchParams, initialSnapshot.range.kind])

  const setRange = useCallback(
    (next: CostRangeKind) => {
      const params = new URLSearchParams(searchParams.toString())
      if (next === "30d") params.delete("range")
      else params.set("range", next)
      const qs = params.toString()
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
    },
    [pathname, router, searchParams],
  )

  const refresh = useCallback(
    async (rangeKind: CostRangeKind) => {
      const r = rangeFromKind(rangeKind)
      const fetchedAt = new Date().toISOString()
      const [primary, budgets, cache] = await Promise.allSettled([
        api.cost({ from: r.from, to: r.to }),
        api.budgets.list(),
        api.llm.cacheStats(),
      ])
      const primaryValue = primary.status === "fulfilled" ? primary.value : null
      const top = topNTenants(primaryValue, 5)
      const byTenant: Record<string, NonNullable<typeof primaryValue>> = {}
      if (primaryValue !== null) {
        const perTenant = await Promise.allSettled(
          top.map((t) =>
            api.cost({ from: r.from, to: r.to, tenant: t.id }),
          ),
        )
        perTenant.forEach((res, idx) => {
          if (res.status === "fulfilled") {
            byTenant[top[idx].id] = res.value
          }
        })
      }
      setSnapshot((prev) => ({
        ...prev,
        primary: primaryValue,
        byTenant,
        budgets:
          budgets.status === "fulfilled" ? budgets.value.budgets : prev.budgets,
        cache: cache.status === "fulfilled" ? cache.value : prev.cache,
        range: r,
        fetchedAt,
        primaryHealthy: primary.status === "fulfilled",
        budgetsHealthy: budgets.status === "fulfilled",
        cacheHealthy: cache.status === "fulfilled",
      }))
    },
    [],
  )

  // Refresh on range change.
  useEffect(() => {
    if (range !== snapshot.range.kind) {
      void refresh(range)
    }
  }, [range, snapshot.range.kind, refresh])

  // Background polling tick keeps the page live without blocking renders.
  useEffect(() => {
    let cancelled = false
    const id = setInterval(() => {
      if (!cancelled) void refresh(range)
    }, POLL_MS)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [range, refresh])

  // Initial fan-out for per-tenant data (server snapshot only has the
  // platform-wide summary). Runs once after first paint.
  useEffect(() => {
    if (Object.keys(snapshot.byTenant).length === 0 && snapshot.primary) {
      void refresh(range)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const onBudgetSaved = useCallback(async () => {
    await refresh(range)
  }, [range, refresh])

  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <CostHeader range={range} fetchedAt={snapshot.fetchedAt} />
      <DegradedBanner
        primaryHealthy={snapshot.primaryHealthy}
        budgetsHealthy={snapshot.budgetsHealthy}
        cacheHealthy={snapshot.cacheHealthy}
        onRetry={() => refresh(range)}
      />
      <RangeChips range={range} onChange={setRange} />
      <Zone1AnomalyStrip snapshot={snapshot} />
      <Zone2Hierarchy snapshot={snapshot} />
      <Zone3Explorer snapshot={snapshot} />
      <Zone4Economics snapshot={snapshot} onBudgetSaved={onBudgetSaved} />
      <AdapterFooter />
    </div>
  )
}

function CostHeader({
  range,
  fetchedAt,
}: {
  range: CostRangeKind
  fetchedAt: string
}) {
  const ageSec = Math.max(0, Math.round((Date.now() - Date.parse(fetchedAt)) / 1000))
  const ageLabel = ageSec < 5 ? "now" : `${ageSec}s ago`
  const rangeLabel = labelForRange(range)
  return (
    <header className="flex items-baseline justify-between">
      <div className="flex flex-col gap-tile-tight">
        <h1 className="text-xl font-semibold tracking-tight text-foreground">
          Cost
        </h1>
        <p className="text-meta text-muted-foreground">
          Inference economics — {rangeLabel}.
        </p>
      </div>
      <span className="text-meta text-muted-foreground tabular-nums">
        updated {ageLabel}
      </span>
    </header>
  )
}

function labelForRange(range: CostRangeKind): string {
  switch (range) {
    case "today":
      return "today"
    case "7d":
      return "last 7 days"
    case "30d":
      return "last 30 days"
    case "90d":
      return "last 90 days"
  }
}

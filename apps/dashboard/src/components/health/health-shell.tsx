// SPDX-License-Identifier: Apache-2.0

"use client"

import { RefreshCw } from "lucide-react"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useCallback, useEffect, useMemo, useState } from "react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"

import { api } from "@/lib/api"
import { summarise } from "@/lib/health/derive"
import { isProviderWindowKind } from "@/lib/health/types"
import type { HealthSnapshot, ProviderWindowKind } from "@/lib/health/types"
import { polling } from "@/lib/theme"

import { IncidentBanner } from "./incident-banner"
import { ZoneAProviders } from "./zone-a-providers"
import { ZoneBServices } from "./zone-b-services"
import { ZoneCDatabase } from "./zone-c-database"
import { ZoneDRuntime } from "./zone-d-runtime"

// HealthShell owns:
//   - URL-persistent provider window (?window=24h|7d|30d)
//   - Polling tick (5s — anchors cadence; Health is operator-watched)
//   - Refresh button
//   - Overall summary computation → IncidentBanner

const POLL_MS = polling.services // 10s — DB queries are expensive

interface HealthShellProps {
  initialSnapshot: HealthSnapshot
}

export function HealthShell({ initialSnapshot }: HealthShellProps) {
  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()

  const [snapshot, setSnapshot] = useState<HealthSnapshot>(initialSnapshot)
  const [refreshing, setRefreshing] = useState(false)

  const window: ProviderWindowKind = useMemo(() => {
    const raw = searchParams.get("window")
    return raw && isProviderWindowKind(raw)
      ? raw
      : initialSnapshot.providerWindow
  }, [searchParams, initialSnapshot.providerWindow])

  const setWindow = useCallback(
    (next: ProviderWindowKind) => {
      const params = new URLSearchParams(searchParams.toString())
      if (next === "24h") params.delete("window")
      else params.set("window", next)
      const qs = params.toString()
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
    },
    [pathname, router, searchParams],
  )

  const refresh = useCallback(
    async (w: ProviderWindowKind, manual = false) => {
      if (manual) setRefreshing(true)
      const fetchedAt = new Date().toISOString()
      const [services, providers, db, metrics] = await Promise.allSettled([
        api.admin.services.list(),
        api.admin.llm.providerHealth({ window: w }),
        api.db.health(),
        api.metrics.summary(),
      ])
      setSnapshot((prev) => ({
        services:
          services.status === "fulfilled"
            ? services.value.services
            : prev.services,
        providers:
          providers.status === "fulfilled"
            ? providers.value.providers
            : prev.providers,
        providerWindow: w,
        db: db.status === "fulfilled" ? db.value : prev.db,
        metrics:
          metrics.status === "fulfilled" ? metrics.value : prev.metrics,
        fetchedAt,
        servicesHealthy: services.status === "fulfilled",
        providersHealthy: providers.status === "fulfilled",
        dbHealthy: db.status === "fulfilled",
        metricsHealthy: metrics.status === "fulfilled",
      }))
      if (manual) {
        setRefreshing(false)
        toast.success("Refreshed health probes")
      }
    },
    [],
  )

  // Refresh on window change.
  useEffect(() => {
    if (window !== snapshot.providerWindow) {
      void refresh(window)
    }
  }, [window, snapshot.providerWindow, refresh])

  // Background polling tick.
  useEffect(() => {
    let cancelled = false
    const id = setInterval(() => {
      if (!cancelled) void refresh(window)
    }, POLL_MS)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [window, refresh])

  const summary = useMemo(() => summarise(snapshot), [snapshot])

  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <Header
        fetchedAt={snapshot.fetchedAt}
        refreshing={refreshing}
        onRefresh={() => refresh(window, true)}
      />
      <IncidentBanner summary={summary} fetchedAt={snapshot.fetchedAt} />
      <ZoneAProviders snapshot={snapshot} onWindowChange={setWindow} />
      <ZoneBServices snapshot={snapshot} />
      <ZoneCDatabase db={snapshot.db} />
      <ZoneDRuntime metrics={snapshot.metrics} />
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
          Health
        </h1>
        <p className="text-meta text-muted-foreground">
          Connected services, providers, database, and runtime self.
        </p>
      </div>
      <div className="flex items-center gap-stack text-meta text-muted-foreground">
        <span className="tabular-nums">last sweep {ageLabel}</span>
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

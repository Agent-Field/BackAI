// SPDX-License-Identifier: Apache-2.0

"use client"

import { RefreshCw } from "lucide-react"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useCallback, useEffect, useState } from "react"

import { Button } from "@/components/ui/button"
import { FilterChip, FilterChipGroup } from "@/components/ui/filter-chip"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import { api } from "@/lib/api"
import type { ErrorStatusFilter, ErrorsSnapshot } from "@/lib/errors-page/types"
import {
  DEFAULT_ERROR_STATUS,
  isErrorStatusFilter,
} from "@/lib/errors-page/types"
import { polling } from "@/lib/theme"

import { ErrorsTable } from "./errors-table"

// ErrorsShell owns:
//   - URL-persistent status filter (?status=open|muted|resolved)
//   - polling tick so new groups surface without a manual reload
//   - post-mutation refresh (mute / resolve / reopen re-lists the bucket)
// List + capabilities re-fetch together on every tick; capabilities are
// cheap and this keeps the degraded → recovered path symmetric with the
// server snapshot in @/lib/errors-page/data.

const STATUS_OPTIONS: { value: ErrorStatusFilter; label: string }[] = [
  { value: "open", label: "Open" },
  { value: "muted", label: "Muted" },
  { value: "resolved", label: "Resolved" },
]

interface ErrorsShellProps {
  initialSnapshot: ErrorsSnapshot
}

export function ErrorsShell({ initialSnapshot }: ErrorsShellProps) {
  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()

  const [snapshot, setSnapshot] = useState<ErrorsSnapshot>(initialSnapshot)
  const [refreshing, setRefreshing] = useState(false)

  const status = parseStatus(searchParams.get("status"))

  const setStatus = useCallback(
    (next: ErrorStatusFilter) => {
      const params = new URLSearchParams(searchParams.toString())
      if (next === DEFAULT_ERROR_STATUS) params.delete("status")
      else params.set("status", next)
      const qs = params.toString()
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
    },
    [pathname, router, searchParams],
  )

  const refresh = useCallback(
    async (nextStatus: ErrorStatusFilter, manual = false) => {
      if (manual) setRefreshing(true)
      const fetchedAt = new Date().toISOString()
      const [list, capabilities] = await Promise.all([
        api.errors.list({ status: nextStatus, limit: 100 }).catch(() => null),
        api.errors.capabilities().catch(() => null),
      ])
      if (list === null) {
        setSnapshot((prev) => ({ ...prev, fetchedAt, healthy: false }))
      } else {
        setSnapshot((prev) => ({
          groups: list.groups,
          total: list.total ?? list.groups.length,
          capabilities: capabilities ?? prev.capabilities,
          fetchedAt,
          healthy: true,
        }))
      }
      if (manual) setRefreshing(false)
    },
    [],
  )

  useEffect(() => {
    void refresh(status)
  }, [status, refresh])

  useEffect(() => {
    let cancelled = false
    const id = setInterval(() => {
      if (!cancelled) void refresh(status)
    }, polling.home)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [status, refresh])

  const volatile = snapshot.capabilities?.persistence === "volatile"

  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <Header
        fetchedAt={snapshot.fetchedAt}
        refreshing={refreshing}
        onRefresh={() => refresh(status, true)}
      />
      <ZoneCard>
        <ZoneCardHeader
          title="Error groups"
          subtitle={`${snapshot.total} ${status}`}
          trailing={
            <FilterChipGroup label="Status">
              {STATUS_OPTIONS.map((opt) => (
                <FilterChip
                  key={opt.value}
                  label={opt.label}
                  active={status === opt.value}
                  onSelect={() => setStatus(opt.value)}
                />
              ))}
            </FilterChipGroup>
          }
        />
        {volatile ? (
          <p className="border-b px-row-x py-tile-tight text-meta text-muted-foreground">
            Volatile store: groups are derived from the runtime&apos;s
            in-memory log ring and reset on restart. Configure an external
            errors adapter (
            <code className="font-mono">AF_STACK_ERRORS_ADAPTER</code>, e.g.
            GlitchTip) to make them durable.
          </p>
        ) : null}
        <ErrorsTable
          groups={snapshot.groups}
          total={snapshot.total}
          loading={refreshing}
          healthy={snapshot.healthy}
          capabilities={snapshot.capabilities}
          onMutated={() => refresh(status, false)}
        />
      </ZoneCard>
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
          Errors
        </h1>
        <p className="text-meta text-muted-foreground">
          Grouped errors across services and agent runs.
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

function parseStatus(raw: string | null): ErrorStatusFilter {
  if (raw && isErrorStatusFilter(raw)) return raw
  return DEFAULT_ERROR_STATUS
}

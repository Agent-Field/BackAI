// SPDX-License-Identifier: Apache-2.0

"use client"

import { RefreshCw, Search, X } from "lucide-react"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useCallback, useEffect, useMemo, useState } from "react"

import { Button } from "@/components/ui/button"
import { FilterChip, FilterChipGroup } from "@/components/ui/filter-chip"
import { Input } from "@/components/ui/input"

import { api } from "@/lib/api"
import type { SessionsFilters, SessionsSnapshot } from "@/lib/sessions-page/types"

import { SessionsTable } from "./sessions-table"

// SessionsShell owns:
//   - URL-persistent filter state (?email / ?expired)
//   - Client refetch when filters change (server snapshot is first paint)
//   - Refresh after a revoke mutation

interface SessionsShellProps {
  initialSnapshot: SessionsSnapshot
}

export function SessionsShell({ initialSnapshot }: SessionsShellProps) {
  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()

  const [snapshot, setSnapshot] = useState<SessionsSnapshot>(initialSnapshot)
  const [refreshing, setRefreshing] = useState(false)

  const filters = useMemo<SessionsFilters>(
    () => ({
      email: searchParams.get("email") ?? "",
      includeExpired: searchParams.get("expired") === "1",
    }),
    [searchParams],
  )

  const setFilters = useCallback(
    (next: Partial<SessionsFilters>) => {
      const params = new URLSearchParams(searchParams.toString())
      const final = { ...filters, ...next }
      if (!final.email.trim()) params.delete("email")
      else params.set("email", final.email)
      if (!final.includeExpired) params.delete("expired")
      else params.set("expired", "1")
      const qs = params.toString()
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
    },
    [filters, pathname, router, searchParams],
  )

  const refresh = useCallback(
    async (current: SessionsFilters, manual = false) => {
      if (manual) setRefreshing(true)
      const fetchedAt = new Date().toISOString()
      try {
        const next = await api.admin.sessions.list({
          email: current.email.trim() || undefined,
          include_expired: current.includeExpired || undefined,
          limit: 200,
        })
        setSnapshot({
          sessions: next.sessions,
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

  // Refetch whenever the URL-driven filters change. Email is filtered
  // server-side, so a keystroke means a round trip — sessions lists are
  // small enough that the simplicity wins over a debounce cache.
  useEffect(() => {
    void refresh(filters)
  }, [filters, refresh])

  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <header className="flex items-baseline justify-between">
        <div className="flex flex-col gap-tile-tight">
          <h1 className="text-xl font-semibold tracking-tight text-foreground">
            Sessions
          </h1>
          <p className="text-meta text-muted-foreground">
            Live better-auth sessions across the operator dashboard and the
            customer app. Both apps share the same auth tables — only the
            cookie prefix differs per app.
          </p>
        </div>
        <div className="flex items-center gap-stack text-meta text-muted-foreground">
          <span className="tabular-nums">
            updated {ageLabel(snapshot.fetchedAt)}
          </span>
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() => refresh(filters, true)}
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

      <div className="flex flex-wrap items-center gap-stack rounded-md border bg-card/95 px-row-x py-row-y">
        <FilterChipGroup label="Show">
          <FilterChip
            label="Active only"
            active={!filters.includeExpired}
            onSelect={() => setFilters({ includeExpired: false })}
          />
          <FilterChip
            label="Include expired"
            active={filters.includeExpired}
            onSelect={() => setFilters({ includeExpired: true })}
          />
        </FilterChipGroup>
        <div className="ml-auto flex min-w-0 flex-1 items-center gap-inline">
          <div className="relative w-full max-w-sm">
            <Search
              className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
              aria-hidden
            />
            <Input
              type="search"
              value={filters.email}
              placeholder="Filter by email…"
              onChange={(e) => setFilters({ email: e.target.value })}
              className="h-7 pl-7 pr-7 text-meta"
            />
            {filters.email ? (
              <button
                type="button"
                aria-label="Clear email filter"
                onClick={() => setFilters({ email: "" })}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              >
                <X className="size-3.5" aria-hidden />
              </button>
            ) : null}
          </div>
        </div>
      </div>

      <SessionsTable
        sessions={snapshot.sessions}
        total={snapshot.total}
        healthy={snapshot.healthy}
        onMutated={() => refresh(filters)}
      />
    </div>
  )
}

function ageLabel(fetchedAt: string): string {
  const ageSec = Math.max(
    0,
    Math.round((Date.now() - Date.parse(fetchedAt)) / 1000),
  )
  return ageSec < 5 ? "now" : `${ageSec}s ago`
}

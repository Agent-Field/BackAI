// SPDX-License-Identifier: Apache-2.0

"use client"

import { RefreshCw, Search, X } from "lucide-react"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useCallback, useEffect, useMemo, useState } from "react"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"

import { api } from "@/lib/api"
import type { User } from "@/lib/api"
import type { UsersFilters, UsersSnapshot } from "@/lib/users-page/types"

import { EraseUserDialog } from "./erase-user-dialog"
import { UsersTable } from "./users-table"

// UsersShell owns:
//   - URL-persistent filter state (?search / ?tenant)
//   - Client refetch when filters change / after a GDPR erase
//   - The erase double-confirm dialog

interface UsersShellProps {
  initialSnapshot: UsersSnapshot
}

export function UsersShell({ initialSnapshot }: UsersShellProps) {
  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()

  const [snapshot, setSnapshot] = useState<UsersSnapshot>(initialSnapshot)
  const [refreshing, setRefreshing] = useState(false)
  const [eraseTarget, setEraseTarget] = useState<User | null>(null)

  const filters = useMemo<UsersFilters>(
    () => ({
      search: searchParams.get("search") ?? "",
      tenant: searchParams.get("tenant") ?? "",
    }),
    [searchParams],
  )

  const setFilters = useCallback(
    (next: Partial<UsersFilters>) => {
      const params = new URLSearchParams(searchParams.toString())
      const final = { ...filters, ...next }
      if (!final.search.trim()) params.delete("search")
      else params.set("search", final.search)
      if (!final.tenant) params.delete("tenant")
      else params.set("tenant", final.tenant)
      const qs = params.toString()
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
    },
    [filters, pathname, router, searchParams],
  )

  const refresh = useCallback(
    async (current: UsersFilters, manual = false) => {
      if (manual) setRefreshing(true)
      const fetchedAt = new Date().toISOString()
      try {
        const [users, tenants] = await Promise.all([
          api.admin.users.list({
            tenant: current.tenant || undefined,
            search: current.search.trim() || undefined,
          }),
          api.admin.tenants.list().catch(() => null),
        ])
        setSnapshot((prev) => ({
          users: users.users,
          tenants: tenants?.tenants ?? prev.tenants,
          fetchedAt,
          healthy: true,
        }))
      } catch {
        setSnapshot((prev) => ({ ...prev, fetchedAt, healthy: false }))
      } finally {
        if (manual) setRefreshing(false)
      }
    },
    [],
  )

  useEffect(() => {
    void refresh(filters)
  }, [filters, refresh])

  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <header className="flex items-baseline justify-between">
        <div className="flex flex-col gap-tile-tight">
          <h1 className="text-xl font-semibold tracking-tight text-foreground">
            Users
          </h1>
          <p className="text-meta text-muted-foreground">
            Every user across the customer app and the operator dashboard.
            GDPR export / erase live behind each row&apos;s menu.
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
        <label className="flex items-center gap-inline text-meta text-muted-foreground">
          <span className="text-eyebrow uppercase tracking-wide">Tenant</span>
          <select
            value={filters.tenant}
            onChange={(e) => setFilters({ tenant: e.target.value })}
            className="h-7 rounded-lg border border-input bg-transparent px-2 text-meta text-foreground outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30"
          >
            <option value="">All tenants</option>
            {snapshot.tenants.map((t) => (
              <option key={t.id} value={t.id}>
                {t.name || t.slug}
              </option>
            ))}
          </select>
        </label>
        <div className="ml-auto flex min-w-0 flex-1 items-center gap-inline">
          <div className="relative w-full max-w-sm">
            <Search
              className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
              aria-hidden
            />
            <Input
              type="search"
              value={filters.search}
              placeholder="Search email / name…"
              onChange={(e) => setFilters({ search: e.target.value })}
              className="h-7 pl-7 pr-7 text-meta"
            />
            {filters.search ? (
              <button
                type="button"
                aria-label="Clear search"
                onClick={() => setFilters({ search: "" })}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              >
                <X className="size-3.5" aria-hidden />
              </button>
            ) : null}
          </div>
        </div>
      </div>

      <UsersTable
        users={snapshot.users}
        healthy={snapshot.healthy}
        onErase={setEraseTarget}
      />

      <EraseUserDialog
        user={eraseTarget}
        onClose={() => setEraseTarget(null)}
        onErased={() => refresh(filters)}
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

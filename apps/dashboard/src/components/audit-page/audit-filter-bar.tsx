// SPDX-License-Identifier: Apache-2.0

"use client"

import { Search, X } from "lucide-react"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useCallback, useEffect, useState } from "react"

import { FilterChip, FilterChipGroup } from "@/components/ui/filter-chip"
import { Input } from "@/components/ui/input"

import type { AuditPageFilters, TenantOption } from "@/lib/audit-page/types"

// Sticky filter bar for the Audit log. Filters live in the URL
// (?action / ?tenant) so the server page refetches on change and views
// stay linkable. The action input debounces before touching the URL —
// unlike Runs (client-side filtering) every change here is a server
// round trip. Changing any filter resets ?offset.

interface AuditFilterBarProps {
  filters: AuditPageFilters
  tenants: TenantOption[]
}

export function AuditFilterBar({ filters, tenants }: AuditFilterBarProps) {
  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()

  const [action, setAction] = useState(filters.action)
  // Re-sync the local input when the URL changes underneath us
  // (back/forward navigation, chip-driven resets). Adjust-during-render
  // pattern — setState in an effect would cascade renders.
  const [prevUrlAction, setPrevUrlAction] = useState(filters.action)
  if (prevUrlAction !== filters.action) {
    setPrevUrlAction(filters.action)
    setAction(filters.action)
  }

  const apply = useCallback(
    (next: { action?: string; tenant?: string }) => {
      const params = new URLSearchParams(searchParams.toString())
      const finalAction = (next.action ?? filters.action).trim()
      const finalTenant = next.tenant ?? filters.tenant
      if (finalAction) params.set("action", finalAction)
      else params.delete("action")
      if (finalTenant) params.set("tenant", finalTenant)
      else params.delete("tenant")
      // New filter, new result set — offset no longer means anything.
      params.delete("offset")
      const qs = params.toString()
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
    },
    [filters.action, filters.tenant, pathname, router, searchParams],
  )

  // Debounced action filter — apply ~350ms after the operator stops
  // typing.
  useEffect(() => {
    if (action.trim() === filters.action.trim()) return
    const id = setTimeout(() => apply({ action }), 350)
    return () => clearTimeout(id)
  }, [action, filters.action, apply])

  return (
    <div className="sticky top-12 z-20 flex flex-col gap-stack rounded-md border bg-card/95 px-row-x py-row-y backdrop-blur supports-[backdrop-filter]:bg-card/80">
      <div className="flex flex-wrap items-center gap-stack">
        <FilterChipGroup label="Tenant">
          <FilterChip
            label="All"
            active={filters.tenant === ""}
            onSelect={() => apply({ tenant: "" })}
          />
          {tenants.map((t) => (
            <FilterChip
              key={t.id}
              label={t.name}
              active={filters.tenant === t.id}
              onSelect={() => apply({ tenant: t.id })}
              ariaLabel={`Tenant ${t.name} filter`}
            />
          ))}
        </FilterChipGroup>
        <div className="ml-auto flex min-w-0 flex-1 items-center gap-inline">
          <div className="relative w-full max-w-sm">
            <Search
              className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
              aria-hidden
            />
            <Input
              type="search"
              value={action}
              placeholder="Filter by action — api_key.create, budget.update…"
              onChange={(e) => setAction(e.target.value)}
              className="h-7 pl-7 pr-7 text-meta"
            />
            {action ? (
              <button
                type="button"
                aria-label="Clear action filter"
                onClick={() => {
                  setAction("")
                  apply({ action: "" })
                }}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              >
                <X className="size-3.5" aria-hidden />
              </button>
            ) : null}
          </div>
        </div>
      </div>
    </div>
  )
}

// SPDX-License-Identifier: Apache-2.0

"use client"

import { Search, X } from "lucide-react"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useCallback, useEffect, useState } from "react"

import { FilterChip, FilterChipGroup } from "@/components/ui/filter-chip"
import { Input } from "@/components/ui/input"

import type {
  ActivityPageFilters,
  TenantOption,
} from "@/lib/activity-page/types"

// Sticky filter bar for the Activity log. Filters live in the URL
// (?tenant / ?action / ?resource_type) so the server page refetches on
// change and views stay linkable. Text inputs debounce before touching
// the URL — every change is a server round trip. Changing any filter
// resets ?offset.

interface ActivityFilterBarProps {
  filters: ActivityPageFilters
  tenants: TenantOption[]
}

export function ActivityFilterBar({
  filters,
  tenants,
}: ActivityFilterBarProps) {
  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()

  const [action, setAction] = useState(filters.action)
  const [resourceType, setResourceType] = useState(filters.resourceType)
  // Re-sync local inputs when the URL changes underneath us
  // (back/forward navigation, chip-driven resets). Adjust-during-render
  // pattern — setState in an effect would cascade renders.
  const [prevUrlAction, setPrevUrlAction] = useState(filters.action)
  if (prevUrlAction !== filters.action) {
    setPrevUrlAction(filters.action)
    setAction(filters.action)
  }
  const [prevUrlResource, setPrevUrlResource] = useState(filters.resourceType)
  if (prevUrlResource !== filters.resourceType) {
    setPrevUrlResource(filters.resourceType)
    setResourceType(filters.resourceType)
  }

  const apply = useCallback(
    (next: { tenant?: string; action?: string; resourceType?: string }) => {
      const params = new URLSearchParams(searchParams.toString())
      const finalTenant = next.tenant ?? filters.tenant
      const finalAction = (next.action ?? filters.action).trim()
      const finalResource = (next.resourceType ?? filters.resourceType).trim()
      if (finalTenant) params.set("tenant", finalTenant)
      else params.delete("tenant")
      if (finalAction) params.set("action", finalAction)
      else params.delete("action")
      if (finalResource) params.set("resource_type", finalResource)
      else params.delete("resource_type")
      // New filter, new result set — offset no longer means anything.
      params.delete("offset")
      const qs = params.toString()
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
    },
    [
      filters.action,
      filters.resourceType,
      filters.tenant,
      pathname,
      router,
      searchParams,
    ],
  )

  // Debounced text filters — apply ~350ms after the operator stops
  // typing.
  useEffect(() => {
    if (action.trim() === filters.action.trim()) return
    const id = setTimeout(() => apply({ action }), 350)
    return () => clearTimeout(id)
  }, [action, filters.action, apply])
  useEffect(() => {
    if (resourceType.trim() === filters.resourceType.trim()) return
    const id = setTimeout(() => apply({ resourceType }), 350)
    return () => clearTimeout(id)
  }, [resourceType, filters.resourceType, apply])

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
        <div className="ml-auto flex min-w-0 flex-1 flex-wrap items-center justify-end gap-inline">
          <div className="relative w-full max-w-56">
            <Search
              className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
              aria-hidden
            />
            <Input
              type="search"
              value={action}
              placeholder="Action — invoice.paid…"
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
          <div className="relative w-full max-w-56">
            <Search
              className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
              aria-hidden
            />
            <Input
              type="search"
              value={resourceType}
              placeholder="Resource type — invoice…"
              onChange={(e) => setResourceType(e.target.value)}
              className="h-7 pl-7 pr-7 text-meta"
            />
            {resourceType ? (
              <button
                type="button"
                aria-label="Clear resource type filter"
                onClick={() => {
                  setResourceType("")
                  apply({ resourceType: "" })
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

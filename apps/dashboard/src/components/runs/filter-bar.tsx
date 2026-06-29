// SPDX-License-Identifier: Apache-2.0

"use client"

import { Search, X } from "lucide-react"

import { FilterChip, FilterChipGroup } from "@/components/ui/filter-chip"
import { Input } from "@/components/ui/input"

import type { StatusCounts } from "@/lib/runs/derive"
import type {
  RunStatusFilter,
  RunTimeRange,
  RunsFilters,
} from "@/lib/runs/types"

// Sticky filter bar. Status + time chips use the standardised FilterChip
// primitive so contrast + density match Inbox / Cost / Health. Search
// box is debounced upstream by the shell so we stay declarative here.

const STATUS_OPTIONS: { value: RunStatusFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "succeeded", label: "OK" },
  { value: "failed", label: "Failed" },
  { value: "running", label: "Running" },
  { value: "queued", label: "Queued" },
  { value: "cancelled", label: "Cancelled" },
]

const TIME_OPTIONS: { value: RunTimeRange; label: string }[] = [
  { value: "1h", label: "1h" },
  { value: "24h", label: "24h" },
  { value: "7d", label: "7d" },
  { value: "30d", label: "30d" },
  { value: "all", label: "All" },
]

interface FilterBarProps {
  filters: RunsFilters
  counts: StatusCounts
  onChange: (next: Partial<RunsFilters>) => void
}

export function FilterBar({ filters, counts, onChange }: FilterBarProps) {
  return (
    <div className="sticky top-12 z-20 flex flex-col gap-stack rounded-md border bg-card/95 px-row-x py-row-y backdrop-blur supports-[backdrop-filter]:bg-card/80">
      <div className="flex flex-wrap items-center gap-stack">
        <FilterChipGroup label="Status">
          {STATUS_OPTIONS.map((opt) => (
            <FilterChip
              key={opt.value}
              label={opt.label}
              count={countFor(opt.value, counts)}
              active={filters.status === opt.value}
              onSelect={() => onChange({ status: opt.value })}
            />
          ))}
        </FilterChipGroup>
        <span aria-hidden className="text-meta text-muted-foreground">
          ·
        </span>
        <FilterChipGroup label="Time">
          {TIME_OPTIONS.map((opt) => (
            <FilterChip
              key={opt.value}
              label={opt.label}
              active={filters.time === opt.value}
              onSelect={() => onChange({ time: opt.value })}
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
              value={filters.search}
              placeholder="Search id / agent / tenant / error…"
              onChange={(e) => onChange({ search: e.target.value })}
              className="h-7 pl-7 pr-7 text-meta"
            />
            {filters.search ? (
              <button
                type="button"
                aria-label="Clear search"
                onClick={() => onChange({ search: "" })}
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

function countFor(value: RunStatusFilter, counts: StatusCounts): number {
  return counts[value]
}

// SPDX-License-Identifier: Apache-2.0

"use client"

import { FilterChip, FilterChipGroup } from "@/components/ui/filter-chip"

import type { CostRangeKind } from "@/lib/cost/types"

const OPTIONS: { value: CostRangeKind; label: string }[] = [
  { value: "today", label: "Today" },
  { value: "7d", label: "7d" },
  { value: "30d", label: "30d" },
  { value: "90d", label: "90d" },
]

// Range selector — reuses the standard FilterChip primitive so the Cost
// page's chips share contrast + density with the Inbox chips. New
// surfaces inherit the same lesson by default.

export function RangeChips({
  range,
  onChange,
}: {
  range: CostRangeKind
  onChange: (next: CostRangeKind) => void
}) {
  return (
    <FilterChipGroup label="Range">
      {OPTIONS.map((opt) => (
        <FilterChip
          key={opt.value}
          label={opt.label}
          active={range === opt.value}
          onSelect={() => onChange(opt.value)}
        />
      ))}
    </FilterChipGroup>
  )
}

// SPDX-License-Identifier: Apache-2.0

"use client"

import { FilterChip, FilterChipGroup } from "@/components/ui/filter-chip"
import {
  countsByKind,
  countsBySeverity,
} from "@/lib/inbox/derive"
import type {
  InboxFilters,
  InboxItem,
  InboxKind,
  InboxSeverity,
} from "@/lib/inbox/types"

// Two chip rows — severity and kind — both URL-persistent through the
// parent shell. Selecting "all" drops the param so a clean URL means
// default state. The counts live on each chip so the operator can see
// the distribution at a glance.
//
// Visual semantics come from the FilterChip primitive
// (components/ui/filter-chip.tsx) — this file maps inbox-domain options
// onto it without re-styling.

interface FilterChipsProps {
  filters: InboxFilters
  items: InboxItem[]
  onChange: (next: Partial<InboxFilters>) => void
}

const SEVERITY_OPTIONS: { value: InboxFilters["severity"]; label: string }[] = [
  { value: "all", label: "All" },
  { value: "critical", label: "Critical" },
  { value: "warning", label: "Warning" },
  { value: "info", label: "Info" },
]

const KIND_OPTIONS: { value: InboxFilters["kind"]; label: string }[] = [
  { value: "all", label: "All" },
  { value: "approval", label: "Approvals" },
  { value: "alert", label: "Alerts" },
]

export function FilterChips({ filters, items, onChange }: FilterChipsProps) {
  const severityCounts = countsBySeverity(items)
  const kindCounts = countsByKind(items)
  return (
    <div
      role="toolbar"
      aria-label="Inbox filters"
      className="flex flex-wrap items-center gap-stack"
    >
      <FilterChipGroup label="Severity">
        {SEVERITY_OPTIONS.map((opt) => (
          <FilterChip
            key={opt.value}
            label={opt.label}
            count={countForSeverity(opt.value, severityCounts, items.length)}
            active={filters.severity === opt.value}
            onSelect={() => onChange({ severity: opt.value })}
          />
        ))}
      </FilterChipGroup>
      <span aria-hidden className="text-meta text-muted-foreground">
        ·
      </span>
      <FilterChipGroup label="Kind">
        {KIND_OPTIONS.map((opt) => (
          <FilterChip
            key={opt.value}
            label={opt.label}
            count={countForKind(opt.value, kindCounts, items.length)}
            active={filters.kind === opt.value}
            onSelect={() => onChange({ kind: opt.value })}
          />
        ))}
      </FilterChipGroup>
    </div>
  )
}

function countForSeverity(
  v: InboxFilters["severity"],
  counts: Record<InboxSeverity, number>,
  total: number,
): number {
  return v === "all" ? total : counts[v]
}

function countForKind(
  v: InboxFilters["kind"],
  counts: Record<InboxKind, number>,
  total: number,
): number {
  return v === "all" ? total : counts[v]
}

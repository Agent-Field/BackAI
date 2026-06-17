// SPDX-License-Identifier: Apache-2.0

"use client"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
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

// Two chip rows — severity and kind — both URL-persistent. Selecting "all"
// drops the param so a clean URL means default state. The counts live on
// the chip so the operator can see the distribution at a glance.

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
      <ChipGroup label="Severity">
        {SEVERITY_OPTIONS.map((opt) => (
          <Chip
            key={opt.value}
            label={opt.label}
            active={filters.severity === opt.value}
            count={countForSeverity(opt.value, severityCounts, items.length)}
            onSelect={() => onChange({ severity: opt.value })}
          />
        ))}
      </ChipGroup>
      <span aria-hidden className="text-meta text-muted-foreground">
        ·
      </span>
      <ChipGroup label="Kind">
        {KIND_OPTIONS.map((opt) => (
          <Chip
            key={opt.value}
            label={opt.label}
            active={filters.kind === opt.value}
            count={countForKind(opt.value, kindCounts, items.length)}
            onSelect={() => onChange({ kind: opt.value })}
          />
        ))}
      </ChipGroup>
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

function ChipGroup({
  label,
  children,
}: {
  label: string
  children: React.ReactNode
}) {
  return (
    <div className="flex items-center gap-inline">
      <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
        {label}
      </span>
      <div className="flex items-center gap-inline">{children}</div>
    </div>
  )
}

function Chip({
  label,
  count,
  active,
  onSelect,
}: {
  label: string
  count: number
  active: boolean
  onSelect: () => void
}) {
  return (
    <Button
      type="button"
      size="sm"
      variant={active ? "default" : "outline"}
      onClick={onSelect}
      className="h-7 gap-inline text-meta"
    >
      <span>{label}</span>
      <Badge
        variant="secondary"
        className="h-4 px-1.5 font-mono text-meta tabular-nums"
      >
        {count}
      </Badge>
    </Button>
  )
}

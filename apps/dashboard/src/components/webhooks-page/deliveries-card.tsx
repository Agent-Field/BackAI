// SPDX-License-Identifier: Apache-2.0

"use client"

import { useState } from "react"

import { FilterChip, FilterChipGroup } from "@/components/ui/filter-chip"
import { Skeleton } from "@/components/ui/skeleton"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import type { WebhookDelivery } from "@/lib/api"
import type { WebhookDirectionFilter } from "@/lib/webhooks-page/types"

import { DELIVERY_ROW_COLUMNS, DeliveryRow } from "./delivery-row"

// Deliveries zone — the unified inbound + outbound delivery feed with a
// direction filter, inline expansion, and Retry on failed rows. Header
// row stays visible per the "structure visible even at zero" rule.

const DIRECTION_OPTIONS: { value: WebhookDirectionFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "inbound", label: "Inbound" },
  { value: "outbound", label: "Outbound" },
]

interface DeliveriesCardProps {
  deliveries: WebhookDelivery[]
  total: number
  direction: WebhookDirectionFilter
  loading: boolean
  healthy: boolean
  onDirectionChange: (next: WebhookDirectionFilter) => void
  onMutated: () => Promise<void> | void
}

export function DeliveriesCard({
  deliveries,
  total,
  direction,
  loading,
  healthy,
  onDirectionChange,
  onMutated,
}: DeliveriesCardProps) {
  const [expandedId, setExpandedId] = useState<string | null>(null)
  return (
    <ZoneCard aria-labelledby="webhooks-deliveries">
      <ZoneCardHeader
        id="webhooks-deliveries"
        title="Deliveries"
        subtitle={healthy ? `${total} total` : "unavailable"}
        trailing={
          <FilterChipGroup label="Direction">
            {DIRECTION_OPTIONS.map((opt) => (
              <FilterChip
                key={opt.value}
                label={opt.label}
                active={direction === opt.value}
                onSelect={() => onDirectionChange(opt.value)}
              />
            ))}
          </FilterChipGroup>
        }
      />
      <TableHeader shown={deliveries.length} total={total} />
      {!healthy ? (
        <DegradedRow />
      ) : loading && deliveries.length === 0 ? (
        <SkeletonRows />
      ) : deliveries.length === 0 ? (
        <EmptyRow />
      ) : (
        <ul role="list" className="flex flex-col">
          {deliveries.map((d) => (
            <li key={d.id}>
              <DeliveryRow
                delivery={d}
                expanded={d.id === expandedId}
                onToggle={(next) =>
                  setExpandedId((prev) => (prev === next.id ? null : next.id))
                }
                onMutated={onMutated}
              />
            </li>
          ))}
        </ul>
      )}
    </ZoneCard>
  )
}

function TableHeader({ shown, total }: { shown: number; total: number }) {
  return (
    <header
      className={`grid items-center gap-stack border-b bg-card px-row-x py-tile-tight text-eyebrow uppercase tracking-wide text-muted-foreground ${DELIVERY_ROW_COLUMNS}`}
    >
      <span aria-hidden />
      <span>Time</span>
      <span>Direction</span>
      <span>Event / Destination / Error</span>
      <span className="text-right">Tries</span>
      <span className="text-right">Response</span>
      <span className="text-right">Status</span>
      <span className="text-right tabular-nums">
        {shown === total ? total : `${shown}/${total}`}
      </span>
    </header>
  )
}

function SkeletonRows() {
  return (
    <ul role="list" className="flex flex-col">
      {Array.from({ length: 6 }).map((_, i) => (
        <li
          key={i}
          className={`grid items-center gap-stack border-l-4 border-b border-l-transparent px-row-x py-row-y ${DELIVERY_ROW_COLUMNS}`}
        >
          <Skeleton className="size-icon-dot rounded-pill" />
          <Skeleton className="h-3 w-12" />
          <Skeleton className="h-4 w-14" />
          <Skeleton className="h-3 w-full" />
          <Skeleton className="h-3 w-8 justify-self-end" />
          <Skeleton className="h-3 w-12 justify-self-end" />
          <Skeleton className="h-3 w-14 justify-self-end" />
          <span aria-hidden />
        </li>
      ))}
    </ul>
  )
}

function EmptyRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">No deliveries yet.</p>
      <p className="max-w-md text-meta text-muted-foreground">
        Send one with{" "}
        <code className="font-mono">POST /api/v1/webhooks/send</code> or
        point a provider at an inbound endpoint above.
      </p>
    </div>
  )
}

function DegradedRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">Delivery log unavailable.</p>
      <p className="max-w-md text-meta text-muted-foreground">
        The runtime did not return webhook deliveries. Check the Health
        page for a database probe, then retry.
      </p>
    </div>
  )
}

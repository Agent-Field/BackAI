// SPDX-License-Identifier: Apache-2.0

"use client"

import { useState } from "react"

import { FilterChip, FilterChipGroup } from "@/components/ui/filter-chip"
import { Skeleton } from "@/components/ui/skeleton"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import type { Notification } from "@/lib/api"
import type {
  NotificationKindFilter,
  NotificationStatusFilter,
} from "@/lib/notifications-page/types"

import { NOTIFICATION_ROW_COLUMNS, NotificationRow } from "./notification-row"

// Notifications feed — the outbox drain log with status + kind filters,
// inline expansion, and a live-updating count. Header row stays visible
// per the "structure visible even at zero" rule.

const STATUS_OPTIONS: { value: NotificationStatusFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "queued", label: "Queued" },
  { value: "sending", label: "Sending" },
  { value: "sent", label: "Sent" },
  { value: "failed", label: "Failed" },
  { value: "skipped", label: "Skipped" },
]

const KIND_OPTIONS: { value: NotificationKindFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "email", label: "Email" },
  { value: "sms", label: "SMS" },
  { value: "push", label: "Push" },
  { value: "log", label: "Log" },
]

interface NotificationsFeedProps {
  notifications: Notification[]
  total: number
  status: NotificationStatusFilter
  kind: NotificationKindFilter
  loading: boolean
  healthy: boolean
  onStatusChange: (next: NotificationStatusFilter) => void
  onKindChange: (next: NotificationKindFilter) => void
}

export function NotificationsFeed({
  notifications,
  total,
  status,
  kind,
  loading,
  healthy,
  onStatusChange,
  onKindChange,
}: NotificationsFeedProps) {
  const [expandedId, setExpandedId] = useState<string | null>(null)
  return (
    <ZoneCard aria-labelledby="notifications-feed">
      <ZoneCardHeader
        id="notifications-feed"
        title="Outbox"
        subtitle={healthy ? `${total} total` : "unavailable"}
        trailing={
          <div className="flex flex-wrap items-center gap-stack">
            <FilterChipGroup label="Kind">
              {KIND_OPTIONS.map((opt) => (
                <FilterChip
                  key={opt.value}
                  label={opt.label}
                  active={kind === opt.value}
                  onSelect={() => onKindChange(opt.value)}
                />
              ))}
            </FilterChipGroup>
            <FilterChipGroup label="Status">
              {STATUS_OPTIONS.map((opt) => (
                <FilterChip
                  key={opt.value}
                  label={opt.label}
                  active={status === opt.value}
                  onSelect={() => onStatusChange(opt.value)}
                />
              ))}
            </FilterChipGroup>
          </div>
        }
      />
      <TableHeader shown={notifications.length} total={total} />
      {!healthy ? (
        <DegradedRow />
      ) : loading && notifications.length === 0 ? (
        <SkeletonRows />
      ) : notifications.length === 0 ? (
        <EmptyRow />
      ) : (
        <ul role="list" className="flex flex-col">
          {notifications.map((n) => (
            <li key={n.id}>
              <NotificationRow
                notification={n}
                expanded={n.id === expandedId}
                onToggle={(next) => setExpandedId((prev) => (prev === next.id ? null : next.id))}
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
      className={`grid items-center gap-stack border-b bg-card px-row-x py-tile-tight text-eyebrow uppercase tracking-wide text-muted-foreground ${NOTIFICATION_ROW_COLUMNS}`}
    >
      <span aria-hidden />
      <span>Time</span>
      <span>Kind</span>
      <span>Template / Recipient / Error</span>
      <span className="text-right">Adapter</span>
      <span className="text-right">Tries</span>
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
          className={`grid items-center gap-stack border-l-4 border-b border-l-transparent px-row-x py-row-y ${NOTIFICATION_ROW_COLUMNS}`}
        >
          <Skeleton className="size-icon-dot rounded-pill" />
          <Skeleton className="h-3 w-12" />
          <Skeleton className="h-4 w-14" />
          <Skeleton className="h-3 w-full" />
          <Skeleton className="h-3 w-12 justify-self-end" />
          <Skeleton className="h-3 w-8 justify-self-end" />
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
      <p className="text-body text-foreground">No notifications yet.</p>
      <p className="max-w-md text-meta text-muted-foreground">
        Enqueue one with <code className="font-mono">POST /api/v1/notifications</code> or use the
        Send test action above.
      </p>
    </div>
  )
}

function DegradedRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">Outbox unavailable.</p>
      <p className="max-w-md text-meta text-muted-foreground">
        The runtime did not return notifications. Check the Health page for a database probe, then
        retry.
      </p>
    </div>
  )
}

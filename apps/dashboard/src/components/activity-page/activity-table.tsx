// SPDX-License-Identifier: Apache-2.0

"use client"

import { useMemo, useState } from "react"

import { OffsetPager } from "@/components/ui/offset-pager"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import type {
  ActivityPageFilters,
  ActivitySnapshot,
  TenantOption,
} from "@/lib/activity-page/types"
import { ACTIVITY_PAGE_SIZE } from "@/lib/activity-page/types"

import { ACTIVITY_ROW_COLUMNS, ActivityRow } from "./activity-row"

// Activity table — column header stays visible even at zero data per
// the framework's "structure visible even at zero" rule. The empty
// state teaches the integration (POST /api/v1/activity) since this
// surface is only as useful as what customer apps record.

interface ActivityTableProps {
  snapshot: ActivitySnapshot
  tenants: TenantOption[]
  filters: ActivityPageFilters
}

export function ActivityTable({
  snapshot,
  tenants,
  filters,
}: ActivityTableProps) {
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const tenantNames = useMemo(
    () => new Map(tenants.map((t) => [t.id, t.name])),
    [tenants],
  )
  return (
    <ZoneCard>
      <ZoneCardHeader
        title="Activity entries"
        subtitle={`${snapshot.total} recorded`}
      />
      <header
        className={`grid items-center gap-stack border-b px-row-x py-tile-tight text-eyebrow uppercase tracking-wide text-muted-foreground ${ACTIVITY_ROW_COLUMNS}`}
      >
        <span>Time</span>
        <span>Actor</span>
        <span>Action</span>
        <span>Resource</span>
        <span>Tenant</span>
        <span>IP / Agent</span>
        <span aria-hidden />
      </header>
      {!snapshot.healthy ? (
        <DegradedRow />
      ) : snapshot.entries.length === 0 ? (
        <EmptyRow
          filtered={Boolean(
            filters.tenant || filters.action || filters.resourceType,
          )}
        />
      ) : (
        <ul role="list" className="flex flex-col">
          {snapshot.entries.map((entry) => (
            <li key={entry.id}>
              <ActivityRow
                entry={entry}
                tenantName={tenantNames.get(entry.tenant_id)}
                expanded={entry.id === expandedId}
                onToggle={() =>
                  setExpandedId((prev) =>
                    prev === entry.id ? null : entry.id,
                  )
                }
              />
            </li>
          ))}
        </ul>
      )}
      <OffsetPager
        offset={filters.offset}
        pageSize={ACTIVITY_PAGE_SIZE}
        count={snapshot.entries.length}
        total={snapshot.total}
        hasMore={snapshot.hasMore}
      />
    </ZoneCard>
  )
}

function EmptyRow({ filtered }: { filtered: boolean }) {
  if (filtered) {
    return (
      <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
        <p className="text-body text-foreground">
          No activity matches the current filters.
        </p>
        <p className="max-w-md text-meta text-muted-foreground">
          Clear the tenant / action / resource type filters to see the
          full log.
        </p>
      </div>
    )
  }
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">No activity yet.</p>
      <p className="max-w-lg text-meta text-muted-foreground">
        Customer apps record events with{" "}
        <code className="font-mono">
          POST /api/v1/activity{" "}
          {"{action, resource_type, resource_id, metadata}"}
        </code>{" "}
        using their tenant session or API key. Entries land here the
        moment the first app logs one.
      </p>
    </div>
  )
}

function DegradedRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">Activity log unavailable.</p>
      <p className="max-w-md text-meta text-muted-foreground">
        The runtime did not return the activity feed. It may be down, or
        the activity module may not be configured. Check the Health
        page, then retry.
      </p>
    </div>
  )
}

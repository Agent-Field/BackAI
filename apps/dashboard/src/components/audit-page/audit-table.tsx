// SPDX-License-Identifier: Apache-2.0

"use client"

import { useMemo, useState } from "react"

import { OffsetPager } from "@/components/ui/offset-pager"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import type {
  AuditPageFilters,
  AuditSnapshot,
  TenantOption,
} from "@/lib/audit-page/types"
import { AUDIT_PAGE_SIZE } from "@/lib/audit-page/types"

import { AUDIT_ROW_COLUMNS, AuditRow } from "./audit-row"

// Audit table — column header stays visible even at zero data per the
// framework's "structure visible even at zero" rule. Empty + degraded
// states render inside the same card shell. Client component only for
// the expand-metadata toggle; data arrives fully server-fetched.

interface AuditTableProps {
  snapshot: AuditSnapshot
  tenants: TenantOption[]
  filters: AuditPageFilters
}

export function AuditTable({ snapshot, tenants, filters }: AuditTableProps) {
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const tenantNames = useMemo(
    () => new Map(tenants.map((t) => [t.id, t.name])),
    [tenants],
  )
  return (
    <ZoneCard>
      <ZoneCardHeader
        title="Audit entries"
        subtitle={`${snapshot.total} recorded`}
      />
      <header
        className={`grid items-center gap-stack border-b px-row-x py-tile-tight text-eyebrow uppercase tracking-wide text-muted-foreground ${AUDIT_ROW_COLUMNS}`}
      >
        <span>Time</span>
        <span>Action</span>
        <span>Resource</span>
        <span>Tenant</span>
        <span>Actor</span>
        <span aria-hidden />
      </header>
      {!snapshot.healthy ? (
        <DegradedRow />
      ) : snapshot.entries.length === 0 ? (
        <EmptyRow filtered={Boolean(filters.action || filters.tenant)} />
      ) : (
        <ul role="list" className="flex flex-col">
          {snapshot.entries.map((entry) => (
            <li key={entry.id}>
              <AuditRow
                entry={entry}
                tenantName={
                  entry.tenant_id
                    ? tenantNames.get(entry.tenant_id)
                    : undefined
                }
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
        pageSize={AUDIT_PAGE_SIZE}
        count={snapshot.entries.length}
        total={snapshot.total}
        hasMore={snapshot.hasMore}
      />
    </ZoneCard>
  )
}

function EmptyRow({ filtered }: { filtered: boolean }) {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">
        {filtered
          ? "No audit entries match the current filters."
          : "No audit entries yet."}
      </p>
      <p className="max-w-md text-meta text-muted-foreground">
        Control-plane mutations — issuing API keys, updating budgets,
        adding memberships — are recorded here automatically. Perform an
        admin action (e.g. issue a key from a tenant page) and it will
        appear.
      </p>
    </div>
  )
}

function DegradedRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">Audit log unavailable.</p>
      <p className="max-w-md text-meta text-muted-foreground">
        The runtime did not return the audit trail. It may be down, or
        the multi-tenancy module may be disabled. Check the Health page,
        then retry.
      </p>
    </div>
  )
}

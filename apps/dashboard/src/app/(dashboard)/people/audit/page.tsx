// SPDX-License-Identifier: Apache-2.0

import { AuditFilterBar } from "@/components/audit-page/audit-filter-bar"
import { AuditTable } from "@/components/audit-page/audit-table"
import { fetchAuditSnapshot, fetchTenantOptions } from "@/lib/audit-page/data"
import { parseOffset } from "@/lib/audit-page/derive"
import type { AuditPageFilters } from "@/lib/audit-page/types"

// People → Audit log. Server-rendered per navigation: filters +
// pagination live in the URL, so every change re-runs this page with a
// fresh server-side fetch. No polling — audit is a forensic surface,
// not a live feed.

export const dynamic = "force-dynamic"

export default async function AuditPage({
  searchParams,
}: {
  searchParams: Promise<{ action?: string; tenant?: string; offset?: string }>
}) {
  const sp = await searchParams
  const filters: AuditPageFilters = {
    action: sp.action?.trim() ?? "",
    tenant: sp.tenant?.trim() ?? "",
    offset: parseOffset(sp.offset),
  }
  const [snapshot, tenants] = await Promise.all([
    fetchAuditSnapshot(filters),
    fetchTenantOptions(),
  ])
  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <header className="flex items-baseline justify-between">
        <div className="flex flex-col gap-tile-tight">
          <h1 className="text-xl font-semibold tracking-tight text-foreground">
            Audit log
          </h1>
          <p className="text-meta text-muted-foreground">
            Every control-plane mutation, recorded server-side.
          </p>
        </div>
      </header>
      <AuditFilterBar filters={filters} tenants={tenants} />
      <AuditTable snapshot={snapshot} tenants={tenants} filters={filters} />
    </div>
  )
}

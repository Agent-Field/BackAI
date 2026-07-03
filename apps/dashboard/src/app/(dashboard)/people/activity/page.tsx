// SPDX-License-Identifier: Apache-2.0

import { ActivityFilterBar } from "@/components/activity-page/activity-filter-bar"
import { ActivityTable } from "@/components/activity-page/activity-table"
import {
  fetchActivitySnapshot,
  fetchTenantOptions,
} from "@/lib/activity-page/data"
import { parseOffset } from "@/lib/activity-page/derive"
import type { ActivityPageFilters } from "@/lib/activity-page/types"

// People → Activity log. Server-rendered per navigation: filters +
// pagination live in the URL, so every change re-runs this page with a
// fresh server-side fetch. Shows what tenant users / apps did — events
// customer apps record via POST /api/v1/activity — as opposed to the
// operator audit log next door.

export const dynamic = "force-dynamic"

export default async function ActivityPage({
  searchParams,
}: {
  searchParams: Promise<{
    tenant?: string
    action?: string
    resource_type?: string
    offset?: string
  }>
}) {
  const sp = await searchParams
  const filters: ActivityPageFilters = {
    tenant: sp.tenant?.trim() ?? "",
    action: sp.action?.trim() ?? "",
    resourceType: sp.resource_type?.trim() ?? "",
    offset: parseOffset(sp.offset),
  }
  const [snapshot, tenants] = await Promise.all([
    fetchActivitySnapshot(filters),
    fetchTenantOptions(),
  ])
  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <header className="flex items-baseline justify-between">
        <div className="flex flex-col gap-tile-tight">
          <h1 className="text-xl font-semibold tracking-tight text-foreground">
            Activity log
          </h1>
          <p className="text-meta text-muted-foreground">
            What tenant users and apps did — events recorded by customer
            apps via <code className="font-mono">POST /api/v1/activity</code>.
          </p>
        </div>
      </header>
      <ActivityFilterBar filters={filters} tenants={tenants} />
      <ActivityTable snapshot={snapshot} tenants={tenants} filters={filters} />
    </div>
  )
}

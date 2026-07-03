// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"

import type {
  ActivityPageFilters,
  ActivitySnapshot,
  TenantOption,
} from "./types"
import { ACTIVITY_PAGE_SIZE } from "./types"

// Server-side fetch for the Activity page. Failures degrade to
// healthy:false — the table renders a "runtime unavailable" notice
// instead of crashing the page.

export async function fetchActivitySnapshot(
  filters: ActivityPageFilters,
): Promise<ActivitySnapshot> {
  const fetchedAt = new Date().toISOString()
  const result = await api.admin.activity
    .list({
      tenant: filters.tenant || undefined,
      action: filters.action || undefined,
      resource_type: filters.resourceType || undefined,
      limit: ACTIVITY_PAGE_SIZE,
      offset: filters.offset,
    })
    .catch(() => null)

  if (result === null) {
    return {
      entries: [],
      total: 0,
      hasMore: false,
      fetchedAt,
      healthy: false,
    }
  }

  return {
    entries: result.entries,
    total: result.total,
    hasMore: result.has_more,
    fetchedAt,
    healthy: true,
  }
}

// Tenant id → name options for the filter chips + tenant column.
// Degrades to an empty list — the page then shows short tenant ids,
// which is still useful.
export async function fetchTenantOptions(): Promise<TenantOption[]> {
  const list = await api.admin.tenants.list().catch(() => null)
  return (list?.tenants ?? []).map((t) => ({
    id: t.id,
    name: t.name || t.slug,
  }))
}

// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"

import type { AuditPageFilters, AuditSnapshot, TenantOption } from "./types"
import { AUDIT_PAGE_SIZE } from "./types"

// Server-side fetch for the Audit page. Failures degrade to
// healthy:false — the table renders a "runtime unavailable" notice
// instead of crashing the page. Note the admin audit endpoint is gated
// on the multi-tenancy module (503 MT_DISABLED when off), which lands
// in the same degraded state.

export async function fetchAuditSnapshot(
  filters: AuditPageFilters,
): Promise<AuditSnapshot> {
  const fetchedAt = new Date().toISOString()
  const result = await api.admin.audit
    .list({
      tenant: filters.tenant || undefined,
      action: filters.action || undefined,
      limit: AUDIT_PAGE_SIZE,
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

// SPDX-License-Identifier: Apache-2.0

import type { ActivityEntry } from "@/lib/api"

// People → Activity log. Operator cross-tenant view of
// suite_user_activity — product events that customer apps record via
// POST /api/v1/activity (distinct from the operator audit log, which
// tracks control-plane mutations).

export const ACTIVITY_PAGE_SIZE = 50

// URL-driven filters. `offset` pages through results server-side.
export interface ActivityPageFilters {
  tenant: string
  action: string
  resourceType: string
  offset: number
}

export interface ActivitySnapshot {
  entries: ActivityEntry[]
  total: number
  hasMore: boolean
  fetchedAt: string
  healthy: boolean
}

// tenant_id → friendly-name option for the tenant filter chips and the
// tenant column. Resolved once server-side from api.admin.tenants.list.
export interface TenantOption {
  id: string
  name: string
}

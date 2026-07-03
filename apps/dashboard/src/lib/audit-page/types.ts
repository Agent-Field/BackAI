// SPDX-License-Identifier: Apache-2.0

import type { AuditEntry } from "@/lib/api"

// People → Audit log. Operator-facing view of suite_audit_log — every
// control-plane mutation (api_key.create, budget.update,
// membership.add, …) recorded server-side by the runtime.

export const AUDIT_PAGE_SIZE = 50

// URL-driven filters. `offset` pages through results server-side.
export interface AuditPageFilters {
  action: string
  tenant: string
  offset: number
}

export interface AuditSnapshot {
  entries: AuditEntry[]
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

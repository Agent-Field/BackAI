// SPDX-License-Identifier: Apache-2.0

// suite.admin.audit.* — audit log search.
//
// Endpoint (mirroring `apps/dashboard/src/lib/api.ts`):
//
//   GET /api/v1/admin/audit?tenant=&action=&from=&to=&limit=&offset=

import { request, toCamel, type HttpOptions } from "../_http.js"
import { AuditListSchema, type AuditList } from "./_models.js"

export interface ListAuditOptions extends HttpOptions {
  tenant?: string
  action?: string
  /** RFC3339 timestamp or Date. */
  from?: string | Date
  /** RFC3339 timestamp or Date. */
  to?: string | Date
  limit?: number
  offset?: number
}

/** List audit log entries with optional filters. */
export async function list(opts: ListAuditOptions = {}): Promise<AuditList> {
  const {
    tenant,
    action,
    from,
    to,
    limit = 100,
    offset = 0,
    ...http
  } = opts
  if (limit <= 0) throw new Error("limit must be a positive integer")
  if (offset < 0) throw new Error("offset must be non-negative")
  const qs = new URLSearchParams()
  if (tenant !== undefined && tenant !== "") qs.set("tenant", tenant)
  if (action !== undefined && action !== "") qs.set("action", action)
  if (from !== undefined) qs.set("from", toIso(from))
  if (to !== undefined) qs.set("to", toIso(to))
  qs.set("limit", String(limit))
  qs.set("offset", String(offset))
  const raw = await request<unknown>("GET", `/admin/audit?${qs.toString()}`, null, http)
  return AuditListSchema.parse(toCamel(raw))
}

function toIso(value: string | Date): string {
  return value instanceof Date ? value.toISOString() : value
}

/** Namespace object — the shape `suite.admin.audit` is built from. */
export const audit = {
  list,
} as const
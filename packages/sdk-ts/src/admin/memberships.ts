// suite.admin.memberships.* — tenant<->user role mapping.
//
// Endpoints (mirroring `apps/dashboard/src/lib/api.ts`):
//
//   GET    /api/v1/admin/memberships?tenant=&user=
//   POST   /api/v1/admin/memberships          { tenant_id, user_id, role }
//   DELETE /api/v1/admin/memberships/{tenantId}/{userId}

import { request, toCamel, type HttpOptions } from "../_http.js"
import {
  MembershipListSchema,
  MembershipSchema,
  type Membership,
  type MembershipList,
  type Role,
} from "./_models.js"

export interface ListMembershipsOptions extends HttpOptions {
  tenant?: string
  user?: string
}

/** List memberships, optionally filtered by tenant id and/or user id. */
export async function list(
  opts: ListMembershipsOptions = {},
): Promise<MembershipList> {
  const { tenant, user, ...http } = opts
  const qs = new URLSearchParams()
  if (tenant !== undefined && tenant !== "") qs.set("tenant", tenant)
  if (user !== undefined && user !== "") qs.set("user", user)
  const query = qs.toString()
  const path =
    query.length > 0 ? `/admin/memberships?${query}` : "/admin/memberships"
  const raw = await request<unknown>("GET", path, null, http)
  return MembershipListSchema.parse(toCamel(raw))
}

/** Add (or upsert) a tenant member. */
export async function add(
  tenantId: string,
  userId: string,
  role: Role | string,
  opts: HttpOptions = {},
): Promise<Membership> {
  if (typeof tenantId !== "string" || tenantId.length === 0) {
    throw new Error("tenantId must be a non-empty string")
  }
  if (typeof userId !== "string" || userId.length === 0) {
    throw new Error("userId must be a non-empty string")
  }
  if (typeof role !== "string" || role.length === 0) {
    throw new Error("role must be a non-empty string")
  }
  // Body uses snake_case to match the dashboard contract verbatim.
  const raw = await request<unknown>(
    "POST",
    "/admin/memberships",
    { tenant_id: tenantId, user_id: userId, role },
    opts,
  )
  return MembershipSchema.parse(toCamel(raw))
}

/** Remove a membership. Returns `true` on success. */
export async function remove(
  tenantId: string,
  userId: string,
  opts: HttpOptions = {},
): Promise<boolean> {
  if (typeof tenantId !== "string" || tenantId.length === 0) {
    throw new Error("tenantId must be a non-empty string")
  }
  if (typeof userId !== "string" || userId.length === 0) {
    throw new Error("userId must be a non-empty string")
  }
  const raw = await request<{ deleted?: boolean } | null>(
    "DELETE",
    `/admin/memberships/${encodeURIComponent(tenantId)}/${encodeURIComponent(userId)}`,
    null,
    opts,
  )
  if (raw === null || raw === undefined) return true
  return Boolean(raw.deleted ?? true)
}

/** Namespace object — the shape `suite.admin.memberships` is built from. */
export const memberships = {
  list,
  add,
  remove,
} as const

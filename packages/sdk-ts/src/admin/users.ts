// suite.admin.users.* — user lookup.
//
// The admin user surface in v1 is read-only — provisioning happens via
// the dashboard's identity flow. Endpoint shape matches
// `apps/dashboard/src/lib/api.ts`:
//
//   GET /api/v1/admin/users?tenant=&search=

import { request, toCamel, type HttpOptions } from "../_http.js"
import { UserListSchema, type UserList } from "./_models.js"

export interface ListUsersOptions extends HttpOptions {
  /** Restrict results to members of this tenant id. */
  tenant?: string
  /** Case-insensitive substring against email and name. */
  search?: string
}

/** List users, optionally restricted to a tenant and/or matching `search`. */
export async function list(opts: ListUsersOptions = {}): Promise<UserList> {
  const { tenant, search, ...http } = opts
  const qs = new URLSearchParams()
  if (tenant !== undefined && tenant !== "") qs.set("tenant", tenant)
  if (search !== undefined && search !== "") qs.set("search", search)
  const query = qs.toString()
  const path = query.length > 0 ? `/admin/users?${query}` : "/admin/users"
  const raw = await request<unknown>("GET", path, null, http)
  return UserListSchema.parse(toCamel(raw))
}

/** Namespace object — the shape `suite.admin.users` is built from. */
export const users = {
  list,
} as const

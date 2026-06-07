// suite.admin.tenants.* — tenant CRUD.
//
// Endpoint paths and JSON shapes are the canonical contract from
// `apps/dashboard/src/lib/api.ts` (Phase 6 Tenancy section). The wire
// format is snake_case; the SDK exposes camelCase via the helpers in
// `_http.ts`.
//
// All endpoints are gated by the runtime's `multi-tenancy` module flag —
// calls against a disabled runtime surface as `SuiteError` with
// `code === "MT_DISABLED"`.

import { request, toCamel, toSnake, type HttpOptions } from "../_http.js"
import {
  TenantListSchema,
  TenantSchema,
  type CreateTenantInput,
  type Tenant,
  type TenantDetail,
  type TenantList,
  type UpdateTenantInput,
} from "./_models.js"
import { TenantDetailSchema } from "./_models.js"

/** List all non-soft-deleted tenants. */
export async function list(opts: HttpOptions = {}): Promise<TenantList> {
  const raw = await request<unknown>("GET", "/admin/tenants", null, opts)
  return TenantListSchema.parse(toCamel(raw))
}

/** Fetch a tenant's joined detail view (tenant + members + keys + usage). */
export async function get(
  id: string,
  opts: HttpOptions = {},
): Promise<TenantDetail> {
  if (typeof id !== "string" || id.length === 0) {
    throw new Error("tenant id must be a non-empty string")
  }
  const raw = await request<unknown>(
    "GET",
    `/admin/tenants/${encodeURIComponent(id)}`,
    null,
    opts,
  )
  return TenantDetailSchema.parse(toCamel(raw))
}

/** Create a tenant. */
export async function create(
  input: CreateTenantInput,
  opts: HttpOptions = {},
): Promise<Tenant> {
  if (typeof input.slug !== "string" || input.slug.length === 0) {
    throw new Error("input.slug must be a non-empty string")
  }
  if (typeof input.name !== "string" || input.name.length === 0) {
    throw new Error("input.name must be a non-empty string")
  }
  const raw = await request<unknown>(
    "POST",
    "/admin/tenants",
    toSnake(input),
    opts,
  )
  return TenantSchema.parse(toCamel(raw))
}

/** Update selected tenant fields. */
export async function update(
  id: string,
  input: UpdateTenantInput,
  opts: HttpOptions = {},
): Promise<Tenant> {
  if (typeof id !== "string" || id.length === 0) {
    throw new Error("tenant id must be a non-empty string")
  }
  const raw = await request<unknown>(
    "PATCH",
    `/admin/tenants/${encodeURIComponent(id)}`,
    toSnake(input),
    opts,
  )
  return TenantSchema.parse(toCamel(raw))
}

/** Soft-delete a tenant. Returns `true` on success. */
async function deleteTenant(
  id: string,
  opts: HttpOptions = {},
): Promise<boolean> {
  if (typeof id !== "string" || id.length === 0) {
    throw new Error("tenant id must be a non-empty string")
  }
  const raw = await request<{ deleted?: boolean } | null>(
    "DELETE",
    `/admin/tenants/${encodeURIComponent(id)}`,
    null,
    opts,
  )
  if (raw === null || raw === undefined) return true
  return Boolean(raw.deleted ?? true)
}
// `delete` is a reserved word in some import positions, so we re-export
// under both names for ergonomics.
export { deleteTenant as delete }

/** Namespace object — the shape `suite.admin.tenants` is built from. */
export const tenants = {
  list,
  get,
  create,
  update,
  delete: deleteTenant,
} as const

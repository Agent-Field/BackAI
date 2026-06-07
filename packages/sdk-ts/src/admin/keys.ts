// suite.admin.keys.* — per-tenant API key management.
//
// Endpoints (mirroring `apps/dashboard/src/lib/api.ts`):
//
//   GET    /api/v1/admin/keys?tenant=
//   POST   /api/v1/admin/keys
//   DELETE /api/v1/admin/keys/{id}
//
// IMPORTANT: `issue()` is the ONLY code path that returns the plaintext
// secret. The runtime stores a bcrypt hash; subsequent `list` calls
// expose the prefix only. Persist `IssuedAPIKey.value` immediately —
// there is no recovery.

import { request, toCamel, toSnake, type HttpOptions } from "../_http.js"
import {
  APIKeyListSchema,
  IssuedAPIKeySchema,
  type APIKeyList,
  type IssueAPIKeyInput,
  type IssuedAPIKey,
} from "./_models.js"

export interface ListKeysOptions extends HttpOptions {
  tenant?: string
}

/** List API keys, optionally scoped to a tenant id. */
export async function list(opts: ListKeysOptions = {}): Promise<APIKeyList> {
  const { tenant, ...http } = opts
  const qs = new URLSearchParams()
  if (tenant !== undefined && tenant !== "") qs.set("tenant", tenant)
  const query = qs.toString()
  const path = query.length > 0 ? `/admin/keys?${query}` : "/admin/keys"
  const raw = await request<unknown>("GET", path, null, http)
  return APIKeyListSchema.parse(toCamel(raw))
}

/** Issue a new API key and return the one-time-reveal record. */
export async function issue(
  input: IssueAPIKeyInput,
  opts: HttpOptions = {},
): Promise<IssuedAPIKey> {
  if (typeof input.tenantId !== "string" || input.tenantId.length === 0) {
    throw new Error("input.tenantId must be a non-empty string")
  }
  // Default scopes to [] so the wire body always carries the field even
  // if the caller omits it (matches the dashboard zod default).
  const payload = { ...input, scopes: input.scopes ?? [] }
  const raw = await request<unknown>(
    "POST",
    "/admin/keys",
    toSnake(payload),
    opts,
  )
  return IssuedAPIKeySchema.parse(toCamel(raw))
}

/** Revoke an API key. Returns `true` on success. */
export async function revoke(
  id: string,
  opts: HttpOptions = {},
): Promise<boolean> {
  if (typeof id !== "string" || id.length === 0) {
    throw new Error("key id must be a non-empty string")
  }
  const raw = await request<{ revoked?: boolean } | null>(
    "DELETE",
    `/admin/keys/${encodeURIComponent(id)}`,
    null,
    opts,
  )
  if (raw === null || raw === undefined) return true
  return Boolean(raw.revoked ?? true)
}

/** Namespace object — the shape `suite.admin.keys` is built from. */
export const keys = {
  list,
  issue,
  revoke,
} as const

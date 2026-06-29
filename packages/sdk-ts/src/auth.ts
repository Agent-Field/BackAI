// SPDX-License-Identifier: Apache-2.0

// suite.auth.* — identity introspection for the current credential.
//
// Auth lifecycle (sign-up / sign-in / reset) is owned by the app layer
// (better-auth in the customer-app). The SDK exposes the read-only "who am
// I": which tenant / user / key the current credential resolves to.
//
//   GET /api/v1/auth/whoami -> { authenticated, tenant_id, user_id, api_key_id }

import { request, toCamel, type HttpOptions } from "./_http.js"
import { z } from "zod"

const WhoAmISchema = z.object({
  authenticated: z.boolean(),
  tenantId: z.string(),
  userId: z.string(),
  apiKeyId: z.string(),
})
export type WhoAmI = z.infer<typeof WhoAmISchema>

/** Resolve the tenant / user / key the current credential maps to.
 *
 * `authenticated` is false (and the ids empty) for an unauthenticated call. */
export async function whoami(opts: HttpOptions = {}): Promise<WhoAmI> {
  const raw = await request<unknown>("GET", "/auth/whoami", null, opts)
  return WhoAmISchema.parse(toCamel(raw))
}

/** Namespace object — the shape `suite.auth` is built from. */
export const auth = {
  whoami,
} as const

// suite.secrets.* — per-tenant secret reads.
//
// The main SDK only exposes `get` (reveal the plaintext value). Admin
// verbs (list/set/delete/rotate) move to a separate `@af-stack/sdk-admin`
// package post-v1.
//
// Endpoint comes from `apps/dashboard/src/lib/api.ts`:
//   POST /api/v1/secrets/{key}/reveal -> { key, value }

import { z } from "zod"
import { request, type HttpOptions } from "./_http.js"

// TODO(admin-sdk): list/set/delete/rotate live in `@af-stack/sdk-admin`
// once that package ships. Keep them out of this module so accidental
// writes from app code are impossible.

const SecretValueSchema = z.object({
  key: z.string(),
  value: z.string(),
})

/** Return the plaintext value of a secret. */
export async function get(key: string, opts: HttpOptions = {}): Promise<string> {
  if (typeof key !== "string" || key.length === 0) {
    throw new Error("secret key must be a non-empty string")
  }
  const raw = await request<unknown>(
    "POST",
    `/secrets/${encodeURIComponent(key)}/reveal`,
    null,
    opts,
  )
  return SecretValueSchema.parse(raw).value
}

/** Namespace object — the shape `suite.secrets` is built from. */
export const secrets = {
  get,
} as const

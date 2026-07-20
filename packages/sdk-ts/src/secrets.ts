// SPDX-License-Identifier: Apache-2.0

// suite.secrets.* — per-tenant secret reads.
//
// The main SDK exposes `get` (reveal the plaintext value). It targets the
// tenant-principal vault surface, which is authorized by the caller's own
// API key (or session) — no operator privileges required:
//   POST /api/v1/vault/secrets/{key}/reveal -> { key, value }
//
// The runtime keeps a separate operator-gated surface at /api/v1/secrets
// for the dashboard; the SDK never touches it.

import { z } from "zod"
import { request, type HttpOptions } from "./_http.js"

const SecretValueSchema = z.object({
  key: z.string(),
  value: z.string(),
})

/** Return the plaintext value of a secret (tenant-scoped, audited). */
export async function get(key: string, opts: HttpOptions = {}): Promise<string> {
  if (typeof key !== "string" || key.length === 0) {
    throw new Error("secret key must be a non-empty string")
  }
  const raw = await request<unknown>(
    "POST",
    `/vault/secrets/${encodeURIComponent(key)}/reveal`,
    null,
    opts,
  )
  return SecretValueSchema.parse(raw).value
}

/** Namespace object — the shape `suite.secrets` is built from. */
export const secrets = {
  get,
} as const

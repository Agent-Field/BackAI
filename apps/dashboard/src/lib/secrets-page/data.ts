// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"
import type { SecretMetadata } from "@/lib/api"

import type { SecretsSnapshot } from "./types"

// Server-side initial fetch for /platform/secrets. The vault is
// single-tenant today (the runtime pins the default tenant), so the list
// takes no filter. A failed list degrades the page to a notice rather
// than throwing — the runtime returns 503 SECRETS_NOT_CONFIGURED when the
// vault is not wired up.

export async function fetchSecretsSnapshot(): Promise<SecretsSnapshot> {
  const fetchedAt = new Date().toISOString()

  const result = await api.secrets.list().catch(() => null)
  const secrets: SecretMetadata[] = result ? result.secrets : []

  return {
    secrets,
    fetchedAt,
    healthy: result !== null,
  }
}

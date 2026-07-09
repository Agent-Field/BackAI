// SPDX-License-Identifier: Apache-2.0

import type { SecretMetadata } from "@/lib/api"

// Client-safe types for the secrets surface. The server snapshot is
// produced by ./data.ts (server-only); the shell keeps the same shape
// alive across client refreshes after set / rotate / delete mutations.

export interface SecretsSnapshot {
  secrets: SecretMetadata[]
  fetchedAt: string
  // false when the vault list is unreachable (e.g. the runtime returned
  // SECRETS_NOT_CONFIGURED, or the DB probe is down) — the table renders a
  // degraded notice instead of an empty state.
  healthy: boolean
}

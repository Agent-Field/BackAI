// SPDX-License-Identifier: Apache-2.0

import type { APIKey, Tenant } from "@/lib/api"

// Client-safe types for the API keys surface. The server snapshot is
// produced by ./data.ts (server-only); the shell keeps the same shape
// alive across client refreshes.

export interface KeysFilters {
  /** Tenant id filter (?tenant=). Empty string = all tenants. */
  tenant: string
}

export const DEFAULT_KEYS_FILTERS: KeysFilters = {
  tenant: "",
}

export interface KeysSnapshot {
  keys: APIKey[]
  tenants: Tenant[]
  fetchedAt: string
  healthy: boolean
}

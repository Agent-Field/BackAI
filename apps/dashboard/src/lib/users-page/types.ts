// SPDX-License-Identifier: Apache-2.0

import type { Tenant, User } from "@/lib/api"

// Client-safe types for the Users surface. The server snapshot is
// produced by ./data.ts (server-only); the shell keeps the same shape
// alive across client refreshes.

export interface UsersFilters {
  /** Substring search over email / name, forwarded as ?search=. */
  search: string
  /** Tenant id filter (?tenant=). Empty string = all tenants. */
  tenant: string
}

export const DEFAULT_USERS_FILTERS: UsersFilters = {
  search: "",
  tenant: "",
}

export interface UsersSnapshot {
  users: User[]
  tenants: Tenant[]
  fetchedAt: string
  healthy: boolean
}

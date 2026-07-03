// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"
import type { Tenant, User } from "@/lib/api"

import type { UsersFilters, UsersSnapshot } from "./types"

// Server-side initial fetch for /people/users. Users and tenants
// tolerate failure independently — the page mounts with a degraded
// notice when the users list is unreachable, and the tenant filter
// simply renders empty when the tenants call fails.

export async function fetchUsersSnapshot(
  filters: Partial<UsersFilters> = {},
): Promise<UsersSnapshot> {
  const fetchedAt = new Date().toISOString()

  const [usersResult, tenantsResult] = await Promise.allSettled([
    api.admin.users.list({
      tenant: filters.tenant || undefined,
      search: filters.search?.trim() || undefined,
    }),
    api.admin.tenants.list(),
  ])

  const users: User[] =
    usersResult.status === "fulfilled" ? usersResult.value.users : []
  const tenants: Tenant[] =
    tenantsResult.status === "fulfilled" ? tenantsResult.value.tenants : []

  return {
    users,
    tenants,
    fetchedAt,
    healthy: usersResult.status === "fulfilled",
  }
}

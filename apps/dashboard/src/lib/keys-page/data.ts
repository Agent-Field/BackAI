// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"
import type { APIKey, Tenant } from "@/lib/api"

import type { KeysSnapshot } from "./types"

// Server-side initial fetch for /people/keys. Keys and tenants tolerate
// failure independently — the page mounts with a degraded notice when
// the keys list is unreachable, and the tenant lookup simply degrades
// to short ids when the tenants call fails.

export async function fetchKeysSnapshot(tenant?: string): Promise<KeysSnapshot> {
  const fetchedAt = new Date().toISOString()

  const [keysResult, tenantsResult] = await Promise.allSettled([
    api.admin.keys.list(tenant ? { tenant } : undefined),
    api.admin.tenants.list(),
  ])

  const keys: APIKey[] =
    keysResult.status === "fulfilled" ? keysResult.value.keys : []
  const tenants: Tenant[] =
    tenantsResult.status === "fulfilled" ? tenantsResult.value.tenants : []

  return {
    keys,
    tenants,
    fetchedAt,
    healthy: keysResult.status === "fulfilled",
  }
}

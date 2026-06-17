// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"
import type { Budget, CacheStats, CostSummary, Tenant } from "@/lib/api"

import { rangeFromKind } from "./range"
import type { CostRangeKind, CostSnapshot } from "./types"

// Server-side parallel fetch. Each source tolerates failure independently
// — the page mounts even if budgets or cache returned an error, and the
// client tick recovers the missing piece on the next round.
//
// Per-tenant secondary calls happen client-side after the first paint so
// the initial response stays fast; the server snapshot only contains the
// platform-wide summary + budgets + cache.

export async function fetchCostSnapshot(
  rangeKind: CostRangeKind = "30d",
): Promise<CostSnapshot> {
  const range = rangeFromKind(rangeKind)
  const fetchedAt = new Date().toISOString()

  const [primaryResult, budgetsResult, cacheResult, tenantsResult] =
    await Promise.allSettled([
      api.cost({ from: range.from, to: range.to }),
      api.budgets.list(),
      api.llm.cacheStats(),
      api.admin.tenants.list(),
    ])

  const primary: CostSummary | null =
    primaryResult.status === "fulfilled" ? primaryResult.value : null
  const budgets: Budget[] =
    budgetsResult.status === "fulfilled"
      ? budgetsResult.value.budgets
      : []
  const cache: CacheStats | null =
    cacheResult.status === "fulfilled" ? cacheResult.value : null
  const tenants: Tenant[] =
    tenantsResult.status === "fulfilled" ? tenantsResult.value.tenants : []

  return {
    primary,
    byTenant: {},
    budgets,
    cache,
    tenants,
    range,
    fetchedAt,
    primaryHealthy: primaryResult.status === "fulfilled",
    budgetsHealthy: budgetsResult.status === "fulfilled",
    cacheHealthy: cacheResult.status === "fulfilled",
  }
}

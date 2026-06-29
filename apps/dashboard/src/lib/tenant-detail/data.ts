// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"

import type { TenantDetailSnapshot } from "./types"

// One fetch hydrates the entire page header + Overview + Members + Keys
// + Settings tabs. Tab-specific deeper data (Runs, Cost, Errors) is
// loaded by the upstream pages when the operator drills out.

export async function fetchTenantDetailSnapshot(
  tenantId: string,
): Promise<TenantDetailSnapshot | null> {
  try {
    const drilldown = await api.tenantDrilldown(tenantId)
    return {
      drilldown,
      fetchedAt: new Date().toISOString(),
    }
  } catch {
    return null
  }
}

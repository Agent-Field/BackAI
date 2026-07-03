// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"

import type { ErrorStatusFilter, ErrorsSnapshot } from "./types"

// Server-side initial fetch for the Errors page. Capabilities ride
// along so the first paint already knows which row actions to render
// and whether to show the volatility note. Failures degrade to
// healthy:false — the client polling tick recovers on the next round.

export async function fetchErrorsSnapshot(
  status: ErrorStatusFilter = "open",
): Promise<ErrorsSnapshot> {
  const fetchedAt = new Date().toISOString()
  const [list, capabilities] = await Promise.all([
    api.errors.list({ status, limit: 100 }).catch(() => null),
    api.errors.capabilities().catch(() => null),
  ])

  if (list === null) {
    return {
      groups: [],
      total: 0,
      capabilities,
      fetchedAt,
      healthy: false,
    }
  }

  return {
    groups: list.groups,
    total: list.total ?? list.groups.length,
    capabilities,
    fetchedAt,
    healthy: true,
  }
}

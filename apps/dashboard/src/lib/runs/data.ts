// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"

import type { RunsSnapshot, RunStatusFilter } from "./types"

// Server-side initial fetch. The runs endpoint is one of the few that
// returns a quasi-stable shape across releases, so the snapshot is
// straight from the API. Failures degrade silently — the client tick
// recovers on the next round.

export async function fetchRunsSnapshot(
  status: RunStatusFilter = "all",
): Promise<RunsSnapshot> {
  const fetchedAt = new Date().toISOString()
  // Server-side filter for status only when the operator picked a single
  // shipped enum value the backend understands. Backend v1 accepts
  // succeeded/failed/etc. as a flat filter; "all" plus client-side
  // filters cover the rest until Gaps 27-31 land.
  const result = await api
    .runs({
      limit: 200,
      status: status === "all" ? undefined : status,
    })
    .catch(() => null)

  if (result === null) {
    return {
      runs: [],
      total: 0,
      hasMore: false,
      fetchedAt,
      healthy: false,
    }
  }

  return {
    runs: result.runs,
    total: result.total,
    hasMore: result.has_more,
    fetchedAt,
    healthy: true,
  }
}

// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"

import { sortNewestFirst } from "./derive"
import type { LogsFilters, LogsSnapshot } from "./types"

// Server-side initial fetch for the Logs page. Filters are applied by
// the runtime's log store; we only re-order newest-first. Failures
// degrade to healthy:false — the client polling tick recovers on the
// next round.

export async function fetchLogsSnapshot(
  filters: LogsFilters,
): Promise<LogsSnapshot> {
  const fetchedAt = new Date().toISOString()
  const result = await api
    .logs({
      level: filters.level === "all" ? undefined : filters.level,
      service: filters.service.trim() || undefined,
      search: filters.search.trim() || undefined,
      limit: 200,
    })
    .catch(() => null)

  if (result === null) {
    return { logs: [], fetchedAt, healthy: false }
  }

  return {
    logs: sortNewestFirst(result.logs),
    fetchedAt,
    healthy: true,
  }
}

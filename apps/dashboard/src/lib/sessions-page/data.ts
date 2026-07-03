// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"

import type { SessionsFilters, SessionsSnapshot } from "./types"

// Server-side initial fetch for /people/sessions. Failures degrade to
// healthy:false — the page mounts with a degraded notice and the client
// shell recovers on the next manual refresh.

export async function fetchSessionsSnapshot(
  filters: Partial<SessionsFilters> = {},
): Promise<SessionsSnapshot> {
  const fetchedAt = new Date().toISOString()
  const result = await api.admin.sessions
    .list({
      email: filters.email?.trim() || undefined,
      include_expired: filters.includeExpired || undefined,
      limit: 200,
    })
    .catch(() => null)

  if (result === null) {
    return {
      sessions: [],
      total: 0,
      hasMore: false,
      fetchedAt,
      healthy: false,
    }
  }

  return {
    sessions: result.sessions,
    total: result.total,
    hasMore: result.has_more,
    fetchedAt,
    healthy: true,
  }
}

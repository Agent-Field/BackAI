// SPDX-License-Identifier: Apache-2.0

import type { SessionInfo } from "@/lib/api"

// Client-safe types for the Sessions surface. The server snapshot is
// produced by ./data.ts (server-only); the shell keeps the same shape
// alive across client refreshes.

export interface SessionsFilters {
  /** Substring email filter, forwarded to the runtime as ?email=. */
  email: string
  /** Include already-expired sessions in the list. */
  includeExpired: boolean
}

export const DEFAULT_SESSIONS_FILTERS: SessionsFilters = {
  email: "",
  includeExpired: false,
}

export interface SessionsSnapshot {
  sessions: SessionInfo[]
  total: number
  hasMore: boolean
  fetchedAt: string
  healthy: boolean
}

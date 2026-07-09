// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"

import type { CacheSnapshot } from "./types"

// Server-side fetch. The cache page has a single source, but we still
// tolerate its failure independently — the page mounts (with the empty
// state) even when the gateway returned an error, and the client tick
// recovers on the next round.

export async function fetchCacheSnapshot(): Promise<CacheSnapshot> {
  const fetchedAt = new Date().toISOString()
  const [stats] = await Promise.allSettled([api.llm.cacheStats()])

  return {
    stats: stats.status === "fulfilled" ? stats.value : null,
    fetchedAt,
    statsHealthy: stats.status === "fulfilled",
  }
}

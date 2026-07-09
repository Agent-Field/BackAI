// SPDX-License-Identifier: Apache-2.0

import type { CacheStats } from "@/lib/api"

// Server-rendered snapshot for the LLM-gateway cache page. The single
// source (cache stats) degrades on its own — a stale-but-mounted page
// beats a blank one, and the client polling tick recovers the missing
// piece on the next round.

export interface CacheSnapshot {
  stats: CacheStats | null
  fetchedAt: string
  statsHealthy: boolean
}

// Everything the widgets read, pre-derived once so the tiles, donut and
// breakdown bar all agree on the same normalised numbers.
export interface CacheSummary {
  // hit rate as a 0..1 fraction, reconciled against hits/total so the
  // donut never disagrees with the tiles even if the backend ships the
  // rate in a different unit.
  ratio: number
  ratioPct: number
  totalCalls: number
  hits: number
  misses: number
  savingsUsd: number
  entries: number
  // average dollars saved per served cache hit — the "each hit is worth
  // this much" read that makes the gateway's value legible.
  savingsPerHit: number
  // true once anything has flowed through the cache (calls or stored
  // entries); drives the empty-state copy.
  hasData: boolean
}

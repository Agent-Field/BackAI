// SPDX-License-Identifier: Apache-2.0

import type { CacheStats } from "@/lib/api"

import type { CacheSnapshot, CacheSummary } from "./types"

// Summary ------------------------------------------------------------------

// Fold the raw CacheStats into the numbers every widget shares. All
// normalisation happens here so the donut, tiles and breakdown bar can't
// drift apart.
export function summarise(snapshot: CacheSnapshot): CacheSummary {
  const s = snapshot.stats
  if (s === null) {
    return {
      ratio: 0,
      ratioPct: 0,
      totalCalls: 0,
      hits: 0,
      misses: 0,
      savingsUsd: 0,
      entries: 0,
      savingsPerHit: 0,
      hasData: false,
    }
  }
  const hits = Math.max(0, s.cache_hits)
  const misses = Math.max(0, s.cache_misses)
  const totalCalls = Math.max(0, s.total_calls)
  const ratio = normaliseRate(s, hits, totalCalls)
  return {
    ratio,
    ratioPct: Math.round(ratio * 100),
    totalCalls,
    hits,
    misses,
    savingsUsd: Math.max(0, s.savings_usd),
    entries: Math.max(0, s.entries),
    savingsPerHit: hits > 0 ? Math.max(0, s.savings_usd) / hits : 0,
    hasData: totalCalls > 0 || hits > 0 || Math.max(0, s.entries) > 0,
  }
}

// The backend ships `hit_rate` as a 0..1 fraction, but different gateway
// builds have shipped it as a 0..100 percentage. Normalise defensively,
// and fall back to hits/total when the field is missing or nonsensical so
// the donut always matches the tile counts.
function normaliseRate(s: CacheStats, hits: number, totalCalls: number): number {
  const raw = s.hit_rate
  let ratio: number
  if (Number.isFinite(raw) && raw > 0) {
    ratio = raw > 1 ? raw / 100 : raw
  } else if (totalCalls > 0) {
    ratio = hits / totalCalls
  } else {
    ratio = 0
  }
  return Math.max(0, Math.min(1, ratio))
}

// Formatting ---------------------------------------------------------------

export function formatInt(n: number): string {
  if (!Number.isFinite(n)) return "—"
  return Math.round(n).toLocaleString()
}

// Matches the Cost page's currency convention: sub-dollar values keep
// four decimals (cache savings are often fractions of a cent per hit),
// mid values two, large values round to whole dollars.
export function formatUsd(n: number): string {
  if (!Number.isFinite(n)) return "—"
  if (n === 0) return "$0"
  if (Math.abs(n) < 1) return `$${n.toFixed(4)}`
  if (Math.abs(n) < 100) return `$${n.toFixed(2)}`
  return `$${Math.round(n).toLocaleString()}`
}

export function formatPct(fraction: number): string {
  if (!Number.isFinite(fraction)) return "—"
  const pct = fraction * 100
  // Keep a decimal below 10% so a barely-warming cache doesn't read as a
  // flat integer, but whole numbers above that stay clean.
  if (pct > 0 && pct < 10) return `${pct.toFixed(1)}%`
  return `${Math.round(pct)}%`
}

// Single timestamp convention, mirroring the Health page (C5).
export function formatRelative(iso: string | null): string {
  if (!iso) return "—"
  const ts = Date.parse(iso)
  if (Number.isNaN(ts)) return iso
  const diffMs = Math.max(0, Date.now() - ts)
  if (diffMs < 1000) return "now"
  const sec = Math.floor(diffMs / 1000)
  if (sec < 60) return `${sec}s ago`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  if (sec < 86_400) return `${Math.floor(sec / 3600)}h ago`
  return new Date(ts).toISOString().slice(5, 10)
}

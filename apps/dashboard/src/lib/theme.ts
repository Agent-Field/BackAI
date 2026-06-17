// SPDX-License-Identifier: Apache-2.0

// Single source of truth for design-system values that JSX consumes
// outside of Tailwind classes (chart heights, animation timings, status
// keys, etc.). All visual tokens live in `app/globals.css` under
// `@theme inline`; this file mirrors the non-className subset for TS.
//
// Rules:
//   1. No hardcoded pixel / color / duration values in components — import
//      from here instead.
//   2. If a value belongs in JSX as a className (padding, gap, radius,
//      color, font-size), declare it in `globals.css` and use the Tailwind
//      utility — not this file.
//   3. If a value needs to flow into JS APIs (Recharts props, setTimeout,
//      framer-motion configs), declare it here and re-import.

/** Animation timings (ms). Mirror of `--animate-duration-*` tokens. */
export const motion = {
  /** KPI value change, badge transition, hover. */
  tick: 200,
  /** Status-pill state change, new-event highlight. */
  pulse: 600,
} as const

/** Chart sizing for the home KPI sparkline strip. */
export const chart = {
  /** Sparkline strip height in px. Matches the KPI tile's reserved area. */
  sparklineHeight: 36,
  /** Sparkline line stroke width. */
  sparklineStroke: 1.5,
} as const

/**
 * Semantic status keys. The values are the names of `@theme inline`
 * color tokens (Tailwind class fragments). Use them like:
 *   className={`bg-${status[state]}`}  // ← not allowed, see below
 *
 * Tailwind v4 cannot resolve dynamic class names, so callers MUST map
 * status -> a fixed class via a switch / lookup. This object is the
 * canonical naming.
 */
export const status = {
  /** Healthy / on-track / green. */
  ok: "success",
  /** Watch / yellow. */
  watch: "warning",
  /** Act / red. */
  act: "destructive",
  /** No signal / gray. Never used to mean "ok". */
  idle: "muted",
} as const

export type StatusKey = keyof typeof status

/** Polling cadences (ms) for surfaces that don't yet have WebSocket. */
export const polling = {
  /** Home anchors + KPI strip soft refresh. */
  home: 10_000,
  /** Backing services strip (per home.md §11). */
  services: 10_000,
} as const

/** Density / layout constants the grid can't express through tokens alone. */
export const layout = {
  /** KPI strip column count at md+. Home brief §15 calls for 8 tiles. */
  kpiColumnsMd: 4,
  kpiColumnsLg: 4,
  kpiColumnsXl: 8,
  /** Activity feed default item count. */
  activityFeedDefault: 20,
  /** Quick action card count. */
  quickActionsCount: 4,
} as const

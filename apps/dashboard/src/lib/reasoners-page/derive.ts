// SPDX-License-Identifier: Apache-2.0

import type { ReasonerWindowKind } from "./types"

// Window helpers + formatters for the reasoner analytics table. Latency
// and relative-time formatting reuse @/lib/runs/derive; only the
// reasoner-specific shapes live here.

export function windowFromISO(kind: ReasonerWindowKind): string {
  return new Date(Date.now() - hoursFor(kind) * 3_600_000).toISOString()
}

function hoursFor(kind: ReasonerWindowKind): number {
  switch (kind) {
    case "24h":
      return 24
    case "7d":
      return 7 * 24
    case "30d":
      return 30 * 24
  }
}

// Cost at fixed 4dp — reasoner-level costs are routinely sub-cent, so
// the adaptive run formatter would collapse most rows to "$0".
export function formatReasonerCost(usd: number): string {
  return `$${usd.toFixed(4)}`
}

// error_rate arrives as a 0..1 fraction (errors / calls).
export function formatErrorRatePct(rate: number): string {
  return `${(rate * 100).toFixed(1)}%`
}

// Threshold for painting the errors cell red. 10% per the Build brief.
export const ERROR_RATE_ALERT = 0.1

// SPDX-License-Identifier: Apache-2.0

import { cn } from "@/lib/utils"

// ForecastBar — multi-segment progress for "spent / projected / budget".
//
// Segment widths:
//   spent      → solid bar
//   projected  → striped extension up to the forecasted total
//   over       → destructive-tinted extension when projected exceeds budget
//   remaining  → empty track to the right of projected (no fill)
//
// All colour comes from semantic tokens — no raw hex, no per-component
// overrides. Caller controls track height via `className` (defaults to h-2).

interface ForecastBarProps {
  spent: number
  projected: number
  budget: number | null
  /** Highlight projected as warning once it crosses this share of budget. */
  warnThresholdPct?: number
  className?: string
}

export function ForecastBar({
  spent,
  projected,
  budget,
  warnThresholdPct = 90,
  className,
}: ForecastBarProps) {
  // No budget configured: degrade to a single "spent vs projected" bar
  // sized against the projected total. The caller's surrounding context
  // (label copy, gauge) should communicate the unconfigured state.
  const denominator = budget ?? Math.max(projected, spent, 1)
  const spentPct = clamp01(spent / denominator) * 100
  const projectedPct = clamp01(Math.max(projected, spent) / denominator) * 100
  const overBudget = budget !== null && projected > budget
  const nearBudget =
    budget !== null && projected / budget >= warnThresholdPct / 100

  return (
    <div
      role="img"
      aria-label={
        budget === null
          ? `$${spent.toFixed(2)} spent so far, $${projected.toFixed(2)} projected — no budget configured`
          : `$${spent.toFixed(2)} of $${budget.toFixed(2)} spent (${spentPct.toFixed(0)}%), $${projected.toFixed(2)} projected`
      }
      className={cn(
        "relative w-full overflow-hidden rounded-pill bg-muted",
        className ?? "h-2",
      )}
    >
      {/* Projected extension. Sits behind spent so spent overlays it. */}
      <div
        className={cn(
          "absolute inset-y-0 left-0 transition-[width] duration-300",
          overBudget
            ? "bg-destructive/30"
            : nearBudget
              ? "bg-warning/30"
              : "bg-foreground/20",
        )}
        style={{ width: `${projectedPct}%` }}
      />
      {/* Spent (solid). */}
      <div
        className={cn(
          "absolute inset-y-0 left-0 transition-[width] duration-300",
          overBudget
            ? "bg-destructive"
            : nearBudget
              ? "bg-warning"
              : "bg-foreground",
        )}
        style={{ width: `${spentPct}%` }}
      />
      {/* Budget marker line (only when a budget is set). */}
      {budget !== null ? (
        <div
          aria-hidden
          className="absolute inset-y-0 w-px bg-foreground/60"
          style={{ left: "100%", transform: "translateX(-1px)" }}
        />
      ) : null}
    </div>
  )
}

function clamp01(n: number): number {
  if (!Number.isFinite(n) || n < 0) return 0
  if (n > 1) return 1
  return n
}

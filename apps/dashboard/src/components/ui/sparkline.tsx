// SPDX-License-Identifier: Apache-2.0

"use client"

import {
  Bar,
  BarChart,
  Line,
  LineChart,
  ResponsiveContainer,
} from "recharts"

import { chart } from "@/lib/theme"
import type { StatusState } from "@/lib/home/types"

// Generic monochrome sparkline. Sized to fill its container's width with a
// fixed pixel height. Stroke / fill colour follows the semantic StatusState
// so callers don't pass raw colour values — everything routes through the
// theme tokens declared in globals.css.
//
// Shape selection: a 1-2 point series doesn't read as a line, so we fall
// back to a sparse bar chart. That gives the empty / barely-populated
// state a visible histogram-style frame instead of a dashed underline.
// Once the series has 3+ values the line chart kicks in.

const TINT: Record<StatusState, string> = {
  ok: "var(--color-foreground)",
  watch: "var(--color-warning)",
  act: "var(--color-destructive)",
  idle: "var(--color-muted-foreground)",
}

export interface SparklineProps {
  data: number[]
  status?: StatusState
  /** Height in px. Defaults to the shared chart.sparklineHeight token. */
  height?: number
  /** Stroke width override. Defaults to chart.sparklineStroke. */
  strokeWidth?: number
  /** Force the chart kind regardless of data length. */
  kind?: "auto" | "line" | "bars"
  /** Optional aria-label. If omitted the sparkline is presentational. */
  ariaLabel?: string
}

export function Sparkline({
  data,
  status = "ok",
  height = chart.sparklineHeight,
  strokeWidth = chart.sparklineStroke,
  kind = "auto",
  ariaLabel,
}: SparklineProps) {
  const safe = data.length > 0 ? data : [0]
  const nonZero = safe.some((v) => v > 0)

  // Empty data: don't lie with a flat line. Render a barely-visible
  // baseline + dots so the frame still occupies its slot.
  if (!nonZero) {
    return (
      <EmptyFrame
        aria-hidden={ariaLabel ? undefined : true}
        aria-label={ariaLabel}
        height={height}
      />
    )
  }

  const resolvedKind: "line" | "bars" =
    kind === "auto" ? (safe.length < 4 ? "bars" : "line") : kind

  const series = safe.map((v, idx) => ({ idx, v }))
  return (
    <div
      aria-hidden={ariaLabel ? undefined : true}
      aria-label={ariaLabel}
      role={ariaLabel ? "img" : undefined}
      className="w-full overflow-hidden"
      style={{ height }}
    >
      <ResponsiveContainer width="100%" height="100%">
        {resolvedKind === "line" ? (
          <LineChart
            data={series}
            margin={{ top: 2, right: 0, bottom: 2, left: 0 }}
          >
            <Line
              dataKey="v"
              type="monotone"
              stroke={TINT[status]}
              strokeWidth={strokeWidth}
              dot={false}
              isAnimationActive={false}
            />
          </LineChart>
        ) : (
          <BarChart
            data={series}
            margin={{ top: 2, right: 0, bottom: 2, left: 0 }}
            barCategoryGap={2}
          >
            <Bar
              dataKey="v"
              fill={TINT[status]}
              radius={[1, 1, 0, 0]}
              isAnimationActive={false}
            />
          </BarChart>
        )}
      </ResponsiveContainer>
    </div>
  )
}

function EmptyFrame({
  height,
  ...rest
}: {
  height: number
} & React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      {...rest}
      className="relative w-full overflow-hidden"
      style={{ height }}
    >
      {/* Faint baseline */}
      <div className="absolute inset-x-0 bottom-1/2 h-px bg-border" />
      {/* Three baseline dots so the frame doesn't read as empty space. */}
      <div className="absolute inset-x-0 bottom-1/2 flex translate-y-1/2 justify-between">
        <span className="block size-1 rounded-pill bg-muted-foreground/40" />
        <span className="block size-1 rounded-pill bg-muted-foreground/40" />
        <span className="block size-1 rounded-pill bg-muted-foreground/40" />
      </div>
    </div>
  )
}

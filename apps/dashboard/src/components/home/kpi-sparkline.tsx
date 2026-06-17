// SPDX-License-Identifier: Apache-2.0

"use client"

import { Line, LineChart } from "recharts"

import { ChartContainer, type ChartConfig } from "@/components/ui/chart"
import { chart } from "@/lib/theme"
import type { StatusState } from "@/lib/home/types"

const STROKE: Record<StatusState, string> = {
  ok: "var(--color-foreground)",
  watch: "var(--color-warning)",
  act: "var(--color-destructive)",
  idle: "var(--color-muted-foreground)",
}

const config = {
  v: { label: "value", color: "var(--color-foreground)" },
} satisfies ChartConfig

interface KpiSparklineProps {
  data: number[]
  status: StatusState
}

export function KpiSparkline({ data, status }: KpiSparklineProps) {
  if (data.length === 0) {
    // Empty-state frame so the layout doesn't shift between tiles that
    // have a sparkline and tiles that don't. Hairline ghost line.
    return (
      <div
        aria-hidden
        className="w-20 shrink-0 border-b border-dashed border-muted-foreground/30"
        style={{ height: chart.sparklineHeight }}
      />
    )
  }
  const meaningful = data.some((v) => v > 0)
  if (!meaningful) {
    return (
      <div
        aria-hidden
        className="w-20 shrink-0 border-b border-dashed border-muted-foreground/30"
        style={{ height: chart.sparklineHeight }}
      />
    )
  }
  const series = data.map((v, idx) => ({ idx, v }))
  return (
    <ChartContainer
      config={config}
      className="w-20 shrink-0"
      style={{ height: chart.sparklineHeight }}
    >
      <LineChart
        data={series}
        margin={{ top: 2, right: 0, bottom: 2, left: 0 }}
      >
        <Line
          dataKey="v"
          type="monotone"
          stroke={STROKE[status]}
          strokeWidth={chart.sparklineStroke}
          dot={false}
          isAnimationActive={false}
        />
      </LineChart>
    </ChartContainer>
  )
}

// SPDX-License-Identifier: Apache-2.0

"use client"

import { Area, AreaChart } from "recharts"

import { ChartContainer, type ChartConfig } from "@/components/ui/chart"
import { chart } from "@/lib/theme"

// Minimal 24-bucket sparkline rendered inside a KPI tile. The data array
// is treated as opaque time-series — we don't render axes, gridlines, or
// labels. Stroke and fill colours come from chart-1 (monochrome).
const config = {
  v: {
    label: "value",
    color: "var(--color-chart-1)",
  },
} satisfies ChartConfig

interface KpiSparklineProps {
  /** 24 hourly buckets. Empty array hides the chart. */
  data: number[]
}

export function KpiSparkline({ data }: KpiSparklineProps) {
  if (data.length === 0) return null
  const series = data.map((v, idx) => ({ idx, v }))
  return (
    <ChartContainer
      config={config}
      className="aspect-auto w-full"
      style={{ height: chart.sparklineHeight }}
    >
      <AreaChart
        data={series}
        margin={{ top: 0, right: 0, bottom: 0, left: 0 }}
      >
        <defs>
          <linearGradient id="kpi-sparkline-fill" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="var(--color-chart-1)" stopOpacity={0.18} />
            <stop offset="100%" stopColor="var(--color-chart-1)" stopOpacity={0} />
          </linearGradient>
        </defs>
        <Area
          dataKey="v"
          type="monotone"
          stroke="var(--color-chart-1)"
          strokeWidth={chart.sparklineStroke}
          fill="url(#kpi-sparkline-fill)"
          isAnimationActive={false}
        />
      </AreaChart>
    </ChartContainer>
  )
}

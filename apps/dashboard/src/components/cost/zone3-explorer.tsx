// SPDX-License-Identifier: Apache-2.0

"use client"

import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts"

import { FilterChip, FilterChipGroup } from "@/components/ui/filter-chip"
import { ScrollArea } from "@/components/ui/scroll-area"

import { buildTenantSeries, formatCurrency, topNTenants } from "@/lib/cost/derive"
import type { CostSnapshot } from "@/lib/cost/types"

import { ZoneCard, ZoneCardHeader } from "./zone-card"

// Zone 3 — Slice-Over-Time Explorer.
//
// v1 group-by axis: tenant only. Per the brief §24 the agent / model /
// day axes wait on Gap 16 (ByDayByDimension composite endpoint) so we
// don't fan out unbounded calls. The chips render in v1 with the
// disabled axes labelled "v0.2" so operators know the surface will fill
// in.
//
// Compare picker is similarly stubbed for v1; the page already returns
// previous_total_usd from /cost so the Period-Total tile carries the
// delta. The picker chips are placeholders.

const SERIES_COLOR: Record<number, string> = {
  1: "var(--chart-1)",
  2: "var(--chart-2)",
  3: "var(--chart-3)",
  4: "var(--chart-4)",
  5: "var(--chart-5)",
}
const OTHER_COLOR = "var(--muted-foreground)"

export function Zone3Explorer({ snapshot }: { snapshot: CostSnapshot }) {
  const top = topNTenants(snapshot.primary, 5)
  const series = buildTenantSeries(snapshot.primary, snapshot.byTenant, top)
  const totalForTopN = top.reduce((acc, t) => acc + t.cost, 0)
  const otherCost = (snapshot.primary?.period_total_usd ?? 0) - totalForTopN
  // Suppress the "other" stack when there is no remainder to render — a
  // tiny near-zero band on the bottom of the chart muddies the legend
  // and the colour palette.
  const renderOther = otherCost > 0.0001 && top.length >= 2

  return (
    <ZoneCard aria-labelledby="zone3-heading">
      <ZoneCardHeader
        id="zone3-heading"
        title="Explore"
        subtitle="Pivot tenant spend over the selected range"
      />
      <div className="flex flex-col gap-stack border-b px-row-x py-row-y">
        <FilterChipGroup label="Group by">
          <FilterChip label="Tenant" active onSelect={() => {}} />
          <FilterChip label="Agent" active={false} onSelect={() => {}} />
          <FilterChip label="Model" active={false} onSelect={() => {}} />
          <FilterChip label="Day" active={false} onSelect={() => {}} />
        </FilterChipGroup>
        <span className="text-meta text-muted-foreground">
          Tenant axis ships in v1; agent · model · day arrive with the
          composite endpoint (Gap 16).
        </span>
      </div>
      <div className="grid gap-stack p-row-x lg:grid-cols-[1fr_240px]">
        <ChartCard
          series={series}
          top={top}
          renderOther={renderOther}
        />
        <TopNCard top={top} otherCost={otherCost} />
      </div>
    </ZoneCard>
  )
}

function ChartCard({
  series,
  top,
  renderOther,
}: {
  series: ReturnType<typeof buildTenantSeries>
  top: ReturnType<typeof topNTenants>
  renderOther: boolean
}) {
  const empty = series.length === 0 || top.length === 0
  return (
    <div className="rounded-md border bg-card p-tile">
      {empty ? (
        <div className="flex h-56 items-center justify-center text-meta text-muted-foreground">
          No tenant spend in this range yet.
        </div>
      ) : (
        <div className="h-56 w-full">
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart
              data={series}
              margin={{ top: 8, right: 8, left: 0, bottom: 0 }}
            >
              <CartesianGrid
                vertical={false}
                stroke="var(--border)"
                strokeDasharray="3 3"
              />
              <XAxis
                dataKey="date"
                tickLine={false}
                axisLine={false}
                tickFormatter={(d) => formatDateTick(d)}
                tick={{
                  fill: "var(--muted-foreground)",
                  fontSize: 11,
                }}
              />
              <YAxis
                tickLine={false}
                axisLine={false}
                tickFormatter={(v) =>
                  v === 0
                    ? "$0"
                    : v < 1
                      ? `$${v.toFixed(2)}`
                      : `$${Math.round(v)}`
                }
                tick={{
                  fill: "var(--muted-foreground)",
                  fontSize: 11,
                }}
                width={48}
              />
              <Tooltip
                cursor={{ stroke: "var(--border)" }}
                content={<StackedTooltip top={top} />}
              />
              {top.map((t, idx) => (
                <Area
                  key={t.id}
                  type="monotone"
                  dataKey={t.id}
                  stackId="cost"
                  stroke={SERIES_COLOR[t.colorIndex]}
                  fill={SERIES_COLOR[t.colorIndex]}
                  // Ramp opacity so each series sits cleanly above the
                  // next in monochrome dark mode. Top series most
                  // saturated; lower series fade.
                  fillOpacity={Math.max(0.25, 0.75 - idx * 0.1)}
                  strokeWidth={1}
                  isAnimationActive={false}
                />
              ))}
              {renderOther ? (
                <Area
                  type="monotone"
                  dataKey="other"
                  stackId="cost"
                  stroke={OTHER_COLOR}
                  fill={OTHER_COLOR}
                  fillOpacity={0.15}
                  strokeWidth={1}
                  isAnimationActive={false}
                />
              ) : null}
            </AreaChart>
          </ResponsiveContainer>
        </div>
      )}
    </div>
  )
}

function TopNCard({
  top,
  otherCost,
}: {
  top: ReturnType<typeof topNTenants>
  otherCost: number
}) {
  const empty = top.length === 0
  return (
    <div className="flex min-h-0 flex-col rounded-md border bg-card">
      <header className="flex items-center justify-between border-b px-row-x py-row-y">
        <span className="text-body font-medium text-foreground">Top tenants</span>
        <span className="text-meta text-muted-foreground">{top.length}</span>
      </header>
      {empty ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          No tenant spend yet.
        </p>
      ) : (
        <ScrollArea className="min-h-0 flex-1">
          <ul role="list" className="divide-y">
            {top.map((t) => (
              <li
                key={t.id}
                className="flex items-center gap-stack px-row-x py-row-y text-body"
              >
                <span
                  aria-hidden
                  className="inline-block size-icon-dot rounded-pill"
                  style={{ backgroundColor: SERIES_COLOR[t.colorIndex] }}
                />
                <span className="min-w-0 flex-1 truncate text-foreground">
                  {t.label}
                </span>
                <span className="shrink-0 font-mono text-meta tabular-nums text-muted-foreground">
                  {formatCurrency(t.cost)}
                </span>
              </li>
            ))}
            {otherCost > 0 ? (
              <li className="flex items-center gap-stack px-row-x py-row-y text-body">
                <span
                  aria-hidden
                  className="inline-block size-icon-dot rounded-pill bg-muted-foreground/60"
                />
                <span className="min-w-0 flex-1 truncate text-muted-foreground">
                  Other
                </span>
                <span className="shrink-0 font-mono text-meta tabular-nums text-muted-foreground">
                  {formatCurrency(otherCost)}
                </span>
              </li>
            ) : null}
          </ul>
        </ScrollArea>
      )}
    </div>
  )
}

interface TooltipPayloadItem {
  dataKey?: string
  value?: number
  color?: string
}

function StackedTooltip({
  active,
  payload,
  label,
  top,
}: {
  active?: boolean
  payload?: TooltipPayloadItem[]
  label?: string
  top: ReturnType<typeof topNTenants>
}) {
  if (!active || !payload || payload.length === 0) return null
  const labelLookup = new Map(top.map((t) => [t.id, t.label]))
  const total = payload.reduce((acc, p) => acc + (p.value ?? 0), 0)
  return (
    <div className="rounded-md border bg-popover px-row-x py-row-y text-meta text-popover-foreground shadow-lg">
      <div className="mb-1 font-mono text-muted-foreground">
        {label ? formatDateTick(label) : ""}
      </div>
      <ul className="flex flex-col gap-tile-tight">
        {payload
          .slice()
          .reverse()
          .map((p) => {
            const key = p.dataKey ?? ""
            const name =
              key === "other" ? "Other" : (labelLookup.get(key) ?? key)
            return (
              <li
                key={key}
                className="flex items-center justify-between gap-stack"
              >
                <span className="flex items-center gap-inline">
                  <span
                    aria-hidden
                    className="inline-block size-icon-dot rounded-pill"
                    style={{ backgroundColor: p.color }}
                  />
                  <span className="min-w-0 truncate">{name}</span>
                </span>
                <span className="font-mono tabular-nums">
                  {formatCurrency(p.value ?? 0)}
                </span>
              </li>
            )
          })}
      </ul>
      <div className="mt-1 flex items-center justify-between border-t pt-1 font-mono text-muted-foreground">
        <span>Total</span>
        <span className="tabular-nums">{formatCurrency(total)}</span>
      </div>
    </div>
  )
}

function formatDateTick(raw: string): string {
  const ts = Date.parse(raw)
  if (Number.isNaN(ts)) return raw
  const d = new Date(ts)
  return `${d.getUTCMonth() + 1}/${d.getUTCDate()}`
}

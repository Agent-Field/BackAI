// SPDX-License-Identifier: Apache-2.0

"use client"

import { Cell, Pie, PieChart, ResponsiveContainer } from "recharts"

import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import { formatInt, formatPct, formatUsd } from "@/lib/cache-page/derive"
import type { CacheSummary } from "@/lib/cache-page/types"

// Zone A — the headline. A hit-rate donut anchors the operator's eye,
// flanked by the four numbers that make the gateway's cache legible:
// savings, hits, misses and total calls. Renders structure even at zero
// data so the page never collapses into emptiness.

export function HeadlineStats({ summary }: { summary: CacheSummary }) {
  return (
    <ZoneCard aria-labelledby="cache-headline-heading">
      <ZoneCardHeader
        id="cache-headline-heading"
        title="Cache performance"
        subtitle={summary.hasData ? "LLM gateway response cache" : "No cached calls yet"}
      />
      <div className="grid gap-stack p-row-x lg:grid-cols-[minmax(220px,1fr)_2fr]">
        <HitRateDonut summary={summary} />
        <div className="grid gap-stack sm:grid-cols-2">
          <Tile
            label="Estimated savings"
            value={summary.hasData ? formatUsd(summary.savingsUsd) : "—"}
            hint="cost avoided by serving from cache"
            emphasis
          />
          <Tile
            label="Cache hits"
            value={summary.hasData ? formatInt(summary.hits) : "—"}
            hint="calls served from the cache"
          />
          <Tile
            label="Cache misses"
            value={summary.hasData ? formatInt(summary.misses) : "—"}
            hint="calls that reached a provider"
          />
          <Tile
            label="Total calls"
            value={summary.hasData ? formatInt(summary.totalCalls) : "—"}
            hint="gateway completions this window"
          />
        </div>
      </div>
    </ZoneCard>
  )
}

// Donut mirrors the Health page's DB cache card: a placeholder ring on a
// cold cache so we never draw a misleadingly-perfect circle at zero data.
function HitRateDonut({ summary }: { summary: CacheSummary }) {
  const pct = summary.ratioPct
  const sparse = !summary.hasData || summary.totalCalls === 0
  const data = sparse
    ? [{ name: "placeholder", value: 1 }]
    : [
        { name: "hit", value: pct },
        { name: "miss", value: Math.max(0, 100 - pct) },
      ]
  return (
    <div className="flex flex-col items-center justify-center gap-tile rounded-md border bg-card p-tile">
      <div className="relative h-40 w-full">
        <ResponsiveContainer width="100%" height="100%">
          <PieChart>
            <Pie
              data={data}
              dataKey="value"
              nameKey="name"
              innerRadius={52}
              outerRadius={72}
              startAngle={90}
              endAngle={-270}
              stroke="var(--card)"
              strokeWidth={2}
              isAnimationActive={false}
            >
              {data.map((d, idx) => (
                <Cell
                  key={d.name}
                  fill={
                    sparse
                      ? "var(--color-muted)"
                      : idx === 0
                        ? "var(--color-success)"
                        : "var(--color-muted)"
                  }
                />
              ))}
            </Pie>
          </PieChart>
        </ResponsiveContainer>
        <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
          <span className="text-2xl font-semibold tabular-nums text-foreground">
            {sparse ? "—" : formatPct(summary.ratio)}
          </span>
          <span className="text-meta text-muted-foreground">hit rate</span>
        </div>
      </div>
      <p className="text-center text-meta text-muted-foreground">
        {sparse
          ? "Send repeated prompts through the gateway to warm the cache."
          : "Share of gateway calls answered from cache."}
      </p>
    </div>
  )
}

function Tile({
  label,
  value,
  hint,
  emphasis,
}: {
  label: string
  value: string
  hint: string
  emphasis?: boolean
}) {
  return (
    <div className="flex flex-col gap-tile rounded-md border bg-card px-row-x py-tile">
      <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">{label}</span>
      <span
        className={`tabular-nums font-semibold text-foreground ${emphasis ? "text-2xl text-success" : "text-xl"}`}
      >
        {value}
      </span>
      <span className="text-meta text-muted-foreground">{hint}</span>
    </div>
  )
}

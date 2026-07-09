// SPDX-License-Identifier: Apache-2.0

import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import { formatInt, formatPct, formatUsd } from "@/lib/cache-page/derive"
import type { CacheSummary } from "@/lib/cache-page/types"

// Zone B — the breakdown. A single stacked bar splits hits from misses so
// the ratio reads at a glance, then three economics tiles explain what
// the cache is actually holding and what each hit is worth.

export function HitBreakdown({ summary }: { summary: CacheSummary }) {
  return (
    <ZoneCard aria-labelledby="cache-breakdown-heading">
      <ZoneCardHeader
        id="cache-breakdown-heading"
        title="Hit / miss breakdown"
        subtitle={summary.hasData ? `${formatInt(summary.totalCalls)} calls` : "awaiting traffic"}
      />
      <div className="flex flex-col gap-stack p-row-x">
        <HitMissBar summary={summary} />
        <div className="grid gap-stack sm:grid-cols-3">
          <Economics
            label="Cached entries"
            value={formatInt(summary.entries)}
            hint="distinct prompt responses stored"
            hasData={summary.hasData}
          />
          <Economics
            label="Savings / hit"
            value={formatUsd(summary.savingsPerHit)}
            hint="avg cost avoided per cache hit"
            hasData={summary.hits > 0}
          />
          <Economics
            label="Total saved"
            value={formatUsd(summary.savingsUsd)}
            hint="cumulative provider cost avoided"
            hasData={summary.hasData}
          />
        </div>
      </div>
    </ZoneCard>
  )
}

function HitMissBar({ summary }: { summary: CacheSummary }) {
  // Prefer the raw hit/miss counts for the bar geometry; they always sum
  // to something the operator can reconcile against the tiles. Fall back
  // to an empty rail when nothing has flowed through yet.
  const denom = summary.hits + summary.misses
  const hitPct = denom > 0 ? (summary.hits / denom) * 100 : 0
  const missPct = denom > 0 ? (summary.misses / denom) * 100 : 0
  return (
    <div className="flex flex-col gap-tile">
      <div
        role="img"
        aria-label={
          denom > 0 ? `${summary.hits} hits, ${summary.misses} misses` : "no cache activity yet"
        }
        className="flex h-4 w-full overflow-hidden rounded-pill bg-muted"
      >
        {summary.hits > 0 ? (
          <div
            className="h-full bg-success"
            style={{ width: `${hitPct}%` }}
            title={`Hits ${summary.hits}`}
          />
        ) : null}
        {summary.misses > 0 ? (
          <div
            className="h-full bg-muted-foreground/40"
            style={{ width: `${missPct}%` }}
            title={`Misses ${summary.misses}`}
          />
        ) : null}
      </div>
      <div className="flex flex-wrap items-center gap-section text-meta">
        <Legend
          swatch="bg-success"
          label="Hits"
          value={`${formatInt(summary.hits)} · ${denom > 0 ? formatPct(summary.hits / denom) : "—"}`}
        />
        <Legend
          swatch="bg-muted-foreground/40"
          label="Misses"
          value={`${formatInt(summary.misses)} · ${denom > 0 ? formatPct(summary.misses / denom) : "—"}`}
        />
      </div>
    </div>
  )
}

function Legend({ swatch, label, value }: { swatch: string; label: string; value: string }) {
  return (
    <span className="flex items-center gap-inline text-muted-foreground">
      <span aria-hidden className={`size-2.5 shrink-0 rounded-pill ${swatch}`} />
      <span className="text-foreground">{label}</span>
      <span className="font-mono tabular-nums">{value}</span>
    </span>
  )
}

function Economics({
  label,
  value,
  hint,
  hasData,
}: {
  label: string
  value: string
  hint: string
  hasData: boolean
}) {
  return (
    <div className="flex flex-col gap-tile rounded-md border bg-card px-row-x py-tile">
      <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">{label}</span>
      <span className="text-body font-medium tabular-nums text-foreground">
        {hasData ? value : "—"}
      </span>
      <span className="text-meta text-muted-foreground">{hint}</span>
    </div>
  )
}

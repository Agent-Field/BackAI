// SPDX-License-Identifier: Apache-2.0

"use client"

import { ChevronDown, ChevronRight } from "lucide-react"
import { useMemo, useState } from "react"

import type { CostSummary } from "@/lib/api"
import { formatCurrency, topNTenants } from "@/lib/cost/derive"
import type { CostSnapshot } from "@/lib/cost/types"
import { cn } from "@/lib/utils"

import { ZoneCard, ZoneCardHeader } from "./zone-card"

// Zone 2 — Explain-Spend Hierarchy (v1 minimum).
//
// The brief calls for a 4-level lineage (Tenant → Agent → Reasoner →
// Model). v1 ships **two** of those levels:
//   • Tenant (root)  — from CostSummary.by_tenant
//   • Model (leaf)   — from per-tenant CostSummary.by_model
//
// Agent + Reasoner levels are deferred (Gaps 13/14). The page surfaces
// the gap inline so operators know the hierarchy is intentionally
// shallow rather than broken.
//
// Visual rules from the critique:
// 1. Neutral share-bar colour (bg-muted-foreground/30). Green is reserved
//    for "under budget" / "success" semantics, not for "look at me".
// 2. Hide the share-bar entirely when there is only one tenant — a full
//    bar with N=1 reads as an alert state rather than a proportion.
// 3. Auto-expand the row when there is only one tenant — the model
//    breakdown is the actual content in that case, and the chevron
//    would otherwise look like it does nothing.

export function Zone2Hierarchy({ snapshot }: { snapshot: CostSnapshot }) {
  const top = useMemo(() => topNTenants(snapshot.primary, 8), [snapshot.primary])
  const total = snapshot.primary?.period_total_usd ?? 0
  const hideBars = top.length <= 1
  return (
    <ZoneCard aria-labelledby="zone2-heading">
      <ZoneCardHeader
        id="zone2-heading"
        title="Explain spend"
        subtitle="Tenant → Model · Agent + Reasoner in v0.2"
      />
      {top.length === 0 ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          No tenant spend in this range.
        </p>
      ) : (
        <ul role="list" className="divide-y">
          {top.map((t, idx) => {
            const tenantSummary = snapshot.byTenant[t.id] ?? null
            return (
              <TenantRow
                key={t.id}
                id={t.id}
                name={t.label}
                cost={t.cost}
                total={total}
                summary={tenantSummary}
                hideBar={hideBars}
                defaultExpanded={top.length === 1 || idx === 0}
              />
            )
          })}
        </ul>
      )}
    </ZoneCard>
  )
}

function TenantRow({
  id,
  name,
  cost,
  total,
  summary,
  hideBar,
  defaultExpanded,
}: {
  id: string
  name: string
  cost: number
  total: number
  summary: CostSummary | null
  hideBar: boolean
  defaultExpanded: boolean
}) {
  const [expanded, setExpanded] = useState(defaultExpanded)
  const share = total > 0 ? (cost / total) * 100 : 0
  const Chevron = expanded ? ChevronDown : ChevronRight
  const canExpand = summary !== null && summary.by_model.length > 0
  const cols = hideBar
    ? "grid-cols-[24px_minmax(0,1fr)_72px_72px]"
    : "grid-cols-[24px_minmax(0,1fr)_minmax(120px,2fr)_72px_72px]"
  return (
    <li>
      <button
        type="button"
        onClick={() => canExpand && setExpanded((v) => !v)}
        disabled={!canExpand}
        className={cn(
          "grid w-full items-center gap-stack px-row-x py-row-y text-left text-body",
          cols,
          canExpand
            ? "hover:bg-accent/40 focus-visible:bg-accent/40 focus-visible:outline-none"
            : "cursor-default",
        )}
        aria-expanded={expanded}
        aria-controls={`tenant-models-${id}`}
      >
        <Chevron
          className={cn(
            "size-3.5 text-muted-foreground",
            canExpand ? "" : "opacity-30",
          )}
          aria-hidden
        />
        <span className="min-w-0 truncate font-medium text-foreground">
          {name}
        </span>
        {hideBar ? null : <ShareBar value={cost} total={total} />}
        <span className="text-right font-mono text-meta tabular-nums text-muted-foreground">
          {share.toFixed(0)}%
        </span>
        <span className="text-right font-mono text-meta tabular-nums text-foreground">
          {formatCurrency(cost)}
        </span>
      </button>
      {expanded && summary ? (
        <ul
          id={`tenant-models-${id}`}
          role="list"
          className="divide-y border-t bg-background/40"
        >
          {[...summary.by_model]
            .sort((a, b) => b.cost_usd - a.cost_usd)
            .map((m) => {
              const modelShare = cost > 0 ? (m.cost_usd / cost) * 100 : 0
              return (
                <li
                  key={m.model}
                  className={cn(
                    "grid items-center gap-stack px-row-x py-row-y text-meta",
                    cols,
                  )}
                >
                  <span className="text-muted-foreground" aria-hidden>
                    ↳
                  </span>
                  <span className="min-w-0 truncate font-mono text-foreground">
                    {friendlyModelName(m.model)}
                  </span>
                  {hideBar ? null : (
                    <ShareBar value={m.cost_usd} total={cost} />
                  )}
                  <span className="text-right font-mono tabular-nums text-muted-foreground">
                    {modelShare.toFixed(0)}%
                  </span>
                  <span className="text-right font-mono tabular-nums text-foreground">
                    {formatCurrency(m.cost_usd)}
                  </span>
                </li>
              )
            })}
        </ul>
      ) : null}
    </li>
  )
}

// Neutral, low-contrast proportion bar. Distinct from GaugeBar which
// carries status semantics (green/yellow/red). Hierarchy rows just need
// to read a share — no status colour.
function ShareBar({ value, total }: { value: number; total: number }) {
  const pct = total <= 0 ? 0 : Math.max(0, Math.min(100, (value / total) * 100))
  return (
    <div
      aria-hidden
      className="relative h-1.5 w-full overflow-hidden rounded-pill bg-muted/60"
    >
      <div
        className="h-full bg-muted-foreground/30"
        style={{ width: `${pct}%` }}
      />
    </div>
  )
}

// Trim provider prefix + version noise off model identifiers so the
// hierarchy reads at a glance. Full identifier still shows on hover via
// the row tooltip (deferred — v1 keeps the friendly form only).
function friendlyModelName(raw: string): string {
  const lastSlash = raw.lastIndexOf("/")
  const leaf = lastSlash >= 0 ? raw.slice(lastSlash + 1) : raw
  return leaf.length > 32 ? `${leaf.slice(0, 30)}…` : leaf
}

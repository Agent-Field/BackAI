// SPDX-License-Identifier: Apache-2.0

"use client"

import { ChevronDown, ChevronRight } from "lucide-react"
import { useMemo, useState } from "react"

import { GaugeBar } from "@/components/ui/gauge-bar"

import type { CostSummary } from "@/lib/api"
import { formatCurrency, topNTenants } from "@/lib/cost/derive"
import type { CostSnapshot } from "@/lib/cost/types"

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
// Reusing topNTenants() and per-tenant byTenant data already fetched for
// Zone 3 means this zone costs zero extra requests.

export function Zone2Hierarchy({ snapshot }: { snapshot: CostSnapshot }) {
  const top = useMemo(() => topNTenants(snapshot.primary, 8), [snapshot.primary])
  const total = snapshot.primary?.period_total_usd ?? 0
  return (
    <section
      aria-labelledby="zone2-heading"
      className="flex flex-col gap-stack"
    >
      <header className="flex items-baseline justify-between">
        <h2
          id="zone2-heading"
          className="text-eyebrow uppercase tracking-wide text-muted-foreground"
        >
          Explain spend
        </h2>
        <span className="text-meta text-muted-foreground">
          Tenant → Model · Agent + Reasoner in v0.2
        </span>
      </header>
      <div className="rounded-md border bg-card">
        {top.length === 0 ? (
          <p className="px-row-x py-tile text-meta text-muted-foreground">
            No tenant spend in this range.
          </p>
        ) : (
          <ul role="list" className="divide-y">
            {top.map((t) => {
              const tenantSummary = snapshot.byTenant[t.id] ?? null
              return (
                <TenantRow
                  key={t.id}
                  id={t.id}
                  name={t.label}
                  cost={t.cost}
                  total={total}
                  summary={tenantSummary}
                />
              )
            })}
          </ul>
        )}
      </div>
    </section>
  )
}

function TenantRow({
  id,
  name,
  cost,
  total,
  summary,
}: {
  id: string
  name: string
  cost: number
  total: number
  summary: CostSummary | null
}) {
  const [expanded, setExpanded] = useState(false)
  const share = total > 0 ? (cost / total) * 100 : 0
  const Chevron = expanded ? ChevronDown : ChevronRight
  const canExpand = summary !== null && summary.by_model.length > 0
  return (
    <li>
      <button
        type="button"
        onClick={() => canExpand && setExpanded((v) => !v)}
        disabled={!canExpand}
        className={`grid w-full grid-cols-[24px_minmax(0,1fr)_minmax(120px,2fr)_72px_72px] items-center gap-stack px-row-x py-row-y text-left text-body ${
          canExpand
            ? "hover:bg-accent/40 focus-visible:bg-accent/40 focus-visible:outline-none"
            : "cursor-default"
        }`}
        aria-expanded={expanded}
        aria-controls={`tenant-models-${id}`}
      >
        <Chevron
          className={`size-3.5 text-muted-foreground ${canExpand ? "" : "opacity-30"}`}
          aria-hidden
        />
        <span className="min-w-0 truncate font-medium text-foreground">
          {name}
        </span>
        <GaugeBar value={cost} max={Math.max(total, cost)} status="ok" />
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
                  className="grid grid-cols-[24px_minmax(0,1fr)_minmax(120px,2fr)_72px_72px] items-center gap-stack px-row-x py-row-y text-meta"
                >
                  <span className="text-muted-foreground" aria-hidden>
                    ↳
                  </span>
                  <span className="min-w-0 truncate font-mono text-foreground">
                    {m.model}
                  </span>
                  <GaugeBar
                    value={m.cost_usd}
                    max={cost}
                    status="idle"
                  />
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

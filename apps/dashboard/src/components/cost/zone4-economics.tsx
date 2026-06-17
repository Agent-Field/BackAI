// SPDX-License-Identifier: Apache-2.0

"use client"

import { useState } from "react"
import { Cell, Pie, PieChart, ResponsiveContainer } from "recharts"

import { Button } from "@/components/ui/button"
import { GaugeBar } from "@/components/ui/gauge-bar"

import type { Budget, CacheStats } from "@/lib/api"
import { formatCurrency, modelCostRows, sortedBudgetRows } from "@/lib/cost/derive"
import type { CostSnapshot } from "@/lib/cost/types"

import { BudgetEditDialog } from "./budget-edit-dialog"

// Zone 4 — Inference economics: cache hit donut + $/call by model bars +
// budgets table with edit. Per the brief, $/1k tokens bars and Cost÷MRR
// scatter are v0.2 (Gap 17/18/19). v1 surfaces a $/share-of-spend bar so
// the page still teaches operators about model mix.

export function Zone4Economics({
  snapshot,
  onBudgetSaved,
}: {
  snapshot: CostSnapshot
  onBudgetSaved: () => Promise<void> | void
}) {
  return (
    <section
      aria-labelledby="zone4-heading"
      className="flex flex-col gap-stack"
    >
      <header className="flex items-baseline justify-between">
        <h2
          id="zone4-heading"
          className="text-eyebrow uppercase tracking-wide text-muted-foreground"
        >
          Inference economics
        </h2>
        <span className="text-meta text-muted-foreground">
          Cache · Models · Budgets
        </span>
      </header>
      <div className="grid gap-stack lg:grid-cols-[320px_1fr]">
        <CacheCard cache={snapshot.cache} />
        <ModelMixCard snapshot={snapshot} />
      </div>
      <BudgetsCard
        budgets={snapshot.budgets}
        tenantLookup={Object.fromEntries(
          snapshot.tenants.map((t) => [t.id, t.name] as const),
        )}
        onBudgetSaved={onBudgetSaved}
      />
    </section>
  )
}

// ──────────────────────────────────────────────────────────────────────
// Cache donut
// ──────────────────────────────────────────────────────────────────────

function CacheCard({ cache }: { cache: CacheStats | null }) {
  if (!cache || cache.total_calls === 0) {
    return (
      <div className="flex h-44 flex-col items-center justify-center rounded-md border bg-card p-tile text-meta text-muted-foreground">
        <span>No cache traffic yet.</span>
      </div>
    )
  }
  const data = [
    { name: "hit", value: cache.cache_hits },
    { name: "miss", value: cache.cache_misses },
  ]
  const hitPct = Math.round(cache.hit_rate * 100)
  return (
    <div className="rounded-md border bg-card p-tile">
      <header className="mb-tile flex items-baseline justify-between">
        <span className="text-body font-medium text-foreground">Cache</span>
        <span className="text-meta text-muted-foreground">
          {cache.total_calls.toLocaleString()} calls
        </span>
      </header>
      <div className="relative h-32 w-full">
        <ResponsiveContainer width="100%" height="100%">
          <PieChart>
            <Pie
              data={data}
              dataKey="value"
              nameKey="name"
              innerRadius={42}
              outerRadius={60}
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
                    idx === 0 ? "var(--color-foreground)" : "var(--color-muted)"
                  }
                />
              ))}
            </Pie>
          </PieChart>
        </ResponsiveContainer>
        <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
          <span className="text-lg font-semibold tabular-nums text-foreground">
            {hitPct}%
          </span>
          <span className="text-meta text-muted-foreground">hit rate</span>
        </div>
      </div>
      <div className="mt-tile flex items-center justify-between text-meta">
        <span className="text-muted-foreground">Saved</span>
        <span className="font-mono tabular-nums text-foreground">
          {formatCurrency(cache.savings_usd)}
        </span>
      </div>
    </div>
  )
}

// ──────────────────────────────────────────────────────────────────────
// Model mix horizontal bars
// ──────────────────────────────────────────────────────────────────────

function ModelMixCard({ snapshot }: { snapshot: CostSnapshot }) {
  const rows = modelCostRows(snapshot.primary).slice(0, 8)
  return (
    <div className="rounded-md border bg-card p-tile">
      <header className="mb-tile flex items-baseline justify-between">
        <span className="text-body font-medium text-foreground">
          Cost by model
        </span>
        <span className="text-meta text-muted-foreground">
          v0.2: $/1k tokens once token totals land
        </span>
      </header>
      {rows.length === 0 ? (
        <p className="py-tile text-meta text-muted-foreground">
          No model usage in this range.
        </p>
      ) : (
        <ul role="list" className="flex flex-col gap-tile-tight">
          {rows.map((row) => (
            <li
              key={row.model}
              className="grid grid-cols-[minmax(0,1fr)_minmax(120px,2fr)_72px] items-center gap-stack text-meta"
            >
              <span className="min-w-0 truncate font-mono text-foreground">
                {row.model}
              </span>
              <div
                aria-hidden
                className="h-1.5 w-full overflow-hidden rounded-pill bg-muted"
              >
                <div
                  className="h-full bg-foreground/80"
                  style={{ width: `${row.share}%` }}
                />
              </div>
              <span className="text-right font-mono tabular-nums text-muted-foreground">
                {formatCurrency(row.cost)}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

// ──────────────────────────────────────────────────────────────────────
// Budgets table
// ──────────────────────────────────────────────────────────────────────

function BudgetsCard({
  budgets,
  tenantLookup,
  onBudgetSaved,
}: {
  budgets: Budget[]
  tenantLookup: Record<string, string>
  onBudgetSaved: () => Promise<void> | void
}) {
  const rows = sortedBudgetRows(budgets)
  const [editing, setEditing] = useState<Budget | null>(null)
  return (
    <div id="budgets" className="rounded-md border bg-card">
      <header className="flex items-center justify-between border-b px-row-x py-row-y">
        <span className="text-body font-medium text-foreground">Budgets</span>
        <span className="text-meta text-muted-foreground">
          {rows.length} configured
        </span>
      </header>
      {rows.length === 0 ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          No tenant budgets configured yet. Set one to surface budget
          alerts in the Inbox.
        </p>
      ) : (
        <ul role="list" className="divide-y">
          {rows.map((b) => (
            <BudgetRow
              key={b.tenant_id}
              budget={b}
              tenantName={tenantLookup[b.tenant_id] ?? b.tenant_id.slice(0, 8)}
              onEdit={() => setEditing(b)}
            />
          ))}
        </ul>
      )}
      <BudgetEditDialog
        budget={editing}
        tenantName={
          editing ? tenantLookup[editing.tenant_id] ?? editing.tenant_id.slice(0, 8) : ""
        }
        onClose={() => setEditing(null)}
        onSaved={async () => {
          setEditing(null)
          await onBudgetSaved()
        }}
      />
    </div>
  )
}

function BudgetRow({
  budget,
  tenantName,
  onEdit,
}: {
  budget: Budget
  tenantName: string
  onEdit: () => void
}) {
  const spent = budget.spent_this_period_usd
  const cap = budget.monthly_usd
  const pct = cap > 0 ? Math.round((spent / cap) * 100) : 0
  const overrun = spent > cap
  return (
    <li className="grid grid-cols-[minmax(0,1fr)_minmax(160px,2fr)_120px_64px] items-center gap-stack px-row-x py-row-y text-body">
      <span className="min-w-0 truncate text-foreground">{tenantName}</span>
      <GaugeBar
        value={spent}
        max={cap}
        ariaLabel={`${tenantName} budget ${pct}%`}
      />
      <span
        className={`text-right font-mono text-meta tabular-nums ${overrun ? "text-destructive" : "text-muted-foreground"}`}
      >
        {formatCurrency(spent)} / {formatCurrency(cap)}
      </span>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="h-7 justify-self-end text-meta"
        onClick={onEdit}
      >
        Edit
      </Button>
    </li>
  )
}

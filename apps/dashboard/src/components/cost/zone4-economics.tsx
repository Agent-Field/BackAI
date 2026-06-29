// SPDX-License-Identifier: Apache-2.0

"use client"

import { LineChart as ScatterIcon } from "lucide-react"
import { useState } from "react"
import { Cell, Pie, PieChart, ResponsiveContainer } from "recharts"

import { Button } from "@/components/ui/button"
import { GaugeBar } from "@/components/ui/gauge-bar"

import type { Budget, CacheStats } from "@/lib/api"
import { formatCurrency, modelCostRows, sortedBudgetRows } from "@/lib/cost/derive"
import type { CostSnapshot } from "@/lib/cost/types"

import { BudgetEditDialog } from "./budget-edit-dialog"
import { ZoneCard, ZoneCardHeader } from "./zone-card"

// Zone 4 — Inference economics: cache hit donut + cost-by-model bars +
// break-even scatter placeholder + budgets table with edit. Per the
// brief, $/1k tokens bars and Cost÷MRR scatter are v0.2 (Gap 17/18/19).
// v1 still renders the SHAPE of those panels (placeholders + chart
// frames) so the page reads "real platform without your data" rather
// than "half-built".

export function Zone4Economics({
  snapshot,
  onBudgetSaved,
}: {
  snapshot: CostSnapshot
  onBudgetSaved: () => Promise<void> | void
}) {
  return (
    <ZoneCard aria-labelledby="zone4-heading">
      <ZoneCardHeader
        id="zone4-heading"
        title="Inference economics"
        subtitle="Cache · Models · Break-even · Budgets"
      />
      <div className="grid gap-stack p-row-x lg:grid-cols-[280px_1fr_280px]">
        <CacheCard cache={snapshot.cache} />
        <ModelMixCard snapshot={snapshot} />
        <BreakEvenPlaceholder />
      </div>
      <div className="border-t bg-card/40 p-row-x">
        <BudgetsCard
          budgets={snapshot.budgets}
          tenantLookup={Object.fromEntries(
            snapshot.tenants.map((t) => [t.id, t.name] as const),
          )}
          onBudgetSaved={onBudgetSaved}
        />
      </div>
    </ZoneCard>
  )
}

// ──────────────────────────────────────────────────────────────────────
// Cache donut
// ──────────────────────────────────────────────────────────────────────

function CacheCard({ cache }: { cache: CacheStats | null }) {
  const hasData = cache !== null && cache.total_calls > 0
  const data = hasData
    ? [
        { name: "hit", value: cache.cache_hits },
        { name: "miss", value: cache.cache_misses },
      ]
    : [{ name: "placeholder", value: 1 }]
  const hitPct = hasData ? Math.round(cache.hit_rate * 100) : 0
  return (
    <div className="rounded-md border bg-card p-tile">
      <header className="mb-tile flex items-baseline justify-between">
        <span className="text-body font-medium text-foreground">Cache</span>
        <span className="text-meta text-muted-foreground">
          {hasData ? `${cache.total_calls.toLocaleString()} calls` : "no traffic"}
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
                    !hasData
                      ? "var(--color-muted)"
                      : idx === 0
                        ? "var(--color-foreground)"
                        : "var(--color-muted)"
                  }
                />
              ))}
            </Pie>
          </PieChart>
        </ResponsiveContainer>
        <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
          <span className="text-lg font-semibold tabular-nums text-foreground">
            {hasData ? `${hitPct}%` : "—"}
          </span>
          <span className="text-meta text-muted-foreground">hit rate</span>
        </div>
      </div>
      <div className="mt-tile flex items-center justify-between text-meta">
        <span className="text-muted-foreground">Saved</span>
        <span className="font-mono tabular-nums text-foreground">
          {hasData ? formatCurrency(cache.savings_usd) : "—"}
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
        <ul role="list" className="flex flex-col gap-stack">
          {rows.map((row) => (
            <li
              key={row.model}
              className="grid grid-cols-[minmax(0,1fr)_minmax(140px,2fr)_80px] items-center gap-stack text-meta"
            >
              <div className="flex min-w-0 flex-col gap-tile-tight">
                <span className="truncate font-medium text-foreground">
                  {friendlyModelName(row.model)}
                </span>
                <span className="truncate font-mono text-meta text-muted-foreground">
                  {modelProvider(row.model)}
                </span>
              </div>
              <div
                aria-hidden
                className="h-3 w-full overflow-hidden rounded-pill bg-muted"
              >
                <div
                  className="h-full bg-foreground/80"
                  style={{ width: `${Math.max(row.share, 2)}%` }}
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
// Break-even scatter — placeholder card (Gap 17 deferred)
// ──────────────────────────────────────────────────────────────────────

function BreakEvenPlaceholder() {
  return (
    <div className="flex flex-col gap-tile rounded-md border bg-card p-tile">
      <header className="flex items-baseline justify-between">
        <span className="text-body font-medium text-foreground">
          Cost ÷ MRR
        </span>
        <span className="text-meta text-muted-foreground">v0.2</span>
      </header>
      <div
        aria-label="Break-even scatter placeholder"
        role="img"
        className="relative h-32 w-full overflow-hidden rounded-md border border-dashed border-muted-foreground/40 bg-background/40"
      >
        {/* Stylised break-even diagonal so the frame reads as a chart, not a void. */}
        <svg
          className="absolute inset-0 h-full w-full text-muted-foreground/60"
          viewBox="0 0 100 100"
          preserveAspectRatio="none"
        >
          <line
            x1="0"
            y1="100"
            x2="100"
            y2="0"
            stroke="currentColor"
            strokeDasharray="4 4"
            strokeWidth="0.6"
          />
        </svg>
        <div className="absolute inset-0 flex items-center justify-center">
          <ScatterIcon
            className="size-icon-inline text-muted-foreground/50"
            aria-hidden
          />
        </div>
      </div>
      <p className="text-meta text-muted-foreground">
        Connect a billing adapter to see unit economics per tenant.
      </p>
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
        <div className="flex flex-col items-start gap-stack px-row-x py-tile">
          <p className="text-meta text-muted-foreground">
            No tenant budgets configured yet. Setting a budget surfaces
            alerts in the Inbox when consumption crosses the threshold.
          </p>
        </div>
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
          editing
            ? (tenantLookup[editing.tenant_id] ?? editing.tenant_id.slice(0, 8))
            : ""
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

// ──────────────────────────────────────────────────────────────────────
// Model name formatting
// ──────────────────────────────────────────────────────────────────────

function friendlyModelName(raw: string): string {
  const lastSlash = raw.lastIndexOf("/")
  const leaf = lastSlash >= 0 ? raw.slice(lastSlash + 1) : raw
  return leaf
    .replace(/^claude[-_]/i, "Claude ")
    .replace(/^qwen[-_]/i, "Qwen ")
    .replace(/^gemini[-_]/i, "Gemini ")
    .replace(/^gpt[-_]/i, "GPT-")
    .replace(/-/g, " ")
}

function modelProvider(raw: string): string {
  const parts = raw.split("/")
  if (parts.length >= 2) return parts.slice(0, -1).join(" · ")
  if (raw.startsWith("claude")) return "anthropic"
  if (raw.startsWith("gpt") || raw.startsWith("o1")) return "openai"
  if (raw.startsWith("gemini")) return "google"
  if (raw.startsWith("qwen")) return "qwen"
  return "—"
}

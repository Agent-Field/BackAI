// SPDX-License-Identifier: Apache-2.0

"use client"

// Breakdown tabs: Model / Agent / Tenant / Day. Each tab renders a
// sorted (largest first) list of rows with a colored share bar.

import { useMemo, useState } from "react"
import { CircleDollarSign } from "lucide-react"

import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import type { CostSummary } from "@/lib/api"

import { formatCurrency } from "./format"

type Row = { key: string; label: string; cost_usd: number }

type CostBreakdownsProps = {
  data: CostSummary
}

function formatDayLabel(iso: string): string {
  const [y, m, d] = iso.slice(0, 10).split("-").map((n) => parseInt(n, 10))
  if (!y || !m || !d) return iso
  const date = new Date(y, m - 1, d)
  return date.toLocaleDateString("en-US", {
    month: "short",
    day: "2-digit",
    year: "numeric",
  })
}

function BreakdownTable({
  rows,
  dimensionLabel,
}: {
  rows: Row[]
  dimensionLabel: string
}) {
  const total = rows.reduce((acc, r) => acc + r.cost_usd, 0)
  const max = rows[0]?.cost_usd ?? 0

  if (rows.length === 0 || total <= 0) {
    return (
      <Empty className="border-dashed">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <CircleDollarSign />
          </EmptyMedia>
          <EmptyTitle>No spend by {dimensionLabel}</EmptyTitle>
          <EmptyDescription>
            Spend across this dimension appears here once usage is recorded.
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <ul className="divide-y">
      {rows.map((row) => {
        const sharePct = total > 0 ? (row.cost_usd / total) * 100 : 0
        const barPct = max > 0 ? (row.cost_usd / max) * 100 : 0
        return (
          <li
            key={row.key}
            className="grid grid-cols-[1fr_auto_auto] items-center gap-4 px-4 py-3 text-sm"
          >
            <div className="min-w-0">
              <div className="truncate font-medium" title={row.label}>
                {row.label}
              </div>
              <div className="bg-muted mt-1.5 h-1 w-full overflow-hidden rounded-full">
                <div
                  className="bg-primary h-full rounded-full transition-[width]"
                  style={{ width: `${barPct}%` }}
                  aria-hidden
                />
              </div>
            </div>
            <div className="text-muted-foreground w-12 text-right text-xs tabular-nums">
              {sharePct.toFixed(1)}%
            </div>
            <div className="w-24 text-right font-mono text-sm tabular-nums">
              {formatCurrency(row.cost_usd)}
            </div>
          </li>
        )
      })}
    </ul>
  )
}

export function CostBreakdowns({ data }: CostBreakdownsProps) {
  const [tab, setTab] = useState<string>("model")

  const modelRows: Row[] = useMemo(
    () =>
      [...data.by_model]
        .sort((a, b) => b.cost_usd - a.cost_usd)
        .map((m) => ({ key: m.model, label: m.model, cost_usd: m.cost_usd })),
    [data.by_model],
  )
  const agentRows: Row[] = useMemo(
    () =>
      [...data.by_agent]
        .sort((a, b) => b.cost_usd - a.cost_usd)
        .map((a) => ({ key: a.agent, label: a.agent, cost_usd: a.cost_usd })),
    [data.by_agent],
  )
  const tenantRows: Row[] = useMemo(
    () =>
      [...data.by_tenant]
        .sort((a, b) => b.cost_usd - a.cost_usd)
        .map((t) => ({
          key: t.tenant_id,
          label: t.tenant_name ?? t.tenant_id,
          cost_usd: t.cost_usd,
        })),
    [data.by_tenant],
  )
  const dayRows: Row[] = useMemo(
    () =>
      [...data.by_day]
        .sort((a, b) => b.cost_usd - a.cost_usd)
        .map((d) => ({
          key: d.date,
          label: formatDayLabel(d.date),
          cost_usd: d.cost_usd,
        })),
    [data.by_day],
  )

  return (
    <Card className="gap-0">
      <CardHeader className="border-b pb-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <CardTitle>Breakdown</CardTitle>
            <CardDescription>
              Spend share across each dimension, largest first.
            </CardDescription>
          </div>
          <Tabs value={tab} onValueChange={(v) => setTab(String(v))}>
            <TabsList>
              <TabsTrigger value="model">Model</TabsTrigger>
              <TabsTrigger value="agent">Agent</TabsTrigger>
              <TabsTrigger value="tenant">Tenant</TabsTrigger>
              <TabsTrigger value="day">Day</TabsTrigger>
            </TabsList>
          </Tabs>
        </div>
      </CardHeader>
      <Tabs value={tab} onValueChange={(v) => setTab(String(v))}>
        <TabsContent value="model" className="m-0">
          <BreakdownTable rows={modelRows} dimensionLabel="model" />
        </TabsContent>
        <TabsContent value="agent" className="m-0">
          <BreakdownTable rows={agentRows} dimensionLabel="agent" />
        </TabsContent>
        <TabsContent value="tenant" className="m-0">
          <BreakdownTable rows={tenantRows} dimensionLabel="tenant" />
        </TabsContent>
        <TabsContent value="day" className="m-0">
          <BreakdownTable rows={dayRows} dimensionLabel="day" />
        </TabsContent>
      </Tabs>
    </Card>
  )
}
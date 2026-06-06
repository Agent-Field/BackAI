"use client"

// Filter bar for the Cost page. Updates URL search params so the server
// component above re-fetches with the new range. The download button
// produces a CSV in-memory from the current breakdown (no roundtrip).

import { useRouter, useSearchParams } from "next/navigation"
import { useTransition } from "react"
import { Download } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import type { CostSummary } from "@/lib/api"

const RANGE_OPTIONS = [
  { value: "1d", label: "Last 24h" },
  { value: "7d", label: "Last 7d" },
  { value: "30d", label: "Last 30d" },
  { value: "90d", label: "Last 90d" },
] as const

type RangeValue = (typeof RANGE_OPTIONS)[number]["value"]

const ALL = "__all__"

type CostFiltersProps = {
  data: CostSummary
  initialRange: RangeValue
  initialTenant: string
  initialAgent: string
  initialModel: string
}

function downloadCsv(filename: string, rows: (string | number)[][]) {
  const csv = rows
    .map((row) =>
      row
        .map((cell) => {
          const s = String(cell ?? "")
          // Quote anything containing a comma, quote, or newline.
          if (/[",\n]/.test(s)) return `"${s.replace(/"/g, '""')}"`
          return s
        })
        .join(","),
    )
    .join("\n")
  const blob = new Blob([csv], { type: "text/csv;charset=utf-8" })
  const url = URL.createObjectURL(blob)
  const a = document.createElement("a")
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

export function CostFilters({
  data,
  initialRange,
  initialTenant,
  initialAgent,
  initialModel,
}: CostFiltersProps) {
  const router = useRouter()
  const searchParams = useSearchParams()
  const [pending, startTransition] = useTransition()

  const pushParam = (key: string, value: string) => {
    const next = new URLSearchParams(searchParams?.toString() ?? "")
    if (value === ALL || value === "") {
      next.delete(key)
    } else {
      next.set(key, value)
    }
    startTransition(() => {
      const qs = next.toString()
      router.replace(qs ? `?${qs}` : "?", { scroll: false })
    })
  }

  const handleExport = () => {
    const rows: (string | number)[][] = [
      ["dimension", "key", "label", "cost_usd"],
    ]
    for (const row of data.by_model) {
      rows.push(["model", row.model, row.model, row.cost_usd.toFixed(6)])
    }
    for (const row of data.by_agent) {
      rows.push(["agent", row.agent, row.agent, row.cost_usd.toFixed(6)])
    }
    for (const row of data.by_tenant) {
      rows.push([
        "tenant",
        row.tenant_id,
        row.tenant_name ?? row.tenant_id,
        row.cost_usd.toFixed(6),
      ])
    }
    for (const row of data.by_day) {
      rows.push(["day", row.date, row.date, row.cost_usd.toFixed(6)])
    }
    const today = new Date().toISOString().slice(0, 10)
    downloadCsv(`cost-${today}.csv`, rows)
  }

  // Build option lists from the current data so we never show stale
  // tenants or models that no longer exist.
  const tenantOptions = data.by_tenant.map((t) => ({
    value: t.tenant_id,
    label: t.tenant_name ?? t.tenant_id,
  }))
  const agentOptions = data.by_agent.map((a) => ({
    value: a.agent,
    label: a.agent,
  }))
  const modelOptions = data.by_model.map((m) => ({
    value: m.model,
    label: m.model,
  }))

  return (
    <div
      className="flex flex-wrap items-center gap-2"
      data-pending={pending ? "" : undefined}
    >
      <Select
        value={initialRange}
        onValueChange={(v) => pushParam("range", String(v))}
      >
        <SelectTrigger className="min-w-[8rem]">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {RANGE_OPTIONS.map((opt) => (
            <SelectItem key={opt.value} value={opt.value}>
              {opt.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <Select
        value={initialTenant || ALL}
        onValueChange={(v) => pushParam("tenant", String(v))}
      >
        <SelectTrigger className="min-w-[9rem]">
          <SelectValue placeholder="All tenants" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL}>All tenants</SelectItem>
          {tenantOptions.map((opt) => (
            <SelectItem key={opt.value} value={opt.value}>
              {opt.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <Select
        value={initialAgent || ALL}
        onValueChange={(v) => pushParam("agent", String(v))}
      >
        <SelectTrigger className="min-w-[9rem]">
          <SelectValue placeholder="All agents" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL}>All agents</SelectItem>
          {agentOptions.map((opt) => (
            <SelectItem key={opt.value} value={opt.value}>
              {opt.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <Select
        value={initialModel || ALL}
        onValueChange={(v) => pushParam("model", String(v))}
      >
        <SelectTrigger className="min-w-[9rem]">
          <SelectValue placeholder="All models" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL}>All models</SelectItem>
          {modelOptions.map((opt) => (
            <SelectItem key={opt.value} value={opt.value}>
              {opt.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <div className="ml-auto">
        <Button variant="outline" size="sm" onClick={handleExport}>
          <Download data-icon="inline-start" />
          Download CSV
        </Button>
      </div>
    </div>
  )
}

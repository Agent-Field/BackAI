// SPDX-License-Identifier: Apache-2.0

"use client"

import { TenantKpiTile } from "@/components/tenant-detail/kpi-tile"

import type { QueueSummary } from "@/lib/api"

// Four-tile KPI strip for the Queue page, straight from
// GET /api/v1/queues/summary. Same tile shape as Home / Tenant detail
// so the operator's eye reads it instantly. When the summary endpoint
// is degraded the tiles render em-dashes — structure stays visible at
// zero data.

interface QueueKpisProps {
  summary: QueueSummary | null
}

export function QueueKpis({ summary }: QueueKpisProps) {
  const fmt = (n: number | undefined) =>
    n === undefined ? "—" : n.toLocaleString()
  return (
    <div className="grid grid-cols-2 gap-stack md:grid-cols-4">
      <TenantKpiTile
        label="Pending"
        value={fmt(summary?.pending)}
        sublabel="waiting for a worker"
        status="idle"
      />
      <TenantKpiTile
        label="Running"
        value={fmt(summary?.running)}
        sublabel="in flight now"
        status="watch"
      />
      <TenantKpiTile
        label="Failed"
        value={fmt(summary?.failed)}
        sublabel="retryable + discarded"
        status={summary && summary.failed > 0 ? "act" : "ok"}
      />
      <TenantKpiTile
        label="Succeeded today"
        value={fmt(summary?.succeeded_today)}
        sublabel="completed since midnight"
        status="ok"
      />
    </div>
  )
}

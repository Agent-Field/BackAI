// SPDX-License-Identifier: Apache-2.0

"use client"

import { TenantKpiTile } from "@/components/tenant-detail/kpi-tile"

import type { NotificationStats } from "@/lib/api"

// Four-tile KPI strip for the Notifications page, from
// GET /api/v1/notifications/stats. Same tile shape as Home / Queue /
// Tenant detail so the operator's eye reads it instantly. When the stats
// endpoint is degraded the tiles render em-dashes — structure stays
// visible at zero data.

interface NotificationsKpisProps {
  stats: NotificationStats | null
}

export function NotificationsKpis({ stats }: NotificationsKpisProps) {
  const fmt = (n: number | undefined) => (n === undefined ? "—" : n.toLocaleString())
  const queued = stats?.by_status.queued
  const topAdapter = stats?.by_adapter.slice().sort((a, b) => b.count - a.count)[0]
  return (
    <div className="grid grid-cols-2 gap-stack md:grid-cols-4">
      <TenantKpiTile
        label="Sent today"
        value={fmt(stats?.sent_today)}
        sublabel="delivered since midnight"
        status="ok"
      />
      <TenantKpiTile
        label="Failed today"
        value={fmt(stats?.failed_today)}
        sublabel="adapter rejected or errored"
        status={stats && stats.failed_today > 0 ? "act" : "ok"}
      />
      <TenantKpiTile
        label="Queued"
        value={fmt(queued)}
        sublabel="waiting for the worker"
        status={queued && queued > 0 ? "watch" : "idle"}
      />
      <TenantKpiTile
        label="Top adapter"
        value={topAdapter ? topAdapter.adapter : "—"}
        sublabel={topAdapter ? `${topAdapter.count.toLocaleString()} handled` : "no traffic"}
        status="idle"
      />
    </div>
  )
}

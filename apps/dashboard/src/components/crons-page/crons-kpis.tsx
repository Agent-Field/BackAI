// SPDX-License-Identifier: Apache-2.0

"use client"

import { RelativeTime } from "@/components/ui/relative-time"

import { TenantKpiTile } from "@/components/tenant-detail/kpi-tile"

import type { Cron } from "@/lib/api"

import { formatRelativeTime, nextFireAt } from "@/lib/crons-page/derive"

// Four-tile KPI strip for the Crons page, derived from the same list the
// table renders. Same tile shape as Home / Queue so the operator's eye
// reads it instantly. At zero data the tiles show em-dashes / "—" and the
// structure stays visible.

interface CronsKpisProps {
  crons: Cron[]
  healthy: boolean
}

export function CronsKpis({ crons, healthy }: CronsKpisProps) {
  const active = crons.filter((c) => c.is_active).length
  const paused = crons.length - active
  const nextFire = nextFireAt(crons)
  const dash = (n: number) => (healthy ? n.toLocaleString() : "—")
  return (
    <div className="grid grid-cols-2 gap-stack md:grid-cols-4">
      <TenantKpiTile
        label="Schedules"
        value={healthy ? crons.length.toLocaleString() : "—"}
        sublabel="registered crons"
        status="idle"
      />
      <TenantKpiTile
        label="Active"
        value={dash(active)}
        sublabel="firing on schedule"
        status={active > 0 ? "ok" : "idle"}
      />
      <TenantKpiTile
        label="Paused"
        value={dash(paused)}
        sublabel="toggled off"
        status={paused > 0 ? "watch" : "ok"}
      />
      <TenantKpiTile
        label="Next fire"
        value={
          healthy ? (
            <RelativeTime iso={nextFire} format={formatRelativeTime} />
          ) : (
            "—"
          )
        }
        sublabel="soonest active tick"
        status="watch"
      />
    </div>
  )
}

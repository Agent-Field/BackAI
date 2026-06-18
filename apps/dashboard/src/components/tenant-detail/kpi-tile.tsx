// SPDX-License-Identifier: Apache-2.0

import { Sparkline } from "@/components/ui/sparkline"

// Compact KPI tile used in the tenant header. Same shape as Home /
// Cost tiles so the operator's eye reads it instantly. Sparkline +
// label optional.

interface TenantKpiTileProps {
  label: string
  value: string
  sublabel?: string
  sparkline?: number[]
  status?: "ok" | "watch" | "act" | "idle"
}

export function TenantKpiTile({
  label,
  value,
  sublabel,
  sparkline,
  status = "ok",
}: TenantKpiTileProps) {
  return (
    <div className="flex min-w-0 flex-col gap-tile rounded-md border bg-card px-row-x py-tile">
      <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
        {label}
      </span>
      <span className="text-body font-semibold tabular-nums text-foreground">
        {value}
      </span>
      {sparkline ? <Sparkline data={sparkline} status={status} height={24} /> : null}
      {sublabel ? (
        <span className="truncate text-meta text-muted-foreground">
          {sublabel}
        </span>
      ) : null}
    </div>
  )
}

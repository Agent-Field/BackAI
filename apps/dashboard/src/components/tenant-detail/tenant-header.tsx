// SPDX-License-Identifier: Apache-2.0

"use client"

import { Pause, Play } from "lucide-react"

import { Button } from "@/components/ui/button"
import { CopyButton } from "@/components/ui/copy-button"
import { RelativeTime } from "@/components/ui/relative-time"

import type { TenantDrilldown } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"
import {
  classifyKeyStatus,
  deriveAtRiskSignals,
  formatAge,
  formatBytes,
  formatCurrency,
  formatNumber,
  tenantStatusLabel,
  tenantStatusState,
} from "@/lib/tenant-detail/derive"

import { AtRiskAlert } from "./at-risk-alert"
import { TenantKpiTile } from "./kpi-tile"

// Sticky tenant header. Identity row + at-risk callouts + KPI strip +
// quick actions. Each block stays the same height across states so
// the layout doesn't jump when the operator switches tenants.

const DOT: Record<StatusState, string> = {
  ok: "bg-success",
  watch: "bg-warning",
  act: "bg-destructive",
  idle: "bg-muted-foreground/60",
}

interface TenantHeaderProps {
  drilldown: TenantDrilldown
  pendingAction: "suspend" | "reactivate" | null
  onSuspend: () => void
  onReactivate: () => void
}

export function TenantHeader({
  drilldown,
  pendingAction,
  onSuspend,
  onReactivate,
}: TenantHeaderProps) {
  const signals = deriveAtRiskSignals(drilldown)
  const state = tenantStatusState(drilldown, signals)
  const statusLabel = tenantStatusLabel(drilldown, state)
  const statusTone =
    state === "ok"
      ? "text-muted-foreground"
      : state === "watch"
        ? "text-warning font-medium"
        : "text-destructive font-medium"
  const activeKeys = drilldown.api_keys.filter(
    (k) => classifyKeyStatus(k) === "active",
  ).length
  const tenant = drilldown.tenant
  const suspended = Boolean(tenant.deleted_at)
  return (
    <section
      aria-label="Tenant header"
      className="sticky top-12 z-20 flex flex-col gap-stack rounded-md border bg-background/95 px-row-x py-row-y backdrop-blur supports-[backdrop-filter]:bg-background/85"
    >
      <header className="flex flex-wrap items-baseline gap-stack">
        <span
          aria-hidden
          className={`inline-block size-icon-dot rounded-pill ${DOT[state]}`}
        />
        <h1 className="min-w-0 truncate text-xl font-semibold tracking-tight text-foreground">
          {tenant.name || tenant.slug}
        </h1>
        <span className={`text-meta ${statusTone}`}>{statusLabel}</span>
        <span className="ml-auto flex items-center gap-stack text-meta text-muted-foreground">
          <span className="flex items-center gap-inline font-mono">
            {tenant.slug}
          </span>
          <span aria-hidden>·</span>
          <span className="flex items-center gap-inline font-mono">
            {tenant.id.slice(0, 8)}
            <CopyButton value={tenant.id} label="Copy tenant id" />
          </span>
          <span aria-hidden>·</span>
          <span title={tenant.created_at}>
            created <RelativeTime iso={tenant.created_at} format={formatAge} />
          </span>
        </span>
      </header>

      {signals.length > 0 ? (
        <div className="flex flex-col gap-tile">
          {signals.map((sig) => (
            <AtRiskAlert key={sig.id} signal={sig} />
          ))}
        </div>
      ) : null}

      <div className="grid grid-cols-2 gap-stack md:grid-cols-3 lg:grid-cols-5">
        <TenantKpiTile
          label="Cost 30d"
          value={formatCurrency(drilldown.usage.cost_usd_30d)}
          sparkline={drilldown.usage.cost_sparkline}
        />
        <TenantKpiTile
          label="Requests 30d"
          value={formatNumber(drilldown.usage.requests_30d)}
          sparkline={drilldown.usage.request_sparkline}
        />
        <TenantKpiTile
          label="Members"
          value={String(drilldown.members.length)}
          sublabel={`${drilldown.members.filter((m) => m.role === "owner").length} owner`}
        />
        <TenantKpiTile
          label="Keys"
          value={`${activeKeys}`}
          sublabel={`${drilldown.api_keys.length - activeKeys} revoked`}
        />
        <TenantKpiTile
          label="Storage"
          value={formatBytes(drilldown.usage.storage_bytes)}
          sublabel={`${drilldown.usage.secrets_count} secrets`}
        />
      </div>

      <div className="flex flex-wrap items-center justify-end gap-inline">
        {suspended ? (
          <Button
            type="button"
            size="sm"
            className="h-7 gap-inline text-meta"
            disabled={pendingAction !== null}
            onClick={onReactivate}
          >
            <Play className="size-3" aria-hidden />
            {pendingAction === "reactivate" ? "Reactivating…" : "Reactivate"}
          </Button>
        ) : (
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="h-7 gap-inline text-meta"
            disabled={pendingAction !== null}
            onClick={onSuspend}
          >
            <Pause className="size-3" aria-hidden />
            {pendingAction === "suspend" ? "Suspending…" : "Suspend"}
          </Button>
        )}
      </div>
    </section>
  )
}

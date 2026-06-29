// SPDX-License-Identifier: Apache-2.0

"use client"

import { Trash2 } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { CopyButton } from "@/components/ui/copy-button"
import { GaugeBar } from "@/components/ui/gauge-bar"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import { api } from "@/lib/api"
import type { APIKey, TenantDrilldown } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"
import {
  classifyKeyStatus,
  formatAge,
  formatCurrency,
  maskKey,
} from "@/lib/tenant-detail/derive"

// Keys list as Card rows. v1 supports Revoke; Rotate + Issue defer to
// v0.2 (Rotate exists server-side but needs the reveal flow Drawer
// brief calls out).

const DOT: Record<StatusState, string> = {
  ok: "bg-success",
  watch: "bg-warning",
  act: "bg-destructive",
  idle: "bg-muted-foreground/60",
}

interface KeysTabProps {
  drilldown: TenantDrilldown
  onMutated: () => Promise<void> | void
}

export function KeysTab({ drilldown, onMutated }: KeysTabProps) {
  const keys = drilldown.api_keys
  return (
    <ZoneCard>
      <ZoneCardHeader
        title="API keys"
        subtitle={`${keys.filter((k) => classifyKeyStatus(k) === "active").length} active · ${keys.filter((k) => classifyKeyStatus(k) !== "active").length} inactive`}
      />
      {keys.length === 0 ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          No API keys issued yet for this tenant.
        </p>
      ) : (
        <ul role="list" className="flex flex-col gap-stack p-row-x">
          {keys.map((key) => (
            <KeyCard key={key.id} apiKey={key} onMutated={onMutated} />
          ))}
        </ul>
      )}
    </ZoneCard>
  )
}

function KeyCard({
  apiKey,
  onMutated,
}: {
  apiKey: APIKey
  onMutated: () => Promise<void> | void
}) {
  const [pending, setPending] = useState(false)
  const status = classifyKeyStatus(apiKey)
  const tone: StatusState =
    status === "active" ? "ok" : status === "expired" ? "watch" : "idle"
  const cap = apiKey.budget_max_usd ?? 0
  const spent = apiKey.live_spend_usd ?? 0
  const showBudget = cap > 0

  const revoke = async () => {
    if (pending) return
    setPending(true)
    try {
      await api.admin.keys.revoke(apiKey.id)
      toast.success("Key revoked", {
        description: apiKey.name ?? apiKey.prefix,
      })
      await onMutated()
    } catch (err) {
      toast.error("Could not revoke key", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setPending(false)
    }
  }

  return (
    <li className="flex flex-col gap-tile rounded-md border bg-card px-row-x py-tile">
      <header className="flex flex-wrap items-center gap-stack">
        <span
          aria-hidden
          className={`inline-block size-icon-dot rounded-pill ${DOT[tone]}`}
        />
        <span className="min-w-0 flex-1 truncate text-body font-medium text-foreground">
          {apiKey.name ?? apiKey.prefix}
        </span>
        <span className="font-mono text-meta text-muted-foreground">
          {maskKey(apiKey.prefix)}
        </span>
        <CopyButton value={apiKey.prefix} label="Copy key prefix" />
        <span
          className={`text-meta ${
            status === "active"
              ? "text-muted-foreground"
              : "text-destructive font-medium"
          }`}
        >
          {status}
        </span>
      </header>

      <div className="grid grid-cols-2 gap-stack md:grid-cols-4 text-meta">
        <Meta label="Created" value={formatAge(apiKey.created_at)} />
        <Meta label="Last used" value={formatAge(apiKey.last_used_at)} />
        <Meta
          label="Rate limits"
          value={
            apiKey.rate_limit_rpm
              ? `${apiKey.rate_limit_rpm} rpm`
              : "default"
          }
          sublabel={
            apiKey.rate_limit_tpm
              ? `${apiKey.rate_limit_tpm.toLocaleString()} tpm`
              : undefined
          }
        />
        <Meta
          label="Budget"
          value={
            showBudget
              ? `${formatCurrency(spent)} / ${formatCurrency(cap)}`
              : "—"
          }
        />
      </div>

      {showBudget ? (
        <GaugeBar value={spent} max={cap} watchAt={70} actAt={90} />
      ) : null}

      {status === "active" ? (
        <footer className="flex items-center justify-end gap-inline">
          <Button
            type="button"
            size="sm"
            variant="ghost"
            className="h-7 gap-inline text-meta text-destructive hover:text-destructive"
            disabled={pending}
            onClick={revoke}
          >
            <Trash2 className="size-3" aria-hidden />
            {pending ? "Revoking…" : "Revoke"}
          </Button>
        </footer>
      ) : null}
    </li>
  )
}

function Meta({
  label,
  value,
  sublabel,
}: {
  label: string
  value: string
  sublabel?: string
}) {
  return (
    <div className="flex min-w-0 flex-col gap-tile-tight">
      <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
        {label}
      </span>
      <span className="truncate font-mono text-foreground" title={value}>
        {value}
      </span>
      {sublabel ? (
        <span className="truncate text-meta text-muted-foreground">
          {sublabel}
        </span>
      ) : null}
    </div>
  )
}

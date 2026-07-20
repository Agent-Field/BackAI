// SPDX-License-Identifier: Apache-2.0

"use client"

import { RotateCw, Trash2 } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { CopyButton } from "@/components/ui/copy-button"
import { RelativeTime } from "@/components/ui/relative-time"

import { api } from "@/lib/api"
import type { APIKey, IssuedAPIKey, Tenant } from "@/lib/api"
import { isOperatorKey, tenantLabel } from "@/lib/keys-page/derive"
import {
  classifyKeyStatus,
  formatAge,
  formatCurrency,
  maskKey,
} from "@/lib/tenant-detail/derive"

// Keys table — sticky header + one row per key. Rotate reveals the new
// one-time value via the shell's KeyRevealDialog; Revoke uses a two-step
// inline confirm. Degraded / empty states keep the column shell visible.

const KEY_ROW_COLUMNS =
  "grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)_minmax(0,1fr)_120px_100px_80px_80px_150px]"

interface KeysTableProps {
  keys: APIKey[]
  tenants: Tenant[]
  healthy: boolean
  onMutated: () => Promise<void> | void
  onRotated: (issued: IssuedAPIKey) => void
}

export function KeysTable({
  keys,
  tenants,
  healthy,
  onMutated,
  onRotated,
}: KeysTableProps) {
  return (
    <section
      aria-label="API keys table"
      className="flex min-h-0 flex-col rounded-md border bg-card"
    >
      <header
        className={`sticky top-0 z-10 grid items-center gap-stack border-b bg-card px-row-x py-tile-tight text-eyebrow uppercase tracking-wide text-muted-foreground ${KEY_ROW_COLUMNS}`}
      >
        <span>Key</span>
        <span>Tenant</span>
        <span>Scopes</span>
        <span className="text-right">Spend / Budget</span>
        <span className="text-right">Limits</span>
        <span className="text-right">Created</span>
        <span className="text-right">Last used</span>
        <span className="text-right tabular-nums normal-case">{keys.length}</span>
      </header>
      {!healthy ? (
        <DegradedRow />
      ) : keys.length === 0 ? (
        <EmptyRow />
      ) : (
        <ul role="list" className="flex flex-col divide-y">
          {keys.map((key) => (
            <KeyRow
              key={key.id}
              apiKey={key}
              tenants={tenants}
              onMutated={onMutated}
              onRotated={onRotated}
            />
          ))}
        </ul>
      )}
    </section>
  )
}

function KeyRow({
  apiKey,
  tenants,
  onMutated,
  onRotated,
}: {
  apiKey: APIKey
  tenants: Tenant[]
  onMutated: () => Promise<void> | void
  onRotated: (issued: IssuedAPIKey) => void
}) {
  const [phase, setPhase] = useState<
    "idle" | "confirm-revoke" | "revoking" | "rotating"
  >("idle")
  const status = classifyKeyStatus(apiKey)
  const operator = isOperatorKey(apiKey)
  const cap = apiKey.budget_max_usd ?? null
  const spend = apiKey.live_spend_usd ?? null

  const rotate = async () => {
    if (phase !== "idle") return
    setPhase("rotating")
    try {
      const issued = await api.admin.keys.rotate(apiKey.id)
      toast.success("Key rotated", { description: issued.prefix })
      onRotated(issued)
      await onMutated()
    } catch (err) {
      toast.error("Could not rotate key", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setPhase("idle")
    }
  }

  const revoke = async () => {
    if (phase === "revoking") return
    setPhase("revoking")
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
      setPhase("idle")
    }
  }

  return (
    <li
      className={`grid items-center gap-stack px-row-x py-row-y text-meta ${KEY_ROW_COLUMNS}`}
    >
      <div className="flex min-w-0 flex-col gap-tile-tight">
        <span className="flex min-w-0 items-center gap-inline">
          <span className="truncate text-body font-medium text-foreground">
            {apiKey.name ?? apiKey.prefix}
          </span>
          {operator ? <Badge variant="secondary">operator key</Badge> : null}
          {status !== "active" ? (
            <Badge variant="destructive">{status}</Badge>
          ) : null}
        </span>
        <span className="flex items-center gap-inline">
          <span className="truncate font-mono text-meta text-muted-foreground">
            {maskKey(apiKey.prefix)}
          </span>
          <CopyButton value={apiKey.prefix} label="Copy key prefix" />
        </span>
      </div>
      <span
        className="truncate text-muted-foreground"
        title={apiKey.tenant_id}
      >
        {tenantLabel(apiKey.tenant_id, tenants)}
      </span>
      <span className="flex min-w-0 flex-wrap items-center gap-inline">
        {apiKey.scopes.length === 0 ? (
          <span className="text-muted-foreground">—</span>
        ) : (
          apiKey.scopes.map((scope) => (
            <Badge key={scope} variant="outline" className="font-mono">
              {scope}
            </Badge>
          ))
        )}
      </span>
      <span className="text-right font-mono tabular-nums text-muted-foreground">
        {spend === null ? "—" : formatCurrency(spend)}
        {" / "}
        {cap === null || cap === 0 ? "∞" : formatCurrency(cap)}
      </span>
      <span className="text-right font-mono tabular-nums text-muted-foreground">
        {apiKey.rate_limit_rpm ? `${apiKey.rate_limit_rpm} rpm` : "default"}
        {apiKey.rate_limit_tpm ? (
          <>
            <br />
            {apiKey.rate_limit_tpm.toLocaleString()} tpm
          </>
        ) : null}
      </span>
      <span
        className="text-right font-mono tabular-nums text-muted-foreground"
        title={apiKey.created_at}
      >
        <RelativeTime iso={apiKey.created_at} format={formatAge} />
      </span>
      <span
        className="text-right font-mono tabular-nums text-muted-foreground"
        title={apiKey.last_used_at ?? undefined}
      >
        <RelativeTime iso={apiKey.last_used_at} format={formatAge} />
      </span>
      <div className="flex items-center justify-end gap-inline">
        {status === "active" ? (
          phase === "confirm-revoke" ? (
            <>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                className="h-7 text-meta"
                onClick={() => setPhase("idle")}
              >
                Cancel
              </Button>
              <Button
                type="button"
                size="sm"
                variant="destructive"
                className="h-7 text-meta"
                onClick={revoke}
              >
                Confirm
              </Button>
            </>
          ) : (
            <>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                className="h-7 gap-inline text-meta"
                disabled={phase !== "idle"}
                onClick={rotate}
              >
                <RotateCw className="size-3" aria-hidden />
                {phase === "rotating" ? "Rotating…" : "Rotate"}
              </Button>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                className="h-7 gap-inline text-meta text-destructive hover:text-destructive"
                disabled={phase !== "idle"}
                onClick={() => setPhase("confirm-revoke")}
              >
                <Trash2 className="size-3" aria-hidden />
                {phase === "revoking" ? "Revoking…" : "Revoke"}
              </Button>
            </>
          )
        ) : null}
      </div>
    </li>
  )
}

function EmptyRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">No API keys match the current filter.</p>
      <p className="max-w-md text-meta text-muted-foreground">
        Issue a key with the button above, or mint one from the Home
        quick-actions rail.
      </p>
    </div>
  )
}

function DegradedRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">Key list unavailable.</p>
      <p className="max-w-md text-meta text-muted-foreground">
        The runtime did not return an API key list. Check the Health page
        for a database probe, then retry.
      </p>
    </div>
  )
}

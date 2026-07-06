// SPDX-License-Identifier: Apache-2.0

"use client"

import { RefreshCw } from "lucide-react"
import { useCallback, useState } from "react"

import { Button } from "@/components/ui/button"

import { api } from "@/lib/api"
import type { IntegrationsSnapshot } from "@/lib/integrations-page/data"

import { IntegrationSlotCard } from "./integration-slot-card"

// IntegrationsShell owns the snapshot (one slot per adapter that accepts
// credentials) and re-fetches the whole list after any write, so masked
// status stays consistent across cards. Each slot card owns its own form
// and calls back via onSaved.

interface IntegrationsShellProps {
  initialSnapshot: IntegrationsSnapshot
}

export function IntegrationsShell({ initialSnapshot }: IntegrationsShellProps) {
  const [snapshot, setSnapshot] = useState<IntegrationsSnapshot>(initialSnapshot)
  const [refreshing, setRefreshing] = useState(false)

  const refresh = useCallback(async (manual = false) => {
    if (manual) setRefreshing(true)
    const fetchedAt = new Date().toISOString()
    try {
      const status = await api.admin.integrations.list().catch(() => null)
      setSnapshot((prev) => ({
        slots: status?.integrations ?? prev.slots,
        fetchedAt,
        ok: status !== null,
      }))
    } finally {
      if (manual) setRefreshing(false)
    }
  }, [])

  const { slots } = snapshot

  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <header className="flex items-baseline justify-between">
        <div className="flex flex-col gap-tile-tight">
          <h1 className="text-xl font-semibold tracking-tight text-foreground">
            Integrations
          </h1>
          <p className="max-w-prose text-meta text-muted-foreground">
            Adapter credentials for the pluggable slots — email (Resend),
            Slack, Twilio SMS, push (FCM), and the remote storage / secrets /
            LLM adapters. Values are stored encrypted server-side and never
            shown again; each field reports whether it&apos;s set and a masked
            fingerprint.
          </p>
        </div>
        <div className="flex items-center gap-stack text-meta text-muted-foreground">
          <span className="tabular-nums">updated {ageLabel(snapshot.fetchedAt)}</span>
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() => refresh(true)}
            disabled={refreshing}
            className="h-7 gap-inline text-meta"
          >
            <RefreshCw
              className={`size-3.5 ${refreshing ? "animate-spin" : ""}`}
              aria-hidden
            />
            Refresh
          </Button>
        </div>
      </header>

      {!snapshot.ok ? (
        <div className="rounded-md border bg-card/40 px-row-x py-tile text-meta text-muted-foreground">
          Could not reach the runtime integrations endpoint. Start the backend
          with af-stack dev, then refresh.
        </div>
      ) : slots.length === 0 ? (
        <div className="rounded-md border bg-card/40 px-row-x py-tile text-meta text-muted-foreground">
          No configurable integration slots are exposed by the runtime.
        </div>
      ) : (
        slots.map((slot) => (
          <IntegrationSlotCard
            key={slot.slot}
            slot={slot}
            onSaved={() => refresh()}
          />
        ))
      )}
    </div>
  )
}

function ageLabel(fetchedAt: string): string {
  const ageSec = Math.max(
    0,
    Math.round((Date.now() - Date.parse(fetchedAt)) / 1000),
  )
  return ageSec < 5 ? "now" : `${ageSec}s ago`
}

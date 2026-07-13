// SPDX-License-Identifier: Apache-2.0

"use client"

import { RefreshCw } from "lucide-react"
import Link from "next/link"
import { useCallback, useState } from "react"

import { Button } from "@/components/ui/button"

import { api } from "@/lib/api"
import type { IntegrationsSnapshot } from "@/lib/integrations-page/data"

import { IntegrationSlotCard } from "./integration-slot-card"

// IntegrationsShell owns the snapshot (one slot per adapter that accepts
// credentials) and re-fetches the whole list after any write, so masked
// status stays consistent across cards. Each slot card owns its own form
// and calls back via onSaved.

// Per-capability copy for the focused single-slot pages reached from the
// sidebar sub-nav. The key is the sub-nav slug: a slot name, or "oauth"
// which fans out to every oauth_* provider slot.
const CAPABILITY_COPY: Record<string, { title: string; description: string }> = {
  browser: {
    title: "Browser",
    description:
      "Give agents a real browser: point at the self-hosted sidecar, paste a Steel or Browserbase API key, or use any CDP/Playwright websocket endpoint.",
  },
  sandbox: {
    title: "Sandbox",
    description:
      "Credentials for sandboxed code execution — an E2B API key for hosted microVMs, or the URL + token of a remote sandbox sidecar.",
  },
  llm: {
    title: "LLM",
    description: "Remote LLM-gateway adapter endpoint + bearer token.",
  },
  notifications: {
    title: "Notifications",
    description:
      "Delivery channels for notifications: Resend email, Slack webhook, Twilio SMS, or Firebase push.",
  },
  storage: {
    title: "Storage",
    description: "Remote object-storage adapter endpoint + bearer token.",
  },
  secrets: {
    title: "Secrets",
    description: "Remote secrets-vault adapter endpoint + bearer token.",
  },
  oauth: {
    title: "OAuth providers",
    description:
      "Client credentials for sign-in-with providers used by OAuth-on-behalf-of-user flows.",
  },
}

// matchesFilter maps a sub-nav slug onto slot names.
function matchesFilter(slotName: string, filter: string): boolean {
  if (filter === "oauth") return slotName.startsWith("oauth_")
  return slotName === filter
}

interface IntegrationsShellProps {
  initialSnapshot: IntegrationsSnapshot
  /** When set, render only the matching capability's slot(s) with focused
   *  header copy — the single-form pages behind the sidebar sub-nav. */
  filter?: string
}

export function IntegrationsShell({ initialSnapshot, filter }: IntegrationsShellProps) {
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

  const slots = filter
    ? snapshot.slots.filter((s) => matchesFilter(s.slot, filter))
    : snapshot.slots
  const copy = filter ? CAPABILITY_COPY[filter] : undefined

  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <header className="flex items-baseline justify-between">
        <div className="flex flex-col gap-tile-tight">
          <h1 className="text-xl font-semibold tracking-tight text-foreground">
            {copy?.title ?? "Integrations"}
          </h1>
          <p className="max-w-prose text-meta text-muted-foreground">
            {copy?.description ??
              "Adapter credentials for the pluggable slots. Values are stored encrypted server-side and never shown again; each field reports whether it's set and a masked fingerprint."}
          </p>
          {filter ? (
            <Link
              href="/platform/integrations"
              className="text-meta text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
            >
              ← All integrations
            </Link>
          ) : null}
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
            <RefreshCw className={`size-3.5 ${refreshing ? "animate-spin" : ""}`} aria-hidden />
            Refresh
          </Button>
        </div>
      </header>

      {!snapshot.ok ? (
        <div className="rounded-md border bg-card/40 px-row-x py-tile text-meta text-muted-foreground">
          Could not reach the runtime integrations endpoint. Start the backend with af-stack dev,
          then refresh.
        </div>
      ) : slots.length === 0 ? (
        <div className="rounded-md border bg-card/40 px-row-x py-tile text-meta text-muted-foreground">
          No configurable integration slots are exposed by the runtime.
        </div>
      ) : (
        slots.map((slot) => (
          <IntegrationSlotCard key={slot.slot} slot={slot} onSaved={() => refresh()} />
        ))
      )}
    </div>
  )
}

function ageLabel(fetchedAt: string): string {
  const ageSec = Math.max(0, Math.round((Date.now() - Date.parse(fetchedAt)) / 1000))
  return ageSec < 5 ? "now" : `${ageSec}s ago`
}

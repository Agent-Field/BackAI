// SPDX-License-Identifier: Apache-2.0

"use client"

import { RefreshCw } from "lucide-react"
import { useCallback, useState } from "react"

import { Button } from "@/components/ui/button"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import { api } from "@/lib/api"
import type { FeatureFlag } from "@/lib/api"
import { sortFlags } from "@/lib/flags-page/derive"
import type { FlagsSnapshot } from "@/lib/flags-page/data"

import { FlagRow } from "./flag-row"

// FlagsShell owns the flag list and re-sorts it whenever a row reports a
// change, so an enabled flag hops to the top without a full round trip.
// Refresh re-lists from the runtime (authoritative source/updated_at). The
// list endpoint always returns the built-in defaults even with no flag
// store, so the empty state only fires when the runtime is unreachable.

interface FlagsShellProps {
  initialSnapshot: FlagsSnapshot
}

export function FlagsShell({ initialSnapshot }: FlagsShellProps) {
  const [snapshot, setSnapshot] = useState<FlagsSnapshot>(initialSnapshot)
  const [refreshing, setRefreshing] = useState(false)

  const refresh = useCallback(async () => {
    setRefreshing(true)
    const fetchedAt = new Date().toISOString()
    try {
      const res = await api.config.flags.list().catch(() => null)
      setSnapshot((prev) => ({
        flags: res ? sortFlags(res.flags) : prev.flags,
        fetchedAt,
        ok: res !== null,
      }))
    } finally {
      setRefreshing(false)
    }
  }, [])

  // Merge one flag's post-write state back in and re-sort. We keep the
  // fetchedAt stamp — this is a targeted update, not a fresh list.
  const applyChange = useCallback((updated: FeatureFlag) => {
    setSnapshot((prev) => ({
      ...prev,
      flags: sortFlags(prev.flags.map((f) => (f.key === updated.key ? updated : f))),
    }))
  }, [])

  const { flags } = snapshot
  const enabledCount = flags.filter((f) => f.enabled).length

  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <header className="flex items-baseline justify-between">
        <div className="flex flex-col gap-tile-tight">
          <h1 className="text-xl font-semibold tracking-tight text-foreground">Feature flags</h1>
          <p className="max-w-prose text-meta text-muted-foreground">
            Toggle experimental and opt-in behaviours for this tenant. Each flag ships with a
            built-in default; flipping one persists an operator override in the flag store. Changes
            take effect on the next render — no redeploy.
          </p>
        </div>
        <div className="flex items-center gap-stack text-meta text-muted-foreground">
          <span className="tabular-nums">updated {ageLabel(snapshot.fetchedAt)}</span>
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={refresh}
            disabled={refreshing}
            className="h-7 gap-inline text-meta"
          >
            <RefreshCw className={`size-3.5 ${refreshing ? "animate-spin" : ""}`} aria-hidden />
            Refresh
          </Button>
        </div>
      </header>

      <ZoneCard>
        <ZoneCardHeader
          title="Flags"
          subtitle={
            snapshot.ok
              ? `${flags.length} flag${flags.length === 1 ? "" : "s"} · ${enabledCount} enabled`
              : "runtime unreachable"
          }
        />
        {flags.length === 0 ? (
          <p className="px-row-x py-tile text-meta text-muted-foreground">
            {snapshot.ok
              ? "The runtime returned no feature flags."
              : "Could not reach the runtime config endpoint. Start the backend with af-stack dev, then refresh."}
          </p>
        ) : (
          <ul role="list" className="divide-y">
            {flags.map((flag) => (
              <FlagRow key={flag.key} flag={flag} onChanged={applyChange} />
            ))}
          </ul>
        )}
      </ZoneCard>
    </div>
  )
}

function ageLabel(fetchedAt: string): string {
  const ageSec = Math.max(0, Math.round((Date.now() - Date.parse(fetchedAt)) / 1000))
  return ageSec < 5 ? "now" : `${ageSec}s ago`
}

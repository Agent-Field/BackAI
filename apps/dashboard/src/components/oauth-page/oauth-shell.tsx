// SPDX-License-Identifier: Apache-2.0

"use client"

import { RefreshCw } from "lucide-react"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useCallback, useMemo, useState } from "react"

import { Button } from "@/components/ui/button"

import { api } from "@/lib/api"
import {
  DEFAULT_PROVIDER_FILTER,
  type OAuthProviderFilter,
  type OAuthSnapshot,
} from "@/lib/oauth-page/types"

import { ConnectionsCard } from "./connections-card"
import { ProvidersCard } from "./providers-card"
import { RefreshHistoryCard } from "./refresh-history-card"

// OAuthShell owns the snapshot (providers + live connections + token-
// refresh feed) and re-fetches the whole thing after any write or filter
// change, so all three zones stay consistent. The ?provider filter is
// URL-persistent and narrows only the refresh feed; changing it triggers
// the refetch directly (no reactive effect), mirroring the server-side
// fetch. First paint is server-rendered — see the page.

interface OAuthShellProps {
  initialSnapshot: OAuthSnapshot
}

export function OAuthShell({ initialSnapshot }: OAuthShellProps) {
  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()

  const [snapshot, setSnapshot] = useState<OAuthSnapshot>(initialSnapshot)
  const [refreshing, setRefreshing] = useState(false)

  const provider = useMemo<OAuthProviderFilter>(
    () => searchParams.get("provider") ?? DEFAULT_PROVIDER_FILTER,
    [searchParams],
  )

  const refresh = useCallback(async (providerFilter: OAuthProviderFilter, manual = false) => {
    if (manual) setRefreshing(true)
    const fetchedAt = new Date().toISOString()
    const [providers, connections, history] = await Promise.all([
      api.oauth.providers().catch(() => null),
      api.oauth.connections().catch(() => null),
      api.oauth
        .refreshHistory({ provider: providerFilter || undefined, limit: 100 })
        .catch(() => null),
    ])
    setSnapshot({
      providers: providers?.providers ?? [],
      connections: connections?.connections ?? [],
      events: history?.events ?? [],
      fetchedAt,
      healthy: providers !== null || connections !== null,
    })
    if (manual) setRefreshing(false)
  }, [])

  const setProvider = useCallback(
    (next: OAuthProviderFilter) => {
      const params = new URLSearchParams(searchParams.toString())
      if (next === DEFAULT_PROVIDER_FILTER) params.delete("provider")
      else params.set("provider", next)
      const qs = params.toString()
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
      // Refetch in the handler rather than reacting to the URL change so
      // the new feed lands immediately without a state-in-effect hop.
      void refresh(next)
    },
    [pathname, refresh, router, searchParams],
  )

  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <Header
        fetchedAt={snapshot.fetchedAt}
        refreshing={refreshing}
        onRefresh={() => refresh(provider, true)}
      />
      <ProvidersCard
        providers={snapshot.providers}
        connections={snapshot.connections}
        healthy={snapshot.healthy}
        onMutated={() => refresh(provider, false)}
      />
      <ConnectionsCard connections={snapshot.connections} healthy={snapshot.healthy} />
      <RefreshHistoryCard
        events={snapshot.events}
        providers={snapshot.providers}
        provider={provider}
        loading={refreshing}
        healthy={snapshot.healthy}
        onProviderChange={setProvider}
      />
    </div>
  )
}

function Header({
  fetchedAt,
  refreshing,
  onRefresh,
}: {
  fetchedAt: string
  refreshing: boolean
  onRefresh: () => void
}) {
  return (
    <header className="flex items-baseline justify-between">
      <div className="flex flex-col gap-tile-tight">
        <h1 className="text-xl font-semibold tracking-tight text-foreground">OAuth connections</h1>
        <p className="max-w-prose text-meta text-muted-foreground">
          Third-party OAuth providers, the tenant tokens currently linked, and the background
          token-refresh log. Connect a provider to start its consent flow; disconnect to revoke the
          stored grant.
        </p>
      </div>
      <div className="flex items-center gap-stack text-meta text-muted-foreground">
        <span className="tabular-nums">updated {ageLabel(fetchedAt)}</span>
        <Button
          type="button"
          size="sm"
          variant="outline"
          onClick={onRefresh}
          disabled={refreshing}
          className="h-7 gap-inline text-meta"
        >
          <RefreshCw className={`size-3.5 ${refreshing ? "animate-spin" : ""}`} aria-hidden />
          Refresh
        </Button>
      </div>
    </header>
  )
}

// Module-level so the render stays pure — the clock read happens outside
// the component body (mirrors the integrations shell).
function ageLabel(fetchedAt: string): string {
  const ageSec = Math.max(0, Math.round((Date.now() - Date.parse(fetchedAt)) / 1000))
  return ageSec < 5 ? "now" : `${ageSec}s ago`
}

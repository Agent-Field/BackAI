// SPDX-License-Identifier: Apache-2.0

"use client"

import { RefreshCw } from "lucide-react"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useCallback, useEffect, useMemo, useState } from "react"

import { Button } from "@/components/ui/button"

import { api } from "@/lib/api"
import { isWebhookDirectionFilter } from "@/lib/webhooks-page/derive"
import type {
  WebhookDirectionFilter,
  WebhooksSnapshot,
} from "@/lib/webhooks-page/types"
import { DEFAULT_DIRECTION_FILTER } from "@/lib/webhooks-page/types"
import { polling } from "@/lib/theme"

import { DeliveriesCard } from "./deliveries-card"
import { EndpointsCard } from "./endpoints-card"

// WebhooksShell owns:
//   - URL-persistent ?direction filter for the delivery feed
//   - 5s polling tick — deliveries land continuously while the
//     operator watches
//   - Refresh after create/delete/retry mutations

interface WebhooksShellProps {
  initialSnapshot: WebhooksSnapshot
}

export function WebhooksShell({ initialSnapshot }: WebhooksShellProps) {
  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()

  const [snapshot, setSnapshot] = useState<WebhooksSnapshot>(initialSnapshot)
  const [refreshing, setRefreshing] = useState(false)

  const direction = useMemo<WebhookDirectionFilter>(
    () => parseDirection(searchParams.get("direction")),
    [searchParams],
  )

  const setDirection = useCallback(
    (next: WebhookDirectionFilter) => {
      const params = new URLSearchParams(searchParams.toString())
      if (next === DEFAULT_DIRECTION_FILTER) params.delete("direction")
      else params.set("direction", next)
      const qs = params.toString()
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
    },
    [pathname, router, searchParams],
  )

  const refresh = useCallback(
    async (directionFilter: WebhookDirectionFilter, manual = false) => {
      if (manual) setRefreshing(true)
      const fetchedAt = new Date().toISOString()
      const [endpoints, deliveries] = await Promise.all([
        api.webhooks.endpoints().catch(() => null),
        api.webhooks
          .deliveries({
            limit: 100,
            direction:
              directionFilter === "all" ? undefined : directionFilter,
          })
          .catch(() => null),
      ])
      setSnapshot({
        endpoints: endpoints?.endpoints ?? [],
        deliveries: deliveries?.deliveries ?? [],
        total: deliveries?.total ?? 0,
        hasMore: deliveries?.has_more ?? false,
        fetchedAt,
        healthy: endpoints !== null || deliveries !== null,
      })
      if (manual) setRefreshing(false)
    },
    [],
  )

  useEffect(() => {
    void refresh(direction)
  }, [direction, refresh])

  useEffect(() => {
    let cancelled = false
    const id = setInterval(() => {
      if (!cancelled) void refresh(direction)
    }, polling.home)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [direction, refresh])

  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <Header
        fetchedAt={snapshot.fetchedAt}
        refreshing={refreshing}
        onRefresh={() => refresh(direction, true)}
      />
      <EndpointsCard
        endpoints={snapshot.endpoints}
        healthy={snapshot.healthy}
        onMutated={() => refresh(direction, false)}
      />
      <DeliveriesCard
        deliveries={snapshot.deliveries}
        total={snapshot.total}
        direction={direction}
        loading={refreshing}
        healthy={snapshot.healthy}
        onDirectionChange={setDirection}
        onMutated={() => refresh(direction, false)}
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
  const ageSec = Math.max(0, Math.round((Date.now() - Date.parse(fetchedAt)) / 1000))
  const ageLabel = ageSec < 5 ? "now" : `${ageSec}s ago`
  return (
    <header className="flex items-baseline justify-between">
      <div className="flex flex-col gap-tile-tight">
        <h1 className="text-xl font-semibold tracking-tight text-foreground">
          Webhooks
        </h1>
        <p className="text-meta text-muted-foreground">
          Inbound endpoints and the unified delivery feed, newest first.
        </p>
      </div>
      <div className="flex items-center gap-stack text-meta text-muted-foreground">
        <span className="tabular-nums">updated {ageLabel}</span>
        <Button
          type="button"
          size="sm"
          variant="outline"
          onClick={onRefresh}
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
  )
}

function parseDirection(raw: string | null): WebhookDirectionFilter {
  if (raw && isWebhookDirectionFilter(raw)) return raw
  return DEFAULT_DIRECTION_FILTER
}

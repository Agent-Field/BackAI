// SPDX-License-Identifier: Apache-2.0

"use client"

import { RefreshCw, Send } from "lucide-react"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useCallback, useEffect, useMemo, useState } from "react"

import { Button } from "@/components/ui/button"

import { api } from "@/lib/api"
import {
  isNotificationKindFilter,
  isNotificationStatusFilter,
} from "@/lib/notifications-page/derive"
import type {
  NotificationKindFilter,
  NotificationStatusFilter,
  NotificationsSnapshot,
} from "@/lib/notifications-page/types"
import { DEFAULT_KIND_FILTER, DEFAULT_STATUS_FILTER } from "@/lib/notifications-page/types"
import { polling } from "@/lib/theme"

import { ChannelsCard } from "./channels-card"
import { MutesCard } from "./mutes-card"
import { NotificationsFeed } from "./notifications-feed"
import { NotificationsKpis } from "./notifications-kpis"
import { SendTestDialog } from "./send-test-dialog"

// NotificationsShell owns:
//   - URL-persistent ?status and ?kind filters (server-side filters on
//     the outbox list)
//   - 5s polling tick — the outbox drains continuously while the operator
//     watches queued rows flip to sent/failed
//   - Refresh after send-test / mute mutations so the feed + KPIs update

interface NotificationsShellProps {
  initialSnapshot: NotificationsSnapshot
}

export function NotificationsShell({ initialSnapshot }: NotificationsShellProps) {
  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()

  const [snapshot, setSnapshot] = useState<NotificationsSnapshot>(initialSnapshot)
  const [refreshing, setRefreshing] = useState(false)
  const [sending, setSending] = useState(false)

  const status = useMemo<NotificationStatusFilter>(
    () => parseStatus(searchParams.get("status")),
    [searchParams],
  )
  const kind = useMemo<NotificationKindFilter>(
    () => parseKind(searchParams.get("kind")),
    [searchParams],
  )

  const refresh = useCallback(
    async (
      statusFilter: NotificationStatusFilter,
      kindFilter: NotificationKindFilter,
      manual = false,
    ) => {
      if (manual) setRefreshing(true)
      const fetchedAt = new Date().toISOString()
      const [stats, list, mutes, channels] = await Promise.all([
        api.notifications.stats().catch(() => null),
        api.notifications
          .list({
            limit: 100,
            status: statusFilter === "all" ? undefined : statusFilter,
            kind: kindFilter === "all" ? undefined : kindFilter,
          })
          .catch(() => null),
        api.notifications.mutes.list().catch(() => null),
        api.notifications.channels.list().catch(() => null),
      ])
      setSnapshot({
        stats,
        notifications: list?.notifications ?? [],
        total: list?.total ?? 0,
        hasMore: list?.has_more ?? false,
        mutes: mutes?.mutes ?? [],
        channels: channels?.channels ?? [],
        fetchedAt,
        healthy: stats !== null || list !== null || mutes !== null || channels !== null,
      })
      if (manual) setRefreshing(false)
    },
    [],
  )

  // Filter changes drive the refetch from the change handler (not a
  // reactive effect): update the URL, then pull the newly-filtered slice.
  // The server-rendered initialSnapshot already matches the URL filter on
  // first paint, so no mount refetch is needed.
  const setStatus = useCallback(
    (next: NotificationStatusFilter) => {
      const params = new URLSearchParams(searchParams.toString())
      if (next === DEFAULT_STATUS_FILTER) params.delete("status")
      else params.set("status", next)
      const qs = params.toString()
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
      void refresh(next, kind)
    },
    [pathname, router, searchParams, kind, refresh],
  )

  const setKind = useCallback(
    (next: NotificationKindFilter) => {
      const params = new URLSearchParams(searchParams.toString())
      if (next === DEFAULT_KIND_FILTER) params.delete("kind")
      else params.set("kind", next)
      const qs = params.toString()
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
      void refresh(status, next)
    },
    [pathname, router, searchParams, status, refresh],
  )

  useEffect(() => {
    let cancelled = false
    const id = setInterval(() => {
      if (!cancelled) void refresh(status, kind)
    }, polling.home)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [status, kind, refresh])

  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <Header
        fetchedAt={snapshot.fetchedAt}
        refreshing={refreshing}
        onRefresh={() => refresh(status, kind, true)}
        onSendTest={() => setSending(true)}
      />
      <NotificationsKpis stats={snapshot.stats} />
      <NotificationsFeed
        notifications={snapshot.notifications}
        total={snapshot.total}
        status={status}
        kind={kind}
        loading={refreshing}
        healthy={snapshot.healthy}
        onStatusChange={setStatus}
        onKindChange={setKind}
      />
      <ChannelsCard channels={snapshot.channels} healthy={snapshot.healthy} />
      <MutesCard
        mutes={snapshot.mutes}
        healthy={snapshot.healthy}
        onMutated={() => refresh(status, kind, false)}
      />
      <SendTestDialog
        open={sending}
        onClose={() => setSending(false)}
        onSent={() => refresh(status, kind, false)}
      />
    </div>
  )
}

function Header({
  fetchedAt,
  refreshing,
  onRefresh,
  onSendTest,
}: {
  fetchedAt: string
  refreshing: boolean
  onRefresh: () => void
  onSendTest: () => void
}) {
  // Tick the "updated Xs ago" label off a 1s interval rather than reading
  // the clock during render — keeps the component render-pure.
  const [ageLabel, setAgeLabel] = useState("now")
  useEffect(() => {
    const id = setInterval(() => {
      const ageSec = Math.max(0, Math.round((Date.now() - Date.parse(fetchedAt)) / 1000))
      setAgeLabel(ageSec < 5 ? "now" : `${ageSec}s ago`)
    }, 1000)
    return () => clearInterval(id)
  }, [fetchedAt])
  return (
    <header className="flex items-baseline justify-between">
      <div className="flex flex-col gap-tile-tight">
        <h1 className="text-xl font-semibold tracking-tight text-foreground">Notifications</h1>
        <p className="text-meta text-muted-foreground">
          The outbox the worker drains through the configured adapter, plus channels and mute
          patterns.
        </p>
      </div>
      <div className="flex items-center gap-stack text-meta text-muted-foreground">
        <span className="tabular-nums">updated {ageLabel}</span>
        <Button
          type="button"
          size="sm"
          variant="outline"
          onClick={onSendTest}
          className="h-7 gap-inline text-meta"
        >
          <Send className="size-3.5" aria-hidden />
          Send test
        </Button>
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

function parseStatus(raw: string | null): NotificationStatusFilter {
  if (raw && isNotificationStatusFilter(raw)) return raw
  return DEFAULT_STATUS_FILTER
}

function parseKind(raw: string | null): NotificationKindFilter {
  if (raw && isNotificationKindFilter(raw)) return raw
  return DEFAULT_KIND_FILTER
}

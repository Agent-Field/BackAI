// Notifications — outbox stats + recent delivery history.
//
// Server component pre-fetches the stats snapshot and the first page of
// notifications so the table doesn't flash empty on first paint. Both
// fetches are independent and either failing alone shouldn't blank the
// page, so we resolve with Promise.allSettled and let the client view
// degrade gracefully on whichever piece is missing.

import { Bell } from "lucide-react"

import { PageHeader } from "@/components/layout/page-header"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import {
  api,
  type NotificationList,
  type NotificationStats,
} from "@/lib/api"

import { NotificationsView } from "./_components/notifications-view"

export const dynamic = "force-dynamic"

type Snapshot = {
  stats: NotificationStats | null
  list: NotificationList | null
  error: string | null
}

async function loadSnapshot(): Promise<Snapshot> {
  const [statsResult, listResult] = await Promise.allSettled([
    api.notifications.stats(),
    api.notifications.list({ limit: 50 }),
  ])
  let error: string | null = null
  if (statsResult.status === "rejected" && listResult.status === "rejected") {
    const reason = statsResult.reason
    error =
      reason instanceof Error
        ? reason.message
        : "Notifications module unreachable"
  }
  return {
    stats: statsResult.status === "fulfilled" ? statsResult.value : null,
    list: listResult.status === "fulfilled" ? listResult.value : null,
    error,
  }
}

export default async function NotificationsPage() {
  const { stats, list, error } = await loadSnapshot()

  if (stats === null && list === null) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader
          title="Notifications"
          description="Outbox-style notifications. Queue an email, watch it cascade through the worker, and see the provider message id once delivered."
        />
        <Empty className="border-dashed">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Bell />
            </EmptyMedia>
            <EmptyTitle>Notifications module unreachable</EmptyTitle>
            <EmptyDescription>
              {error
                ? error
                : "Enable the notifications module in main.go and restart the runtime to see deliveries here."}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Notifications"
        description="Outbox-style notifications. Queue an email, watch it cascade through the worker, and see the provider message id once delivered."
      />
      <NotificationsView
        initialStats={stats}
        initialList={list}
        initialError={error}
      />
    </div>
  )
}

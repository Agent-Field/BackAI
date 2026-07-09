// SPDX-License-Identifier: Apache-2.0

import { NotificationsShell } from "@/components/notifications-page/notifications-shell"
import { fetchNotificationsSnapshot } from "@/lib/notifications-page/data"
import {
  isNotificationKindFilter,
  isNotificationStatusFilter,
} from "@/lib/notifications-page/derive"
import type {
  NotificationKindFilter,
  NotificationStatusFilter,
} from "@/lib/notifications-page/types"

// Server-rendered first paint. Shell takes over for live polling +
// URL-driven status/kind filters + send-test and mute dialogs.

export const dynamic = "force-dynamic"

export default async function NotificationsPage({
  searchParams,
}: {
  searchParams: Promise<{ status?: string; kind?: string }>
}) {
  const sp = await searchParams
  const status: NotificationStatusFilter =
    sp.status && isNotificationStatusFilter(sp.status) ? sp.status : "all"
  const kind: NotificationKindFilter =
    sp.kind && isNotificationKindFilter(sp.kind) ? sp.kind : "all"
  const snapshot = await fetchNotificationsSnapshot(status, kind)
  return <NotificationsShell initialSnapshot={snapshot} />
}

// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"

import type {
  NotificationKindFilter,
  NotificationStatusFilter,
  NotificationsSnapshot,
} from "./types"

// Server-side initial fetch for the Notifications page. Four round trips
// (headline stats, the outbox feed, mute patterns, configured channels)
// fired in parallel; each degrades to null/empty independently so one
// flaky endpoint doesn't blank the whole page. healthy flips false only
// when everything failed — that's the "runtime down" signal.

export async function fetchNotificationsSnapshot(
  status: NotificationStatusFilter = "all",
  kind: NotificationKindFilter = "all",
): Promise<NotificationsSnapshot> {
  const fetchedAt = new Date().toISOString()
  const [stats, list, mutes, channels] = await Promise.all([
    api.notifications.stats().catch(() => null),
    api.notifications
      .list({
        limit: 100,
        status: status === "all" ? undefined : status,
        kind: kind === "all" ? undefined : kind,
      })
      .catch(() => null),
    api.notifications.mutes.list().catch(() => null),
    api.notifications.channels.list().catch(() => null),
  ])

  return {
    stats,
    notifications: list?.notifications ?? [],
    total: list?.total ?? 0,
    hasMore: list?.has_more ?? false,
    mutes: mutes?.mutes ?? [],
    channels: channels?.channels ?? [],
    fetchedAt,
    healthy: stats !== null || list !== null || mutes !== null || channels !== null,
  }
}

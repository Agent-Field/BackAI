// SPDX-License-Identifier: Apache-2.0

import type {
  Notification,
  NotificationChannel,
  NotificationKind,
  NotificationMute,
  NotificationStats,
  NotificationStatus,
} from "@/lib/api"

// The list endpoint filters on the wire enums; "all" is a UI-only
// sentinel that drops the query param.
export type NotificationStatusFilter = NotificationStatus | "all"
export type NotificationKindFilter = NotificationKind | "all"

export const DEFAULT_STATUS_FILTER: NotificationStatusFilter = "all"
export const DEFAULT_KIND_FILTER: NotificationKindFilter = "all"

export interface NotificationsSnapshot {
  stats: NotificationStats | null
  notifications: Notification[]
  total: number
  hasMore: boolean
  mutes: NotificationMute[]
  channels: NotificationChannel[]
  fetchedAt: string
  healthy: boolean
}

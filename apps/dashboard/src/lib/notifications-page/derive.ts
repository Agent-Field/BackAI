// SPDX-License-Identifier: Apache-2.0

import type { Notification, NotificationKind, NotificationStatus } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"

import type { NotificationKindFilter, NotificationStatusFilter } from "./types"

// Status helpers ---------------------------------------------------------

// Map the wire status onto the four-state dot the dashboard speaks. sent
// is the happy terminal; queued/sending are in-flight; failed needs an
// operator; skipped means a mute pattern swallowed it — quiet, not an
// error.
export function classifyNotificationStatus(status: NotificationStatus): StatusState {
  switch (status) {
    case "sent":
      return "ok"
    case "queued":
    case "sending":
      return "watch"
    case "failed":
      return "act"
    case "skipped":
      return "idle"
  }
}

export function notificationStatusLabel(status: NotificationStatus): string {
  switch (status) {
    case "sent":
      return "sent"
    case "queued":
      return "queued"
    case "sending":
      return "sending"
    case "failed":
      return "failed"
    case "skipped":
      return "skipped (muted)"
  }
}

// The wire adapter is nullable until the worker claims a row. Read the
// unclaimed state as "pending" so the column never shows a bare dash for
// something that's simply waiting.
export function adapterLabel(notification: Notification): string {
  return notification.adapter ?? "pending"
}

// Filter guards ----------------------------------------------------------

const STATUS_VALUES: NotificationStatus[] = ["queued", "sending", "sent", "failed", "skipped"]

const KIND_VALUES: NotificationKind[] = ["email", "sms", "push", "log"]

export function isNotificationStatusFilter(v: string): v is NotificationStatusFilter {
  return v === "all" || (STATUS_VALUES as string[]).includes(v)
}

export function isNotificationKindFilter(v: string): v is NotificationKindFilter {
  return v === "all" || (KIND_VALUES as string[]).includes(v)
}

// Formatters -------------------------------------------------------------

export function formatNotificationAge(iso: string | null): string {
  if (!iso) return "—"
  const ts = Date.parse(iso)
  if (Number.isNaN(ts)) return iso
  const diffMs = Math.max(0, Date.now() - ts)
  if (diffMs < 1000) return "now"
  const sec = Math.floor(diffMs / 1000)
  if (sec < 60) return `${sec}s ago`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  if (sec < 86_400) return `${Math.floor(sec / 3600)}h ago`
  return new Date(ts).toISOString().slice(5, 16).replace("T", " ")
}

export function shortNotificationId(id: string): string {
  return id.length > 12 ? id.slice(0, 12) : id
}

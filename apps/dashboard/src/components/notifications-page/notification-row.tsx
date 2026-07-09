// SPDX-License-Identifier: Apache-2.0

"use client"

import { ChevronDown, ChevronRight } from "lucide-react"

import { Badge } from "@/components/ui/badge"

import type { Notification } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"
import {
  adapterLabel,
  classifyNotificationStatus,
  formatNotificationAge,
  notificationStatusLabel,
} from "@/lib/notifications-page/derive"
import { cn } from "@/lib/utils"

// One row in the notifications outbox table. Status dot left, kind badge,
// template + recipient (or last error), adapter, attempts, status.
// Clicking expands an inline detail panel with the rendered subject, the
// structured data payload, the adapter/provider ids, and any last error.

const DOT: Record<StatusState, string> = {
  ok: "bg-success",
  watch: "bg-warning",
  act: "bg-destructive",
  idle: "bg-muted-foreground/60",
}

interface NotificationRowProps {
  notification: Notification
  expanded: boolean
  onToggle: (notification: Notification) => void
}

export function NotificationRow({ notification, expanded, onToggle }: NotificationRowProps) {
  const tone = classifyNotificationStatus(notification.status)
  const statusTone =
    tone === "ok"
      ? "text-muted-foreground"
      : tone === "watch"
        ? "text-warning font-medium"
        : tone === "act"
          ? "text-destructive font-medium"
          : "text-muted-foreground"
  const accent =
    tone === "act"
      ? "border-l-destructive"
      : tone === "watch"
        ? "border-l-warning"
        : "border-l-transparent"
  return (
    <div className="flex flex-col">
      <button
        type="button"
        onClick={() => onToggle(notification)}
        aria-expanded={expanded}
        className={cn(
          "group grid w-full items-center gap-stack border-l-4 border-b px-row-x py-row-y text-left text-meta transition-colors",
          NOTIFICATION_ROW_COLUMNS,
          accent,
          expanded ? "bg-accent/30" : "hover:bg-accent/40",
        )}
      >
        <span aria-hidden className={`inline-block size-icon-dot rounded-pill ${DOT[tone]}`} />
        <span className="truncate font-mono tabular-nums text-muted-foreground">
          {formatNotificationAge(notification.created_at)}
        </span>
        <Badge variant="secondary">{notification.kind}</Badge>
        <div className="flex min-w-0 flex-col gap-tile-tight">
          <span
            className="truncate font-mono text-body text-foreground"
            title={notification.template}
          >
            {notification.template}
          </span>
          {notification.last_error ? (
            <span className="truncate text-meta text-destructive" title={notification.last_error}>
              {notification.last_error.length > 80
                ? `${notification.last_error.slice(0, 78)}…`
                : notification.last_error}
            </span>
          ) : (
            <span
              className="truncate font-mono text-meta text-muted-foreground"
              title={notification.to}
            >
              {notification.to}
            </span>
          )}
        </div>
        <span className="truncate text-right font-mono text-muted-foreground">
          {adapterLabel(notification)}
        </span>
        <span className="text-right font-mono tabular-nums text-foreground">
          {notification.attempts}
        </span>
        <span className={`text-right ${statusTone}`}>
          {notificationStatusLabel(notification.status)}
        </span>
        {expanded ? (
          <ChevronDown aria-hidden className="size-3.5 text-muted-foreground" />
        ) : (
          <ChevronRight
            aria-hidden
            className="size-3.5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100"
          />
        )}
      </button>
      {expanded ? <NotificationDetail notification={notification} /> : null}
    </div>
  )
}

// Inline detail panel — recipient + subject + adapter/provider ids, the
// structured data payload the worker renders against, and any last
// error. Sibling of the row button so interactive elements never nest.

function NotificationDetail({ notification }: { notification: Notification }) {
  const hasData = Object.keys(notification.data).length > 0
  return (
    <div className="flex flex-col gap-stack border-b bg-accent/10 px-row-x py-tile">
      <dl className="grid grid-cols-2 gap-stack text-meta md:grid-cols-4">
        <DetailMeta label="Notification ID" value={notification.id} mono />
        <DetailMeta label="Recipient" value={notification.to} mono />
        <DetailMeta label="Subject" value={notification.subject ?? "—"} />
        <DetailMeta label="From" value={notification.from ?? "adapter default"} mono />
        <DetailMeta label="Adapter" value={adapterLabel(notification)} mono />
        <DetailMeta
          label="Provider message id"
          value={notification.provider_message_id ?? "—"}
          mono
        />
        <DetailMeta label="Scheduled" value={formatNotificationAge(notification.scheduled_at)} />
        <DetailMeta label="Sent" value={formatNotificationAge(notification.sent_at)} />
      </dl>
      <div className="flex flex-col gap-tile-tight">
        <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
          Template data
        </span>
        {hasData ? (
          <pre className="max-h-48 overflow-auto rounded-md border bg-background px-row-x py-tile font-mono text-meta text-foreground">
            {JSON.stringify(notification.data, null, 2)}
          </pre>
        ) : (
          <p className="rounded-md border border-dashed border-muted-foreground/40 px-row-x py-tile text-meta text-muted-foreground">
            No template data on this notification.
          </p>
        )}
      </div>
      {notification.last_error ? (
        <div className="rounded-md border border-l-4 border-l-destructive bg-destructive/5 px-row-x py-tile">
          <span className="text-eyebrow uppercase tracking-wide text-destructive">Last error</span>
          <pre className="mt-tile-tight max-h-48 overflow-auto whitespace-pre-wrap font-mono text-meta text-foreground">
            {notification.last_error}
          </pre>
        </div>
      ) : null}
    </div>
  )
}

function DetailMeta({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex min-w-0 flex-col gap-tile-tight">
      <dt className="text-eyebrow uppercase tracking-wide text-muted-foreground">{label}</dt>
      <dd className={`truncate text-meta text-foreground ${mono ? "font-mono" : ""}`} title={value}>
        {value}
      </dd>
    </div>
  )
}

export const NOTIFICATION_ROW_COLUMNS =
  "grid-cols-[12px_72px_76px_minmax(0,1fr)_92px_56px_128px_18px]"

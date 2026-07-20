// SPDX-License-Identifier: Apache-2.0

"use client"

import { useState } from "react"

import { Button } from "@/components/ui/button"
import { RelativeTime } from "@/components/ui/relative-time"

import { api } from "@/lib/api"
import type { ErrorCapabilities, ErrorGroup } from "@/lib/api"
import { formatCount, formatErrorAge } from "@/lib/errors-page/derive"
import { cn } from "@/lib/utils"

// One row in the error-groups table. Status dot left, title + culprit
// stacked, then service / counts / recency, with capability-gated
// actions on the right. Open groups pick up the destructive left-edge
// accent so the scan-down-the-left-edge story matches Runs.

const DOT: Record<ErrorGroup["status"], string> = {
  open: "bg-destructive",
  muted: "bg-muted-foreground/60",
  resolved: "bg-success",
}

type ErrorAction = "mute" | "resolve" | "reopen"

interface ErrorRowProps {
  group: ErrorGroup
  capabilities: ErrorCapabilities | null
  onMutated: () => Promise<void> | void
}

export function ErrorRow({ group, capabilities, onMutated }: ErrorRowProps) {
  const [pending, setPending] = useState<ErrorAction | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const act = async (action: ErrorAction) => {
    if (pending) return
    setPending(action)
    setActionError(null)
    try {
      await api.errors[action](group.id)
      await onMutated()
    } catch {
      setActionError(`Could not ${action} this group — retry in a moment.`)
    } finally {
      setPending(null)
    }
  }

  // Capability-gated action set per current status. Mute needs
  // supports_mute; resolve and reopen both ride on supports_resolve
  // (reopen is the inverse transition of resolve on the backend).
  const actions: { action: ErrorAction; label: string; busy: string }[] = []
  if (group.status === "open") {
    if (capabilities?.supports_mute)
      actions.push({ action: "mute", label: "Mute", busy: "Muting…" })
    if (capabilities?.supports_resolve)
      actions.push({ action: "resolve", label: "Resolve", busy: "Resolving…" })
  } else if (group.status === "muted") {
    if (capabilities?.supports_resolve) {
      actions.push({ action: "resolve", label: "Resolve", busy: "Resolving…" })
      actions.push({ action: "reopen", label: "Reopen", busy: "Reopening…" })
    }
  } else if (capabilities?.supports_resolve) {
    actions.push({ action: "reopen", label: "Reopen", busy: "Reopening…" })
  }

  const accent =
    group.status === "open" ? "border-l-destructive" : "border-l-transparent"

  return (
    <div
      className={cn(
        "grid items-center gap-stack border-l-4 border-b px-row-x py-row-y text-meta transition-colors hover:bg-accent/40",
        ERROR_ROW_COLUMNS,
        accent,
      )}
    >
      <span
        aria-hidden
        className={`inline-block size-icon-dot rounded-pill ${DOT[group.status]}`}
      />
      <div className="flex min-w-0 flex-col gap-tile-tight">
        <span
          className="truncate text-body font-medium text-foreground"
          title={group.title}
        >
          {group.title || group.id}
        </span>
        {actionError ? (
          <span className="truncate text-meta text-destructive">
            {actionError}
          </span>
        ) : group.culprit ? (
          <span
            className="truncate font-mono text-meta text-muted-foreground"
            title={group.culprit}
          >
            {group.culprit}
          </span>
        ) : (
          <span className="truncate font-mono text-meta text-muted-foreground">
            {group.id.length > 24 ? `${group.id.slice(0, 22)}…` : group.id}
          </span>
        )}
      </div>
      <span
        className="truncate font-mono text-meta text-muted-foreground"
        title={group.service}
      >
        {group.service || "—"}
      </span>
      <span className="text-right font-mono tabular-nums text-foreground">
        {formatCount(group.count)}
      </span>
      <span className="text-right font-mono tabular-nums text-muted-foreground">
        {group.user_count !== undefined ? formatCount(group.user_count) : "—"}
      </span>
      <RelativeTime
        iso={group.last_seen}
        format={formatErrorAge}
        className="text-right font-mono tabular-nums text-muted-foreground"
      />
      <div className="flex items-center justify-end gap-inline">
        {actions.map(({ action, label, busy }) => (
          <Button
            key={action}
            type="button"
            size="sm"
            variant="outline"
            disabled={pending !== null}
            onClick={() => void act(action)}
            className="h-6 px-2 text-meta"
          >
            {pending === action ? busy : label}
          </Button>
        ))}
      </div>
    </div>
  )
}

export const ERROR_ROW_COLUMNS =
  "grid-cols-[12px_minmax(0,1fr)_minmax(96px,160px)_56px_56px_88px_minmax(120px,168px)]"

// SPDX-License-Identifier: Apache-2.0

"use client"

import { AlertTriangle, RefreshCw } from "lucide-react"

import { Button } from "@/components/ui/button"

// DEGRADED state per page brief §10: one source failed, the other is still
// rendered. Banner explains the partial nature so the operator doesn't
// trust an incomplete view.

interface InboxBannerProps {
  approvalsHealthy: boolean
  alertsHealthy: boolean
  onRetry: () => void | Promise<void>
}

export function InboxBanner({
  approvalsHealthy,
  alertsHealthy,
  onRetry,
}: InboxBannerProps) {
  if (approvalsHealthy && alertsHealthy) return null

  const broken: string[] = []
  if (!approvalsHealthy) broken.push("approvals service")
  if (!alertsHealthy) broken.push("home overview")

  return (
    <div
      role="status"
      className="flex items-center gap-stack rounded-md border border-warning/40 bg-warning/5 px-row-x py-row-y text-body text-foreground"
    >
      <AlertTriangle
        className="size-icon-inline shrink-0 text-warning"
        aria-hidden
      />
      <span className="flex-1">
        Inbox may be incomplete —{" "}
        <span className="font-medium">{broken.join(" and ")}</span> unavailable.
      </span>
      <Button
        type="button"
        size="sm"
        variant="outline"
        onClick={() => void onRetry()}
        className="h-7 gap-inline text-meta"
      >
        <RefreshCw className="size-3.5" aria-hidden />
        Retry
      </Button>
    </div>
  )
}

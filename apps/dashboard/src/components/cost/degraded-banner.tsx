// SPDX-License-Identifier: Apache-2.0

"use client"

import { AlertTriangle, RefreshCw } from "lucide-react"

import { Button } from "@/components/ui/button"

// Surfaces partial-source failures inline. Same shape as the Inbox
// banner so operators see a consistent pattern across pages.

interface DegradedBannerProps {
  primaryHealthy: boolean
  budgetsHealthy: boolean
  cacheHealthy: boolean
  onRetry: () => void | Promise<void>
}

export function DegradedBanner({
  primaryHealthy,
  budgetsHealthy,
  cacheHealthy,
  onRetry,
}: DegradedBannerProps) {
  const broken: string[] = []
  if (!primaryHealthy) broken.push("cost summary")
  if (!budgetsHealthy) broken.push("budgets")
  if (!cacheHealthy) broken.push("cache stats")
  if (broken.length === 0) return null
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
        Some panels may be stale —{" "}
        <span className="font-medium">{broken.join(", ")}</span> unavailable.
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

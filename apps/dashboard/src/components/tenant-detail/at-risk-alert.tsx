// SPDX-License-Identifier: Apache-2.0

import { AlertTriangle, OctagonAlert } from "lucide-react"

import type { AtRiskSignal } from "@/lib/tenant-detail/types"

// Slim Alert variant for composite-signal warnings on the tenant
// header. Same height across severity tiers so the layout doesn't
// shift when the dominant signal changes.

const ACCENT: Record<AtRiskSignal["severity"], string> = {
  warning: "border-l-warning bg-warning/5",
  critical: "border-l-destructive bg-destructive/5",
}

const ICON_TINT: Record<AtRiskSignal["severity"], string> = {
  warning: "text-warning",
  critical: "text-destructive",
}

export function AtRiskAlert({ signal }: { signal: AtRiskSignal }) {
  const Icon = signal.severity === "critical" ? OctagonAlert : AlertTriangle
  return (
    <article
      role="alert"
      className={`flex items-start gap-stack rounded-md border border-l-4 ${ACCENT[signal.severity]} px-row-x py-row-y`}
    >
      <Icon
        className={`mt-0.5 size-icon-inline shrink-0 ${ICON_TINT[signal.severity]}`}
        aria-hidden
      />
      <div className="flex min-w-0 flex-col gap-tile-tight">
        <span className="text-body font-medium text-foreground">
          {signal.message}
        </span>
        {signal.detail ? (
          <span className="text-meta text-muted-foreground">
            {signal.detail}
          </span>
        ) : null}
      </div>
    </article>
  )
}

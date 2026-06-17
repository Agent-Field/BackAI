// SPDX-License-Identifier: Apache-2.0

import { AlertTriangle, ArrowUpRight } from "lucide-react"
import Link from "next/link"

import { Button } from "@/components/ui/button"
import { Sparkline } from "@/components/ui/sparkline"

import type { CostAnomaly } from "@/lib/cost/types"

// AnomalyCard — severity-bordered card with title, context, optional
// spike sparkline, and 1-2 action buttons. Used in Zone 1 of the Cost
// page; reusable on Errors and Inbox surfaces.

const ACCENT: Record<CostAnomaly["severity"], string> = {
  critical: "border-l-destructive",
  warning: "border-l-warning",
  info: "border-l-muted-foreground",
}

const ICON_TINT: Record<CostAnomaly["severity"], string> = {
  critical: "text-destructive",
  warning: "text-warning",
  info: "text-muted-foreground",
}

const SPARK_STATUS: Record<CostAnomaly["severity"], "act" | "watch" | "idle"> = {
  critical: "act",
  warning: "watch",
  info: "idle",
}

export function AnomalyCard({ anomaly }: { anomaly: CostAnomaly }) {
  return (
    <article
      className={`flex flex-col gap-tile rounded-md border border-l-4 ${ACCENT[anomaly.severity]} bg-card px-row-x py-tile`}
    >
      <header className="flex items-start gap-stack">
        <AlertTriangle
          className={`mt-0.5 size-icon-inline shrink-0 ${ICON_TINT[anomaly.severity]}`}
          aria-hidden
        />
        <div className="flex min-w-0 flex-1 flex-col gap-tile-tight">
          <h3 className="text-body font-medium text-foreground">
            {anomaly.title}
          </h3>
          <p className="text-meta text-muted-foreground">{anomaly.context}</p>
        </div>
      </header>
      {anomaly.sparkline && anomaly.sparkline.length > 0 ? (
        <Sparkline
          data={anomaly.sparkline}
          status={SPARK_STATUS[anomaly.severity]}
          height={28}
        />
      ) : null}
      {anomaly.primaryAction || anomaly.secondaryAction ? (
        <div className="flex flex-wrap items-center gap-inline">
          {anomaly.primaryAction ? (
            <Button
              size="sm"
              className="h-7 gap-inline text-meta"
              render={<Link href={anomaly.primaryAction.href} />}
            >
              {anomaly.primaryAction.label}
            </Button>
          ) : null}
          {anomaly.secondaryAction ? (
            <Button
              size="sm"
              variant="outline"
              className="h-7 gap-inline text-meta"
              render={<Link href={anomaly.secondaryAction.href} />}
            >
              {anomaly.secondaryAction.label}
              <ArrowUpRight className="size-3" aria-hidden />
            </Button>
          ) : null}
        </div>
      ) : null}
    </article>
  )
}

// SPDX-License-Identifier: Apache-2.0

import { CheckCircle2, OctagonAlert } from "lucide-react"

import { formatRelative } from "@/lib/health/derive"
import type { OverallSummary } from "@/lib/health/types"

// Zone 0 — Incident banner / All-clear card.
//
// The card stays in the same slot regardless of state so the page
// doesn't jump on status transitions. Per the brief, "since X" deltas
// require Gap 20 (status transition log) and aren't shown in v1.

interface IncidentBannerProps {
  summary: OverallSummary
  fetchedAt: string
}

export function IncidentBanner({ summary, fetchedAt }: IncidentBannerProps) {
  if (summary.status === "down" || summary.status === "degraded") {
    return <DegradedBanner summary={summary} fetchedAt={fetchedAt} />
  }
  return <AllClearCard fetchedAt={fetchedAt} status={summary.status} />
}

function DegradedBanner({
  summary,
  fetchedAt,
}: {
  summary: OverallSummary
  fetchedAt: string
}) {
  const lines = describeDegradation(summary)
  const critical = summary.status === "down"
  return (
    <article
      role="alert"
      className={`flex flex-col gap-tile rounded-md border border-l-4 ${
        critical
          ? "border-l-destructive bg-destructive/5"
          : "border-l-warning bg-warning/5"
      } px-row-x py-tile`}
    >
      <header className="flex items-start gap-stack">
        <OctagonAlert
          className={`mt-0.5 size-icon-inline shrink-0 ${
            critical ? "text-destructive" : "text-warning"
          }`}
          aria-hidden
        />
        <div className="flex min-w-0 flex-1 flex-col gap-tile-tight">
          <h2 className="text-body font-medium text-foreground">
            {critical ? "Service degradation" : "Something needs attention"}
          </h2>
          <ul className="flex flex-col gap-tile-tight text-meta text-foreground">
            {lines.map((line) => (
              <li key={line}>{line}</li>
            ))}
          </ul>
        </div>
        <span className="shrink-0 text-meta text-muted-foreground tabular-nums">
          last check {formatRelative(fetchedAt)}
        </span>
      </header>
    </article>
  )
}

function AllClearCard({
  fetchedAt,
  status,
}: {
  fetchedAt: string
  status: "healthy" | "unknown"
}) {
  return (
    <article
      role="status"
      className="flex items-center gap-stack rounded-md border border-l-4 border-l-success bg-card px-row-x py-tile"
    >
      <CheckCircle2
        className="size-icon-inline shrink-0 text-success"
        aria-hidden
      />
      <div className="flex min-w-0 flex-1 flex-col gap-tile-tight">
        <h2 className="text-body font-medium text-foreground">
          {status === "unknown"
            ? "Awaiting first probes"
            : "All systems operational"}
        </h2>
        <p className="text-meta text-muted-foreground">
          {status === "unknown"
            ? "Backend probes will populate as soon as services check in."
            : "Every connected service + provider is healthy."}
        </p>
      </div>
      <span className="shrink-0 text-meta text-muted-foreground tabular-nums">
        last full sweep {formatRelative(fetchedAt)}
      </span>
    </article>
  )
}

function describeDegradation(summary: OverallSummary): string[] {
  const out: string[] = []
  if (summary.servicesDown > 0) {
    out.push(
      `${summary.servicesDown} service${summary.servicesDown === 1 ? "" : "s"} offline`,
    )
  }
  if (summary.servicesDegraded > 0) {
    out.push(
      `${summary.servicesDegraded} service${summary.servicesDegraded === 1 ? "" : "s"} degraded`,
    )
  }
  if (summary.providersDown > 0) {
    out.push(
      `${summary.providersDown} LLM provider${summary.providersDown === 1 ? "" : "s"} down`,
    )
  }
  if (summary.providersDegraded > 0) {
    out.push(
      `${summary.providersDegraded} LLM provider${summary.providersDegraded === 1 ? "" : "s"} degraded`,
    )
  }
  return out.length > 0 ? out : ["Awaiting transition log details (v0.2)."]
}

// SPDX-License-Identifier: Apache-2.0

import { ArrowUpRight } from "lucide-react"

import { Sparkline } from "@/components/ui/sparkline"

import type { ProviderHealth } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"
import {
  classifyProviderStatus,
  formatLatency,
  formatRelative,
  providerSparkline,
} from "@/lib/health/derive"

// One card per LLM provider. The card stays the same height across
// healthy / degraded / awaiting states so the grid layout never
// reflows — that matches the framework's "structure visible even at
// zero" principle.

const DOT: Record<StatusState, string> = {
  ok: "bg-success",
  watch: "bg-warning",
  act: "bg-destructive",
  idle: "bg-muted-foreground/60",
}

const STATUS_LABEL: Record<string, string> = {
  healthy: "healthy",
  degraded: "degraded",
  down: "down",
  unknown: "awaiting first check",
}

export function ProviderHealthCard({
  provider,
}: {
  provider: ProviderHealth
}) {
  const state = classifyProviderStatus(provider.status)
  const sparkline = providerSparkline(provider)
  const sampleCount = provider.observations
  const empty = sampleCount === 0
  return (
    <article className="flex flex-col gap-tile rounded-md border bg-card px-row-x py-tile">
      <header className="flex items-center gap-stack">
        <span
          aria-hidden
          className={`inline-block size-icon-dot rounded-pill ${DOT[state]}`}
        />
        <span className="min-w-0 flex-1 truncate font-medium text-foreground">
          {provider.provider}
        </span>
        <span
          className={`shrink-0 text-meta ${
            state === "ok"
              ? "text-muted-foreground"
              : state === "watch"
                ? "text-warning font-medium"
                : state === "act"
                  ? "text-destructive font-medium"
                  : "text-muted-foreground"
          }`}
        >
          {STATUS_LABEL[provider.status] ?? provider.status}
        </span>
      </header>
      <dl className="grid grid-cols-2 gap-stack text-meta">
        <Metric
          label="Uptime"
          value={
            empty
              ? "—"
              : `${(provider.availability_pct ?? 0).toFixed(1)}%`
          }
        />
        <Metric
          label="p95 latency"
          value={empty ? "—" : formatLatency(provider.p95_latency_ms)}
          warn={!empty && provider.p95_latency_ms >= 1000}
          critical={!empty && provider.p95_latency_ms >= 3000}
        />
      </dl>
      <Sparkline
        data={sparkline}
        status={state === "ok" ? "ok" : state === "watch" ? "watch" : state === "act" ? "act" : "idle"}
        height={28}
      />
      <footer className="flex items-center justify-between text-meta text-muted-foreground">
        <span>
          {sampleCount.toLocaleString()} sample
          {sampleCount === 1 ? "" : "s"}
        </span>
        <span className="tabular-nums">
          {formatRelative(provider.last_observed_at)}
        </span>
      </footer>
      {state !== "ok" && state !== "idle" ? (
        <a
          href="/platform/adapters#llm"
          className="inline-flex items-center gap-inline self-start text-meta text-foreground underline hover:no-underline"
        >
          Switch fallback
          <ArrowUpRight className="size-3" aria-hidden />
        </a>
      ) : null}
    </article>
  )
}

function Metric({
  label,
  value,
  warn,
  critical,
}: {
  label: string
  value: string
  warn?: boolean
  critical?: boolean
}) {
  return (
    <div className="flex flex-col gap-tile-tight">
      <dt className="text-eyebrow uppercase tracking-wide text-muted-foreground">
        {label}
      </dt>
      <dd
        className={`font-mono text-body tabular-nums ${
          critical
            ? "text-destructive"
            : warn
              ? "text-warning"
              : "text-foreground"
        }`}
      >
        {value}
      </dd>
    </div>
  )
}

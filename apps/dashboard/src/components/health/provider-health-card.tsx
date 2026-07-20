// SPDX-License-Identifier: Apache-2.0

import { ArrowUpRight } from "lucide-react"

import { Button } from "@/components/ui/button"
import { RelativeTime } from "@/components/ui/relative-time"
import { Sparkline } from "@/components/ui/sparkline"

import type { ProviderHealth } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"
import {
  deriveProviderStatus,
  formatLatency,
  providerSparkline,
} from "@/lib/health/derive"

// One card per LLM upstream provider. Status pill is derived from the
// threshold table in derive.ts (uptime + p95 latency) so the colour
// never contradicts the metrics next to it.
//
// Layout has two modes:
//   - Compact (default, grid context): two-column metrics + sparkline
//   - Wide (single-card case, controlled by `layout="wide"`): single
//     row of metrics with the sparkline on its own row and actions
//     pinned to the footer right
//
// Recovery actions (Switch fallback, Open LiteLLM) always render so the
// operator has an inline path during incidents and a quick "what is
// this routing through?" link even when everything is healthy.

const DOT: Record<StatusState, string> = {
  ok: "bg-success",
  watch: "bg-warning",
  act: "bg-destructive",
  idle: "bg-muted-foreground/60",
}

type Tone = "ok" | "watch" | "act" | "idle"

const STATUS_LABEL: Record<string, string> = {
  healthy: "healthy",
  degraded: "degraded",
  down: "down",
  unknown: "awaiting first check",
}

const LITELLM_UI_URL =
  typeof process !== "undefined" && process.env.NEXT_PUBLIC_LITELLM_UI_URL
    ? process.env.NEXT_PUBLIC_LITELLM_UI_URL
    : "http://localhost:4000/ui"

interface ProviderHealthCardProps {
  provider: ProviderHealth
  layout?: "compact" | "wide"
}

export function ProviderHealthCard({
  provider,
  layout = "compact",
}: ProviderHealthCardProps) {
  const derived = deriveProviderStatus(provider)
  const tone: Tone =
    derived === "healthy"
      ? "ok"
      : derived === "degraded"
        ? "watch"
        : derived === "down"
          ? "act"
          : "idle"
  const sparkline = providerSparkline(provider)
  const sampleCount = provider.observations
  const empty = sampleCount === 0
  const statusLabel = STATUS_LABEL[derived] ?? derived
  const statusTone =
    tone === "ok"
      ? "text-muted-foreground"
      : tone === "watch"
        ? "text-warning font-medium"
        : tone === "act"
          ? "text-destructive font-medium"
          : "text-muted-foreground"
  return (
    <article className="flex flex-col gap-tile rounded-md border bg-card px-row-x py-tile">
      <header className="flex items-center gap-stack">
        <span
          aria-hidden
          className={`inline-block size-icon-dot rounded-pill ${DOT[tone]}`}
        />
        <span className="min-w-0 flex-1 truncate text-body font-medium text-foreground">
          {provider.provider}
        </span>
        <span className={`shrink-0 text-meta ${statusTone}`}>{statusLabel}</span>
      </header>
      {layout === "wide" ? (
        <WideMetrics provider={provider} empty={empty} />
      ) : (
        <CompactMetrics provider={provider} empty={empty} />
      )}
      <Sparkline data={sparkline} status={tone} height={layout === "wide" ? 36 : 28} />
      <footer className="flex flex-wrap items-center justify-between gap-stack text-meta text-muted-foreground">
        <span>
          {sampleCount.toLocaleString()} sample
          {sampleCount === 1 ? "" : "s"} · last check{" "}
          <RelativeTime iso={provider.last_observed_at} className="tabular-nums" />
        </span>
        <div className="flex items-center gap-inline">
          {tone !== "ok" && tone !== "idle" ? (
            <Button
              size="sm"
              className="h-7 gap-inline text-meta"
              render={<a href="/platform/adapters#llm" />}
            >
              Switch fallback
            </Button>
          ) : null}
          <Button
            size="sm"
            variant="outline"
            className="h-7 gap-inline text-meta"
            render={
              <a href={LITELLM_UI_URL} target="_blank" rel="noopener noreferrer" />
            }
          >
            Open LiteLLM
            <ArrowUpRight className="size-3" aria-hidden />
          </Button>
        </div>
      </footer>
    </article>
  )
}

function CompactMetrics({
  provider,
  empty,
}: {
  provider: ProviderHealth
  empty: boolean
}) {
  return (
    <dl className="grid grid-cols-2 gap-stack text-meta">
      <Metric
        label="Uptime"
        value={empty ? "—" : `${provider.availability_pct.toFixed(1)}%`}
      />
      <Metric
        label="p95 latency"
        value={empty ? "—" : formatLatency(provider.p95_latency_ms)}
        warn={!empty && provider.p95_latency_ms >= 1000}
        critical={!empty && provider.p95_latency_ms >= 3000}
      />
    </dl>
  )
}

function WideMetrics({
  provider,
  empty,
}: {
  provider: ProviderHealth
  empty: boolean
}) {
  return (
    <dl className="grid grid-cols-3 gap-stack text-meta">
      <Metric
        label="Uptime"
        value={empty ? "—" : `${provider.availability_pct.toFixed(1)}%`}
      />
      <Metric
        label="p95 latency"
        value={empty ? "—" : formatLatency(provider.p95_latency_ms)}
        warn={!empty && provider.p95_latency_ms >= 1000}
        critical={!empty && provider.p95_latency_ms >= 3000}
      />
      <Metric
        label="Median latency"
        value={empty ? "—" : formatLatency(provider.median_latency_ms)}
      />
    </dl>
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

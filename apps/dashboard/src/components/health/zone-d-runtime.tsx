// SPDX-License-Identifier: Apache-2.0

import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import type { MetricsSummary } from "@/lib/api"
import { formatBytes, formatDuration, formatLatency } from "@/lib/health/derive"

// Zone D — Runtime self. Even on a fresh fork these tiles render real
// data (version, uptime, mem, goroutines) so the page never collapses
// into emptiness.

export function ZoneDRuntime({ metrics }: { metrics: MetricsSummary | null }) {
  return (
    <ZoneCard aria-labelledby="zone-d-heading">
      <ZoneCardHeader
        id="zone-d-heading"
        title="Runtime"
        subtitle={
          metrics === null
            ? "Metrics summary unavailable"
            : metrics.version
              ? metrics.version
              : "BackAI runtime"
        }
      />
      <div className="grid gap-stack p-row-x sm:grid-cols-2 lg:grid-cols-4">
        <Tile
          label="Version"
          value={metrics?.version ?? "—"}
          mono
        />
        <Tile
          label="Uptime"
          value={
            metrics ? formatDuration(metrics.uptime_seconds) : "—"
          }
        />
        <Tile
          label="Memory"
          value={metrics ? formatBytes(metrics.heap_alloc_bytes) : "—"}
        />
        <Tile
          label="Goroutines"
          value={metrics?.goroutines.toLocaleString() ?? "—"}
        />
      </div>
      <div className="grid gap-stack border-t p-row-x lg:grid-cols-[1fr_minmax(280px,2fr)]">
        <HttpSummary metrics={metrics} />
        <TopRoutes metrics={metrics} />
      </div>
    </ZoneCard>
  )
}

function Tile({
  label,
  value,
  mono,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div className="flex flex-col gap-tile rounded-md border bg-card px-row-x py-tile">
      <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
        {label}
      </span>
      <span
        className={`text-body font-medium text-foreground ${mono ? "font-mono" : "tabular-nums"}`}
      >
        {value}
      </span>
    </div>
  )
}

function HttpSummary({ metrics }: { metrics: MetricsSummary | null }) {
  const total = metrics?.http_requests_total ?? 0
  const p95 = metrics?.http_p95_ms ?? 0
  const errors = metrics?.by_route.reduce((acc, r) => acc + r.error_count, 0) ?? 0
  const errPct = total > 0 ? (errors / total) * 100 : 0
  return (
    <div className="flex flex-col gap-tile rounded-md border bg-card p-tile">
      <span className="text-body font-medium text-foreground">HTTP</span>
      <dl className="grid grid-cols-3 gap-stack text-meta">
        <Metric label="Total" value={total > 0 ? total.toLocaleString() : "—"} />
        <Metric
          label="p95"
          value={total > 0 ? formatLatency(p95) : "—"}
        />
        <Metric
          label="Errors"
          value={total > 0 ? `${errPct.toFixed(2)}%` : "—"}
          critical={errPct >= 1}
          warn={errPct >= 0.5}
        />
      </dl>
      <p className="text-meta text-muted-foreground">
        Live counters since the last runtime boot.
      </p>
    </div>
  )
}

function TopRoutes({ metrics }: { metrics: MetricsSummary | null }) {
  const rows = (metrics?.by_route ?? []).slice(0, 6)
  return (
    <div className="flex flex-col gap-tile rounded-md border bg-card p-tile">
      <header className="flex items-baseline justify-between">
        <span className="text-body font-medium text-foreground">Top routes</span>
        <span className="text-meta text-muted-foreground">
          {rows.length === 0 ? "no traffic yet" : `${rows.length} shown`}
        </span>
      </header>
      {rows.length === 0 ? (
        <p className="text-meta text-muted-foreground">
          Send a request through the API explorer or call{" "}
          <code className="font-mono">/api/v1/llm/chat/completions</code> to
          populate this list.
        </p>
      ) : (
        <table className="w-full text-meta">
          <thead>
            <tr className="text-left text-eyebrow uppercase tracking-wide text-muted-foreground">
              <th className="py-tile-tight font-normal">Route</th>
              <th className="py-tile-tight font-normal text-right">Requests</th>
              <th className="py-tile-tight font-normal text-right">Avg</th>
              <th className="py-tile-tight font-normal text-right">Errors</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.route} className="border-t">
                <td className="py-tile-tight font-mono text-foreground">
                  <span className="block truncate" title={r.route}>
                    {r.route.length > 36 ? `${r.route.slice(0, 34)}…` : r.route}
                  </span>
                </td>
                <td className="py-tile-tight text-right font-mono tabular-nums text-muted-foreground">
                  {r.requests.toLocaleString()}
                </td>
                <td className="py-tile-tight text-right font-mono tabular-nums text-muted-foreground">
                  {formatLatency(r.avg_ms)}
                </td>
                <td
                  className={`py-tile-tight text-right font-mono tabular-nums ${r.error_count > 0 ? "text-destructive" : "text-muted-foreground"}`}
                >
                  {r.error_count.toLocaleString()}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
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
        className={`font-mono tabular-nums ${critical ? "text-destructive" : warn ? "text-warning" : "text-foreground"}`}
      >
        {value}
      </dd>
    </div>
  )
}

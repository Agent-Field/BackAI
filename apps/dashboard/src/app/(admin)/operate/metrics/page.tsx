// Metrics page — at-a-glance runtime stats from /api/v1/metrics/summary.
//
// The runtime exposes a Prometheus surface at /metrics for proper
// monitoring; this dashboard tab is the operator's "is the runtime
// healthy right now?" view, NOT a replacement for Grafana.

import {
  Activity,
  Cpu,
  Database,
  Gauge,
  HardDrive,
  Timer,
} from "lucide-react"

import { api, ApiError, type MetricsSummary } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { PageHeader } from "@/components/layout/page-header"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

import { KpiTile } from "../queues/_components/kpi-tile"

export const dynamic = "force-dynamic"

async function loadMetrics(): Promise<{
  summary: MetricsSummary
  error: string | null
}> {
  try {
    const summary = await api.metrics()
    return { summary, error: null }
  } catch (e) {
    const message =
      e instanceof ApiError
        ? `${e.code}: ${e.message}`
        : e instanceof Error
          ? e.message
          : "Failed to load metrics"
    return {
      summary: {
        http_requests_total: 0,
        http_p95_ms: 0,
        goroutines: 0,
        heap_alloc_bytes: 0,
        uptime_seconds: 0,
        version: "—",
        by_route: [],
      },
      error: message,
    }
  }
}

export default async function Page() {
  const { summary, error } = await loadMetrics()

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Metrics"
        description={`Runtime ${summary.version}. Backed by /api/v1/metrics/summary; for the full Prometheus surface point your scraper at /metrics.`}
      />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <KpiTile
          label="Requests total"
          value={formatNumber(summary.http_requests_total)}
          description="Since the runtime booted."
          icon={Activity}
        />
        <KpiTile
          label="p95 latency"
          value={`${summary.http_p95_ms.toFixed(0)} ms`}
          description="95th percentile of the recent in-memory window."
          icon={Gauge}
          tone={summary.http_p95_ms > 1000 ? "danger" : "default"}
        />
        <KpiTile
          label="Goroutines"
          value={formatNumber(summary.goroutines)}
          description="Active Go scheduler tasks."
          icon={Cpu}
        />
        <KpiTile
          label="Heap"
          value={formatBytes(summary.heap_alloc_bytes)}
          description={`Uptime ${formatUptime(summary.uptime_seconds)}.`}
          icon={HardDrive}
        />
      </div>

      <section className="flex flex-col gap-3">
        <div className="flex items-baseline justify-between">
          <h2 className="text-lg font-semibold tracking-tight">Top routes</h2>
          <span className="text-muted-foreground text-xs">
            {summary.by_route.length === 0
              ? "No routes recorded yet"
              : `${summary.by_route.length} route${
                  summary.by_route.length === 1 ? "" : "s"
                }`}
          </span>
        </div>

        {error ? (
          <div className="bg-card text-muted-foreground rounded-xl border border-dashed p-6 text-sm">
            <p className="text-foreground mb-1 font-medium">
              Metrics summary isn&apos;t available
            </p>
            <p>
              The runtime returned:{" "}
              <code className="bg-muted rounded px-1 font-mono text-xs">
                {error}
              </code>
              .
            </p>
          </div>
        ) : summary.by_route.length === 0 ? (
          <div className="text-muted-foreground bg-card flex items-center gap-2 rounded-xl border p-6 text-xs">
            <Timer className="size-3.5" />
            No traffic recorded yet. Make a few requests against the runtime
            and they&apos;ll show up here.
          </div>
        ) : (
          <div className="bg-card rounded-xl border">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                    Route
                  </TableHead>
                  <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                    Requests
                  </TableHead>
                  <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                    Avg
                  </TableHead>
                  <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                    Errors
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {summary.by_route.map((row) => (
                  <TableRow key={row.route} className="hover:bg-muted/30">
                    <TableCell>
                      <code className="font-mono text-xs">{row.route}</code>
                    </TableCell>
                    <TableCell className="tabular-nums text-xs">
                      {formatNumber(row.requests)}
                    </TableCell>
                    <TableCell className="tabular-nums text-xs">
                      {row.avg_ms.toFixed(1)} ms
                    </TableCell>
                    <TableCell>
                      <ErrorBadge count={row.error_count} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </section>

      <div className="text-muted-foreground flex items-center gap-2 text-xs">
        <Database className="size-3.5" />
        Prometheus scrape endpoint at{" "}
        <code className="bg-muted rounded px-1 font-mono">/metrics</code>.
      </div>
    </div>
  )
}

function ErrorBadge({ count }: { count: number }) {
  if (count === 0) {
    return (
      <Badge variant="outline" className="text-xs tabular-nums">
        0
      </Badge>
    )
  }
  if (count < 5) {
    return (
      <Badge variant="secondary" className="text-xs tabular-nums">
        {count}
      </Badge>
    )
  }
  return (
    <Badge variant="destructive" className="text-xs tabular-nums">
      {count}
    </Badge>
  )
}

function formatNumber(value: number): string {
  return new Intl.NumberFormat("en-US").format(value)
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  const value = bytes / Math.pow(1024, i)
  const fixed = value < 10 ? value.toFixed(2) : value.toFixed(1)
  return `${fixed} ${units[i]}`
}

function formatUptime(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) {
    const m = Math.floor(seconds / 60)
    const s = seconds % 60
    return `${m}m ${s}s`
  }
  if (seconds < 86400) {
    const h = Math.floor(seconds / 3600)
    const m = Math.floor((seconds % 3600) / 60)
    return `${h}h ${m}m`
  }
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  return `${d}d ${h}h`
}

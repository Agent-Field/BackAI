// SPDX-License-Identifier: Apache-2.0

// Queues page — operator view of the runtime job queue.
//
// Server component: fetches the queue summary once per render (force-dynamic
// + no-store via lib/api.ts). When the runtime endpoint isn't live yet we
// render a clean empty-state shell rather than crashing.

import {
  CheckCircle2,
  ListChecks,
  Loader2,
  Timer,
  TriangleAlert,
} from "lucide-react"

import { api, ApiError, type QueueSummary } from "@/lib/api"
import { PageHeader } from "@/components/layout/page-header"

import { KpiTile } from "./_components/kpi-tile"
import { RecentJobsTable } from "./_components/recent-jobs-table"

export const dynamic = "force-dynamic"

async function loadQueue(): Promise<{
  summary: QueueSummary
  error: string | null
}> {
  try {
    const summary = await api.queue()
    return { summary, error: null }
  } catch (e) {
    const message =
      e instanceof ApiError
        ? `${e.code}: ${e.message}`
        : e instanceof Error
          ? e.message
          : "Failed to load queue summary"
    return {
      summary: { pending: 0, running: 0, failed: 0, succeeded_today: 0, recent: [] },
      error: message,
    }
  }
}

export default async function Page() {
  const { summary, error } = await loadQueue()
  const totalKnownJobs =
    summary.pending +
    summary.running +
    summary.failed +
    summary.succeeded_today +
    summary.recent.length

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Queues"
        description="Live job queue state. Definitions live in Build → Jobs; this view is the runtime."
      />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <KpiTile
          label="Pending"
          value={summary.pending}
          description={
            summary.pending === 0 ? "No jobs waiting to run." : "Jobs waiting to run."
          }
          icon={Timer}
        />
        <KpiTile
          label="Running"
          value={summary.running}
          description={
            summary.running === 0 ? "No jobs in flight." : "Jobs currently executing."
          }
          icon={Loader2}
        />
        <KpiTile
          label="Failed"
          value={summary.failed}
          description={
            summary.failed === 0
              ? "No failed jobs in the recent window."
              : "Failed jobs in the recent window."
          }
          icon={TriangleAlert}
          tone={summary.failed > 0 ? "danger" : "default"}
        />
      </div>

      <div className="grid grid-cols-1 gap-4 sm:max-w-sm">
        <KpiTile
          label="Succeeded today"
          value={summary.succeeded_today}
          description="Resets at 00:00 UTC."
          icon={CheckCircle2}
        />
      </div>

      <section className="flex flex-col gap-3">
        <div className="flex items-baseline justify-between">
          <h2 className="text-lg font-semibold tracking-tight">Recent jobs</h2>
          <span className="text-muted-foreground text-xs">
            {summary.recent.length === 0
              ? "No jobs in window"
              : `${summary.recent.length} job${summary.recent.length === 1 ? "" : "s"}`}
          </span>
        </div>

        {error && totalKnownJobs === 0 ? (
          <div className="bg-card text-muted-foreground rounded-xl border border-dashed p-6 text-sm">
            <p className="text-foreground mb-1 font-medium">
              Queue summary isn&apos;t available yet
            </p>
            <p className="mb-2">
              The runtime returned:{" "}
              <code className="bg-muted rounded px-1 font-mono text-xs">
                {error}
              </code>
              .
            </p>
            <p>
              This usually means the queues endpoint hasn&apos;t shipped — it
              lands in Phase 5. The view will populate once the endpoint is
              live.
            </p>
          </div>
        ) : (
          <RecentJobsTable jobs={summary.recent} />
        )}
      </section>

      {totalKnownJobs === 0 && !error ? (
        <div className="text-muted-foreground flex items-center gap-2 text-xs">
          <ListChecks className="size-3.5" />
          Define a background job in{" "}
          <code className="font-mono">apps/backend/jobs/</code> to populate this
          page.
        </div>
      ) : null}
    </div>
  )
}
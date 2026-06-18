// SPDX-License-Identifier: Apache-2.0

import { ArrowUpRight, FileText, Send } from "lucide-react"
import Link from "next/link"

import { ScrollArea } from "@/components/ui/scroll-area"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import type { TenantDrilldown } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"
import {
  formatAge,
  formatCurrency,
  formatRunDuration,
  mergeActivity,
} from "@/lib/tenant-detail/derive"

// Overview tab — activity feed (recent runs + webhooks merged) + a
// compact recent-runs list. Cost/Errors deep dives link to the global
// surfaces filtered to this tenant rather than duplicating the
// composition here.

const DOT: Record<StatusState, string> = {
  ok: "bg-success",
  watch: "bg-warning",
  act: "bg-destructive",
  idle: "bg-muted-foreground/60",
}

export function OverviewTab({ drilldown }: { drilldown: TenantDrilldown }) {
  const activity = mergeActivity(drilldown)
  return (
    <div className="grid gap-stack lg:grid-cols-2">
      <ActivityCard
        events={activity}
        tenantId={drilldown.tenant.id}
        tenantName={drilldown.tenant.name}
      />
      <RecentRunsCard drilldown={drilldown} />
    </div>
  )
}

function ActivityCard({
  events,
  tenantId,
  tenantName,
}: {
  events: ReturnType<typeof mergeActivity>
  tenantId: string
  tenantName: string
}) {
  return (
    <ZoneCard>
      <ZoneCardHeader
        title="Recent activity"
        subtitle={`Merged feed across runs + webhooks for ${tenantName}`}
        trailing={
          <Link
            href={`/activity/runs?tenant=${encodeURIComponent(tenantId)}`}
            className="inline-flex items-center gap-inline text-meta text-muted-foreground hover:text-foreground"
          >
            View runs
            <ArrowUpRight className="size-3" aria-hidden />
          </Link>
        }
      />
      {events.length === 0 ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          No recent runs or webhook deliveries for this tenant.
        </p>
      ) : (
        <ScrollArea className="max-h-80">
          <ul role="list" className="divide-y">
            {events.map((event) => {
              const Icon = event.kind === "run" ? FileText : Send
              return (
                <li
                  key={event.id}
                  className="flex items-center gap-stack px-row-x py-row-y text-meta"
                >
                  <span
                    aria-hidden
                    className={`inline-block size-icon-dot rounded-pill ${DOT[event.severity]}`}
                  />
                  <span className="shrink-0 font-mono text-meta tabular-nums text-muted-foreground">
                    {formatAge(event.occurredAt)}
                  </span>
                  <Icon
                    className="size-3.5 shrink-0 text-muted-foreground"
                    aria-hidden
                  />
                  <span className="min-w-0 flex-1 truncate text-foreground">
                    {event.title}
                  </span>
                  <span className="shrink-0 font-mono text-meta uppercase tracking-wide text-muted-foreground">
                    {event.kind}
                  </span>
                </li>
              )
            })}
          </ul>
        </ScrollArea>
      )}
    </ZoneCard>
  )
}

function RecentRunsCard({ drilldown }: { drilldown: TenantDrilldown }) {
  const runs = drilldown.recent_runs.slice(0, 10)
  return (
    <ZoneCard>
      <ZoneCardHeader
        title="Recent runs"
        subtitle={`${runs.length} latest`}
        trailing={
          <Link
            href={`/activity/runs?tenant=${encodeURIComponent(drilldown.tenant.id)}`}
            className="inline-flex items-center gap-inline text-meta text-muted-foreground hover:text-foreground"
          >
            View all
            <ArrowUpRight className="size-3" aria-hidden />
          </Link>
        }
      />
      {runs.length === 0 ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          No runs recorded for this tenant.
        </p>
      ) : (
        <ul role="list" className="divide-y">
          {runs.map((run) => (
            <li
              key={run.id}
              className="grid grid-cols-[12px_72px_minmax(0,1fr)_72px_80px] items-center gap-stack px-row-x py-row-y text-meta"
            >
              <span
                aria-hidden
                className={`inline-block size-icon-dot rounded-pill ${DOT[run.status === "failed" ? "act" : run.status === "succeeded" ? "ok" : "watch"]}`}
              />
              <span className="font-mono tabular-nums text-muted-foreground">
                {formatAge(run.started_at)}
              </span>
              <span className="min-w-0 truncate font-mono text-foreground" title={run.agent}>
                {run.agent}
              </span>
              <span className="text-right font-mono tabular-nums text-foreground">
                {formatRunDuration(run.duration_ms)}
              </span>
              <span className="text-right font-mono tabular-nums text-foreground">
                {formatCurrency(run.cost_usd)}
              </span>
            </li>
          ))}
        </ul>
      )}
    </ZoneCard>
  )
}

// SPDX-License-Identifier: Apache-2.0

// run-summary-card.tsx — #25 inline summary of an AgentField run.
//
// Renders status, agent name, reasoner, timing, cost, and approval
// status. The card is the at-a-glance header on the run detail page;
// the full DAG / step inspector lives in AgentField's own UI and is
// reached via the "View in AgentField" button in <RunActions />.
//
// When AgentField is unreachable we render a graceful "Unavailable"
// badge instead of crashing the page (see run-detail-view.tsx for the
// fetch failure path).

"use client"

import { ExternalLink } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import type { RunAgentField } from "@/lib/api"

import {
  formatCost,
  formatDuration,
  formatRelative,
} from "../../../_lib/format"

type RunSummaryCardProps = {
  data: RunAgentField | null
  unavailable?: boolean
  unavailableReason?: string
}

// statusBadgeVariant maps AgentField's status strings to shadcn Badge
// tones. AgentField's enum isn't strictly typed on our side; we tolerate
// unknown values by falling through to the neutral "secondary" badge so
// the UI never crashes on a schema addition.
function statusBadgeVariant(
  status: string,
): React.ComponentProps<typeof Badge>["variant"] {
  switch (status.toLowerCase()) {
    case "running":
    case "started":
    case "in_progress":
      return "default"
    case "succeeded":
    case "completed":
      return "outline"
    case "failed":
    case "error":
      return "destructive"
    case "paused":
    case "queued":
    case "pending":
    case "awaiting_approval":
    case "awaiting-approval":
      return "secondary"
    case "cancelled":
    case "canceled":
      return "secondary"
    default:
      return "secondary"
  }
}

export function RunSummaryCard({
  data,
  unavailable,
  unavailableReason,
}: RunSummaryCardProps) {
  if (unavailable) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            AgentField summary
            <Badge variant="secondary">Unavailable</Badge>
          </CardTitle>
          <CardDescription>
            {unavailableReason ??
              "AgentField is unreachable right now. The deep inspector is also unavailable. Try again in a moment."}
          </CardDescription>
        </CardHeader>
      </Card>
    )
  }

  if (!data) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            AgentField summary
            <Badge variant="outline">No execution</Badge>
          </CardTitle>
          <CardDescription>
            This run was created before AgentField started recording
            executions. Some details are not available.
          </CardDescription>
        </CardHeader>
      </Card>
    )
  }

  const o = data.overview
  const status = o.status || "unknown"

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex flex-wrap items-center gap-2">
          <Badge variant={statusBadgeVariant(status)}>{status}</Badge>
          <span className="font-mono text-xs text-muted-foreground">
            {o.execution_id}
          </span>
        </CardTitle>
        <CardDescription className="font-mono text-xs">
          {o.agent_name ?? "—"}
          {o.reasoner ? ` · ${o.reasoner}` : ""}
        </CardDescription>
        <CardAction>
          <a
            className="inline-flex items-center gap-1 text-xs underline-offset-2 hover:underline"
            href={data.details_url}
            target="_blank"
            rel="noopener noreferrer"
          >
            View in AgentField
            <ExternalLink className="size-3" />
          </a>
        </CardAction>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-2 gap-3 text-xs sm:grid-cols-4">
          <Stat label="Started" value={formatRelative(o.started_at)} />
          <Stat label="Ended" value={formatRelative(o.ended_at)} />
          <Stat
            label="Duration"
            value={formatDuration(o.duration_ms ?? undefined)}
          />
          <Stat label="Cost" value={formatCost(o.cost_usd)} />
          <Stat label="Approval" value={o.approval_status ?? "—"} />
        </div>
      </CardContent>
    </Card>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-muted-foreground text-[10px] font-medium uppercase tracking-wide">
        {label}
      </span>
      <span className="font-mono text-xs">{value}</span>
    </div>
  )
}

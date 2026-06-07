// Top-of-page hero metric strip: 4 KPI cards from the pool snapshot.
//
//   1. Warm — pre-warmed sandboxes ready to accept a workload.
//   2. Active — sandboxes currently executing a workload.
//   3. Queued — workloads waiting for capacity.
//   4. Cost today — accumulated sandbox spend for the current UTC day.

import {
  Activity,
  Clock,
  DollarSign,
  Flame,
} from "lucide-react"

import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import type { SandboxPoolStats } from "@/lib/api"

import { formatCurrencyCompact } from "../../cost/_components/format"

type SandboxKpiStripProps = {
  pool: SandboxPoolStats
}

export function SandboxKpiStrip({ pool }: SandboxKpiStripProps) {
  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4">
      <Card>
        <CardHeader>
          <CardDescription className="flex items-center gap-1.5">
            <Flame className="size-3" />
            Warm
          </CardDescription>
          <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
            {pool.warm.toLocaleString()}
          </CardTitle>
          <p className="text-muted-foreground mt-2 text-xs">
            Pre-warmed sandboxes ready to accept a workload.
          </p>
        </CardHeader>
      </Card>

      <Card>
        <CardHeader>
          <CardDescription className="flex items-center gap-1.5">
            <Activity className="size-3" />
            Active
          </CardDescription>
          <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
            {pool.active.toLocaleString()}
          </CardTitle>
          <p className="text-muted-foreground mt-2 text-xs">
            Sandboxes currently executing a workload.
          </p>
        </CardHeader>
      </Card>

      <Card>
        <CardHeader>
          <CardDescription className="flex items-center gap-1.5">
            <Clock className="size-3" />
            Queued
          </CardDescription>
          <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
            {pool.queued.toLocaleString()}
          </CardTitle>
          <p className="text-muted-foreground mt-2 text-xs">
            Workloads waiting for capacity to free up.
          </p>
        </CardHeader>
      </Card>

      <Card>
        <CardHeader>
          <CardDescription className="flex items-center gap-1.5">
            <DollarSign className="size-3" />
            Cost today
          </CardDescription>
          <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
            {formatCurrencyCompact(pool.cost_usd_today)}
          </CardTitle>
          <p className="text-muted-foreground mt-2 text-xs">
            {pool.total_runs_today.toLocaleString()} runs ·{" "}
            {pool.cpu_seconds_today.toLocaleString()} CPU·s
          </p>
        </CardHeader>
      </Card>
    </div>
  )
}

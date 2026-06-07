// SPDX-License-Identifier: Apache-2.0

// Top-of-page hero metric strip: 4 KPI cards from the notifications
// stats payload.
//
//   1. Sent today        — count of successful deliveries since UTC midnight.
//   2. Failed today      — count of failed deliveries since UTC midnight.
//   3. Queued            — rows currently at status='queued' or 'sending'.
//   4. Adapters active   — unique adapters with traffic in the active window.

import { Activity, CheckCircle2, Clock, XCircle } from "lucide-react"

import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import type { NotificationStats } from "@/lib/api"

type Props = {
  stats: NotificationStats
}

export function NotificationsKpiStrip({ stats }: Props) {
  const queued =
    (stats.by_status.queued ?? 0) + (stats.by_status.sending ?? 0)
  const adaptersActive = stats.by_adapter.filter((a) => a.count > 0).length

  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4">
      <Card>
        <CardHeader>
          <CardDescription className="flex items-center gap-1.5">
            <CheckCircle2 className="size-3" />
            Sent today
          </CardDescription>
          <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
            {stats.sent_today.toLocaleString()}
          </CardTitle>
          <p className="text-muted-foreground mt-2 text-xs">
            Successful deliveries since UTC midnight.
          </p>
        </CardHeader>
      </Card>

      <Card>
        <CardHeader>
          <CardDescription className="flex items-center gap-1.5">
            <XCircle className="size-3" />
            Failed today
          </CardDescription>
          <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
            {stats.failed_today.toLocaleString()}
          </CardTitle>
          <p className="text-muted-foreground mt-2 text-xs">
            Adapter or provider errors since UTC midnight.
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
            {queued.toLocaleString()}
          </CardTitle>
          <p className="text-muted-foreground mt-2 text-xs">
            Waiting on the worker (queued + sending right now).
          </p>
        </CardHeader>
      </Card>

      <Card>
        <CardHeader>
          <CardDescription className="flex items-center gap-1.5">
            <Activity className="size-3" />
            Adapters active
          </CardDescription>
          <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
            {adaptersActive.toLocaleString()}
          </CardTitle>
          <p className="text-muted-foreground mt-2 text-xs">
            Distinct adapters with traffic in the last 24h.
          </p>
        </CardHeader>
      </Card>
    </div>
  )
}
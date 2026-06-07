// SPDX-License-Identifier: Apache-2.0

// Recent webhook deliveries panel. Server component.

import { formatDistanceToNowStrict } from "date-fns"
import { ArrowDownLeft, ArrowUpRight, Webhook } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import type { HomeOverview } from "@/lib/api"

type Delivery = HomeOverview["recent_webhook_deliveries"][number]

const STATUS_VARIANT: Record<
  Delivery["status"],
  "default" | "secondary" | "destructive" | "outline"
> = {
  delivered: "default",
  failed: "destructive",
  pending: "secondary",
}

function fmtWhen(iso: string): string {
  try {
    return `${formatDistanceToNowStrict(new Date(iso))} ago`
  } catch {
    return iso
  }
}

function shorten(url: string): string {
  try {
    const u = new URL(url)
    return `${u.host}${u.pathname}`
  } catch {
    return url
  }
}

type WebhookListProps = {
  deliveries: Delivery[]
}

export function WebhookList({ deliveries }: WebhookListProps) {
  return (
    <Card className="gap-0">
      <CardHeader className="border-b pb-4">
        <CardTitle>Webhook deliveries</CardTitle>
        <CardDescription>
          Recent incoming and outgoing webhook events.
        </CardDescription>
      </CardHeader>
      {deliveries.length === 0 ? (
        <Empty className="border-0">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Webhook />
            </EmptyMedia>
            <EmptyTitle>No deliveries yet</EmptyTitle>
            <EmptyDescription>
              Configure a webhook in Build → Webhooks to see activity here.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <ul className="divide-y">
          {deliveries.map((d) => {
            const DirIcon = d.direction === "in" ? ArrowDownLeft : ArrowUpRight
            return (
              <li
                key={d.id}
                className="flex items-center gap-3 px-4 py-3 text-sm"
              >
                <span
                  aria-hidden
                  className="text-muted-foreground bg-muted flex size-7 shrink-0 items-center justify-center rounded-md"
                >
                  <DirIcon className="size-3.5" />
                </span>
                <div className="min-w-0 flex-1">
                  <div className="truncate font-medium" title={d.url}>
                    {shorten(d.url)}
                  </div>
                  <div className="text-muted-foreground text-xs">
                    {d.direction === "in" ? "Incoming" : "Outgoing"} ·{" "}
                    {fmtWhen(d.occurred_at)}
                  </div>
                </div>
                <Badge variant={STATUS_VARIANT[d.status]}>{d.status}</Badge>
              </li>
            )
          })}
        </ul>
      )}
    </Card>
  )
}
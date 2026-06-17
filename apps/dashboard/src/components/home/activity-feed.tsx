// SPDX-License-Identifier: Apache-2.0

import { Badge } from "@/components/ui/badge"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Separator } from "@/components/ui/separator"

import type { AdminEvent } from "@/lib/api"

// Unified activity feed. Source is the new /api/v1/admin/events endpoint
// (Gap 5). One row per event; severity drives the badge variant; all
// rendering is monochrome with status accents only on the severity chip.

const SEVERITY_VARIANT: Record<
  AdminEvent["severity"],
  "secondary" | "outline" | "destructive"
> = {
  info: "secondary",
  warning: "outline",
  critical: "destructive",
}

export function ActivityFeed({ events }: { events: AdminEvent[] }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Recent activity</CardTitle>
        <CardDescription>
          Runs, webhooks, alerts and audit events across your stack.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {events.length === 0 ? (
          <p className="text-meta text-muted-foreground">
            No activity yet. Make your first call to see events.
          </p>
        ) : (
          <ScrollArea className="max-h-[28rem] pr-stack">
            <ul className="flex flex-col">
              {events.map((event, idx) => (
                <li key={event.id} className="flex flex-col">
                  <ActivityEvent event={event} />
                  {idx < events.length - 1 ? (
                    <Separator className="my-stack" />
                  ) : null}
                </li>
              ))}
            </ul>
          </ScrollArea>
        )}
      </CardContent>
    </Card>
  )
}

function ActivityEvent({ event }: { event: AdminEvent }) {
  return (
    <div className="flex items-start gap-stack">
      <Badge variant={SEVERITY_VARIANT[event.severity]} className="text-meta">
        {event.severity}
      </Badge>
      <div className="flex flex-1 flex-col gap-inline">
        <div className="flex items-center gap-inline">
          <span className="text-body font-medium text-foreground">
            {event.title}
          </span>
          <span className="text-meta text-muted-foreground">
            {formatRelative(event.occurred_at)}
          </span>
        </div>
        {event.description ? (
          <p className="text-meta text-muted-foreground line-clamp-2">
            {event.description}
          </p>
        ) : null}
        <span className="text-meta text-muted-foreground">{event.source}</span>
      </div>
    </div>
  )
}

// Human-friendly relative time per development/ux/page-design-framework.md §8.
function formatRelative(iso: string): string {
  const ts = Date.parse(iso)
  if (Number.isNaN(ts)) return iso
  const diffMs = Date.now() - ts
  const sec = Math.round(diffMs / 1000)
  if (sec < 30) return "now"
  if (sec < 3600) return `${Math.round(sec / 60)}m ago`
  if (sec < 86_400) return `${Math.round(sec / 3600)}h ago`
  const days = Math.round(sec / 86_400)
  if (days < 7) return `${days}d ago`
  return new Date(ts).toISOString().slice(0, 10)
}

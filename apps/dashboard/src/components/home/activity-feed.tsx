// SPDX-License-Identifier: Apache-2.0

import {
  AlertTriangle,
  ArrowUpRight,
  CheckCircle2,
  CircleAlert,
  FileText,
  Info,
  Send,
  type LucideIcon,
} from "lucide-react"

import { ScrollArea } from "@/components/ui/scroll-area"

import type { AdminEvent } from "@/lib/api"

// Recent activity — fixed total height with internal scroll.
//
// The card height is locked so the page layout never shifts as new events
// arrive; the events themselves scroll inside a shadcn ScrollArea. This
// matches the framework's "anchor surfaces stay anchored" rule — the
// activity card is allowed to grow vertically only on its first render,
// after that newer events arrive at the top and older ones disappear
// below the fold.

// Shared with home-shell so the right column matches this exactly.
export const ACTIVITY_FEED_HEIGHT_PX = 480
const FEED_HEIGHT_PX = ACTIVITY_FEED_HEIGHT_PX

const SEVERITY_ORDER: Record<AdminEvent["severity"], number> = {
  critical: 0,
  warning: 1,
  info: 2,
}

const SEVERITY_DOT: Record<AdminEvent["severity"], string> = {
  critical: "bg-destructive",
  warning: "bg-warning",
  info: "bg-muted-foreground/40",
}

const SOURCE_ICON: Record<AdminEvent["source"], LucideIcon> = {
  runs: CheckCircle2,
  webhooks: Send,
  alerts: CircleAlert,
  activity: FileText,
}

const SOURCE_ICON_FALLBACK: Record<AdminEvent["severity"], LucideIcon> = {
  critical: AlertTriangle,
  warning: AlertTriangle,
  info: Info,
}

export function ActivityFeed({ events }: { events: AdminEvent[] }) {
  const sorted = [...events].sort((a, b) => {
    const sa = SEVERITY_ORDER[a.severity]
    const sb = SEVERITY_ORDER[b.severity]
    if (sa !== sb) return sa - sb
    return a.occurred_at < b.occurred_at ? 1 : -1
  })
  return (
    <section
      aria-labelledby="activity-heading"
      className="flex flex-col rounded-md border bg-card"
      style={{ height: FEED_HEIGHT_PX }}
    >
      <header className="flex shrink-0 items-center justify-between border-b px-row-x py-row-y">
        <h2 id="activity-heading" className="text-body font-medium text-foreground">
          Activity
        </h2>
        <span className="text-meta text-muted-foreground">
          {sorted.length} events
        </span>
      </header>
      {sorted.length === 0 ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          No activity yet. Make a call or run{" "}
          <code className="rounded bg-muted px-1 py-0.5 font-mono text-meta">
            ./scripts/demo-traffic.sh seed
          </code>{" "}
          to see events appear here.
        </p>
      ) : (
        <ScrollArea className="min-h-0 flex-1">
          <ul role="list" className="divide-y">
            {sorted.map((event) => (
              <ActivityRow key={event.id} event={event} />
            ))}
          </ul>
        </ScrollArea>
      )}
    </section>
  )
}

function ActivityRow({ event }: { event: AdminEvent }) {
  const Icon =
    SOURCE_ICON[event.source] ?? SOURCE_ICON_FALLBACK[event.severity]
  return (
    <li className="group flex items-center gap-stack px-row-x py-row-y text-body transition-colors hover:bg-accent/40">
      <span
        aria-hidden
        className={`inline-block size-icon-dot shrink-0 rounded-pill ${SEVERITY_DOT[event.severity]}`}
      />
      <span className="shrink-0 font-mono text-meta tabular-nums text-muted-foreground">
        {formatRelative(event.occurred_at)}
      </span>
      <Icon className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
      <span className="shrink-0 font-mono text-meta uppercase tracking-wide text-muted-foreground">
        {event.kind}
      </span>
      <span className="min-w-0 flex-1 truncate text-foreground">
        {event.title}
      </span>
      <ArrowUpRight
        aria-hidden
        className="size-3.5 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100"
      />
    </li>
  )
}

function formatRelative(iso: string): string {
  const ts = Date.parse(iso)
  if (Number.isNaN(ts)) return iso
  const diffMs = Date.now() - ts
  const sec = Math.round(diffMs / 1000)
  if (sec < 30) return "now"
  if (sec < 3600) return `${Math.round(sec / 60)}m`
  if (sec < 86_400) return `${Math.round(sec / 3600)}h`
  const days = Math.round(sec / 86_400)
  if (days < 7) return `${days}d`
  return new Date(ts).toISOString().slice(5, 10)
}

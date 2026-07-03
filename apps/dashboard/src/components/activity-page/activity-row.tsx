// SPDX-License-Identifier: Apache-2.0

"use client"

import { ChevronDown, ChevronRight } from "lucide-react"

import { Badge } from "@/components/ui/badge"

import type { ActivityEntry } from "@/lib/api"
import {
  formatWhen,
  shortId,
  truncateUserAgent,
} from "@/lib/activity-page/derive"
import { cn } from "@/lib/utils"

// One row in the activity table. Click toggles the expandable metadata
// JSON underneath. actor_type is free-form server-side but the
// runtime's canonical values are user / api_key / system / anonymous.

export const ACTIVITY_ROW_COLUMNS =
  "grid-cols-[130px_84px_minmax(0,1fr)_minmax(0,1fr)_minmax(90px,0.6fr)_minmax(0,1.2fr)_18px]"

const ACTOR_BADGE_VARIANT: Record<
  string,
  "secondary" | "outline" | "ghost"
> = {
  user: "secondary",
  api_key: "outline",
  system: "ghost",
  anonymous: "ghost",
}

interface ActivityRowProps {
  entry: ActivityEntry
  tenantName?: string
  expanded: boolean
  onToggle: () => void
}

export function ActivityRow({
  entry,
  tenantName,
  expanded,
  onToggle,
}: ActivityRowProps) {
  const hasMetadata = Object.keys(entry.metadata).length > 0
  const actorId = entry.user_id ?? entry.api_key_id
  const Chevron = expanded ? ChevronDown : ChevronRight
  return (
    <div className="border-b">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={expanded}
        className={cn(
          "group grid w-full items-center gap-stack px-row-x py-row-y text-left text-meta transition-colors",
          ACTIVITY_ROW_COLUMNS,
          expanded ? "bg-accent/30" : "hover:bg-accent/40",
        )}
      >
        <time
          dateTime={entry.occurred_at}
          suppressHydrationWarning
          className="truncate font-mono tabular-nums text-muted-foreground"
          title={entry.occurred_at}
        >
          {formatWhen(entry.occurred_at)}
        </time>
        <span className="min-w-0">
          <Badge
            variant={ACTOR_BADGE_VARIANT[entry.actor_type] ?? "outline"}
            className="max-w-full font-mono"
          >
            <span className="truncate">{entry.actor_type}</span>
          </Badge>
        </span>
        <div className="flex min-w-0 flex-col gap-tile-tight">
          <span
            className="truncate font-mono text-body text-foreground"
            title={entry.action}
          >
            {entry.action}
          </span>
          {actorId ? (
            <span
              className="truncate font-mono text-meta text-muted-foreground"
              title={actorId}
            >
              {shortId(actorId)}
            </span>
          ) : null}
        </div>
        <div className="flex min-w-0 flex-col gap-tile-tight">
          <span className="truncate text-meta text-foreground">
            {entry.resource_type ?? "—"}
          </span>
          {entry.resource_id ? (
            <span
              className="truncate font-mono text-meta text-muted-foreground"
              title={entry.resource_id}
            >
              {shortId(entry.resource_id)}
            </span>
          ) : null}
        </div>
        <span
          className="truncate text-meta text-muted-foreground"
          title={entry.tenant_id}
        >
          {tenantName ?? entry.tenant_id.slice(0, 8)}
        </span>
        <div className="flex min-w-0 flex-col gap-tile-tight">
          <span className="truncate font-mono text-meta text-foreground">
            {entry.ip ?? "—"}
          </span>
          {entry.user_agent ? (
            <span
              className="truncate text-meta text-muted-foreground"
              title={entry.user_agent}
            >
              {truncateUserAgent(entry.user_agent)}
            </span>
          ) : null}
        </div>
        <Chevron
          aria-hidden
          className={cn(
            "size-3.5 text-muted-foreground transition-opacity",
            expanded ? "opacity-100" : "opacity-0 group-hover:opacity-100",
          )}
        />
      </button>
      {expanded ? (
        hasMetadata ? (
          <pre className="overflow-x-auto border-t bg-muted/40 px-row-x py-row-y font-mono text-meta text-muted-foreground">
            {JSON.stringify(entry.metadata, null, 2)}
          </pre>
        ) : (
          <p className="border-t bg-muted/40 px-row-x py-row-y text-meta text-muted-foreground">
            No metadata recorded for this entry.
          </p>
        )
      ) : null}
    </div>
  )
}

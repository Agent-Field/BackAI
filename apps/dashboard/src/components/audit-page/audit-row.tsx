// SPDX-License-Identifier: Apache-2.0

"use client"

import { ChevronDown, ChevronRight } from "lucide-react"

import { Badge } from "@/components/ui/badge"

import type { AuditEntry } from "@/lib/api"
import { formatWhen, shortId } from "@/lib/audit-page/derive"
import { cn } from "@/lib/utils"

// One row in the audit table. Click toggles the expandable metadata
// JSON underneath. Actor is whichever of user_id / api_key_id the
// runtime recorded for the mutation.

export const AUDIT_ROW_COLUMNS =
  "grid-cols-[130px_minmax(0,1.2fr)_minmax(0,1fr)_minmax(90px,0.6fr)_minmax(0,1fr)_18px]"

interface AuditRowProps {
  entry: AuditEntry
  tenantName?: string
  expanded: boolean
  onToggle: () => void
}

export function AuditRow({
  entry,
  tenantName,
  expanded,
  onToggle,
}: AuditRowProps) {
  const hasMetadata = Object.keys(entry.metadata).length > 0
  const actorId = entry.user_id ?? entry.api_key_id
  const actorKind = entry.user_id ? "user" : entry.api_key_id ? "api key" : null
  const Chevron = expanded ? ChevronDown : ChevronRight
  return (
    <div className="border-b">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={expanded}
        className={cn(
          "group grid w-full items-center gap-stack px-row-x py-row-y text-left text-meta transition-colors",
          AUDIT_ROW_COLUMNS,
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
          <Badge variant="outline" className="max-w-full font-mono">
            <span className="truncate">{entry.action}</span>
          </Badge>
        </span>
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
          title={entry.tenant_id ?? undefined}
        >
          {tenantName ?? (entry.tenant_id ? entry.tenant_id.slice(0, 8) : "—")}
        </span>
        {actorId ? (
          <div className="flex min-w-0 flex-col gap-tile-tight">
            <span
              className="truncate font-mono text-meta text-foreground"
              title={actorId}
            >
              {shortId(actorId)}
            </span>
            <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
              {actorKind}
            </span>
          </div>
        ) : (
          <span className="text-meta text-muted-foreground">—</span>
        )}
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

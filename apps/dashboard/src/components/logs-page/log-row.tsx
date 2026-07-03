// SPDX-License-Identifier: Apache-2.0

"use client"

import { ChevronRight } from "lucide-react"
import { useState } from "react"

import { Badge } from "@/components/ui/badge"
import { CopyButton } from "@/components/ui/copy-button"

import type { LogLine } from "@/lib/api"
import { formatLogTime, logLineDetails } from "@/lib/logs-page/derive"
import { cn } from "@/lib/utils"

// One log line. The whole row is a disclosure button — clicking expands
// the structured metadata (request id / tenant / agent) as
// pretty-printed JSON. Error rows pick up the destructive left-edge
// accent so scanning down the left edge finds trouble fast.

const LEVEL_BADGE: Record<LogLine["level"], { label: string; className: string }> = {
  debug: { label: "debug", className: "border-border text-muted-foreground" },
  info: { label: "info", className: "bg-muted text-muted-foreground" },
  warn: { label: "warn", className: "bg-warning/15 text-warning" },
  error: { label: "error", className: "bg-destructive/10 text-destructive" },
  fatal: { label: "fatal", className: "bg-destructive/10 text-destructive" },
}

interface LogRowProps {
  line: LogLine
}

export function LogRow({ line }: LogRowProps) {
  const [expanded, setExpanded] = useState(false)
  const badge = LEVEL_BADGE[line.level]
  const details = logLineDetails(line)
  const hasDetails = Object.keys(details).length > 0
  const detailsJson = hasDetails ? JSON.stringify(details, null, 2) : null

  const accent =
    line.level === "error" || line.level === "fatal"
      ? "border-l-destructive"
      : line.level === "warn"
        ? "border-l-warning"
        : "border-l-transparent"

  return (
    <div className={cn("border-l-4 border-b", accent)}>
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
        className={cn(
          "group grid w-full items-center gap-stack px-row-x py-row-y text-left text-meta transition-colors",
          LOG_ROW_COLUMNS,
          expanded ? "bg-accent/30" : "hover:bg-accent/40",
        )}
      >
        {/* Server renders UTC, browser renders local — suppress the
            one-off hydration diff instead of flashing an empty cell. */}
        <span
          suppressHydrationWarning
          className="truncate font-mono tabular-nums text-muted-foreground"
          title={line.ts}
        >
          {formatLogTime(line.ts)}
        </span>
        <Badge variant="outline" className={cn("border-transparent", badge.className)}>
          {badge.label}
        </Badge>
        <span
          className="truncate font-mono text-meta text-muted-foreground"
          title={line.service}
        >
          {line.service || "—"}
        </span>
        <span className="truncate text-body text-foreground" title={line.msg}>
          {line.msg}
        </span>
        <ChevronRight
          aria-hidden
          className={cn(
            "size-3.5 text-muted-foreground transition-transform",
            expanded
              ? "rotate-90"
              : "opacity-0 group-hover:opacity-100",
          )}
        />
      </button>
      {expanded ? (
        <div className="flex flex-col gap-stack px-row-x pb-row-y">
          <p className="break-words font-mono text-meta text-foreground">
            {line.msg}
          </p>
          {detailsJson ? (
            <div className="flex items-start gap-inline">
              <pre className="min-w-0 flex-1 overflow-x-auto rounded-md bg-muted/50 px-row-x py-tile-tight font-mono text-meta text-muted-foreground">
                {detailsJson}
              </pre>
              <CopyButton value={detailsJson} label="Copy fields JSON" />
            </div>
          ) : (
            <p className="text-meta text-muted-foreground">
              No structured fields on this line.
            </p>
          )}
        </div>
      ) : null}
    </div>
  )
}

export const LOG_ROW_COLUMNS =
  "grid-cols-[72px_64px_minmax(80px,160px)_minmax(0,1fr)_18px]"

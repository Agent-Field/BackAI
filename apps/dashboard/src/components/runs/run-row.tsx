// SPDX-License-Identifier: Apache-2.0

"use client"

import { ChevronRight } from "lucide-react"

import type { Run } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"
import {
  classifyRunStatus,
  formatRunAge,
  formatRunCost,
  formatRunDuration,
  friendlyAgentName,
  shortRunId,
} from "@/lib/runs/derive"
import { cn } from "@/lib/utils"

// One row in the runs table. Status dot left, cells across, chevron
// right. Failed/running rows pick up a left border accent so the
// scan-down-the-left-edge story works.

const DOT: Record<StatusState, string> = {
  ok: "bg-success",
  watch: "bg-warning",
  act: "bg-destructive",
  idle: "bg-muted-foreground/60",
}

const STATUS_LABEL: Record<Run["status"], string> = {
  succeeded: "ok",
  failed: "failed",
  running: "running",
  queued: "queued",
  cancelled: "cancelled",
}

interface RunRowProps {
  run: Run
  selected: boolean
  onSelect: (run: Run) => void
}

export function RunRow({ run, selected, onSelect }: RunRowProps) {
  const tone = classifyRunStatus(run.status)
  const statusTone =
    tone === "ok"
      ? "text-muted-foreground"
      : tone === "watch"
        ? "text-warning font-medium"
        : tone === "act"
          ? "text-destructive font-medium"
          : "text-muted-foreground"
  const accent =
    tone === "act"
      ? "border-l-destructive"
      : tone === "watch"
        ? "border-l-warning"
        : "border-l-transparent"
  return (
    <button
      type="button"
      onClick={() => onSelect(run)}
      aria-pressed={selected}
      className={cn(
        "group grid w-full items-center gap-stack border-l-4 border-b px-row-x py-row-y text-left text-meta transition-colors",
        "grid-cols-[12px_72px_minmax(0,1fr)_minmax(0,1fr)_72px_80px_80px_18px]",
        accent,
        selected ? "bg-accent/30" : "hover:bg-accent/40",
      )}
    >
      <span
        aria-hidden
        className={`inline-block size-icon-dot rounded-pill ${DOT[tone]}`}
      />
      <span className="truncate font-mono tabular-nums text-muted-foreground">
        {formatRunAge(run.started_at)}
      </span>
      <div className="flex min-w-0 flex-col gap-tile-tight">
        <span
          className="truncate font-mono text-body text-foreground"
          title={run.agent}
        >
          {friendlyAgentName(run.agent)}
        </span>
        {run.error ? (
          <span className="truncate text-meta text-destructive" title={run.error}>
            {run.error.length > 80 ? `${run.error.slice(0, 78)}…` : run.error}
          </span>
        ) : (
          <span className="truncate font-mono text-meta text-muted-foreground">
            {shortRunId(run.id)}
          </span>
        )}
      </div>
      <div className="flex min-w-0 flex-col gap-tile-tight">
        <span
          className="truncate text-meta text-foreground"
          title={run.tenant_id ?? undefined}
        >
          {run.tenant_name ?? "—"}
        </span>
        {run.tenant_id ? (
          <span className="truncate font-mono text-meta text-muted-foreground">
            {run.tenant_id.slice(0, 8)}
          </span>
        ) : null}
      </div>
      <span className="text-right font-mono tabular-nums text-foreground">
        {formatRunDuration(run.duration_ms)}
      </span>
      <span className="text-right font-mono tabular-nums text-foreground">
        {formatRunCost(run.cost_usd)}
      </span>
      <span className={`text-right ${statusTone}`}>
        {STATUS_LABEL[run.status]}
      </span>
      <ChevronRight
        aria-hidden
        className="size-3.5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100"
      />
    </button>
  )
}

export const RUN_ROW_COLUMNS =
  "grid-cols-[12px_72px_minmax(0,1fr)_minmax(0,1fr)_72px_80px_80px_18px]"

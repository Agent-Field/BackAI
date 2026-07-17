// SPDX-License-Identifier: Apache-2.0

"use client"

import { ChevronDown, ChevronRight, Pause, Play, Zap } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { RelativeTime } from "@/components/ui/relative-time"

import { api } from "@/lib/api"
import type { Cron } from "@/lib/api"
import {
  classifyCron,
  formatClock,
  formatRelativeTime,
  safeStringifyArgs,
  shortCronId,
} from "@/lib/crons-page/derive"
import type { StatusState } from "@/lib/home/types"
import { cn } from "@/lib/utils"

// One row in the crons table. Status dot left, cells across, chevron
// right. Clicking expands an inline detail panel with the args JSON, the
// full identity, and the two mutations the SDK exposes — "Trigger now"
// (enqueues a job immediately) and pause/resume (PUT .../active).

const DOT: Record<StatusState, string> = {
  ok: "bg-success",
  watch: "bg-warning",
  act: "bg-destructive",
  idle: "bg-muted-foreground/60",
}

interface CronRowProps {
  cron: Cron
  expanded: boolean
  onToggle: (cron: Cron) => void
  onMutated: () => Promise<void> | void
}

export function CronRow({ cron, expanded, onToggle, onMutated }: CronRowProps) {
  const tone = classifyCron(cron)
  const accent = cron.is_active ? "border-l-success" : "border-l-transparent"
  return (
    <div className="flex flex-col">
      <button
        type="button"
        onClick={() => onToggle(cron)}
        aria-expanded={expanded}
        className={cn(
          "group grid w-full items-center gap-stack border-l-4 border-b px-row-x py-row-y text-left text-meta transition-colors",
          CRON_ROW_COLUMNS,
          accent,
          expanded ? "bg-accent/30" : "hover:bg-accent/40",
        )}
      >
        <span aria-hidden className={`inline-block size-icon-dot rounded-pill ${DOT[tone]}`} />
        <div className="flex min-w-0 flex-col gap-tile-tight">
          <span className="truncate text-body text-foreground" title={cron.name}>
            {cron.name}
          </span>
          <span
            className="truncate font-mono text-meta text-muted-foreground"
            title={cron.job_name}
          >
            {cron.job_name}
          </span>
        </div>
        <span className="min-w-0">
          <Badge variant="secondary" className="font-mono" title={cron.schedule}>
            {cron.schedule}
          </Badge>
        </span>
        <RelativeTime
          iso={cron.last_run_at}
          format={formatRelativeTime}
          className="truncate text-right font-mono tabular-nums text-muted-foreground"
        />
        <span
          className={cn(
            "truncate text-right font-mono tabular-nums",
            cron.is_active ? "text-foreground" : "text-muted-foreground",
          )}
        >
          {cron.is_active ? (
            <RelativeTime iso={cron.next_run_at} format={formatRelativeTime} />
          ) : (
            "—"
          )}
        </span>
        <span
          className={cn(
            "text-right",
            cron.is_active ? "text-success font-medium" : "text-muted-foreground",
          )}
        >
          {cron.is_active ? "active" : "paused"}
        </span>
        {expanded ? (
          <ChevronDown aria-hidden className="size-3.5 text-muted-foreground" />
        ) : (
          <ChevronRight
            aria-hidden
            className="size-3.5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100"
          />
        )}
      </button>
      {expanded ? <CronDetail cron={cron} onMutated={onMutated} /> : null}
    </div>
  )
}

// Inline detail panel — identity + args JSON + the two mutations.
// Rendered as a sibling of the row button so we never nest interactive
// elements.

function CronDetail({ cron, onMutated }: { cron: Cron; onMutated: () => Promise<void> | void }) {
  const [triggering, setTriggering] = useState(false)
  const [toggling, setToggling] = useState(false)
  const args = safeStringifyArgs(cron.args)

  const trigger = async () => {
    if (triggering) return
    setTriggering(true)
    try {
      const job = await api.crons.trigger(cron.id)
      toast.success("Cron triggered", {
        description: `${cron.name} enqueued ${cron.job_name} · ${shortCronId(job.id)}`,
      })
      await onMutated()
    } catch (err) {
      toast.error("Could not trigger cron", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setTriggering(false)
    }
  }

  const toggleActive = async () => {
    if (toggling) return
    setToggling(true)
    try {
      await api.crons.setActive(cron.id, !cron.is_active)
      toast.success(cron.is_active ? "Cron paused" : "Cron resumed", {
        description: cron.name,
      })
      await onMutated()
    } catch (err) {
      toast.error("Could not update cron", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setToggling(false)
    }
  }

  return (
    <div className="flex flex-col gap-stack border-b bg-accent/10 px-row-x py-tile">
      <dl className="grid grid-cols-2 gap-stack text-meta md:grid-cols-4">
        <DetailMeta label="Cron ID" value={cron.id} mono />
        <DetailMeta label="Schedule" value={cron.schedule} mono />
        <DetailMeta label="Last run" value={formatClock(cron.last_run_at)} />
        <DetailMeta
          label="Next run"
          value={cron.is_active ? formatClock(cron.next_run_at) : "paused"}
        />
        <DetailMeta label="Job kind" value={cron.job_name} mono />
        <DetailMeta label="Tenant" value={cron.tenant_id ?? "platform"} mono />
        <DetailMeta label="Created" value={formatClock(cron.created_at)} />
      </dl>
      <div className="flex flex-col gap-tile-tight">
        <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">Args</span>
        {args ? (
          <pre className="max-h-48 overflow-auto rounded-md border bg-background px-row-x py-tile font-mono text-meta text-foreground">
            {args}
          </pre>
        ) : (
          <p className="rounded-md border border-dashed border-muted-foreground/40 px-row-x py-tile text-meta text-muted-foreground">
            No args forwarded — the job runs with an empty payload.
          </p>
        )}
      </div>
      <div className="flex justify-end gap-stack">
        <Button
          type="button"
          size="sm"
          variant="outline"
          className="gap-inline"
          disabled={toggling}
          onClick={toggleActive}
        >
          {cron.is_active ? (
            <Pause className="size-3.5" aria-hidden />
          ) : (
            <Play className="size-3.5" aria-hidden />
          )}
          {toggling ? "Saving…" : cron.is_active ? "Pause" : "Resume"}
        </Button>
        <Button
          type="button"
          size="sm"
          variant="outline"
          className="gap-inline"
          disabled={triggering}
          onClick={trigger}
        >
          <Zap className={`size-3.5 ${triggering ? "animate-pulse" : ""}`} aria-hidden />
          {triggering ? "Triggering…" : "Trigger now"}
        </Button>
      </div>
    </div>
  )
}

function DetailMeta({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex min-w-0 flex-col gap-tile-tight">
      <dt className="text-eyebrow uppercase tracking-wide text-muted-foreground">{label}</dt>
      <dd className={`truncate text-meta text-foreground ${mono ? "font-mono" : ""}`} title={value}>
        {value}
      </dd>
    </div>
  )
}

export const CRON_ROW_COLUMNS = "grid-cols-[12px_minmax(0,1fr)_minmax(0,132px)_88px_88px_64px_18px]"

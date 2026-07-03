// SPDX-License-Identifier: Apache-2.0

"use client"

import { ChevronDown, ChevronRight, RotateCcw } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"

import { api } from "@/lib/api"
import type { Job } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"
import {
  classifyJobState,
  formatAttempts,
  formatJobAge,
  isRetryableJob,
  lastJobError,
  safeStringifyArgs,
  shortJobId,
} from "@/lib/queue-page/derive"
import { cn } from "@/lib/utils"

// One row in the jobs table. Status dot left, cells across, chevron
// right. Clicking expands an inline detail panel with the args JSON,
// the last error, and a Retry action for retryable/discarded jobs.

const DOT: Record<StatusState, string> = {
  ok: "bg-success",
  watch: "bg-warning",
  act: "bg-destructive",
  idle: "bg-muted-foreground/60",
}

interface JobRowProps {
  job: Job
  expanded: boolean
  onToggle: (job: Job) => void
  onMutated: () => Promise<void> | void
}

export function JobRow({ job, expanded, onToggle, onMutated }: JobRowProps) {
  const tone = classifyJobState(job.state)
  const stateTone =
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
  const error = lastJobError(job)
  return (
    <div className="flex flex-col">
      <button
        type="button"
        onClick={() => onToggle(job)}
        aria-expanded={expanded}
        className={cn(
          "group grid w-full items-center gap-stack border-l-4 border-b px-row-x py-row-y text-left text-meta transition-colors",
          JOB_ROW_COLUMNS,
          accent,
          expanded ? "bg-accent/30" : "hover:bg-accent/40",
        )}
      >
        <span
          aria-hidden
          className={`inline-block size-icon-dot rounded-pill ${DOT[tone]}`}
        />
        <span className="truncate font-mono tabular-nums text-muted-foreground">
          {formatJobAge(job.enqueued_at)}
        </span>
        <div className="flex min-w-0 flex-col gap-tile-tight">
          <span
            className="truncate font-mono text-body text-foreground"
            title={job.name}
          >
            {job.name}
          </span>
          {error ? (
            <span className="truncate text-meta text-destructive" title={error}>
              {error.length > 80 ? `${error.slice(0, 78)}…` : error}
            </span>
          ) : (
            <span className="truncate font-mono text-meta text-muted-foreground">
              {shortJobId(job.id)}
            </span>
          )}
        </div>
        <span
          className="truncate font-mono text-meta text-muted-foreground"
          title={job.tenant_id ?? undefined}
        >
          {job.tenant_id ? job.tenant_id.slice(0, 8) : "—"}
        </span>
        <span className="text-right font-mono tabular-nums text-foreground">
          {formatAttempts(job)}
        </span>
        <span className={`text-right ${stateTone}`}>{job.state}</span>
        {expanded ? (
          <ChevronDown aria-hidden className="size-3.5 text-muted-foreground" />
        ) : (
          <ChevronRight
            aria-hidden
            className="size-3.5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100"
          />
        )}
      </button>
      {expanded ? <JobDetail job={job} onMutated={onMutated} /> : null}
    </div>
  )
}

// Inline detail panel — args JSON + last error + Retry. Rendered as a
// sibling of the row button so we never nest interactive elements.

function JobDetail({
  job,
  onMutated,
}: {
  job: Job
  onMutated: () => Promise<void> | void
}) {
  const [retrying, setRetrying] = useState(false)
  const error = lastJobError(job)
  const args = safeStringifyArgs(job.args)

  const retry = async () => {
    if (retrying) return
    setRetrying(true)
    try {
      await api.jobs.retry(job.id)
      toast.success("Job queued for retry", {
        description: `${job.name} · ${shortJobId(job.id)}`,
      })
      await onMutated()
    } catch (err) {
      toast.error("Could not retry job", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setRetrying(false)
    }
  }

  return (
    <div className="flex flex-col gap-stack border-b bg-accent/10 px-row-x py-tile">
      <dl className="grid grid-cols-2 gap-stack text-meta md:grid-cols-4">
        <DetailMeta label="Job ID" value={job.id} mono />
        <DetailMeta label="Scheduled" value={formatJobAge(job.scheduled_at)} />
        <DetailMeta label="Attempted" value={formatJobAge(job.attempted_at)} />
        <DetailMeta label="Finalized" value={formatJobAge(job.finalized_at)} />
      </dl>
      <div className="flex flex-col gap-tile-tight">
        <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
          Args
        </span>
        {args ? (
          <pre className="max-h-48 overflow-auto rounded-md border bg-background px-row-x py-tile font-mono text-meta text-foreground">
            {args}
          </pre>
        ) : (
          <p className="rounded-md border border-dashed border-muted-foreground/40 px-row-x py-tile text-meta text-muted-foreground">
            No args recorded for this job.
          </p>
        )}
      </div>
      {error ? (
        <div className="rounded-md border border-l-4 border-l-destructive bg-destructive/5 px-row-x py-tile">
          <span className="text-eyebrow uppercase tracking-wide text-destructive">
            Last error (attempt {job.errors?.[job.errors.length - 1]?.attempt ?? job.attempt})
          </span>
          <pre className="mt-tile-tight max-h-48 overflow-auto whitespace-pre-wrap font-mono text-meta text-foreground">
            {error}
          </pre>
        </div>
      ) : null}
      {isRetryableJob(job) ? (
        <div className="flex justify-end">
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="gap-inline"
            disabled={retrying}
            onClick={retry}
          >
            <RotateCcw
              className={`size-3.5 ${retrying ? "animate-spin" : ""}`}
              aria-hidden
            />
            {retrying ? "Retrying…" : "Retry"}
          </Button>
        </div>
      ) : null}
    </div>
  )
}

function DetailMeta({
  label,
  value,
  mono,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div className="flex min-w-0 flex-col gap-tile-tight">
      <dt className="text-eyebrow uppercase tracking-wide text-muted-foreground">
        {label}
      </dt>
      <dd
        className={`truncate text-meta text-foreground ${mono ? "font-mono" : ""}`}
        title={value}
      >
        {value}
      </dd>
    </div>
  )
}

export const JOB_ROW_COLUMNS =
  "grid-cols-[12px_72px_minmax(0,1fr)_minmax(0,120px)_64px_96px_18px]"

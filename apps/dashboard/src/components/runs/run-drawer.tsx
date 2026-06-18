// SPDX-License-Identifier: Apache-2.0

"use client"

import { ArrowUpRight } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"

import { api } from "@/lib/api"
import type { Run } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"
import {
  classifyRunStatus,
  formatRunAge,
  formatRunCost,
  formatRunDuration,
} from "@/lib/runs/derive"

// Run drawer — minimal first cut. The brief's _primitive-drawer.md
// envisions a richer config-driven drawer (timeline + tabs + linked
// surfaces); v1 ships the essential surface: header, meta tiles,
// input/output JSON, error block, cancel/pause/resume actions.

const DOT: Record<StatusState, string> = {
  ok: "bg-success",
  watch: "bg-warning",
  act: "bg-destructive",
  idle: "bg-muted-foreground/60",
}

interface RunDrawerProps {
  run: Run | null
  onClose: () => void
  onMutated: () => Promise<void> | void
}

export function RunDrawer({ run, onClose, onMutated }: RunDrawerProps) {
  const [pending, setPending] = useState<"cancel" | "pause" | "resume" | null>(
    null,
  )

  const act = async (action: "cancel" | "pause" | "resume") => {
    if (!run || pending) return
    setPending(action)
    try {
      const fn = api.runActions[action]
      await fn(run.id)
      toast.success(`Run ${action}ed`, { description: shortId(run.id) })
      await onMutated()
    } catch (err) {
      toast.error(`Could not ${action} run`, {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setPending(null)
    }
  }

  return (
    <Sheet
      open={run !== null}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <SheetContent side="right" className="w-full sm:max-w-xl">
        {run ? (
          <>
            <SheetHeader>
              <SheetTitle className="flex items-center gap-stack">
                <span
                  aria-hidden
                  className={`inline-block size-icon-dot rounded-pill ${DOT[classifyRunStatus(run.status)]}`}
                />
                <span className="min-w-0 truncate font-mono">{run.agent}</span>
              </SheetTitle>
              <SheetDescription>
                Run <span className="font-mono">{shortId(run.id)}</span> · started{" "}
                {formatRunAge(run.started_at)}
              </SheetDescription>
            </SheetHeader>

            <div className="flex flex-1 flex-col gap-section overflow-y-auto px-4 pb-4">
              <RunMetaGrid run={run} />
              {run.error ? <ErrorBlock error={run.error} /> : null}
              <JsonBlock label="Input" value={run.input} />
              <JsonBlock label="Output" value={run.output} />
            </div>

            <div className="flex flex-wrap items-center justify-end gap-inline border-t bg-card/40 p-4">
              {run.status === "running" || run.status === "queued" ? (
                <>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    disabled={pending !== null}
                    onClick={() => act("pause")}
                  >
                    {pending === "pause" ? "Pausing…" : "Pause"}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={pending !== null}
                    onClick={() => act("cancel")}
                  >
                    {pending === "cancel" ? "Cancelling…" : "Cancel"}
                  </Button>
                </>
              ) : null}
              {run.status === "running" ? (
                <Button
                  type="button"
                  size="sm"
                  disabled={pending !== null}
                  onClick={() => act("resume")}
                >
                  {pending === "resume" ? "Resuming…" : "Resume"}
                </Button>
              ) : null}
              <Button
                type="button"
                size="sm"
                variant="outline"
                className="gap-inline"
                render={
                  <a
                    href={`/api/v1/runs/${encodeURIComponent(run.id)}/agentfield`}
                    target="_blank"
                    rel="noopener noreferrer"
                  />
                }
              >
                Open in AgentField
                <ArrowUpRight className="size-3" aria-hidden />
              </Button>
            </div>
          </>
        ) : null}
      </SheetContent>
    </Sheet>
  )
}

function RunMetaGrid({ run }: { run: Run }) {
  return (
    <dl className="grid grid-cols-3 gap-stack text-meta">
      <Meta label="Status" value={run.status} />
      <Meta label="Duration" value={formatRunDuration(run.duration_ms)} mono />
      <Meta label="Cost" value={formatRunCost(run.cost_usd)} mono />
      <Meta label="Tenant" value={run.tenant_id ?? "—"} mono />
      <Meta label="Started" value={formatAbsolute(run.started_at)} mono />
      <Meta label="ID" value={run.id} mono />
    </dl>
  )
}

function Meta({
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

function ErrorBlock({ error }: { error: string }) {
  return (
    <section
      aria-label="Error"
      className="rounded-md border border-l-4 border-l-destructive bg-destructive/5 px-row-x py-tile"
    >
      <span className="text-eyebrow uppercase tracking-wide text-destructive">
        Error
      </span>
      <pre className="mt-tile-tight max-h-32 overflow-auto whitespace-pre-wrap font-mono text-meta text-foreground">
        {error}
      </pre>
    </section>
  )
}

function JsonBlock({ label, value }: { label: string; value: unknown }) {
  const json = stringify(value)
  return (
    <section className="flex flex-col gap-stack">
      <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
        {label}
      </span>
      <pre className="max-h-64 overflow-auto rounded-md border bg-background px-row-x py-tile font-mono text-meta text-foreground">
        {json || "—"}
      </pre>
    </section>
  )
}

function stringify(value: unknown): string {
  if (value === undefined || value === null) return ""
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

function shortId(id: string): string {
  return id.length > 14 ? `${id.slice(0, 8)}…${id.slice(-4)}` : id
}

function formatAbsolute(iso: string): string {
  const ts = Date.parse(iso)
  if (Number.isNaN(ts)) return iso
  return `${new Date(ts).toISOString().slice(0, 19).replace("T", " ")}Z`
}

// SPDX-License-Identifier: Apache-2.0

"use client"

import { ArrowUpRight, ChevronLeft, ChevronRight } from "lucide-react"
import Link from "next/link"
import { useEffect, useState } from "react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { CopyButton } from "@/components/ui/copy-button"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"

import { api } from "@/lib/api"
import type { Run, RunAgentField } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"
import {
  classifyRunStatus,
  formatRunAge,
  formatRunCost,
  formatRunDuration,
} from "@/lib/runs/derive"

// Run drawer per development/ux/pages/_primitive-drawer.md.
//
// Structure (top → bottom):
//   1. Title row — status dot + agent + close
//   2. Quick facts grid — Status / Duration / Cost / Tenant / Started / ID
//   3. Tabs — Input · Output · (Errors when present) · Audit
//   4. Related links — Tenant / Agent / Parent run (when applicable)
//   5. Footer actions — Cancel / Pause / Resume / Open in AgentField
//   6. Keyboard hint bar
//
// Steps + Tools tabs in the brief require AgentField timeline data
// that isn't surfaced in v1 — the drawer ships with the four tabs we
// can actually populate today and notes the deferral inline.

const DOT: Record<StatusState, string> = {
  ok: "bg-success",
  watch: "bg-warning",
  act: "bg-destructive",
  idle: "bg-muted-foreground/60",
}

const AGENTFIELD_FALLBACK_URL =
  typeof process !== "undefined" && process.env.NEXT_PUBLIC_AGENTFIELD_URL
    ? process.env.NEXT_PUBLIC_AGENTFIELD_URL
    : "http://localhost:8081"

interface RunDrawerProps {
  run: Run | null
  onClose: () => void
  onMutated: () => Promise<void> | void
  onPrev?: () => void
  onNext?: () => void
  hasPrev?: boolean
  hasNext?: boolean
}

export function RunDrawer({
  run,
  onClose,
  onMutated,
  onPrev,
  onNext,
  hasPrev,
  hasNext,
}: RunDrawerProps) {
  const [pending, setPending] = useState<"cancel" | "pause" | "resume" | null>(
    null,
  )
  const [agentfield, setAgentfield] = useState<RunAgentField | null>(null)

  // Fetch AgentField metadata so we can surface the proper UI URL +
  // overview data. Failures are silent — the drawer still works
  // without AgentField context (fallback URL below).
  useEffect(() => {
    if (!run) {
      setAgentfield(null)
      return
    }
    let cancelled = false
    api
      .runAgentField(run.id)
      .then((next) => {
        if (!cancelled) setAgentfield(next)
      })
      .catch(() => {
        if (!cancelled) setAgentfield(null)
      })
    return () => {
      cancelled = true
    }
  }, [run])

  // Keyboard navigation: ← / → move between rows, Esc closes.
  useEffect(() => {
    if (!run) return
    const handler = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) {
        return
      }
      if (e.key === "ArrowLeft" && hasPrev && onPrev) {
        e.preventDefault()
        onPrev()
      } else if (e.key === "ArrowRight" && hasNext && onNext) {
        e.preventDefault()
        onNext()
      }
    }
    window.addEventListener("keydown", handler)
    return () => window.removeEventListener("keydown", handler)
  }, [run, onPrev, onNext, hasPrev, hasNext])

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
      <SheetContent side="right" className="w-full sm:max-w-2xl">
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
              <SheetDescription className="flex items-center gap-inline">
                Run <span className="font-mono">{shortId(run.id)}</span>
                <CopyButton value={run.id} label="Copy run id" />
                · started {formatRunAge(run.started_at)}
              </SheetDescription>
            </SheetHeader>

            <div className="flex flex-1 flex-col gap-section overflow-y-auto px-4 pb-4">
              <QuickFacts run={run} />
              <DrawerTabs run={run} agentfield={agentfield} />
              <RelatedLinks run={run} />
            </div>

            <div className="flex flex-col gap-stack border-t bg-card/40 p-4">
              <div className="flex flex-wrap items-center justify-between gap-inline">
                <NavPair
                  onPrev={onPrev}
                  onNext={onNext}
                  hasPrev={hasPrev}
                  hasNext={hasNext}
                />
                <div className="flex flex-wrap items-center justify-end gap-inline">
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
                  <OpenInAgentFieldButton
                    runId={run.id}
                    agentfield={agentfield}
                  />
                </div>
              </div>
              <KeyboardHints
                canCancel={run.status === "running" || run.status === "queued"}
                canPause={run.status === "running" || run.status === "queued"}
              />
            </div>
          </>
        ) : null}
      </SheetContent>
    </Sheet>
  )
}

// ──────────────────────────────────────────────────────────────────────
// Quick facts grid
// ──────────────────────────────────────────────────────────────────────

function QuickFacts({ run }: { run: Run }) {
  return (
    <dl className="grid grid-cols-3 gap-stack text-meta">
      <Meta label="Status" value={run.status} />
      <Meta label="Duration" value={formatRunDuration(run.duration_ms)} mono />
      <Meta label="Cost" value={formatRunCost(run.cost_usd)} mono />
      <TenantMeta
        tenantId={run.tenant_id ?? null}
        tenantName={run.tenant_name ?? null}
      />
      <Meta
        label="Started"
        value={formatRunAge(run.started_at)}
        tooltip={formatAbsolute(run.started_at)}
      />
      <IdMeta runId={run.id} />
    </dl>
  )
}

function Meta({
  label,
  value,
  mono,
  tooltip,
}: {
  label: string
  value: string
  mono?: boolean
  tooltip?: string
}) {
  return (
    <div className="flex min-w-0 flex-col gap-tile-tight">
      <dt className="text-eyebrow uppercase tracking-wide text-muted-foreground">
        {label}
      </dt>
      <dd
        className={`truncate text-meta text-foreground ${mono ? "font-mono" : ""}`}
        title={tooltip ?? value}
      >
        {value}
      </dd>
    </div>
  )
}

function TenantMeta({
  tenantId,
  tenantName,
}: {
  tenantId: string | null
  tenantName: string | null
}) {
  if (!tenantId) {
    return <Meta label="Tenant" value="—" />
  }
  return (
    <div className="flex min-w-0 flex-col gap-tile-tight">
      <dt className="text-eyebrow uppercase tracking-wide text-muted-foreground">
        Tenant
      </dt>
      <dd className="flex min-w-0 items-center gap-inline">
        <span className="truncate text-meta text-foreground" title={tenantId}>
          {tenantName ?? tenantId.slice(0, 8)}
        </span>
        <CopyButton value={tenantId} label="Copy tenant id" />
      </dd>
      {tenantName ? (
        <span className="truncate font-mono text-meta text-muted-foreground">
          {tenantId.slice(0, 8)}
        </span>
      ) : null}
    </div>
  )
}

function IdMeta({ runId }: { runId: string }) {
  return (
    <div className="flex min-w-0 flex-col gap-tile-tight">
      <dt className="text-eyebrow uppercase tracking-wide text-muted-foreground">
        ID
      </dt>
      <dd className="flex min-w-0 items-center gap-inline font-mono">
        <span className="truncate text-meta text-foreground" title={runId}>
          {shortId(runId)}
        </span>
        <CopyButton value={runId} label="Copy run id" />
      </dd>
    </div>
  )
}

// ──────────────────────────────────────────────────────────────────────
// Tabs — Input · Output · Errors (when present) · Audit
// ──────────────────────────────────────────────────────────────────────

function DrawerTabs({
  run,
  agentfield,
}: {
  run: Run
  agentfield: RunAgentField | null
}) {
  const hasError = Boolean(run.error)
  const defaultTab = hasError ? "errors" : "input"
  return (
    <Tabs defaultValue={defaultTab}>
      <TabsList variant="line" className="border-b">
        <TabsTrigger value="input">Input</TabsTrigger>
        <TabsTrigger value="output">Output</TabsTrigger>
        {hasError ? (
          <TabsTrigger value="errors">Errors</TabsTrigger>
        ) : null}
        <TabsTrigger value="audit">Audit</TabsTrigger>
      </TabsList>
      <TabsContent value="input" className="pt-stack">
        <JsonPanel
          value={run.input}
          emptyHint="No input payload recorded for this run."
        />
      </TabsContent>
      <TabsContent value="output" className="pt-stack">
        <JsonPanel
          value={run.output}
          emptyHint="No output payload recorded for this run."
        />
      </TabsContent>
      {hasError ? (
        <TabsContent value="errors" className="pt-stack">
          <ErrorPanel error={run.error ?? ""} />
        </TabsContent>
      ) : null}
      <TabsContent value="audit" className="pt-stack">
        <AuditPanel run={run} agentfield={agentfield} />
      </TabsContent>
    </Tabs>
  )
}

function JsonPanel({
  value,
  emptyHint,
}: {
  value: unknown
  emptyHint: string
}) {
  const empty = value === undefined || value === null
  if (empty) {
    return (
      <p className="rounded-md border border-dashed border-muted-foreground/40 px-row-x py-tile text-meta text-muted-foreground">
        {emptyHint}
      </p>
    )
  }
  return (
    <pre className="max-h-64 overflow-auto rounded-md border bg-background px-row-x py-tile font-mono text-meta text-foreground">
      {safeStringify(value)}
    </pre>
  )
}

function ErrorPanel({ error }: { error: string }) {
  return (
    <section
      aria-label="Error"
      className="rounded-md border border-l-4 border-l-destructive bg-destructive/5 px-row-x py-tile"
    >
      <span className="text-eyebrow uppercase tracking-wide text-destructive">
        Error message
      </span>
      <pre className="mt-tile-tight max-h-64 overflow-auto whitespace-pre-wrap font-mono text-meta text-foreground">
        {error}
      </pre>
    </section>
  )
}

function AuditPanel({
  run,
  agentfield,
}: {
  run: Run
  agentfield: RunAgentField | null
}) {
  return (
    <dl className="grid grid-cols-2 gap-stack text-meta">
      <Meta
        label="Started at"
        value={formatAbsolute(run.started_at)}
        mono
        tooltip={run.started_at}
      />
      <Meta
        label="AgentField status"
        value={agentfield?.overview.status ?? "—"}
        mono
      />
      <Meta
        label="Execution ID"
        value={agentfield?.overview.execution_id ?? run.id}
        mono
        tooltip={agentfield?.overview.execution_id ?? run.id}
      />
      <Meta
        label="Tenant ID"
        value={run.tenant_id ?? "—"}
        mono
        tooltip={run.tenant_id ?? undefined}
      />
    </dl>
  )
}

// ──────────────────────────────────────────────────────────────────────
// Related links section
// ──────────────────────────────────────────────────────────────────────

function RelatedLinks({ run }: { run: Run }) {
  // Only Tenant + Agent for v1 — parent run linkage requires a
  // backend column we don't have yet.
  const links: { label: string; href: string }[] = []
  if (run.tenant_id) {
    links.push({
      label: `Tenant ${run.tenant_name ?? run.tenant_id.slice(0, 8)}`,
      href: `/people/tenants/${encodeURIComponent(run.tenant_id)}`,
    })
  }
  if (run.agent) {
    links.push({
      label: `Agent ${run.agent}`,
      href: `/build/agents/${encodeURIComponent(run.agent)}`,
    })
  }
  if (links.length === 0) return null
  return (
    <section
      aria-labelledby="run-drawer-related"
      className="flex flex-col gap-tile-tight"
    >
      <span
        id="run-drawer-related"
        className="text-eyebrow uppercase tracking-wide text-muted-foreground"
      >
        Related
      </span>
      <ul role="list" className="flex flex-col gap-tile-tight text-meta">
        {links.map((l) => (
          <li key={l.href}>
            <Link
              href={l.href}
              className="inline-flex items-center gap-inline text-foreground underline hover:no-underline"
            >
              {l.label}
              <ArrowUpRight className="size-3" aria-hidden />
            </Link>
          </li>
        ))}
      </ul>
    </section>
  )
}

// ──────────────────────────────────────────────────────────────────────
// Open in AgentField — uses ui_url when the runtime returned one
// ──────────────────────────────────────────────────────────────────────

function OpenInAgentFieldButton({
  runId,
  agentfield,
}: {
  runId: string
  agentfield: RunAgentField | null
}) {
  const href =
    agentfield?.ui_url ??
    `${AGENTFIELD_FALLBACK_URL}/executions/${agentfield?.overview.execution_id ?? runId}`
  return (
    <Button
      type="button"
      size="sm"
      variant="outline"
      className="gap-inline"
      render={<a href={href} target="_blank" rel="noopener noreferrer" />}
    >
      Open in AgentField
      <ArrowUpRight className="size-3" aria-hidden />
    </Button>
  )
}

// ──────────────────────────────────────────────────────────────────────
// Footer nav pair (← prev / → next)
// ──────────────────────────────────────────────────────────────────────

function NavPair({
  onPrev,
  onNext,
  hasPrev,
  hasNext,
}: {
  onPrev?: () => void
  onNext?: () => void
  hasPrev?: boolean
  hasNext?: boolean
}) {
  if (!onPrev && !onNext) return null
  return (
    <div className="flex items-center gap-inline">
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="h-7 gap-inline text-meta"
        disabled={!hasPrev}
        onClick={onPrev}
      >
        <ChevronLeft className="size-3" aria-hidden />
        Prev
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="h-7 gap-inline text-meta"
        disabled={!hasNext}
        onClick={onNext}
      >
        Next
        <ChevronRight className="size-3" aria-hidden />
      </Button>
    </div>
  )
}

function KeyboardHints({
  canCancel,
  canPause,
}: {
  canCancel: boolean
  canPause: boolean
}) {
  const hints: { key: string; label: string }[] = [
    { key: "Esc", label: "Close" },
    { key: "← →", label: "Prev / Next run" },
  ]
  if (canPause) hints.push({ key: "P", label: "Pause" })
  if (canCancel) hints.push({ key: "X", label: "Cancel" })
  return (
    <div className="flex flex-wrap items-center gap-stack text-meta text-muted-foreground">
      {hints.map((h) => (
        <span key={h.key} className="flex items-center gap-inline">
          <kbd className="rounded-md border bg-card px-1 py-px font-mono text-meta">
            {h.key}
          </kbd>
          {h.label}
        </span>
      ))}
    </div>
  )
}

// ──────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────

function safeStringify(value: unknown): string {
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

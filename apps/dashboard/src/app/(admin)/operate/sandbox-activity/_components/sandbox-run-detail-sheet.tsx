// SPDX-License-Identifier: Apache-2.0

"use client"

// SandboxRunDetailSheet — side panel for inspecting a single sandbox run.
//
// Renders timing/resource stats, the full command, env + files (when
// present) inside a Collapsible JSON viewer, and Download buttons for
// stdout/stderr that lazily mint a signed URL via `api.storage.signedURL`.
// A "Stop" button appears for runs that are still running and calls
// `api.sandbox.stop`.

import * as React from "react"
import {
  ChevronDown,
  ChevronRight,
  Download,
  Loader2,
  Octagon,
  Package,
} from "lucide-react"
import { toast } from "sonner"

import { api, ApiError, type SandboxRun } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"

import { formatRelative } from "../../_lib/format"
import { formatCurrency } from "../../cost/_components/format"
import { SandboxStatusBadge } from "./sandbox-status-badge"

type SandboxRunDetailSheetProps = {
  run: SandboxRun | null
  onOpenChange: (open: boolean) => void
  onStopped: () => void
}

export function SandboxRunDetailSheet({
  run,
  onOpenChange,
  onStopped,
}: SandboxRunDetailSheetProps) {
  return (
    <Sheet open={run !== null} onOpenChange={onOpenChange}>
      <SheetContent className="flex w-full flex-col gap-0 p-0 sm:max-w-2xl">
        {run ? (
          <SandboxRunDetailBody
            key={run.id}
            run={run}
            onStopped={onStopped}
            onClose={() => onOpenChange(false)}
          />
        ) : null}
      </SheetContent>
    </Sheet>
  )
}

function SandboxRunDetailBody({
  run,
  onStopped,
  onClose,
}: {
  run: SandboxRun
  onStopped: () => void
  onClose: () => void
}) {
  const [stopping, setStopping] = React.useState(false)
  const canStop = run.status === "running"

  // Files + env are not loaded by the list endpoint — they live on the
  // run record returned by `api.sandbox.get`. Fetch lazily when the sheet
  // opens so the list view stays fast.
  const [detail, setDetail] = React.useState<SandboxRun | null>(null)
  const [detailError, setDetailError] = React.useState<string | null>(null)

  React.useEffect(() => {
    let cancelled = false
    void api.sandbox
      .get(run.id)
      .then((full) => {
        if (cancelled) return
        setDetail(full)
      })
      .catch((e: unknown) => {
        if (cancelled) return
        const message =
          e instanceof ApiError
            ? `${e.code}: ${e.message}`
            : e instanceof Error
              ? e.message
              : "Failed to load run detail"
        setDetailError(message)
      })
    return () => {
      cancelled = true
    }
  }, [run.id])

  const handleStop = async () => {
    setStopping(true)
    try {
      await api.sandbox.stop(run.id)
      toast.success("Stop requested", {
        description: `${run.id} will be terminated.`,
      })
      onStopped()
      onClose()
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to stop sandbox"
      toast.error("Could not stop sandbox", { description: message })
    } finally {
      setStopping(false)
    }
  }

  const commandStr = run.command.join(" ")
  const fullRecord = detail ?? run

  return (
    <>
      <SheetHeader className="border-b">
        <div className="flex items-start justify-between gap-3">
          <div className="flex flex-col gap-1">
            <SheetTitle className="flex items-center gap-2">
              <SandboxStatusBadge status={run.status} />
              <span className="text-muted-foreground font-mono text-xs">
                {run.id}
              </span>
            </SheetTitle>
            <SheetDescription className="font-mono text-xs">
              {run.adapter} · {run.image}
            </SheetDescription>
          </div>
          {canStop ? (
            <Button
              variant="destructive"
              size="sm"
              onClick={handleStop}
              disabled={stopping}
            >
              {stopping ? (
                <Loader2 className="animate-spin" />
              ) : (
                <Octagon />
              )}
              {stopping ? "Stopping…" : "Stop"}
            </Button>
          ) : null}
        </div>
      </SheetHeader>

      <ScrollArea className="flex-1">
        <div className="flex flex-col gap-6 p-4">
          <section className="grid grid-cols-2 gap-3 text-xs sm:grid-cols-3">
            <Stat label="Tenant" value={run.tenant_id || "—"} />
            <Stat
              label="Workspace"
              value={run.workspace_id || "—"}
            />
            <Stat label="Started" value={formatRelative(run.started_at)} />
            <Stat label="Ended" value={formatRelative(run.ended_at)} />
            <Stat
              label="Duration"
              value={formatSeconds(run.duration_s)}
            />
            <Stat
              label="Exit code"
              value={run.exit_code === null ? "—" : String(run.exit_code)}
            />
            <Stat label="CPU·s" value={formatNumber(run.cpu_seconds)} />
            <Stat label="Memory peak" value={formatMemory(run.memory_peak_mb)} />
            <Stat label="Cost" value={formatCurrency(run.cost_usd ?? 0)} />
            <Stat
              label="Network in"
              value={formatBytes(run.network_bytes_in)}
            />
            <Stat
              label="Network out"
              value={formatBytes(run.network_bytes_out)}
            />
          </section>

          <section className="flex flex-col gap-1.5">
            <SectionLabel>Command</SectionLabel>
            <pre className="bg-muted/40 max-h-48 overflow-auto rounded-md p-3 font-mono text-xs whitespace-pre-wrap break-all">
              {commandStr}
            </pre>
          </section>

          <section className="flex flex-col gap-1.5">
            <SectionLabel>Artifacts</SectionLabel>
            <div className="flex flex-wrap gap-2">
              <DownloadButton
                label="Download stdout"
                storageKey={fullRecord.stdout_url}
              />
              <DownloadButton
                label="Download stderr"
                storageKey={fullRecord.stderr_url}
              />
              <DownloadButton
                label="Download artifacts"
                storageKey={fullRecord.artifacts_url}
              />
            </div>
            {detailError ? (
              <p className="text-destructive text-xs">{detailError}</p>
            ) : null}
          </section>

          <JsonCollapsible
            label="Environment variables"
            value={extractEnv(fullRecord)}
          />
          <JsonCollapsible
            label="Files"
            value={extractFiles(fullRecord)}
          />
        </div>
      </ScrollArea>
    </>
  )
}

// The list endpoint returns the canonical SandboxRun fields. `env` and
// `files` are only ever set on the input payload — they're surfaced via
// `api.sandbox.get` which carries through the raw shape. We extract them
// defensively in case the runtime adds them later without bumping the
// shared zod schema.
function extractEnv(run: SandboxRun): unknown {
  const r = run as unknown as { env?: unknown }
  return r.env
}

function extractFiles(run: SandboxRun): unknown {
  const r = run as unknown as { files?: unknown }
  return r.files
}

function DownloadButton({
  label,
  storageKey,
}: {
  label: string
  storageKey: string | null
}) {
  const [loading, setLoading] = React.useState(false)

  const handleClick = async () => {
    if (!storageKey) return
    setLoading(true)
    try {
      const signed = await api.storage.signedURL(storageKey)
      window.open(signed.url, "_blank", "noopener,noreferrer")
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to mint signed URL"
      toast.error("Could not open artifact", { description: message })
    } finally {
      setLoading(false)
    }
  }

  return (
    <Button
      variant="outline"
      size="sm"
      onClick={handleClick}
      disabled={!storageKey || loading}
    >
      {loading ? <Loader2 className="animate-spin" /> : <Download />}
      {label}
      {!storageKey ? (
        <Badge variant="ghost" className="ml-1">
          n/a
        </Badge>
      ) : null}
    </Button>
  )
}

function JsonCollapsible({
  label,
  value,
}: {
  label: string
  value: unknown
}) {
  const [open, setOpen] = React.useState(false)
  if (value === undefined || value === null) return null
  if (typeof value === "object" && Object.keys(value as object).length === 0) {
    return null
  }
  let formatted: string
  try {
    formatted = JSON.stringify(value, null, 2)
  } catch {
    formatted = String(value)
  }

  return (
    <section className="flex flex-col gap-1.5">
      <Collapsible open={open} onOpenChange={setOpen}>
        <CollapsibleTrigger
          render={
            <Button
              variant="ghost"
              size="sm"
              className="w-full justify-start gap-1.5 px-2"
            >
              {open ? <ChevronDown /> : <ChevronRight />}
              <Package />
              <span className="text-xs font-medium uppercase tracking-wide">
                {label}
              </span>
            </Button>
          }
        />
        <CollapsibleContent>
          <pre className="bg-muted/40 mt-2 max-h-72 overflow-auto rounded-md p-3 font-mono text-xs">
            {formatted}
          </pre>
        </CollapsibleContent>
      </Collapsible>
    </section>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-muted-foreground text-[10px] font-medium uppercase tracking-wide">
        {label}
      </span>
      <span className="font-mono text-xs">{value}</span>
    </div>
  )
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <span className="text-muted-foreground text-[10px] font-medium uppercase tracking-wide">
      {children}
    </span>
  )
}

function formatSeconds(s: number | null | undefined): string {
  if (s === null || s === undefined) return "—"
  if (s < 1) return `${(s * 1000).toFixed(0)}ms`
  if (s < 60) return `${s.toFixed(1)}s`
  const mins = Math.floor(s / 60)
  const secs = Math.round(s % 60)
  return `${mins}m ${secs}s`
}

function formatNumber(n: number | null | undefined): string {
  if (n === null || n === undefined) return "—"
  return n.toLocaleString(undefined, { maximumFractionDigits: 2 })
}

function formatMemory(mb: number | null | undefined): string {
  if (mb === null || mb === undefined) return "—"
  if (mb < 1024) return `${mb.toFixed(0)} MB`
  return `${(mb / 1024).toFixed(2)} GB`
}

function formatBytes(b: number | null | undefined): string {
  if (b === null || b === undefined) return "—"
  if (b < 1024) return `${b} B`
  if (b < 1024 ** 2) return `${(b / 1024).toFixed(1)} KB`
  if (b < 1024 ** 3) return `${(b / 1024 ** 2).toFixed(1)} MB`
  return `${(b / 1024 ** 3).toFixed(2)} GB`
}
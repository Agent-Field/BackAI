// SPDX-License-Identifier: Apache-2.0

"use client"

import { AlertTriangle, RefreshCw, Trash2 } from "lucide-react"
import { useCallback, useEffect, useMemo, useState } from "react"
import { toast } from "sonner"

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"

import { api } from "@/lib/api"
import { formatInt, summarise } from "@/lib/cache-page/derive"
import type { CacheSnapshot } from "@/lib/cache-page/types"
import { polling } from "@/lib/theme"

import { HeadlineStats } from "./headline-stats"
import { HitBreakdown } from "./hit-breakdown"

// CacheShell owns:
//   - Polling tick (10s — cache stats are a light aggregate query)
//   - Refresh button
//   - The flush control (destructive, confirm-gated)
//   - Derived summary → the two zones

const POLL_MS = polling.services // 10s

interface CacheShellProps {
  initialSnapshot: CacheSnapshot
}

export function CacheShell({ initialSnapshot }: CacheShellProps) {
  const [snapshot, setSnapshot] = useState<CacheSnapshot>(initialSnapshot)
  const [refreshing, setRefreshing] = useState(false)

  const refresh = useCallback(async (manual = false) => {
    if (manual) setRefreshing(true)
    const fetchedAt = new Date().toISOString()
    const [stats] = await Promise.allSettled([api.llm.cacheStats()])
    setSnapshot((prev) => ({
      stats: stats.status === "fulfilled" ? stats.value : prev.stats,
      fetchedAt,
      statsHealthy: stats.status === "fulfilled",
    }))
    if (manual) {
      setRefreshing(false)
      if (stats.status === "fulfilled") toast.success("Refreshed cache stats")
      else toast.error("Could not reach the cache stats endpoint")
    }
  }, [])

  // Background polling tick.
  useEffect(() => {
    let cancelled = false
    const id = setInterval(() => {
      if (!cancelled) void refresh()
    }, POLL_MS)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [refresh])

  const summary = useMemo(() => summarise(snapshot), [snapshot])

  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <Header
        fetchedAt={snapshot.fetchedAt}
        refreshing={refreshing}
        onRefresh={() => refresh(true)}
        onFlushed={() => refresh()}
        entries={summary.entries}
        canFlush={summary.entries > 0}
      />
      {!snapshot.statsHealthy && snapshot.stats === null ? <Degraded /> : null}
      <HeadlineStats summary={summary} />
      <HitBreakdown summary={summary} />
    </div>
  )
}

function Header({
  fetchedAt,
  refreshing,
  onRefresh,
  onFlushed,
  entries,
  canFlush,
}: {
  fetchedAt: string
  refreshing: boolean
  onRefresh: () => void
  onFlushed: () => void
  entries: number
  canFlush: boolean
}) {
  // Tick a local clock so the "updated" age stays live between polls
  // without reading Date.now() during render (react-hooks/purity).
  const [now, setNow] = useState(() => Date.parse(fetchedAt))
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [])
  const ageSec = Math.max(0, Math.round((now - Date.parse(fetchedAt)) / 1000))
  const ageLabel = ageSec < 5 ? "now" : `${ageSec}s ago`
  return (
    <header className="flex items-baseline justify-between gap-stack">
      <div className="flex flex-col gap-tile-tight">
        <h1 className="text-xl font-semibold tracking-tight text-foreground">Cache</h1>
        <p className="text-meta text-muted-foreground">
          LLM gateway response cache — hit rate, cost saved, and stored entries.
        </p>
      </div>
      <div className="flex items-center gap-stack text-meta text-muted-foreground">
        <span className="tabular-nums">updated {ageLabel}</span>
        <FlushButton entries={entries} disabled={!canFlush} onFlushed={onFlushed} />
        <Button
          type="button"
          size="sm"
          variant="outline"
          onClick={onRefresh}
          disabled={refreshing}
          className="h-7 gap-inline text-meta"
        >
          <RefreshCw className={`size-3.5 ${refreshing ? "animate-spin" : ""}`} aria-hidden />
          Refresh
        </Button>
      </div>
    </header>
  )
}

function FlushButton({
  entries,
  disabled,
  onFlushed,
}: {
  entries: number
  disabled: boolean
  onFlushed: () => void
}) {
  const [open, setOpen] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  const onConfirm = async () => {
    if (submitting) return
    setSubmitting(true)
    try {
      const res = await api.llm.cacheFlush()
      toast.success("Cache flushed", {
        description: `${formatInt(res.deleted_rows)} entries cleared`,
      })
      onFlushed()
      setOpen(false)
    } catch (err) {
      toast.error("Could not flush the cache", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <>
      <Button
        type="button"
        size="sm"
        variant="outline"
        disabled={disabled}
        onClick={() => setOpen(true)}
        className="h-7 gap-inline text-meta"
      >
        <Trash2 className="size-3.5" aria-hidden />
        Flush
      </Button>
      <AlertDialog open={open} onOpenChange={(next) => !submitting && setOpen(next)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Flush the response cache?</AlertDialogTitle>
            <AlertDialogDescription>
              This clears all <span className="font-mono">{formatInt(entries)}</span> stored entries
              across every tenant. Subsequent identical prompts will miss and re-bill against
              providers until the cache warms again.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel size="sm" disabled={submitting}>
              Cancel
            </AlertDialogCancel>
            <AlertDialogAction
              size="sm"
              variant="destructive"
              disabled={submitting}
              onClick={onConfirm}
            >
              {submitting ? "Flushing…" : "Flush cache"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

function Degraded() {
  return (
    <div className="flex items-start gap-stack rounded-md border border-l-4 border-l-warning bg-warning/5 px-row-x py-tile text-meta text-foreground">
      <AlertTriangle className="size-icon-inline shrink-0 text-warning" aria-hidden />
      <p>
        Cache statistics aren&apos;t available right now — the gateway cache endpoint didn&apos;t
        respond. The tiles will populate when the next poll succeeds.
      </p>
    </div>
  )
}

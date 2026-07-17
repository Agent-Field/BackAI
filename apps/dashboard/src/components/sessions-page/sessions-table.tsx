// SPDX-License-Identifier: Apache-2.0

"use client"

import { useState } from "react"
import { toast } from "sonner"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { RelativeTime } from "@/components/ui/relative-time"

import { api } from "@/lib/api"
import type { SessionInfo } from "@/lib/api"
import { formatAge } from "@/lib/tenant-detail/derive"

// Sessions table — sticky header + one row per live session. Loading /
// degraded / empty states render the same column shell so the structure
// stays visible even at zero data (framework rule).

const SESSION_ROW_COLUMNS =
  "grid-cols-[minmax(0,1.4fr)_84px_120px_minmax(0,1fr)_90px_110px_150px]"

interface SessionsTableProps {
  sessions: SessionInfo[]
  total: number
  healthy: boolean
  onMutated: () => Promise<void> | void
}

export function SessionsTable({
  sessions,
  total,
  healthy,
  onMutated,
}: SessionsTableProps) {
  return (
    <section
      aria-label="Sessions table"
      className="flex min-h-0 flex-col rounded-md border bg-card"
    >
      <header
        className={`sticky top-0 z-10 grid items-center gap-stack border-b bg-card px-row-x py-tile-tight text-eyebrow uppercase tracking-wide text-muted-foreground ${SESSION_ROW_COLUMNS}`}
      >
        <span>User</span>
        <span>App</span>
        <span>IP</span>
        <span>User agent</span>
        <span>Created</span>
        <span>Expires</span>
        <span className="text-right tabular-nums normal-case">
          {sessions.length === total ? total : `${sessions.length}/${total}`}
        </span>
      </header>
      {!healthy ? (
        <DegradedRow />
      ) : sessions.length === 0 ? (
        <EmptyRow />
      ) : (
        <ul role="list" className="flex flex-col divide-y">
          {sessions.map((session) => (
            <SessionRow
              key={session.id}
              session={session}
              onMutated={onMutated}
            />
          ))}
        </ul>
      )}
    </section>
  )
}

function SessionRow({
  session,
  onMutated,
}: {
  session: SessionInfo
  onMutated: () => Promise<void> | void
}) {
  const [phase, setPhase] = useState<"idle" | "confirm" | "pending">("idle")
  const expired = Date.parse(session.expires_at) < Date.now()

  const revoke = async () => {
    if (phase === "pending") return
    setPhase("pending")
    try {
      await api.admin.sessions.revoke(session.id)
      toast.success("Session revoked", { description: session.email })
      await onMutated()
      setPhase("idle")
    } catch (err) {
      toast.error("Could not revoke session", {
        description: err instanceof Error ? err.message : String(err),
      })
      setPhase("idle")
    }
  }

  return (
    <li
      className={`grid items-center gap-stack px-row-x py-row-y text-meta ${SESSION_ROW_COLUMNS}`}
    >
      <div className="flex min-w-0 flex-col gap-tile-tight">
        <span className="truncate text-body font-medium text-foreground">
          {session.email}
        </span>
        <span className="truncate text-meta text-muted-foreground">
          {session.name ?? "—"}
        </span>
      </div>
      <Badge variant={session.is_operator ? "secondary" : "outline"}>
        {session.is_operator ? "Operator" : "Customer"}
      </Badge>
      <span
        className="truncate font-mono text-muted-foreground"
        title={session.ip_address ?? undefined}
      >
        {session.ip_address ?? "—"}
      </span>
      <span
        className="truncate text-muted-foreground"
        title={session.user_agent ?? undefined}
      >
        {session.user_agent ?? "—"}
      </span>
      <span className="font-mono tabular-nums text-muted-foreground">
        <RelativeTime iso={session.created_at} format={formatAge} />
      </span>
      <span
        className={`font-mono tabular-nums ${
          expired ? "text-destructive" : "text-muted-foreground"
        }`}
        title={session.expires_at}
      >
        {expired ? "expired" : formatUntil(session.expires_at)}
      </span>
      <div className="flex flex-col items-end gap-tile-tight">
        {phase === "confirm" ? (
          <div className="flex items-center gap-inline">
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="h-7 text-meta"
              onClick={() => setPhase("idle")}
            >
              Cancel
            </Button>
            <Button
              type="button"
              size="sm"
              variant="destructive"
              className="h-7 text-meta"
              onClick={revoke}
            >
              Confirm revoke
            </Button>
          </div>
        ) : (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            className="h-7 text-meta text-destructive hover:text-destructive"
            disabled={phase === "pending"}
            onClick={() => setPhase("confirm")}
          >
            {phase === "pending" ? "Revoking…" : "Revoke"}
          </Button>
        )}
        {phase === "confirm" ? (
          <span className="text-right text-eyebrow text-warning">
            Revoking your own session logs you out.
          </span>
        ) : null}
      </div>
    </li>
  )
}

// formatAge covers "how long ago"; sessions also need "how long until
// expiry" for the Expires column.
function formatUntil(iso: string): string {
  const ts = Date.parse(iso)
  if (Number.isNaN(ts)) return iso
  const diffMs = ts - Date.now()
  if (diffMs <= 0) return "expired"
  const sec = Math.floor(diffMs / 1000)
  if (sec < 60) return `in ${sec}s`
  if (sec < 3600) return `in ${Math.floor(sec / 60)}m`
  if (sec < 86_400) return `in ${Math.floor(sec / 3600)}h`
  return `in ${Math.floor(sec / 86_400)}d`
}

function EmptyRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">
        No sessions match the current filters.
      </p>
      <p className="max-w-md text-meta text-muted-foreground">
        Your own operator session should appear here — if the list is
        empty even without filters, check that the runtime can reach the
        auth database.
      </p>
    </div>
  )
}

function DegradedRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">Sessions list unavailable.</p>
      <p className="max-w-md text-meta text-muted-foreground">
        The runtime did not return a sessions list. Check the Health page
        for a database probe, then retry.
      </p>
    </div>
  )
}

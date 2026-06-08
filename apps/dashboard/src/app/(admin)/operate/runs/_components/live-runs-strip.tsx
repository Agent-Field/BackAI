// SPDX-License-Identifier: Apache-2.0

// LiveRunsStrip — #15 operator-side demo.
//
// Mounts a WebSocket against /api/v1/realtime/runs (the runtime endpoint
// added by #15) and renders an auto-scrolling ticker of the last 10
// run events for the current tenant. Used on the Operate → Runs page
// header so operators get an at-a-glance feed of what's happening live
// without polling. The customer-app demo (Shipwright #24) is a separate
// concern and lives in a different package.
//
// Implementation notes:
//
//   * The dashboard talks to the runtime through Next.js rewrites, so we
//     hit `/api/v1/realtime/runs` on the same origin and the browser
//     upgrades it transparently. No SDK import needed — we go straight
//     to the WebSocket so we don't pull a Node-only package into the
//     React tree.
//   * Auto-reconnects with exponential backoff (1s → 30s cap) on
//     transient drop. A `server.degraded` envelope closes the socket
//     and the same backoff loop kicks in.
//   * Tenant is auto-bound server-side under multi-tenancy (the runtime
//     ignores any tenant_id we'd pass), so we don't need to know our
//     own tenant id to render the strip.

"use client"

import * as React from "react"
import { Activity, CircleDashed, CircleSlash, AlertCircle } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ScrollArea } from "@/components/ui/scroll-area"
import { cn } from "@/lib/utils"

// Wire-protocol shape — must match
// services/runtime/internal/server/runs.go runEventWireMessage.
interface RunEvent {
  type:
    | "run.started"
    | "run.step"
    | "run.completed"
    | "run.error"
    | "server.degraded"
  run_id?: string
  execution_id?: string
  agent?: string
  node_id?: string
  status?: string
  duration_ms?: number
  cost_usd?: number
  error?: string
  reason?: string
  updated_at?: string
}

interface StripItem {
  key: string
  event: RunEvent
  receivedAt: number
}

const MAX_VISIBLE = 10

function wsURLForRuns(): string {
  // Use the same origin and upgrade — Next.js rewrites forward
  // /api/v1/* to the runtime. The browser handles the WS upgrade.
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:"
  return `${proto}//${window.location.host}/api/v1/realtime/runs`
}

function describeEvent(evt: RunEvent): string {
  switch (evt.type) {
    case "run.started":
      return `${evt.agent ?? "agent"} started`
    case "run.step":
      return `${evt.agent ?? "agent"} • ${evt.status ?? "running"}`
    case "run.completed":
      return `${evt.agent ?? "agent"} ${evt.status ?? "done"}${
        evt.duration_ms !== undefined ? ` (${(evt.duration_ms / 1000).toFixed(1)}s)` : ""
      }`
    case "run.error":
      return `error: ${evt.error ?? "unknown"}`
    case "server.degraded":
      return `live stream degraded: ${evt.reason ?? "unknown"}`
  }
}

function statusVariant(evt: RunEvent): "default" | "destructive" | "secondary" | "outline" {
  if (evt.type === "run.error" || evt.type === "server.degraded") return "destructive"
  if (evt.type === "run.completed") {
    if (evt.status === "failed") return "destructive"
    if (evt.status === "cancelled") return "outline"
    return "default"
  }
  return "secondary"
}

function statusIcon(evt: RunEvent): React.ReactNode {
  if (evt.type === "run.error") return <AlertCircle className="h-3.5 w-3.5" />
  if (evt.type === "server.degraded") return <CircleSlash className="h-3.5 w-3.5" />
  if (evt.type === "run.completed") return <Activity className="h-3.5 w-3.5" />
  return <CircleDashed className="h-3.5 w-3.5 animate-spin" />
}

function shortenId(id: string | undefined): string {
  if (!id) return ""
  if (id.length <= 12) return id
  return id.slice(0, 8) + "…"
}

export function LiveRunsStrip() {
  const [items, setItems] = React.useState<StripItem[]>([])
  const [connectionState, setConnectionState] = React.useState<"connecting" | "open" | "closed">(
    "connecting",
  )

  React.useEffect(() => {
    // We re-open on close with exponential backoff capped at 30s. The
    // attempt counter resets on a successful open so we don't keep
    // climbing across reconnect cycles.
    let attempt = 0
    let socket: WebSocket | null = null
    let reopenTimer: number | undefined
    let cancelled = false

    const open = () => {
      if (cancelled) return
      setConnectionState("connecting")
      try {
        socket = new WebSocket(wsURLForRuns())
      } catch {
        // Constructor can throw on malformed URL — schedule a retry
        // rather than crash the React tree.
        scheduleReopen()
        return
      }
      socket.addEventListener("open", () => {
        attempt = 0
        setConnectionState("open")
      })
      socket.addEventListener("message", (e) => {
        let parsed: RunEvent
        try {
          parsed = JSON.parse(e.data as string)
        } catch {
          return
        }
        const item: StripItem = {
          key: `${parsed.execution_id ?? parsed.run_id ?? Math.random().toString(36).slice(2)}-${
            parsed.type
          }-${Date.now()}`,
          event: parsed,
          receivedAt: Date.now(),
        }
        setItems((prev) => [item, ...prev].slice(0, MAX_VISIBLE))
      })
      socket.addEventListener("close", () => {
        setConnectionState("closed")
        scheduleReopen()
      })
      socket.addEventListener("error", () => {
        // The runtime closes the WS cleanly on degraded; "error" is the
        // browser's transport-level signal. Either way, schedule a
        // reopen and let the close handler trigger it.
        setConnectionState("closed")
      })
    }

    const scheduleReopen = () => {
      if (cancelled) return
      const delayMs = Math.min(30_000, 1_000 * Math.pow(2, attempt))
      attempt += 1
      reopenTimer = window.setTimeout(open, delayMs)
    }

    open()
    return () => {
      cancelled = true
      if (reopenTimer !== undefined) window.clearTimeout(reopenTimer)
      socket?.close()
    }
  }, [])

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-sm font-medium flex items-center gap-2">
          <Activity className="h-4 w-4" />
          Live activity
        </CardTitle>
        <div className="flex items-center gap-2">
          <span
            className={cn(
              "inline-block h-2 w-2 rounded-full",
              connectionState === "open" && "bg-emerald-500",
              connectionState === "connecting" && "bg-amber-500 animate-pulse",
              connectionState === "closed" && "bg-rose-500",
            )}
            aria-hidden
          />
          <span className="text-xs text-muted-foreground">
            {connectionState === "open" ? "live" : connectionState}
          </span>
        </div>
      </CardHeader>
      <CardContent>
        <ScrollArea className="h-32">
          {items.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              Waiting for run events. They'll appear here as agents execute.
            </p>
          ) : (
            <ul className="space-y-1.5">
              {items.map((item) => (
                <li
                  key={item.key}
                  className="flex items-center gap-2 text-sm transition-opacity"
                >
                  <Badge variant={statusVariant(item.event)} className="gap-1">
                    {statusIcon(item.event)}
                    <span>{item.event.type.replace("run.", "")}</span>
                  </Badge>
                  <span className="font-mono text-xs text-muted-foreground">
                    {shortenId(item.event.run_id ?? item.event.execution_id)}
                  </span>
                  <span className="truncate">{describeEvent(item.event)}</span>
                </li>
              ))}
            </ul>
          )}
        </ScrollArea>
      </CardContent>
    </Card>
  )
}

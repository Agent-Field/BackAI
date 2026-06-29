// SPDX-License-Identifier: Apache-2.0

import { ArrowUpRight } from "lucide-react"

import { Button } from "@/components/ui/button"

import type { AdminService } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"
import { classifyServiceStatus, formatRelative } from "@/lib/health/derive"

// One row in Zone B per backing service. Dot is always left-of-name
// (C4 consistency rule), version/host sit in the middle, and every
// service with an admin_url renders an [Open ↗] button on the right
// (A5: Block 0 decision #4 — Health is the central OSS-link hub).

const DOT: Record<StatusState, string> = {
  ok: "bg-success",
  watch: "bg-warning",
  act: "bg-destructive",
  idle: "bg-muted-foreground/60",
}

const STATUS_LABEL: Record<string, string> = {
  healthy: "healthy",
  degraded: "degraded",
  offline: "offline",
  down: "down",
  configured: "configured",
}

export function ServiceHealthRow({ service }: { service: AdminService }) {
  const state = classifyServiceStatus(service.status)
  const statusTone =
    state === "ok"
      ? "text-muted-foreground"
      : state === "watch"
        ? "text-warning font-medium"
        : state === "act"
          ? "text-destructive font-medium"
          : "text-muted-foreground"
  const statusLabel = STATUS_LABEL[service.status] ?? service.status
  const hostLabel = service.host
    ? service.port
      ? `${service.host}:${service.port}`
      : service.host
    : null
  return (
    <div className="grid grid-cols-[24px_minmax(0,1fr)_minmax(120px,1fr)_120px_88px_92px] items-center gap-stack px-row-x py-row-y text-meta">
      <span
        aria-hidden
        className={`inline-block size-icon-dot rounded-pill ${DOT[state]}`}
      />
      <div className="flex min-w-0 flex-col gap-tile-tight">
        <span className="truncate text-body font-medium text-foreground">
          {service.name}
        </span>
        <span className="truncate text-meta text-muted-foreground">
          {service.purpose}
        </span>
      </div>
      <div className="flex min-w-0 flex-col gap-tile-tight font-mono">
        {hostLabel ? (
          <span className="truncate text-meta text-foreground">{hostLabel}</span>
        ) : (
          <span className="text-meta text-muted-foreground">in-process</span>
        )}
        {service.version ? (
          <span className="truncate text-meta text-muted-foreground">
            {service.version}
          </span>
        ) : null}
      </div>
      <span className={`text-right text-meta ${statusTone}`}>{statusLabel}</span>
      <span className="text-right font-mono text-meta tabular-nums text-muted-foreground">
        {formatRelative(service.checked_at)}
      </span>
      {service.admin_url ? (
        <Button
          size="sm"
          variant="outline"
          className="h-7 gap-inline justify-self-end text-meta"
          render={
            <a
              href={service.admin_url}
              target="_blank"
              rel="noopener noreferrer"
            />
          }
        >
          Open
          <ArrowUpRight className="size-3" aria-hidden />
        </Button>
      ) : (
        <span aria-hidden />
      )}
    </div>
  )
}

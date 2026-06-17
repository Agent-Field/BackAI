// SPDX-License-Identifier: Apache-2.0

import { ArrowUpRight } from "lucide-react"

import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"

import type { AdminServiceList } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"

// Single-row strip of compact pills, one per backing service. Each pill
// has: status dot · short name · trimmed version · ↗ (only when an
// admin_url exists). Hover shows full name, full version, purpose, and
// the admin_url destination.

const DOT: Record<StatusState, string> = {
  ok: "bg-success",
  watch: "bg-warning",
  act: "bg-destructive",
  idle: "bg-muted-foreground/40",
}

export function BackingServicesStrip({
  services,
}: {
  services: AdminServiceList | null
}) {
  const rows = services?.services ?? []
  return (
    <section
      aria-labelledby="services-heading"
      className="rounded-md border bg-card"
    >
      <header className="flex items-center justify-between border-b px-row-x py-row-y">
        <h2
          id="services-heading"
          className="text-body font-medium text-foreground"
        >
          Backing services
        </h2>
        <span className="text-meta text-muted-foreground">
          {rows.length} configured
        </span>
      </header>
      {rows.length === 0 ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          No backing services reported by the runtime.
        </p>
      ) : (
        <div className="flex flex-wrap gap-strip px-row-x py-tile">
          {rows.map((svc) => (
            <ServicePill
              key={svc.id}
              id={svc.id}
              name={svc.name}
              status={classifyServiceStatus(svc.status)}
              statusLabel={svc.status}
              version={svc.version}
              purpose={svc.purpose}
              adminURL={svc.admin_url ?? undefined}
            />
          ))}
        </div>
      )}
    </section>
  )
}

function classifyServiceStatus(raw: string): StatusState {
  switch (raw) {
    case "healthy":
      return "ok"
    case "degraded":
      return "watch"
    case "configured":
      // Configured but not actively probed — we don't know it's healthy.
      // Per the critique: green dot when we haven't checked is misleading.
      return "idle"
    case "offline":
    case "down":
      return "act"
    default:
      return "idle"
  }
}

interface ServicePillProps {
  id: string
  name: string
  status: StatusState
  statusLabel: string
  version?: string
  purpose: string
  adminURL?: string
}

function ServicePill({
  id,
  name,
  status,
  statusLabel,
  version,
  purpose,
  adminURL,
}: ServicePillProps) {
  const trimmed = trimVersion(version)
  const pillClasses =
    "inline-flex h-7 items-center gap-inline rounded-md border px-pill-x text-meta text-foreground transition-colors hover:bg-accent/40"
  const dotEl = (
    <span
      aria-hidden
      className={`inline-block size-icon-dot rounded-pill ${DOT[status]}`}
    />
  )
  const tooltip = (
    <TooltipContent>
      <div className="flex flex-col gap-1 text-meta">
        <span className="font-medium text-foreground">
          {name} · {statusLabel}
        </span>
        {version ? (
          <span className="font-mono text-muted-foreground">{version}</span>
        ) : null}
        <span className="text-muted-foreground">{purpose}</span>
        {adminURL ? (
          <span className="font-mono text-muted-foreground">{adminURL}</span>
        ) : null}
      </div>
    </TooltipContent>
  )
  const inner = (
    <>
      {dotEl}
      <span className="font-medium">{name}</span>
      {trimmed ? (
        <span className="font-mono text-muted-foreground">{trimmed}</span>
      ) : null}
      {adminURL ? (
        <ArrowUpRight aria-hidden className="size-3 text-muted-foreground" />
      ) : null}
    </>
  )
  if (adminURL) {
    return (
      <Tooltip>
        <TooltipTrigger
          key={id}
          render={
            <a
              href={adminURL}
              target="_blank"
              rel="noopener noreferrer"
              className={pillClasses}
            />
          }
        >
          {inner}
        </TooltipTrigger>
        {tooltip}
      </Tooltip>
    )
  }
  return (
    <Tooltip>
      <TooltipTrigger key={id} render={<span className={pillClasses} />}>
        {inner}
      </TooltipTrigger>
      {tooltip}
    </Tooltip>
  )
}

// trimVersion: keep just the major.minor (or major) part — drop Postgres
// vendor suffixes like "(Debian 16.14-1.pgdg12+1)".
function trimVersion(raw?: string): string | null {
  if (!raw) return null
  const match = raw.match(/(\d+(?:\.\d+)?)/)
  if (!match) return null
  return `v${match[1]}`
}

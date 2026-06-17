// SPDX-License-Identifier: Apache-2.0

import { ArrowUpRight } from "lucide-react"

import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"

import type { AdminServiceList } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"

// Compact pills, one per backing service. The shell fills any remaining
// vertical space in the home right-column; pills wrap and the wrap
// container scrolls internally if there are more services than fit.

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
      className="flex min-h-0 flex-1 flex-col rounded-md border bg-card"
    >
      <header className="flex shrink-0 items-center justify-between border-b px-row-x py-row-y">
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
        <ScrollArea className="min-h-0 flex-1">
          <div className="flex flex-wrap gap-inline px-row-x py-tile">
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
        </ScrollArea>
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

function trimVersion(raw?: string): string | null {
  if (!raw) return null
  const match = raw.match(/(\d+(?:\.\d+)?)/)
  if (!match) return null
  return `v${match[1]}`
}

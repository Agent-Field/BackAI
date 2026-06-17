// SPDX-License-Identifier: Apache-2.0

import { ExternalLink } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"

import type { AdminServiceList } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"

// One pill per backing service. Each is a Link when admin_url is set; a
// plain badge otherwise. The status dot is the only non-monochrome
// element — three semantic colours from globals.css.

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
      className="flex flex-col gap-stack"
    >
      <h2
        id="services-heading"
        className="text-eyebrow uppercase text-muted-foreground"
      >
        Backing services
      </h2>
      {rows.length === 0 ? (
        <p className="text-meta text-muted-foreground">
          No backing services reported by the runtime.
        </p>
      ) : (
        <div className="flex flex-wrap gap-inline">
          {rows.map((svc) => (
            <ServicePill
              key={svc.id}
              id={svc.id}
              name={svc.name}
              status={classifyServiceStatus(svc.status)}
              statusLabel={svc.status}
              version={svc.version}
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
    case "configured":
      return "watch"
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
  adminURL?: string
}

function ServicePill({
  id,
  name,
  status,
  statusLabel,
  version,
  adminURL,
}: ServicePillProps) {
  const body = (
    <Badge
      variant="outline"
      className="inline-flex items-center gap-inline px-pill-x py-pill-y"
    >
      <span
        aria-hidden
        className={`inline-block h-2 w-2 rounded-pill ${DOT[status]}`}
      />
      <span>{name}</span>
      {version ? (
        <span className="text-meta text-muted-foreground">v{version}</span>
      ) : null}
      {adminURL ? (
        <ExternalLink aria-hidden className="size-3 text-muted-foreground" />
      ) : null}
    </Badge>
  )
  const tooltip = (
    <TooltipContent>
      <div className="flex flex-col gap-inline">
        <span>{statusLabel}</span>
        {adminURL ? (
          <span className="text-meta text-muted-foreground">
            Opens {adminURL}
          </span>
        ) : null}
      </div>
    </TooltipContent>
  )
  if (adminURL) {
    return (
      <Tooltip>
        <TooltipTrigger
          key={id}
          render={
            <a href={adminURL} target="_blank" rel="noopener noreferrer" />
          }
        >
          {body}
        </TooltipTrigger>
        {tooltip}
      </Tooltip>
    )
  }
  return (
    <Tooltip>
      <TooltipTrigger key={id} render={<span />}>
        {body}
      </TooltipTrigger>
      {tooltip}
    </Tooltip>
  )
}

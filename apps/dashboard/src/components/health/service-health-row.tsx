// SPDX-License-Identifier: Apache-2.0

import { ArrowUpRight } from "lucide-react"

import type { AdminService } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"
import { classifyServiceStatus, formatRelative } from "@/lib/health/derive"

// One row in Zone B per backing service. Status dot tokenised; admin
// URL renders as a "↗" affordance only when present so rows without an
// admin UI stay quiet.

const DOT: Record<StatusState, string> = {
  ok: "bg-success",
  watch: "bg-warning",
  act: "bg-destructive",
  idle: "bg-muted-foreground/60",
}

export function ServiceHealthRow({ service }: { service: AdminService }) {
  const state = classifyServiceStatus(service.status)
  const hostLabel = service.host
    ? service.port
      ? `${service.host}:${service.port}`
      : service.host
    : null
  const Wrapper: React.ElementType = service.admin_url ? "a" : "div"
  const wrapperProps = service.admin_url
    ? {
        href: service.admin_url,
        target: "_blank",
        rel: "noopener noreferrer",
      }
    : {}
  return (
    <Wrapper
      {...wrapperProps}
      className={`group grid grid-cols-[24px_minmax(0,1fr)_minmax(120px,1fr)_140px_18px] items-center gap-stack px-row-x py-row-y text-meta ${
        service.admin_url
          ? "hover:bg-accent/40 focus-visible:bg-accent/40 focus-visible:outline-none"
          : ""
      }`}
    >
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
          <span className="truncate text-meta text-foreground">
            {hostLabel}
          </span>
        ) : (
          <span className="text-meta text-muted-foreground">—</span>
        )}
        {service.version ? (
          <span className="truncate text-meta text-muted-foreground">
            {service.version}
          </span>
        ) : null}
      </div>
      <span className="text-right font-mono text-meta tabular-nums text-muted-foreground">
        {formatRelative(service.checked_at)}
      </span>
      <ArrowUpRight
        aria-hidden
        className={`size-3.5 text-muted-foreground ${
          service.admin_url
            ? "opacity-0 transition-opacity group-hover:opacity-100"
            : "opacity-0"
        }`}
      />
    </Wrapper>
  )
}

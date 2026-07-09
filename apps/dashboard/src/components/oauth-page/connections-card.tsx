// SPDX-License-Identifier: Apache-2.0

"use client"

import { Badge } from "@/components/ui/badge"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import type { OAuthConnection } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"
import { classifyConnectionStatus, formatExpiry, providerLabel } from "@/lib/oauth-page/derive"

// Active connections zone — every stored grant across tenants, one row
// each: which provider, whose account, the granted scopes, token expiry,
// and a status dot. This is the read-only mirror of the Providers zone;
// mutations live up there. Header row stays visible even at zero rows.

const DOT: Record<StatusState, string> = {
  ok: "bg-success",
  watch: "bg-warning",
  act: "bg-destructive",
  idle: "bg-muted-foreground/60",
}

const CONNECTION_COLUMNS = "grid-cols-[12px_minmax(0,140px)_minmax(0,1fr)_minmax(0,120px)_96px]"

interface ConnectionsCardProps {
  connections: OAuthConnection[]
  healthy: boolean
}

export function ConnectionsCard({ connections, healthy }: ConnectionsCardProps) {
  return (
    <ZoneCard aria-labelledby="oauth-connections">
      <ZoneCardHeader
        id="oauth-connections"
        title="Connections"
        subtitle={healthy ? `${connections.length} active` : "unavailable"}
      />
      <TableHeader />
      {!healthy ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          The runtime did not return OAuth connections.
        </p>
      ) : connections.length === 0 ? (
        <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
          <p className="text-body text-foreground">No connections yet.</p>
          <p className="max-w-md text-meta text-muted-foreground">
            Connect a provider above to store its first tenant grant.
          </p>
        </div>
      ) : (
        <ul role="list" className="flex flex-col">
          {connections.map((c, i) => (
            <li key={c.id ?? `${c.provider}-${c.account_id ?? c.user_id ?? i}`}>
              <ConnectionRow connection={c} />
            </li>
          ))}
        </ul>
      )}
    </ZoneCard>
  )
}

function ConnectionRow({ connection }: { connection: OAuthConnection }) {
  const tone = classifyConnectionStatus(connection.status)
  const scopes = connection.scopes ?? []
  const account = connection.account_id ?? connection.user_id ?? connection.tenant_id ?? "—"
  return (
    <div
      className={`grid items-center gap-stack border-b px-row-x py-row-y text-meta ${CONNECTION_COLUMNS}`}
    >
      <span aria-hidden className={`inline-block size-icon-dot rounded-pill ${DOT[tone]}`} />
      <span className="truncate font-mono text-body text-foreground">
        {providerLabel(connection.provider)}
      </span>
      <div className="flex min-w-0 flex-col gap-tile-tight">
        <span className="truncate font-mono text-foreground" title={account}>
          {account}
        </span>
        {scopes.length > 0 ? (
          <span className="truncate font-mono text-muted-foreground" title={scopes.join(" ")}>
            {scopes.join(" ")}
          </span>
        ) : null}
      </div>
      <span
        className="truncate tabular-nums text-muted-foreground"
        title={connection.expires_at ?? undefined}
      >
        {formatExpiry(connection.expires_at)}
      </span>
      <div className="flex items-center justify-end">
        <Badge variant={tone === "act" ? "destructive" : tone === "ok" ? "secondary" : "outline"}>
          {connection.status ?? "unknown"}
        </Badge>
      </div>
    </div>
  )
}

function TableHeader() {
  return (
    <header
      className={`grid items-center gap-stack border-b bg-card px-row-x py-tile-tight text-eyebrow uppercase tracking-wide text-muted-foreground ${CONNECTION_COLUMNS}`}
    >
      <span aria-hidden />
      <span>Provider</span>
      <span>Account / Scopes</span>
      <span>Expires</span>
      <span className="text-right">Status</span>
    </header>
  )
}

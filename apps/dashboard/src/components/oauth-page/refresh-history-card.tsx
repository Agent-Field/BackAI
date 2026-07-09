// SPDX-License-Identifier: Apache-2.0

"use client"

import { FilterChip, FilterChipGroup } from "@/components/ui/filter-chip"
import { Skeleton } from "@/components/ui/skeleton"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import type { OAuthProvider } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"
import {
  classifyRefreshStatus,
  formatOAuthAge,
  providerLabel,
  shortId,
} from "@/lib/oauth-page/derive"
import {
  DEFAULT_PROVIDER_FILTER,
  type OAuthProviderFilter,
  type OAuthRefreshEvent,
} from "@/lib/oauth-page/types"

// Token-refresh history zone — the background worker's attempt log,
// newest first, filterable by provider. Each row: a status dot, when it
// ran, which provider, whose token, the outcome, and any error code. The
// filter chips are URL-driven up in the shell so the view is linkable.

const DOT: Record<StatusState, string> = {
  ok: "bg-success",
  watch: "bg-warning",
  act: "bg-destructive",
  idle: "bg-muted-foreground/60",
}

const HISTORY_COLUMNS = "grid-cols-[12px_88px_minmax(0,140px)_minmax(0,1fr)_120px]"

interface RefreshHistoryCardProps {
  events: OAuthRefreshEvent[]
  providers: OAuthProvider[]
  provider: OAuthProviderFilter
  loading: boolean
  healthy: boolean
  onProviderChange: (next: OAuthProviderFilter) => void
}

export function RefreshHistoryCard({
  events,
  providers,
  provider,
  loading,
  healthy,
  onProviderChange,
}: RefreshHistoryCardProps) {
  return (
    <ZoneCard aria-labelledby="oauth-refresh-history">
      <ZoneCardHeader
        id="oauth-refresh-history"
        title="Refresh history"
        subtitle={healthy ? `${events.length} attempts` : "unavailable"}
        trailing={
          <FilterChipGroup label="Provider">
            <FilterChip
              label="All"
              active={provider === DEFAULT_PROVIDER_FILTER}
              onSelect={() => onProviderChange(DEFAULT_PROVIDER_FILTER)}
            />
            {providers.map((p) => (
              <FilterChip
                key={p.provider}
                label={providerLabel(p.provider)}
                active={provider === p.provider}
                onSelect={() => onProviderChange(p.provider)}
              />
            ))}
          </FilterChipGroup>
        }
      />
      <TableHeader />
      {!healthy ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          The runtime did not return the refresh history.
        </p>
      ) : loading && events.length === 0 ? (
        <SkeletonRows />
      ) : events.length === 0 ? (
        <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
          <p className="text-body text-foreground">No refresh attempts yet.</p>
          <p className="max-w-md text-meta text-muted-foreground">
            The token-refresh worker logs an entry each time it renews or fails to renew a stored
            grant.
          </p>
        </div>
      ) : (
        <ul role="list" className="flex flex-col">
          {events.map((event) => (
            <li key={event.id}>
              <HistoryRow event={event} />
            </li>
          ))}
        </ul>
      )}
    </ZoneCard>
  )
}

function HistoryRow({ event }: { event: OAuthRefreshEvent }) {
  const tone = classifyRefreshStatus(event.status)
  const statusTone =
    tone === "act"
      ? "text-destructive font-medium"
      : tone === "watch"
        ? "text-warning font-medium"
        : "text-muted-foreground"
  return (
    <div
      className={`grid items-center gap-stack border-b px-row-x py-row-y text-meta ${HISTORY_COLUMNS}`}
    >
      <span aria-hidden className={`inline-block size-icon-dot rounded-pill ${DOT[tone]}`} />
      <span className="truncate font-mono tabular-nums text-muted-foreground">
        {formatOAuthAge(event.attempted_at)}
      </span>
      <span className="truncate font-mono text-foreground">{providerLabel(event.provider)}</span>
      <span className="truncate font-mono text-muted-foreground" title={event.user_id ?? undefined}>
        {event.user_id ? shortId(event.user_id) : "—"}
      </span>
      <span className={`truncate text-right ${statusTone}`} title={event.error_code ?? undefined}>
        {event.error_code ? `${event.status} · ${event.error_code}` : event.status}
      </span>
    </div>
  )
}

function TableHeader() {
  return (
    <header
      className={`grid items-center gap-stack border-b bg-card px-row-x py-tile-tight text-eyebrow uppercase tracking-wide text-muted-foreground ${HISTORY_COLUMNS}`}
    >
      <span aria-hidden />
      <span>Time</span>
      <span>Provider</span>
      <span>User</span>
      <span className="text-right">Status / Error</span>
    </header>
  )
}

function SkeletonRows() {
  return (
    <ul role="list" className="flex flex-col">
      {Array.from({ length: 5 }).map((_, i) => (
        <li
          key={i}
          className={`grid items-center gap-stack border-b px-row-x py-row-y ${HISTORY_COLUMNS}`}
        >
          <Skeleton className="size-icon-dot rounded-pill" />
          <Skeleton className="h-3 w-12" />
          <Skeleton className="h-3 w-20" />
          <Skeleton className="h-3 w-24" />
          <Skeleton className="h-3 w-16 justify-self-end" />
        </li>
      ))}
    </ul>
  )
}

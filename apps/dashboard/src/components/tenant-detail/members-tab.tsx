// SPDX-License-Identifier: Apache-2.0

import { Badge } from "@/components/ui/badge"
import { RelativeTime } from "@/components/ui/relative-time"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import type { TenantDrilldown } from "@/lib/api"
import { formatAge } from "@/lib/tenant-detail/derive"

// Members list. Mutations (Invite / Remove) deferred to v0.2 per brief
// — v1 surfaces the roster so operators can audit who has access.

const ROLE_TONE: Record<string, string> = {
  owner: "border-foreground/40 bg-foreground/10 text-foreground",
  admin: "border-border bg-card text-foreground",
  member: "border-border bg-transparent text-muted-foreground",
}

export function MembersTab({ drilldown }: { drilldown: TenantDrilldown }) {
  const members = drilldown.members
  return (
    <ZoneCard>
      <ZoneCardHeader
        title="Members"
        subtitle={`${members.length} member${members.length === 1 ? "" : "s"}`}
      />
      {members.length === 0 ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          No members yet. Invite a teammate to get started.
        </p>
      ) : (
        <ul role="list" className="divide-y">
          {members.map((m) => {
            const roleClass =
              ROLE_TONE[m.role] ?? ROLE_TONE.member
            return (
              <li
                key={m.user.id}
                className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_120px_120px] items-center gap-stack px-row-x py-row-y text-meta"
              >
                <span
                  className="min-w-0 truncate text-body text-foreground"
                  title={m.user.name ?? undefined}
                >
                  {m.user.name ?? m.user.email}
                </span>
                <span
                  className="truncate font-mono text-meta text-muted-foreground"
                  title={m.user.email}
                >
                  {m.user.email}
                </span>
                <Badge
                  variant="outline"
                  className={`justify-self-start text-meta ${roleClass}`}
                >
                  {m.role}
                </Badge>
                <RelativeTime
                  iso={m.last_active_at}
                  format={formatAge}
                  className="text-right font-mono text-meta tabular-nums text-muted-foreground"
                />
              </li>
            )
          })}
        </ul>
      )}
    </ZoneCard>
  )
}

// SPDX-License-Identifier: Apache-2.0

"use client"

import { Badge } from "@/components/ui/badge"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import type { NotificationChannel } from "@/lib/api"

// Channels zone — the delivery channels the runtime has configured
// (which adapter handles each kind, and where its config comes from).
// Read-only: channels are provisioned from env/adapter config, not the
// dashboard, so there's no create/delete here — just visibility into
// what's wired and whether it's enabled.

interface ChannelsCardProps {
  channels: NotificationChannel[]
  healthy: boolean
}

export function ChannelsCard({ channels, healthy }: ChannelsCardProps) {
  return (
    <ZoneCard aria-labelledby="notifications-channels">
      <ZoneCardHeader
        id="notifications-channels"
        title="Channels"
        subtitle={healthy ? `${channels.length} configured` : "unavailable"}
      />
      {!healthy ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          The runtime did not return channels. Check the Health page, then retry.
        </p>
      ) : channels.length === 0 ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          No channels configured. The active notification adapter is set via the{" "}
          <code className="font-mono">NOTIFICATIONS_ADAPTER</code> env var (defaults to{" "}
          <code className="font-mono">log</code> in dev).
        </p>
      ) : (
        <ul role="list" className="divide-y">
          {channels.map((ch) => (
            <li
              key={ch.id}
              className="grid grid-cols-[auto_minmax(0,1fr)_auto_auto] items-center gap-stack px-row-x py-row-y text-meta"
            >
              <Badge variant="secondary">{ch.kind}</Badge>
              <span
                className="truncate font-mono text-meta text-muted-foreground"
                title={JSON.stringify(ch.config_json)}
              >
                {summariseConfig(ch.config_json)}
              </span>
              <Badge variant="outline">{ch.source}</Badge>
              {ch.enabled ? (
                <Badge variant="secondary">enabled</Badge>
              ) : (
                <Badge variant="outline">disabled</Badge>
              )}
            </li>
          ))}
        </ul>
      )}
    </ZoneCard>
  )
}

// The config blob is adapter-specific and may hold secrets-by-reference.
// Render the keys only — never the values — so a copy/paste of the row
// can't leak a token into a screenshot.
function summariseConfig(config: Record<string, unknown>): string {
  const keys = Object.keys(config)
  if (keys.length === 0) return "no config"
  return keys.join(", ")
}

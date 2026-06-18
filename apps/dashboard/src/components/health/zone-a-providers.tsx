// SPDX-License-Identifier: Apache-2.0

"use client"

import { FilterChip, FilterChipGroup } from "@/components/ui/filter-chip"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import type { HealthSnapshot, ProviderWindowKind } from "@/lib/health/types"

import { ProviderHealthCard } from "./provider-health-card"

// Zone A — LLM Provider Availability. Topmost rich zone because
// providers drive incidents. Window chips reuse the standardised
// FilterChip primitive so contrast + density match Inbox / Cost chips.

const WINDOW_OPTIONS: { value: ProviderWindowKind; label: string }[] = [
  { value: "24h", label: "Last 24h" },
  { value: "7d", label: "Last 7d" },
  { value: "30d", label: "Last 30d" },
]

export function ZoneAProviders({
  snapshot,
  onWindowChange,
}: {
  snapshot: HealthSnapshot
  onWindowChange: (next: ProviderWindowKind) => void
}) {
  const providers = snapshot.providers
  const empty = providers.length === 0
  return (
    <ZoneCard aria-labelledby="zone-a-heading">
      <ZoneCardHeader
        id="zone-a-heading"
        title="LLM providers"
        subtitle={empty ? "No providers reported yet" : `${providers.length} configured`}
        trailing={
          <FilterChipGroup label="Window">
            {WINDOW_OPTIONS.map((opt) => (
              <FilterChip
                key={opt.value}
                label={opt.label}
                active={snapshot.providerWindow === opt.value}
                onSelect={() => onWindowChange(opt.value)}
              />
            ))}
          </FilterChipGroup>
        }
      />
      {empty ? (
        <EmptyProvidersCard />
      ) : (
        <div
          className={`grid gap-stack p-row-x ${
            providers.length === 1
              ? "grid-cols-1"
              : "md:grid-cols-2 lg:grid-cols-3"
          }`}
        >
          {providers.map((p) => (
            <ProviderHealthCard key={p.provider} provider={p} />
          ))}
        </div>
      )}
    </ZoneCard>
  )
}

function EmptyProvidersCard() {
  return (
    <div className="flex flex-col items-start gap-stack rounded-md border border-dashed border-muted-foreground/40 bg-card/30 m-row-x px-row-x py-tile">
      <p className="text-meta text-muted-foreground">
        No providers configured. Add a provider in{" "}
        <a
          href="/platform/adapters"
          className="text-foreground underline hover:no-underline"
        >
          PLATFORM → Adapters
        </a>
        , or set <code className="font-mono text-foreground">OPENROUTER_API_KEY</code>{" "}
        in <code className="font-mono text-foreground">.env</code> to start with
        OpenRouter.
      </p>
    </div>
  )
}

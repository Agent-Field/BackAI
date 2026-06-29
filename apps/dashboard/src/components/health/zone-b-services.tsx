// SPDX-License-Identifier: Apache-2.0

import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import { groupServicesByKind } from "@/lib/health/derive"
import type { HealthSnapshot } from "@/lib/health/types"

import { ServiceHealthRow } from "./service-health-row"

// Zone B — Connected services grid grouped by kind. Per the brief
// (decision #4) Health is the single hub where every OSS admin UI is
// surfaced; service rows that carry an admin_url render as click-out
// links. Other pages only get adapter pills for their specific entity.

export function ZoneBServices({ snapshot }: { snapshot: HealthSnapshot }) {
  const groups = groupServicesByKind(snapshot.services)
  return (
    <ZoneCard aria-labelledby="zone-b-heading">
      <ZoneCardHeader
        id="zone-b-heading"
        title="Connected services"
        subtitle={
          snapshot.services.length === 0
            ? "No services reported yet"
            : `${snapshot.services.length} configured`
        }
      />
      {snapshot.services.length === 0 ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          The runtime has not reported any backing services. Check that
          adapters are configured under{" "}
          <a
            href="/platform/adapters"
            className="text-foreground underline hover:no-underline"
          >
            PLATFORM → Adapters
          </a>
          .
        </p>
      ) : (
        <div className="flex flex-col gap-stack p-row-x">
          {groups.map((g) => (
            <section
              key={g.key}
              className="rounded-md border bg-card"
              aria-labelledby={`zone-b-group-${g.key}`}
            >
              <header className="flex items-center justify-between border-b px-row-x py-row-y">
                <h3
                  id={`zone-b-group-${g.key}`}
                  className="text-eyebrow uppercase tracking-wide text-muted-foreground"
                >
                  {g.label}
                </h3>
                <span className="text-meta text-muted-foreground tabular-nums">
                  {g.services.length}
                </span>
              </header>
              <ul role="list" className="divide-y">
                {g.services.map((svc) => (
                  <li key={svc.id}>
                    <ServiceHealthRow service={svc} />
                  </li>
                ))}
              </ul>
            </section>
          ))}
        </div>
      )}
    </ZoneCard>
  )
}

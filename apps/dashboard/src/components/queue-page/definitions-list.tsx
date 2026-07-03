// SPDX-License-Identifier: Apache-2.0

"use client"

import { Badge } from "@/components/ui/badge"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import type { JobDefinition } from "@/lib/api"

// Compact "what kinds of jobs exist" list from
// GET /api/v1/jobs/definitions. One row per registered worker with its
// language, optional cron schedule, and recent outcome counts.

interface DefinitionsListProps {
  definitions: JobDefinition[]
  healthy: boolean
}

export function DefinitionsList({ definitions, healthy }: DefinitionsListProps) {
  return (
    <ZoneCard aria-labelledby="queue-definitions">
      <ZoneCardHeader
        id="queue-definitions"
        title="Job definitions"
        subtitle={
          healthy ? `${definitions.length} registered` : "unavailable"
        }
      />
      {!healthy ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          The runtime did not return job definitions.
        </p>
      ) : definitions.length === 0 ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          No job definitions registered. Declare one with the SDK&apos;s{" "}
          <code className="font-mono">jobs.define()</code> and restart the
          worker.
        </p>
      ) : (
        <ul role="list" className="divide-y">
          {definitions.map((def) => (
            <li
              key={def.name}
              className="grid grid-cols-[minmax(0,1fr)_auto_auto] items-center gap-stack px-row-x py-row-y text-meta"
            >
              <div className="flex min-w-0 flex-col gap-tile-tight">
                <div className="flex min-w-0 items-center gap-inline">
                  <span className="truncate font-mono text-body text-foreground">
                    {def.name}
                  </span>
                  <Badge variant="outline">{def.language}</Badge>
                  {def.cron ? (
                    <Badge variant="secondary" className="font-mono">
                      {def.cron}
                    </Badge>
                  ) : null}
                </div>
                {def.description ? (
                  <span className="truncate text-meta text-muted-foreground">
                    {def.description}
                  </span>
                ) : null}
              </div>
              <RecentCounts recent={def.recent} />
            </li>
          ))}
        </ul>
      )}
    </ZoneCard>
  )
}

function RecentCounts({ recent }: { recent: JobDefinition["recent"] }) {
  return (
    <div className="flex items-center gap-stack font-mono text-meta tabular-nums">
      <span
        className={recent.succeeded > 0 ? "text-success" : "text-muted-foreground"}
        title="Succeeded recently"
      >
        ✓ {recent.succeeded}
      </span>
      <span
        className={recent.failed > 0 ? "text-destructive" : "text-muted-foreground"}
        title="Failed recently"
      >
        ✗ {recent.failed}
      </span>
      <span
        className={recent.running > 0 ? "text-warning" : "text-muted-foreground"}
        title="Running now"
      >
        ▸ {recent.running}
      </span>
    </div>
  )
}

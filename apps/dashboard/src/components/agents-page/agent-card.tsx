// SPDX-License-Identifier: Apache-2.0

import { ArrowRight } from "lucide-react"
import Link from "next/link"

import { Badge } from "@/components/ui/badge"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import type { AgentUsage } from "@/lib/agents-page/types"
import type { AgentInfo } from "@/lib/api"
import { formatRunCost } from "@/lib/runs/derive"

// One card per registered agent: identity (node_id / version / tags),
// last-24h usage rollup, and the reasoner roster. Each reasoner links
// into the analytics table pre-filtered to this agent; the header link
// jumps to the runs ledger with the same filter.

interface AgentCardProps {
  agent: AgentInfo
  usage?: AgentUsage
  analyticsHealthy: boolean
}

export function AgentCard({ agent, usage, analyticsHealthy }: AgentCardProps) {
  const tags = agent.tags ?? []
  const reasoners = agent.reasoners ?? []
  const reasonersHref = `/build/reasoners?agent=${encodeURIComponent(agent.node_id)}`
  return (
    <ZoneCard>
      <ZoneCardHeader
        title={agent.node_id}
        subtitle={agent.version ? `v${agent.version}` : undefined}
        trailing={
          <Link
            href={`/activity/runs?agent=${encodeURIComponent(agent.node_id)}`}
            className="inline-flex items-center gap-inline text-meta text-muted-foreground transition-colors hover:text-foreground"
          >
            View runs
            <ArrowRight aria-hidden className="size-3" />
          </Link>
        }
      />
      <div className="flex flex-col gap-tile px-row-x py-row-y">
        {tags.length > 0 ? (
          <div className="flex flex-wrap gap-inline">
            {tags.map((tag) => (
              <Badge
                key={tag}
                variant="outline"
                className="font-mono text-eyebrow"
              >
                {tag}
              </Badge>
            ))}
          </div>
        ) : null}
        <dl className="grid grid-cols-3 gap-stack text-meta">
          <UsageMetric
            label="Calls · 24h"
            value={analyticsHealthy ? String(usage?.calls ?? 0) : "—"}
          />
          <UsageMetric
            label="Errors · 24h"
            value={analyticsHealthy ? String(usage?.errors ?? 0) : "—"}
            critical={analyticsHealthy && (usage?.errors ?? 0) > 0}
          />
          <UsageMetric
            label="Cost · 24h"
            value={analyticsHealthy ? formatRunCost(usage?.cost_usd ?? 0) : "—"}
          />
        </dl>
        <div className="flex flex-col gap-tile-tight">
          <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
            Reasoners
          </span>
          {reasoners.length === 0 ? (
            <p className="text-meta text-muted-foreground">
              No reasoners registered on this agent.
            </p>
          ) : (
            <ul role="list" className="flex flex-col">
              {reasoners.map((reasoner) => (
                <li key={reasoner}>
                  <Link
                    href={reasonersHref}
                    className="group flex items-center gap-inline py-0.5 font-mono text-meta text-muted-foreground transition-colors hover:text-foreground"
                  >
                    <span className="truncate">{reasoner}</span>
                    <ArrowRight
                      aria-hidden
                      className="size-3 shrink-0 opacity-0 transition-opacity group-hover:opacity-100"
                    />
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </ZoneCard>
  )
}

function UsageMetric({
  label,
  value,
  critical,
}: {
  label: string
  value: string
  critical?: boolean
}) {
  return (
    <div className="flex flex-col gap-tile-tight">
      <dt className="text-eyebrow uppercase tracking-wide text-muted-foreground">
        {label}
      </dt>
      <dd
        className={`font-mono text-body tabular-nums ${
          critical ? "text-destructive" : "text-foreground"
        }`}
      >
        {value}
      </dd>
    </div>
  )
}

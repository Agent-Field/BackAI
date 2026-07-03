// SPDX-License-Identifier: Apache-2.0

import { AgentCard } from "@/components/agents-page/agent-card"
import { DegradedNotice } from "@/components/agents-page/degraded-notice"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import { fetchAgentsSnapshot } from "@/lib/agents-page/data"

// Build → Agents. One card per agent registered with AgentField:
// identity + reasoner roster + last-24h usage rollup from the reasoner
// analytics endpoint. Server-rendered; an unreachable AgentField
// degrades to a notice, not a crash.

export const dynamic = "force-dynamic"

export default async function AgentsPage() {
  const snapshot = await fetchAgentsSnapshot()
  const { agents } = snapshot
  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <header className="flex items-baseline justify-between">
        <div className="flex flex-col gap-tile-tight">
          <h1 className="text-xl font-semibold tracking-tight text-foreground">
            Agents
          </h1>
          <p className="text-meta text-muted-foreground">
            Every agent registered with AgentField on this stack, with its
            reasoners and last-24h usage.
          </p>
        </div>
      </header>
      {!snapshot.agentsHealthy ? (
        <DegradedNotice>
          Agent registry unavailable — AgentField (or the runtime proxy in
          front of it) did not respond. Check the Health page, then reload.
        </DegradedNotice>
      ) : agents.length === 0 ? (
        <ZoneCard>
          <ZoneCardHeader title="No agents registered" />
          <p className="px-row-x py-tile text-meta text-muted-foreground">
            Scaffold one with{" "}
            <code className="font-mono">af-stack agent new &lt;name&gt;</code>{" "}
            and start it — it self-registers via AgentField.
          </p>
        </ZoneCard>
      ) : (
        <>
          {!snapshot.analyticsHealthy ? (
            <DegradedNotice>
              Reasoner analytics unavailable — usage figures on the cards
              are blank until the runtime responds.
            </DegradedNotice>
          ) : null}
          <div className="grid gap-section md:grid-cols-2 xl:grid-cols-3">
            {agents.map((agent) => (
              <AgentCard
                key={agent.node_id}
                agent={agent}
                usage={snapshot.usageByAgent[agent.node_id]}
                analyticsHealthy={snapshot.analyticsHealthy}
              />
            ))}
          </div>
        </>
      )}
    </div>
  )
}

// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"

import type { AgentUsage, AgentsSnapshot } from "./types"

// Server-side initial fetch for Build → Agents. Registry + analytics are
// fetched in parallel and tolerate failure independently: an unreachable
// AgentField degrades to `agentsHealthy: false` (the page shows a notice,
// not a crash), and a missing analytics source just blanks the per-card
// usage figures.

export async function fetchAgentsSnapshot(): Promise<AgentsSnapshot> {
  const fetchedAt = new Date().toISOString()
  const from = new Date(Date.now() - 24 * 3_600_000).toISOString()

  const [agentsResult, analyticsResult] = await Promise.allSettled([
    api.agents(),
    api.reasoners.analytics({ from }),
  ])

  const agents =
    agentsResult.status === "fulfilled" ? agentsResult.value.agents : []

  const usageByAgent: Record<string, AgentUsage> = {}
  if (analyticsResult.status === "fulfilled") {
    for (const row of analyticsResult.value.reasoners) {
      const usage = (usageByAgent[row.agent] ??= {
        calls: 0,
        errors: 0,
        cost_usd: 0,
      })
      usage.calls += row.calls
      usage.errors += row.errors
      usage.cost_usd += row.cost_usd
    }
  }

  return {
    agents,
    usageByAgent,
    fetchedAt,
    agentsHealthy: agentsResult.status === "fulfilled",
    analyticsHealthy: analyticsResult.status === "fulfilled",
  }
}

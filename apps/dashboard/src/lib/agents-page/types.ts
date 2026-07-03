// SPDX-License-Identifier: Apache-2.0

import type { AgentInfo } from "@/lib/api"

// Per-agent usage rollup for the Build → Agents cards. Aggregated from
// the reasoner analytics rows (one row per agent.reasoner pair) over the
// last 24h window.
export interface AgentUsage {
  calls: number
  errors: number
  cost_usd: number
}

export interface AgentsSnapshot {
  agents: AgentInfo[]
  // node_id → last-24h usage rollup. Missing key = no calls in window.
  usageByAgent: Record<string, AgentUsage>
  fetchedAt: string
  // Registry (AgentField via runtime) reachable. When false the page
  // shows a degraded notice instead of the card grid.
  agentsHealthy: boolean
  // Analytics reachable. When false the cards render with usage dashes
  // but the registry list still paints.
  analyticsHealthy: boolean
}

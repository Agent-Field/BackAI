// SPDX-License-Identifier: Apache-2.0

// /operate/runs/[id]/page.tsx — #25 per-run detail page.
//
// AgentField already ships a rich run / DAG / step inspector at :8081.
// Per ARCHITECTURE.md's "don't rebuild what's already excellent" rule
// the af-stack dashboard:
//
//   1. Inlines a summary card (status, agent, timing, cost, approval).
//   2. Inlines control actions (cancel / pause / resume / request
//      approval) proxied to AgentField via the runtime.
//   3. Links out to AgentField's UI for the deep view.
//
// Data flow:
//
//   page.tsx (server)  →  api.runAgentField(id)  →  runtime
//                                                    └→ AgentField
//   RunDetailView (client)                       ← props
//   RunActions (client)  →  runtime POST routes  → AgentField
//
// Fallback: when AgentField is unreachable, the page renders a
// graceful "unavailable" banner instead of throwing — the rest of the
// dashboard remains usable.

import { PageHeader } from "@/components/layout/page-header"
import {
  api,
  ApiError,
  type RunAgentField,
} from "@/lib/api"

import { RunDetailView } from "./_components/run-detail-view"

export const dynamic = "force-dynamic"

type PageProps = {
  params: Promise<{ id: string }>
}

export default async function Page({ params }: PageProps) {
  const { id } = await params

  let data: RunAgentField | null = null
  let unavailable = false
  let unavailableReason: string | undefined

  try {
    data = await api.runAgentField(id)
  } catch (e) {
    unavailable = true
    if (e instanceof ApiError) {
      unavailableReason = `${e.code}: ${e.message}`
    } else if (e instanceof Error) {
      unavailableReason = e.message
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Run detail"
        description={`Inline summary + control actions; deep DAG view in AgentField. (run id: ${id})`}
      />
      <RunDetailView
        runId={id}
        data={data}
        unavailable={unavailable}
        unavailableReason={unavailableReason}
      />
    </div>
  )
}

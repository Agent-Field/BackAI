// SPDX-License-Identifier: Apache-2.0

// run-detail-view.tsx — #25 run detail page composition.
//
// Combines the AgentField summary card + control actions at the top
// (link-out to AgentField for the deep DAG / step inspector) and leaves
// room for the existing run-info section below.
//
// The fetch happens server-side in page.tsx; we receive the resolved
// payload (or the unavailable flag) as props so the page can SSR cleanly
// and not flash an empty state on first paint.

"use client"

import * as React from "react"
import { useRouter } from "next/navigation"

import type { RunAgentField } from "@/lib/api"

import { RunActions } from "./run-actions"
import { RunSummaryCard } from "./run-summary-card"

type RunDetailViewProps = {
  runId: string
  data: RunAgentField | null
  unavailable?: boolean
  unavailableReason?: string
}

export function RunDetailView({
  runId,
  data,
  unavailable,
  unavailableReason,
}: RunDetailViewProps) {
  const router = useRouter()

  // After a control action completes, ask Next.js to refetch the
  // server-rendered payload so the badge / actions reflect the new run
  // state without a hard reload.
  const handleActionComplete = React.useCallback(() => {
    router.refresh()
  }, [router])

  return (
    <div className="flex flex-col gap-4">
      <RunSummaryCard
        data={data}
        unavailable={unavailable}
        unavailableReason={unavailableReason}
      />
      {data ? (
        <RunActions
          runId={runId}
          actionsAvailable={data.actions_available}
          agentfieldDetailsUrl={data.details_url}
          onActionComplete={handleActionComplete}
        />
      ) : null}
    </div>
  )
}

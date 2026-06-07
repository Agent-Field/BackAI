// Sandbox activity — live pool stats + recent run history.
//
// Server component pre-fetches the pool snapshot and the first page of
// runs so the table doesn't flash empty on first paint. Both fetches are
// independent and either failing alone shouldn't blank the page, so we
// resolve with Promise.allSettled and let the client view degrade
// gracefully on whichever piece is missing.

import { ServerCrash } from "lucide-react"

import { PageHeader } from "@/components/layout/page-header"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import {
  api,
  type SandboxPoolStats,
  type SandboxRunList,
} from "@/lib/api"

import { SandboxActivityView } from "./_components/sandbox-activity-view"

export const dynamic = "force-dynamic"

type Snapshot = {
  pool: SandboxPoolStats | null
  runs: SandboxRunList | null
  error: string | null
}

async function loadSnapshot(): Promise<Snapshot> {
  const [poolResult, runsResult] = await Promise.allSettled([
    api.sandbox.pool(),
    api.sandbox.list({ limit: 50 }),
  ])
  let error: string | null = null
  if (poolResult.status === "rejected" && runsResult.status === "rejected") {
    const reason = poolResult.reason
    error =
      reason instanceof Error ? reason.message : "Sandbox module unreachable"
  }
  return {
    pool: poolResult.status === "fulfilled" ? poolResult.value : null,
    runs: runsResult.status === "fulfilled" ? runsResult.value : null,
    error,
  }
}

export default async function SandboxActivityPage() {
  const { pool, runs, error } = await loadSnapshot()

  if (pool === null && runs === null) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader
          title="Sandbox Activity"
          description="Pool snapshot and recent sandbox runs with logs, artifacts, and cost."
        />
        <Empty className="border-dashed">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ServerCrash />
            </EmptyMedia>
            <EmptyTitle>Sandbox module unreachable</EmptyTitle>
            <EmptyDescription>
              {error
                ? error
                : "Enable the sandbox module in config.yaml and restart the runtime to see activity here."}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Sandbox Activity"
        description="Pool snapshot and recent sandbox runs with logs, artifacts, and cost."
      />
      <SandboxActivityView
        initialPool={pool}
        initialRuns={runs}
        initialError={error}
      />
    </div>
  )
}

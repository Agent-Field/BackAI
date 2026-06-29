// SPDX-License-Identifier: Apache-2.0

import { RunsShell } from "@/components/runs/runs-shell"
import { fetchRunsSnapshot } from "@/lib/runs/data"
import { isRunStatusFilter } from "@/lib/runs/derive"
import type { RunStatusFilter } from "@/lib/runs/types"

// Server-rendered first paint. Shell takes over for live polling +
// URL-driven filters + drawer state.

export const dynamic = "force-dynamic"

export default async function RunsPage({
  searchParams,
}: {
  searchParams: Promise<{ status?: string }>
}) {
  const sp = await searchParams
  const status: RunStatusFilter =
    sp.status && isRunStatusFilter(sp.status) ? sp.status : "all"
  const snapshot = await fetchRunsSnapshot(status)
  return <RunsShell initialSnapshot={snapshot} />
}

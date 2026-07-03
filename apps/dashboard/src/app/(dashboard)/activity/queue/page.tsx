// SPDX-License-Identifier: Apache-2.0

import { QueueShell } from "@/components/queue-page/queue-shell"
import { fetchQueueSnapshot } from "@/lib/queue-page/data"
import { isJobStateFilter } from "@/lib/queue-page/derive"
import type { JobStateFilter } from "@/lib/queue-page/types"

// Server-rendered first paint. Shell takes over for live polling +
// URL-driven state filter + inline job expansion.

export const dynamic = "force-dynamic"

export default async function QueuePage({
  searchParams,
}: {
  searchParams: Promise<{ state?: string }>
}) {
  const sp = await searchParams
  const state: JobStateFilter =
    sp.state && isJobStateFilter(sp.state) ? sp.state : "all"
  const snapshot = await fetchQueueSnapshot(state)
  return <QueueShell initialSnapshot={snapshot} />
}

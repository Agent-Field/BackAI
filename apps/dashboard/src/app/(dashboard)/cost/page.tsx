// SPDX-License-Identifier: Apache-2.0

import { CostShell } from "@/components/cost/cost-shell"
import { fetchCostSnapshot } from "@/lib/cost/data"
import { isCostRangeKind } from "@/lib/cost/range"
import type { CostRangeKind } from "@/lib/cost/types"

// Server-rendered first paint. The shell takes over for live polling +
// URL-driven range + per-tenant fan-out for Zone 3.

export const dynamic = "force-dynamic"

export default async function CostPage({
  searchParams,
}: {
  searchParams: Promise<{ range?: string }>
}) {
  const sp = await searchParams
  const range: CostRangeKind = sp.range && isCostRangeKind(sp.range) ? sp.range : "30d"
  const snapshot = await fetchCostSnapshot(range)
  return <CostShell initialSnapshot={snapshot} />
}

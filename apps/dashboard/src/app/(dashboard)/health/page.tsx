// SPDX-License-Identifier: Apache-2.0

import { HealthShell } from "@/components/health/health-shell"
import { fetchHealthSnapshot } from "@/lib/health/data"
import { isProviderWindowKind } from "@/lib/health/types"
import type { ProviderWindowKind } from "@/lib/health/types"

// Server-rendered first paint. Shell takes over for live polling +
// URL-driven provider window + refresh button.

export const dynamic = "force-dynamic"

export default async function HealthPage({
  searchParams,
}: {
  searchParams: Promise<{ window?: string }>
}) {
  const sp = await searchParams
  const window: ProviderWindowKind =
    sp.window && isProviderWindowKind(sp.window) ? sp.window : "24h"
  const snapshot = await fetchHealthSnapshot(window)
  return <HealthShell initialSnapshot={snapshot} />
}

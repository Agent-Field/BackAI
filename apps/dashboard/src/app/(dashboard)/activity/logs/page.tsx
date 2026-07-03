// SPDX-License-Identifier: Apache-2.0

import { LogsShell } from "@/components/logs-page/logs-shell"
import { fetchLogsSnapshot } from "@/lib/logs-page/data"
import type { LogsFilters } from "@/lib/logs-page/types"
import {
  DEFAULT_LOGS_FILTERS,
  isLogLevelFilter,
} from "@/lib/logs-page/types"

// Server-rendered first paint. Shell takes over for live polling +
// URL-driven level / service / search filters.

export const dynamic = "force-dynamic"

export default async function LogsPage({
  searchParams,
}: {
  searchParams: Promise<{ level?: string; service?: string; search?: string }>
}) {
  const sp = await searchParams
  const filters: LogsFilters = {
    level:
      sp.level && isLogLevelFilter(sp.level)
        ? sp.level
        : DEFAULT_LOGS_FILTERS.level,
    service: sp.service ?? "",
    search: sp.search ?? "",
  }
  const snapshot = await fetchLogsSnapshot(filters)
  return <LogsShell initialSnapshot={snapshot} />
}

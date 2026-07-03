// SPDX-License-Identifier: Apache-2.0

import { ErrorsShell } from "@/components/errors-page/errors-shell"
import { fetchErrorsSnapshot } from "@/lib/errors-page/data"
import {
  DEFAULT_ERROR_STATUS,
  isErrorStatusFilter,
} from "@/lib/errors-page/types"
import type { ErrorStatusFilter } from "@/lib/errors-page/types"

// Server-rendered first paint. Shell takes over for live polling +
// URL-driven status filter + capability-gated row actions.

export const dynamic = "force-dynamic"

export default async function ErrorsPage({
  searchParams,
}: {
  searchParams: Promise<{ status?: string }>
}) {
  const sp = await searchParams
  const status: ErrorStatusFilter =
    sp.status && isErrorStatusFilter(sp.status)
      ? sp.status
      : DEFAULT_ERROR_STATUS
  const snapshot = await fetchErrorsSnapshot(status)
  return <ErrorsShell initialSnapshot={snapshot} />
}

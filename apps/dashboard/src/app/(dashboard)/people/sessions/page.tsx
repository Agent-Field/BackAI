// SPDX-License-Identifier: Apache-2.0

import { SessionsShell } from "@/components/sessions-page/sessions-shell"
import { fetchSessionsSnapshot } from "@/lib/sessions-page/data"

// Server-rendered first paint. Shell takes over for URL-driven filters
// (?email / ?expired) and post-revoke refreshes.

export const dynamic = "force-dynamic"

export default async function SessionsPage({
  searchParams,
}: {
  searchParams: Promise<{ email?: string; expired?: string }>
}) {
  const sp = await searchParams
  const snapshot = await fetchSessionsSnapshot({
    email: sp.email ?? "",
    includeExpired: sp.expired === "1",
  })
  return <SessionsShell initialSnapshot={snapshot} />
}

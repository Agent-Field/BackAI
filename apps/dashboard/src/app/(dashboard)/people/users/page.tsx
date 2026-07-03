// SPDX-License-Identifier: Apache-2.0

import { UsersShell } from "@/components/users-page/users-shell"
import { fetchUsersSnapshot } from "@/lib/users-page/data"

// Server-rendered first paint. Shell takes over for URL-driven filters
// (?search / ?tenant) and the GDPR export / erase flows.

export const dynamic = "force-dynamic"

export default async function UsersPage({
  searchParams,
}: {
  searchParams: Promise<{ search?: string; tenant?: string }>
}) {
  const sp = await searchParams
  const snapshot = await fetchUsersSnapshot({
    search: sp.search ?? "",
    tenant: sp.tenant ?? "",
  })
  return <UsersShell initialSnapshot={snapshot} />
}

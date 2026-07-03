// SPDX-License-Identifier: Apache-2.0

import { KeysShell } from "@/components/keys-page/keys-shell"
import { fetchKeysSnapshot } from "@/lib/keys-page/data"

// Server-rendered first paint. Shell takes over for the URL-driven
// tenant filter (?tenant=), issue/rotate/revoke flows, and refreshes.

export const dynamic = "force-dynamic"

export default async function KeysPage({
  searchParams,
}: {
  searchParams: Promise<{ tenant?: string }>
}) {
  const sp = await searchParams
  const snapshot = await fetchKeysSnapshot(sp.tenant || undefined)
  return <KeysShell initialSnapshot={snapshot} />
}

// SPDX-License-Identifier: Apache-2.0

import { OAuthShell } from "@/components/oauth-page/oauth-shell"
import { fetchOAuthSnapshot } from "@/lib/oauth-page/data"

// People → OAuth. Operator surface for third-party OAuth: the registered
// providers (connect / disconnect), the live tenant connections, and the
// background token-refresh log. Server-rendered first paint; the shell
// takes over for the URL-driven ?provider filter, connect/disconnect
// flows, and a slow poll.

export const dynamic = "force-dynamic"

export default async function OAuthPage({
  searchParams,
}: {
  searchParams: Promise<{ provider?: string }>
}) {
  const sp = await searchParams
  const snapshot = await fetchOAuthSnapshot(sp.provider || undefined)
  return <OAuthShell initialSnapshot={snapshot} />
}

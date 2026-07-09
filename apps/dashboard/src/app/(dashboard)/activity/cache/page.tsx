// SPDX-License-Identifier: Apache-2.0

import { CacheShell } from "@/components/cache-page/cache-shell"
import { fetchCacheSnapshot } from "@/lib/cache-page/data"

// Server-rendered first paint. Shell takes over for live polling, the
// refresh button, and the cache-flush control.

export const dynamic = "force-dynamic"

export default async function CachePage() {
  const snapshot = await fetchCacheSnapshot()
  return <CacheShell initialSnapshot={snapshot} />
}

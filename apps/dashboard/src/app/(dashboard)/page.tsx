// SPDX-License-Identifier: Apache-2.0

import { HomeShell } from "@/components/home/home-shell"

import { getHomeSnapshot } from "@/lib/home/data"

// Home (route "/"). The (dashboard) layout owns the sidebar + top bar.
// This page is just the snapshot fetch + shell.

export const dynamic = "force-dynamic"

export default async function HomePage() {
  const snapshot = await getHomeSnapshot()
  return (
    <main className="flex flex-1 flex-col gap-section px-page-x py-page-y">
      <HomeShell snapshot={snapshot} />
    </main>
  )
}

// SPDX-License-Identifier: Apache-2.0

import { HomeShell } from "@/components/home/home-shell"

import { getHomeSnapshot } from "@/lib/home/data"

// Home (route "/"). The (dashboard) layout owns the sidebar + top bar.
// This page is just the snapshot fetch + shell.

export const dynamic = "force-dynamic"

export default async function HomePage() {
  const snapshot = await getHomeSnapshot()
  // Personal mode is read server-side (authoritative, no round-trip). It
  // gates the API-key / tenant onboarding affordances that don't apply when
  // auth is off. Client components can't read this env at runtime, so we
  // thread it down as a prop.
  const personal = process.env.AF_STACK_MODE === "personal"
  return (
    <main className="flex flex-1 flex-col gap-section px-page-x py-page-y">
      <HomeShell snapshot={snapshot} personal={personal} />
    </main>
  )
}

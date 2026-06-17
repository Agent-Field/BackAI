// SPDX-License-Identifier: Apache-2.0

// Home page composition.
//
// Pure render of a HomeSnapshot. No data fetching here — the parent
// (app/page.tsx, a server component) does that and passes the snapshot
// in. This stays presentational so it can be unit-tested without mocking
// the runtime.
//
// Layout order follows the home.md §"Suggested layout zones" preview:
//   Welcome block (stub) -> KPI strip -> Activity feed -> Quick actions ->
//   Backing services strip.

import { ActivityFeed } from "./activity-feed"
import { BackingServicesStrip } from "./backing-services-strip"
import { KpiStrip } from "./kpi-strip"
import { QuickActions } from "./quick-actions"
import { RuntimeUnreachable } from "./states/runtime-unreachable"
import { WelcomeBlock } from "./welcome-block"

import { deriveKpiTiles } from "@/lib/home/derive"
import type { HomeSnapshot } from "@/lib/home/types"

export function HomeShell({ snapshot }: { snapshot: HomeSnapshot }) {
  const tiles = deriveKpiTiles(snapshot)
  const events = snapshot.events?.events ?? []
  return (
    <div className="flex flex-col gap-section">
      {!snapshot.runtimeReachable ? (
        <RuntimeUnreachable details={firstErrorMessage(snapshot)} />
      ) : null}
      <WelcomeBlock />
      <KpiStrip tiles={tiles} />
      <ActivityFeed events={events} />
      <QuickActions />
      <BackingServicesStrip services={snapshot.services} />
    </div>
  )
}

function firstErrorMessage(snapshot: HomeSnapshot): string | undefined {
  return Object.values(snapshot.errors)[0]
}

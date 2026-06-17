// SPDX-License-Identifier: Apache-2.0

"use client"

import { useEffect, useState } from "react"

import { api } from "@/lib/api"
import { deriveKpiTiles } from "@/lib/home/derive"
import type { HomeSnapshot } from "@/lib/home/types"
import { polling } from "@/lib/theme"

import { ActivityFeed } from "./activity-feed"
import { BackingServicesStrip } from "./backing-services-strip"
import { KpiStrip } from "./kpi-strip"
import { QuickActions } from "./quick-actions"
import { RuntimeUnreachable } from "./states/runtime-unreachable"
import { WelcomeBlock } from "./welcome-block"

// Composition follows the page brief's logical zones but with the
// density the operator console demands:
//
//   Welcome block            (dismissible)
//   KPI strip (8 dense tiles)
//   ┌───────────────────────────┬──────────────────┐
//   │ Activity feed (fixed h)   │ Quick actions    │
//   │                           ├──────────────────┤
//   │                           │ Backing services │
//   └───────────────────────────┴──────────────────┘
//
// The activity feed's height is locked so navigation never shifts as the
// feed grows; the right column hosts the two secondary chrome strips.

export function HomeShell({ snapshot: initial }: { snapshot: HomeSnapshot }) {
  const [snapshot, setSnapshot] = useState(initial)

  useEffect(() => {
    let cancelled = false
    const tick = async () => {
      try {
        const [overview, events, services] = await Promise.allSettled([
          api.home(),
          api.admin.events.list({ limit: 20 }),
          api.admin.services.list(),
        ])
        if (cancelled) return
        setSnapshot((prev) => ({
          ...prev,
          overview:
            overview.status === "fulfilled" ? overview.value : prev.overview,
          events: events.status === "fulfilled" ? events.value : prev.events,
          services:
            services.status === "fulfilled" ? services.value : prev.services,
          fetchedAt: new Date().toISOString(),
          runtimeReachable:
            overview.status === "fulfilled" ||
            events.status === "fulfilled" ||
            services.status === "fulfilled" ||
            prev.runtimeReachable,
        }))
      } catch {
        // Silent — leave stale data up.
      }
    }
    const id = setInterval(tick, polling.home)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [])

  const tiles = deriveKpiTiles(snapshot)
  const events = snapshot.events?.events ?? []

  return (
    <div className="flex flex-col gap-section">
      {!snapshot.runtimeReachable ? (
        <RuntimeUnreachable details={firstErrorMessage(snapshot)} />
      ) : null}
      <WelcomeBlock />
      <KpiStrip tiles={tiles} />
      <div className="grid gap-section lg:grid-cols-3">
        <div className="lg:col-span-2">
          <ActivityFeed events={events} />
        </div>
        <div className="flex flex-col gap-section">
          <QuickActions />
          <BackingServicesStrip services={snapshot.services} />
        </div>
      </div>
    </div>
  )
}

function firstErrorMessage(snapshot: HomeSnapshot): string | undefined {
  return Object.values(snapshot.errors)[0]
}

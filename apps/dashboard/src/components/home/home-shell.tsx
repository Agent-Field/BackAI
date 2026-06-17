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

// Composition order follows the page brief's suggested zones, but with
// the visual density the critique demands: dense strips, hairline-
// divided cards, no padded-card-in-void.
//
// Live ticks: the shell polls /api/v1/home/overview and /admin/events
// every `polling.home` ms, updating the snapshot in place. KPI value
// changes pick up the `motion.tick` fade because the tile is the same
// component; React just re-renders with new numbers.

export function HomeShell({ snapshot: initial }: { snapshot: HomeSnapshot }) {
  const [snapshot, setSnapshot] = useState(initial)

  useEffect(() => {
    let cancelled = false
    const tick = async () => {
      try {
        const [overview, events] = await Promise.allSettled([
          api.home(),
          api.admin.events.list({ limit: 20 }),
        ])
        if (cancelled) return
        setSnapshot((prev) => ({
          ...prev,
          overview:
            overview.status === "fulfilled" ? overview.value : prev.overview,
          events: events.status === "fulfilled" ? events.value : prev.events,
          fetchedAt: new Date().toISOString(),
          runtimeReachable:
            overview.status === "fulfilled" ||
            events.status === "fulfilled" ||
            prev.runtimeReachable,
        }))
      } catch {
        // Polling errors are silent — they just leave stale data up.
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

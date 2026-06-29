// SPDX-License-Identifier: Apache-2.0

// Server-side composite snapshot for the Home page.
//
// Reads every endpoint Home depends on in parallel (Promise.allSettled so
// one failure can't take the whole page down) and returns a single typed
// HomeSnapshot. The page is a server component — this runs once per
// request before the shell hydrates.
//
// See development/ux/pages/home.md §7 for the per-source audit. All
// endpoints are server-implemented and validated by zod at the api.ts
// boundary; no fetch in this file talks to the network directly.

import { api } from "@/lib/api"
import type { HomeSnapshot, HomeSource } from "./types"

export async function getHomeSnapshot(): Promise<HomeSnapshot> {
  const [
    overviewR,
    costR,
    servicesR,
    eventsR,
    approvalsR,
    modulesR,
  ] = await Promise.allSettled([
    api.home(),
    api.cost(),
    api.admin.services.list(),
    api.admin.events.list({ limit: 20 }),
    api.approvals.list({ status: "pending", limit: 1 }),
    api.modulesState(),
  ])

  const errors: Partial<Record<HomeSource, string>> = {}
  const errMsg = (r: PromiseSettledResult<unknown>): string | undefined =>
    r.status === "rejected" ? errorMessage(r.reason) : undefined

  if (errMsg(overviewR)) errors.overview = errMsg(overviewR)
  if (errMsg(costR)) errors.cost = errMsg(costR)
  if (errMsg(servicesR)) errors.services = errMsg(servicesR)
  if (errMsg(eventsR)) errors.events = errMsg(eventsR)
  if (errMsg(approvalsR)) errors.approvals = errMsg(approvalsR)
  if (errMsg(modulesR)) errors.modules = errMsg(modulesR)

  return {
    overview: overviewR.status === "fulfilled" ? overviewR.value : null,
    costMTD: costR.status === "fulfilled" ? costR.value : null,
    services: servicesR.status === "fulfilled" ? servicesR.value : null,
    events: eventsR.status === "fulfilled" ? eventsR.value : null,
    pendingApprovalsCount:
      approvalsR.status === "fulfilled" ? approvalsR.value.total : 0,
    modules: modulesR.status === "fulfilled" ? modulesR.value : null,
    fetchedAt: new Date().toISOString(),
    runtimeReachable: anyFulfilled([
      overviewR,
      costR,
      servicesR,
      eventsR,
      approvalsR,
      modulesR,
    ]),
    errors,
  }
}

function anyFulfilled(results: PromiseSettledResult<unknown>[]): boolean {
  return results.some((r) => r.status === "fulfilled")
}

function errorMessage(e: unknown): string {
  if (e instanceof Error) return e.message
  return String(e)
}

// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"

import type { HealthSnapshot, ProviderWindowKind } from "./types"

// Server-side parallel fetch. Each source tolerates failure
// independently — the page mounts even if providers or db returned
// errors, and the client tick recovers the missing piece on the next
// round.

export async function fetchHealthSnapshot(
  providerWindow: ProviderWindowKind = "24h",
): Promise<HealthSnapshot> {
  const fetchedAt = new Date().toISOString()
  const [services, providers, db, metrics] = await Promise.allSettled([
    api.admin.services.list(),
    api.admin.llm.providerHealth({ window: providerWindow }),
    api.db.health(),
    api.metrics.summary(),
  ])

  return {
    services:
      services.status === "fulfilled" ? services.value.services : [],
    providers:
      providers.status === "fulfilled" ? providers.value.providers : [],
    providerWindow,
    db: db.status === "fulfilled" ? db.value : null,
    metrics: metrics.status === "fulfilled" ? metrics.value : null,
    fetchedAt,
    servicesHealthy: services.status === "fulfilled",
    providersHealthy: providers.status === "fulfilled",
    dbHealthy: db.status === "fulfilled",
    metricsHealthy: metrics.status === "fulfilled",
  }
}


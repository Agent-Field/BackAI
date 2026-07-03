// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"

import type { JobStateFilter, QueueSnapshot } from "./types"

// Server-side initial fetch for the Queue page. Three round trips
// (summary KPIs, jobs list, job definitions) fired in parallel; each
// degrades to null/empty independently so one flaky endpoint doesn't
// blank the whole page. healthy flips false only when everything
// failed — that's the "runtime down" signal.

export async function fetchQueueSnapshot(
  state: JobStateFilter = "all",
): Promise<QueueSnapshot> {
  const fetchedAt = new Date().toISOString()
  const [summary, jobs, definitions] = await Promise.all([
    api.queue().catch(() => null),
    api
      .jobs.list({ limit: 100, state: state === "all" ? undefined : state })
      .catch(() => null),
    api.jobs.definitions().catch(() => null),
  ])

  return {
    summary,
    jobs: jobs?.jobs ?? [],
    total: jobs?.total ?? 0,
    hasMore: jobs?.has_more ?? false,
    definitions: definitions?.definitions ?? [],
    fetchedAt,
    healthy: summary !== null || jobs !== null || definitions !== null,
  }
}

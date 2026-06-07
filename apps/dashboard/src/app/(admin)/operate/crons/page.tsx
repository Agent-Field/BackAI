// SPDX-License-Identifier: Apache-2.0

// Crons page — schedules that fire jobs on a cron expression.
//
// Server component: fetches the cron list on render. The CronsView
// client component owns the create dialog, the activation toggle, and
// the delete confirm.

import { PageHeader } from "@/components/layout/page-header"

import { api, type CronList, type JobDefinitionList } from "@/lib/api"

import { CronsView } from "./_components/crons-view"

export const dynamic = "force-dynamic"

export default async function Page() {
  // Two best-effort prefetches. The CronsView falls back to the empty
  // state if either errors, and the create dialog can still render with
  // an empty job-definitions list (the operator just sees fewer
  // pre-fills).
  let initial: CronList = { crons: [] }
  let definitions: JobDefinitionList = { definitions: [] }
  try {
    initial = await api.crons.list()
  } catch {
    initial = { crons: [] }
  }
  try {
    definitions = await api.jobs.definitions()
  } catch {
    definitions = { definitions: [] }
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Crons"
        description="Cron schedules over the background jobs queue. Each row fires its job at the configured cadence — the scheduler ticks once a minute and dispatches everything that's due."
      />
      <CronsView initial={initial} definitions={definitions} />
    </div>
  )
}
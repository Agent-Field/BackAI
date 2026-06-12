// Jobs page — registered background job definitions.
//
// Server component: loads job definitions from the runtime once per render
// (force-dynamic + no-store via lib/api.ts). When the definitions endpoint
// isn't live yet we render an empty-state shell rather than crashing.

import { api, ApiError, type JobDefinition } from "@/lib/api"
import { PageHeader } from "@/components/layout/page-header"

import { JobsView } from "./_components/jobs-view"

export const dynamic = "force-dynamic"

async function loadDefinitions(): Promise<{
  definitions: JobDefinition[]
  error: string | null
}> {
  try {
    const list = await api.jobs.definitions()
    return { definitions: list.definitions, error: null }
  } catch (e) {
    const message =
      e instanceof ApiError
        ? `${e.code}: ${e.message}`
        : e instanceof Error
          ? e.message
          : "Failed to load job definitions"
    return { definitions: [], error: message }
  }
}

export default async function Page() {
  const { definitions, error } = await loadDefinitions()

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Jobs"
        description="Background job definitions. Enqueue manually, or wire a cron schedule in code."
      />
      <JobsView definitions={definitions} loadError={error} />
    </div>
  )
}

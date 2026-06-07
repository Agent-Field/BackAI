// SPDX-License-Identifier: Apache-2.0

import { PageHeader } from "@/components/layout/page-header"
import { RunsView } from "./_components/runs-view"

export const dynamic = "force-dynamic"

export default function Page() {
  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Runs"
        description="Agent executions with filters, summary, and a deep-link to the runtime trace."
      />
      <RunsView />
    </div>
  )
}
// SPDX-License-Identifier: Apache-2.0

import { PageHeader } from "@/components/layout/page-header"
import { api, type ShipwrightTaskList } from "@/lib/api"

import { ShipwrightView } from "./_components/shipwright-view"

export const dynamic = "force-dynamic"

export default async function Page() {
  let initial: ShipwrightTaskList = { tasks: [], total: 0, has_more: false }
  try {
    initial = await api.shipwright.list({ limit: 25, offset: 0 })
  } catch {
    initial = { tasks: [], total: 0, has_more: false }
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Shipwright"
        description="Coding-agent tasks. AF Stack stores task and patch metadata; AgentField owns the live execution graph, logs, spans, traces, and memory."
      />
      <ShipwrightView initial={initial} />
    </div>
  )
}

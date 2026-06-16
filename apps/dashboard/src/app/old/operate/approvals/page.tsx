// SPDX-License-Identifier: Apache-2.0

import { PageHeader } from "@/components/layout/page-header"
import { api, type ApprovalList } from "@/lib/api"

import { ApprovalsView } from "./_components/approvals-view"

export const dynamic = "force-dynamic"

export default async function Page() {
  let initial: ApprovalList = { approvals: [], total: 0, has_more: false }
  try {
    initial = await api.approvals.list({ status: "pending", limit: 25, offset: 0 })
  } catch {
    initial = { approvals: [], total: 0, has_more: false }
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Approvals"
        description="Human decision gates for BackAI workflows. These rows are tenant-scoped business state, separate from AgentField run approvals."
      />
      <ApprovalsView initial={initial} />
    </div>
  )
}

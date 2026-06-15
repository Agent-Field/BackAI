// SPDX-License-Identifier: Apache-2.0

// Dashboard Plugins page — Build → Dashboard Plugins. Read-only list of the
// operator-console tabs discovered from apps/dashboard/plugins/ at build time.
// No runtime API call: DashboardPluginsList reads the build-time manifest and
// renders its own empty-state. Server component (force-dynamic for parity with
// the other Build pages).

import { PageHeader } from "@/components/layout/page-header"
import { DashboardPluginsList } from "@/components/dashboard-plugins-list"

export const dynamic = "force-dynamic"

export default function Page() {
  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Dashboard Plugins"
        description="Operator-console tabs discovered from apps/dashboard/plugins/ at build time."
      />
      <DashboardPluginsList />
    </div>
  )
}

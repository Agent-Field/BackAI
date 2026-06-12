// SPDX-License-Identifier: Apache-2.0

import { PageHeader } from "@/components/layout/page-header"
import { DashboardPluginsList } from "@/components/dashboard-plugins-list"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

export default function DashboardPluginsPage() {
  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Dashboard Plugins"
        description="Read-only list of operator-console tabs bundled into this dashboard build."
      />

      <Card>
        <CardHeader>
          <CardTitle>Discovered plugins</CardTitle>
          <CardDescription>
            Plugins are code in <code>apps/dashboard/plugins/</code>. They are scanned at
            dev/build time and ship with your fork.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <DashboardPluginsList />
        </CardContent>
      </Card>
    </div>
  )
}

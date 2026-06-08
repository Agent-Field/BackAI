// SPDX-License-Identifier: Apache-2.0

import { Sparkles } from "lucide-react"

import { PageHeader } from "@/components/layout/page-header"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

export default function StarterPluginPage() {
  return (
    <div className="space-y-6">
      <PageHeader
        title="Starter Metric"
        description="A copyable dashboard plugin for fork-specific operator views."
      />

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0">
          <div>
            <CardTitle>First actions</CardTitle>
            <CardDescription>Replace this with a real metric from your module.</CardDescription>
          </div>
          <Sparkles className="text-muted-foreground size-5" />
        </CardHeader>
        <CardContent>
          <div className="text-3xl font-semibold tracking-tight">0</div>
          <p className="text-muted-foreground mt-1 text-sm">
            Wire this card to `/workload/starter/events` or your own route.
          </p>
        </CardContent>
      </Card>
    </div>
  )
}

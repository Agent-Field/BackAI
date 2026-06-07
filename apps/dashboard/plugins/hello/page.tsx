// SPDX-License-Identifier: Apache-2.0

// Plugin page. Mounted by the generator at `/plugins/hello` via a route proxy
// under `src/app/(admin)/plugins/hello/page.tsx`. Because the proxy lives in
// the `(admin)` route group, this page gets the dashboard chrome (sidebar,
// topbar, auth gate) for free.

import { Sparkles } from "lucide-react"

import { PageHeader } from "@/components/layout/page-header"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"

export default function HelloPluginPage() {
  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Hello"
        description="Example plugin — proof that the loader works end-to-end"
      />
      <Card>
        <CardHeader>
          <div className="bg-primary/10 text-primary mb-2 flex aspect-square size-9 items-center justify-center rounded-md">
            <Sparkles className="size-4" />
          </div>
          <CardTitle>It works.</CardTitle>
          <CardDescription>
            This page lives at <code>apps/dashboard/plugins/hello/page.tsx</code>.
            Its sidebar entry was registered in <code>plugin.ts</code>; no shell
            code was modified to make it appear here.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <p className="text-muted-foreground text-sm">
            Drop a sibling folder under{" "}
            <code>apps/dashboard/plugins/</code> with its own{" "}
            <code>plugin.ts</code> and <code>page.tsx</code> to ship a new
            plugin. The next dev/build run picks it up automatically.
          </p>
        </CardContent>
      </Card>
    </div>
  )
}
// SPDX-License-Identifier: Apache-2.0

// Plugin index — server component. Lists every installed plugin discovered by
// `loadPlugins()`. Shows the standard `Empty` state when nothing's installed.

import Link from "next/link"
import { PackageOpen } from "lucide-react"

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { loadPlugins } from "@/lib/plugins"

const GROUP_LABEL: Record<string, string> = {
  build: "Build",
  operate: "Operate",
  customers: "Customers",
  system: "System",
}

export function PluginsList() {
  const plugins = loadPlugins()

  if (plugins.length === 0) {
    return (
      <Empty className="border-dashed">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <PackageOpen />
          </EmptyMedia>
          <EmptyTitle>No plugins installed yet</EmptyTitle>
          <EmptyDescription>
            Drop a folder under <code>apps/dashboard/plugins/</code> with a{" "}
            <code>plugin.ts</code> and <code>page.tsx</code> to extend the
            dashboard. See the README for the contract.
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className="grid gap-3 sm:grid-cols-2">
      {plugins.map((plugin) => {
        const Icon = plugin.icon
        return (
          <Card key={plugin.id}>
            <CardHeader>
              <div className="flex items-start gap-3">
                <div className="bg-muted text-foreground/80 flex aspect-square size-9 shrink-0 items-center justify-center rounded-md">
                  <Icon className="size-4" />
                </div>
                <div className="flex min-w-0 flex-col gap-0.5">
                  <CardTitle className="truncate">{plugin.label}</CardTitle>
                  <CardDescription className="truncate">
                    {plugin.description ?? "Installed plugin"}
                  </CardDescription>
                </div>
              </div>
            </CardHeader>
            <CardContent>
              <dl className="text-muted-foreground grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
                <dt>ID</dt>
                <dd className="text-foreground font-mono">{plugin.id}</dd>
                <dt>Group</dt>
                <dd className="text-foreground">
                  {GROUP_LABEL[plugin.group] ?? plugin.group}
                </dd>
                <dt>Route</dt>
                <dd className="text-foreground font-mono">
                  <Link
                    className="underline-offset-4 hover:underline"
                    href={plugin.href}
                  >
                    {plugin.href}
                  </Link>
                </dd>
              </dl>
            </CardContent>
          </Card>
        )
      })}
    </div>
  )
}
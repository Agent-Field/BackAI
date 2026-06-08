// SPDX-License-Identifier: Apache-2.0

import { Suspense } from "react"

import { Hammer } from "lucide-react"

import { ShipwrightView } from "./_components/shipwright-view"
import { Skeleton } from "@/components/ui/skeleton"

export default function ShipwrightPage() {
  return (
    <div className="container mx-auto max-w-6xl space-y-6 p-6">
      <header className="space-y-1">
        <div className="flex items-center gap-2">
          <Hammer className="size-5 text-muted-foreground" />
          <h1 className="text-2xl font-semibold tracking-tight">Shipwright</h1>
        </div>
        <p className="text-sm text-muted-foreground">
          Paste a GitHub issue and an autonomous agent will read your repo,
          plan the change, edit code, run tests, and open a pull request.
        </p>
      </header>

      <Suspense fallback={<Skeleton className="h-96 w-full" />}>
        <ShipwrightView />
      </Suspense>
    </div>
  )
}

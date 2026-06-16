// SPDX-License-Identifier: Apache-2.0

// Compact KPI tile used by the Queues page.
//
// Pure presentation, no chart. Same Card+CardHeader pattern as the Home
// hero tiles but without the sparkline overhead — these are sober numbers
// and we want them to read instantly.

import type { LucideIcon } from "lucide-react"

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"

type KpiTileProps = {
  label: string
  value: string | number
  description?: string
  icon: LucideIcon
  tone?: "default" | "warning" | "danger"
}

export function KpiTile({
  label,
  value,
  description,
  icon: Icon,
  tone = "default",
}: KpiTileProps) {
  const titleClass =
    tone === "danger"
      ? "text-destructive"
      : tone === "warning"
        ? "text-foreground"
        : "text-foreground"
  return (
    <Card>
      <CardHeader>
        <CardDescription className="flex items-center gap-2">
          <Icon className="size-4" />
          {label}
        </CardDescription>
        <CardTitle className={`text-3xl tabular-nums ${titleClass}`}>
          {value}
        </CardTitle>
      </CardHeader>
      {description ? (
        <CardContent className="text-muted-foreground text-xs">
          {description}
        </CardContent>
      ) : null}
    </Card>
  )
}
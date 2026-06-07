// SPDX-License-Identifier: Apache-2.0

// Alerts panel. Hidden entirely when `alerts[]` is empty.
//
// Severity → Alert variant mapping:
//   info       → default
//   warning    → default
//   critical   → destructive

import {
  AlertTriangle,
  CircleAlert,
  Info,
  type LucideIcon,
} from "lucide-react"

import {
  Alert,
  AlertDescription,
  AlertTitle,
} from "@/components/ui/alert"
import type { HomeOverview } from "@/lib/api"

type AlertItem = HomeOverview["alerts"][number]

const SEVERITY: Record<
  AlertItem["severity"],
  { variant: "default" | "destructive"; icon: LucideIcon }
> = {
  info: { variant: "default", icon: Info },
  warning: { variant: "default", icon: AlertTriangle },
  critical: { variant: "destructive", icon: CircleAlert },
}

type AlertsListProps = {
  alerts: AlertItem[]
}

export function AlertsList({ alerts }: AlertsListProps) {
  if (alerts.length === 0) return null

  return (
    <div className="flex flex-col gap-2">
      {alerts.map((a) => {
        const { variant, icon: Icon } = SEVERITY[a.severity]
        return (
          <Alert key={a.id} variant={variant}>
            <Icon />
            <AlertTitle>{a.title}</AlertTitle>
            {a.description ? (
              <AlertDescription>{a.description}</AlertDescription>
            ) : null}
          </Alert>
        )
      })}
    </div>
  )
}
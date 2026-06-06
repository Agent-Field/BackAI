import { Activity, AlertTriangle, Boxes, CircleDollarSign } from "lucide-react"

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { PageHeader } from "@/components/layout/page-header"

export const dynamic = "force-dynamic"

function Kpi({
  label,
  value,
  description,
  icon: Icon,
}: {
  label: string
  value: string
  description: string
  icon: typeof CircleDollarSign
}) {
  return (
    <Card>
      <CardHeader>
        <CardDescription className="flex items-center gap-2">
          <Icon className="size-4" />
          {label}
        </CardDescription>
        <CardTitle className="text-3xl tabular-nums">{value}</CardTitle>
      </CardHeader>
      <CardContent className="text-muted-foreground text-xs">
        {description}
      </CardContent>
    </Card>
  )
}

export default function HomePage() {
  // Phase 4.1 wires this to real data via lib/api.ts. The placeholder
  // values here render correctly with zero traffic and never crash on
  // missing data.
  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Home"
        description="What's happening right now across your stack."
      />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Kpi
          label="Requests / min"
          value="0"
          description="Sample agent ready — no traffic yet."
          icon={Activity}
        />
        <Kpi
          label="Error rate"
          value="0.0%"
          description="Last 60 minutes."
          icon={AlertTriangle}
        />
        <Kpi
          label="Cost today"
          value="$0.00"
          description="Resets at 00:00 UTC."
          icon={CircleDollarSign}
        />
        <Kpi
          label="Queue depth"
          value="0"
          description="No jobs queued."
          icon={Boxes}
        />
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Recent runs</CardTitle>
          <CardDescription>
            Once an agent runs, executions appear here in real time.
          </CardDescription>
        </CardHeader>
        <CardContent className="text-muted-foreground text-sm">
          Hit{" "}
          <code className="bg-muted rounded px-1 text-[0.85em]">
            POST /api/v1/agents/sample.echo
          </code>{" "}
          from the host machine to populate this list.
        </CardContent>
      </Card>
    </div>
  )
}

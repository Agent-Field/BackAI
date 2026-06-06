// Recent runs list. Server component — pure rendering of the `runs[]`
// slice from `home.recent_runs`.

import { formatDistanceToNowStrict } from "date-fns"
import { Workflow } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import {
  Card,
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import type { Run } from "@/lib/api"

const STATUS_VARIANT: Record<
  Run["status"],
  "default" | "secondary" | "destructive" | "outline"
> = {
  queued: "outline",
  running: "secondary",
  succeeded: "default",
  failed: "destructive",
  cancelled: "outline",
}

function fmtDuration(ms?: number): string {
  if (ms === undefined || ms === null) return "—"
  if (ms < 1000) return `${ms} ms`
  const s = ms / 1000
  if (s < 60) return `${s.toFixed(1)} s`
  const m = Math.floor(s / 60)
  const rem = Math.round(s % 60)
  return `${m}m ${rem}s`
}

function fmtCost(usd?: number): string {
  if (usd === undefined || usd === null) return "—"
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: usd >= 1 ? 2 : 4,
    maximumFractionDigits: 4,
  }).format(usd)
}

function fmtStarted(iso: string): string {
  try {
    return `${formatDistanceToNowStrict(new Date(iso))} ago`
  } catch {
    return iso
  }
}

type RunsTableProps = {
  runs: Run[]
}

export function RunsTable({ runs }: RunsTableProps) {
  return (
    <Card className="gap-0">
      <CardHeader className="border-b pb-4">
        <CardTitle>Recent runs</CardTitle>
        <CardDescription>
          Last 20 agent executions across all tenants.
        </CardDescription>
      </CardHeader>
      {runs.length === 0 ? (
        <Empty className="border-0">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Workflow />
            </EmptyMedia>
            <EmptyTitle>No runs yet</EmptyTitle>
            <EmptyDescription>
              Once an agent runs, executions appear here in real time.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="pl-4">Agent</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Tenant</TableHead>
              <TableHead>Started</TableHead>
              <TableHead className="text-right">Duration</TableHead>
              <TableHead className="pr-4 text-right">Cost</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {runs.map((run) => (
              <TableRow key={run.id}>
                <TableCell className="pl-4 font-medium">{run.agent}</TableCell>
                <TableCell>
                  <Badge variant={STATUS_VARIANT[run.status]}>
                    {run.status}
                  </Badge>
                </TableCell>
                <TableCell className="text-muted-foreground">
                  {run.tenant_id ?? "—"}
                </TableCell>
                <TableCell className="text-muted-foreground">
                  {fmtStarted(run.started_at)}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {fmtDuration(run.duration_ms)}
                </TableCell>
                <TableCell className="pr-4 text-right tabular-nums">
                  {fmtCost(run.cost_usd)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </Card>
  )
}

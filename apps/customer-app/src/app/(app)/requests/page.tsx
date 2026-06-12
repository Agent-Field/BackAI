// SPDX-License-Identifier: Apache-2.0

import Link from "next/link"
import { Clock3, MessageSquareText, Plus, ShieldCheck, type LucideIcon } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { buttonVariants } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { api, type CostEvent } from "@/lib/api"
import { requireCustomerContext } from "@/lib/session"

export const dynamic = "force-dynamic"

function formatTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

function supportLabel(event: CostEvent): string {
  if (event.agent?.includes("supportdesk")) return "Support request reviewed"
  return "Assistant response"
}

async function loadRequests(tenantId: string): Promise<CostEvent[]> {
  try {
    const recent = await api.costEvents({ tenant: tenantId, limit: 25 })
    return recent.events
  } catch {
    return []
  }
}

export default async function RequestsPage() {
  const { ctx } = await requireCustomerContext()
  const requests = await loadRequests(ctx.tenantId)

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Requests</h1>
          <p className="text-muted-foreground text-sm">
            Your recent support conversations and assistant answers.
          </p>
        </div>
        <Link href="/support" className={buttonVariants()}>
          <Plus data-icon="inline-start" />
          Start new request
        </Link>
      </div>

      <div className="grid gap-4 md:grid-cols-3">
        <SummaryCard
          icon={MessageSquareText}
          label="Total requests"
          value={String(requests.length)}
        />
        <SummaryCard icon={ShieldCheck} label="Prepared safely" value={String(requests.length)} />
        <SummaryCard
          icon={Clock3}
          label="Latest activity"
          value={requests[0] ? formatTime(requests[0].occurred_at) : "None yet"}
        />
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Request history</CardTitle>
          <CardDescription>
            These are the customer-visible records created after support chats.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {requests.length === 0 ? (
            <div className="flex flex-col items-center gap-3 py-10 text-center">
              <div className="bg-muted flex size-10 items-center justify-center rounded-md">
                <MessageSquareText className="text-muted-foreground size-5" />
              </div>
              <div>
                <p className="text-sm font-medium">No requests yet</p>
                <p className="text-muted-foreground text-sm">
                  Start a support chat and your requests will appear here.
                </p>
              </div>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Request</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Checks</TableHead>
                  <TableHead>Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {requests.map((request) => (
                  <TableRow key={request.id}>
                    <TableCell>
                      <div className="font-medium">{supportLabel(request)}</div>
                      <div className="text-muted-foreground text-xs">
                        {request.request_id
                          ? `Ref ${request.request_id.slice(0, 8)}`
                          : "Support chat"}
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge variant="secondary">Ready</Badge>
                    </TableCell>
                    <TableCell>
                      <span className="text-muted-foreground text-sm">
                        Route, policy, and risk checks
                      </span>
                    </TableCell>
                    <TableCell className="text-muted-foreground text-sm">
                      {formatTime(request.occurred_at)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function SummaryCard({
  icon: Icon,
  label,
  value,
}: {
  icon: LucideIcon
  label: string
  value: string
}) {
  return (
    <Card>
      <CardHeader>
        <CardDescription className="flex items-center gap-2">
          <Icon className="size-4" />
          {label}
        </CardDescription>
        <CardTitle className="text-xl font-semibold">{value}</CardTitle>
      </CardHeader>
    </Card>
  )
}

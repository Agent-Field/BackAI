// Webhooks config — list of configured inbound endpoints + link to the
// activity feed for outbound deliveries.

import { ArrowDownToLine, ArrowRight, ArrowUpFromLine, Webhook } from "lucide-react"
import Link from "next/link"

import { PageHeader } from "@/components/layout/page-header"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { api } from "@/lib/api"

import { formatRelative } from "../../operate/_lib/format"

export const dynamic = "force-dynamic"

export default async function Page() {
  let endpoints: Awaited<ReturnType<typeof api.webhooks.endpoints>> | null = null
  let deliveryCount = 0
  let error: string | null = null
  try {
    endpoints = await api.webhooks.endpoints()
    const deliveries = await api.webhooks.deliveries({ limit: 1 })
    deliveryCount = deliveries.total
  } catch (e) {
    error = e instanceof Error ? e.message : String(e)
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Webhooks"
        description="Inbound endpoint configuration. For delivery history (in + out), see Operate → Webhook Activity."
      />

      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-1.5">
              <ArrowDownToLine className="size-3" />
              Inbound endpoints
            </CardDescription>
            <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
              {endpoints?.endpoints.length ?? 0}
            </CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-1.5">
              <ArrowUpFromLine className="size-3" />
              All deliveries
            </CardDescription>
            <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
              {deliveryCount.toLocaleString()}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <Button
              variant="outline"
              size="sm"
              render={
                <Link href="/operate/webhook-activity">
                  View activity
                  <ArrowRight data-icon="inline-end" />
                </Link>
              }
            />
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-1.5">
              <Webhook className="size-3" />
              Public path
            </CardDescription>
            <CardTitle className="text-base font-mono tracking-tight">
              POST /webhooks/in/{"{slug}"}
            </CardTitle>
          </CardHeader>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Inbound endpoints</CardTitle>
          <CardDescription>
            Each row is an HMAC-verified entry point. Create via{" "}
            <code className="font-mono">POST /api/v1/webhooks/endpoints</code>{" "}
            or the CLI.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {error || !endpoints || endpoints.endpoints.length === 0 ? (
            <Empty className="border-0">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <Webhook />
                </EmptyMedia>
                <EmptyTitle>No endpoints configured</EmptyTitle>
                <EmptyDescription>
                  {error
                    ? `The runtime returned: ${error}`
                    : "Add one with af-stack webhooks add or POST /api/v1/webhooks/endpoints."}
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Slug</TableHead>
                  <TableHead>Provider</TableHead>
                  <TableHead>Forward to</TableHead>
                  <TableHead>HMAC</TableHead>
                  <TableHead>Active</TableHead>
                  <TableHead>Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {endpoints.endpoints.map((e) => (
                  <TableRow key={e.id}>
                    <TableCell>
                      <code className="font-mono text-xs">{e.slug}</code>
                    </TableCell>
                    <TableCell>
                      <Badge variant="secondary">{e.provider}</Badge>
                    </TableCell>
                    <TableCell>
                      <span
                        className="text-muted-foreground font-mono text-xs"
                        title={e.forward_to}
                      >
                        {e.forward_to.length > 40
                          ? e.forward_to.slice(0, 40) + "…"
                          : e.forward_to}
                      </span>
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant={e.signature_algorithm ? "default" : "outline"}
                      >
                        {e.signature_algorithm ?? "none"}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <Badge variant={e.is_active ? "default" : "outline"}>
                        {e.is_active ? "Yes" : "No"}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-muted-foreground text-xs">
                      {formatRelative(e.created_at)}
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

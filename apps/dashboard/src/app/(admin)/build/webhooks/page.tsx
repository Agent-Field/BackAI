// SPDX-License-Identifier: Apache-2.0

// Webhooks page — Build → Webhooks. Catalog of incoming and outgoing
// webhook endpoint definitions, backed by GET /api/v1/webhooks/endpoints.
// Server component: fetched once per render (force-dynamic + no-store
// via lib/api.ts). When the runtime isn't reachable we render a clean
// empty-state shell rather than crashing. Delivery history lives on a
// separate Operate tab.

import { Activity, Webhook } from "lucide-react"

import { api, ApiError, type WebhookEndpointList } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { PageHeader } from "@/components/layout/page-header"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

export const dynamic = "force-dynamic"

async function loadEndpoints(): Promise<{
  data: WebhookEndpointList
  error: string | null
}> {
  try {
    const data = await api.webhooks.endpoints()
    return { data, error: null }
  } catch (e) {
    const message =
      e instanceof ApiError
        ? `${e.code}: ${e.message}`
        : e instanceof Error
          ? e.message
          : "Failed to load webhook endpoints"
    return { data: { endpoints: [] }, error: message }
  }
}

export default async function Page() {
  const { data, error } = await loadEndpoints()
  const endpoints = data.endpoints

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Webhooks"
        description="Incoming and outgoing webhook endpoint definitions. Backed by /api/v1/webhooks/endpoints."
      />

      {error && endpoints.length === 0 ? (
        <div className="bg-card text-muted-foreground rounded-xl border border-dashed p-6 text-sm">
          <p className="text-foreground mb-1 font-medium">
            Webhook endpoints aren&apos;t available
          </p>
          <p>
            The runtime returned:{" "}
            <code className="bg-muted rounded px-1 font-mono text-xs">
              {error}
            </code>
            .
          </p>
        </div>
      ) : endpoints.length === 0 ? (
        <div className="text-muted-foreground bg-card flex items-center gap-2 rounded-xl border p-6 text-xs">
          <Webhook className="size-3.5" />
          No webhook endpoints defined.
        </div>
      ) : (
        <div className="bg-card rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Slug
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Provider
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Forward to
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Active
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {endpoints.map((endpoint) => (
                <TableRow key={endpoint.id} className="hover:bg-muted/30">
                  <TableCell>
                    <code className="font-mono text-xs">{endpoint.slug}</code>
                  </TableCell>
                  <TableCell>
                    <Badge variant="secondary" className="text-xs">
                      {endpoint.provider}
                    </Badge>
                  </TableCell>
                  <TableCell className="max-w-xs">
                    <code className="block truncate font-mono text-xs">
                      {endpoint.forward_to}
                    </code>
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={endpoint.is_active ? "secondary" : "outline"}
                      className="text-xs"
                    >
                      {endpoint.is_active ? "Active" : "Inactive"}
                    </Badge>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <div className="text-muted-foreground flex items-center gap-2 text-xs">
        <Activity className="size-3.5" />
        Recent deliveries live in Operate → Webhook Activity.
      </div>
    </div>
  )
}

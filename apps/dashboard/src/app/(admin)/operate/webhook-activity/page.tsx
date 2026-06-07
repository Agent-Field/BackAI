// Webhook activity — unified inbound + outbound delivery feed.
//
// Server component pre-fetches the first page of deliveries + the
// configured endpoint list so the table doesn't flash empty. Either
// piece can fail independently (the runtime might have endpoints CRUD
// wired but the delivery store not yet running, or vice versa), so we
// allSettled both and pass nullable props to the view.

import { Activity, ServerCrash } from "lucide-react"

import { PageHeader } from "@/components/layout/page-header"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import {
  api,
  type WebhookDeliveryList,
  type WebhookEndpointList,
} from "@/lib/api"

import { WebhookActivityView } from "./_components/webhook-activity-view"

export const dynamic = "force-dynamic"

type Snapshot = {
  deliveries: WebhookDeliveryList | null
  endpoints: WebhookEndpointList | null
  error: string | null
}

async function loadSnapshot(): Promise<Snapshot> {
  const [deliveriesResult, endpointsResult] = await Promise.allSettled([
    api.webhooks.deliveries({ limit: 50 }),
    api.webhooks.endpoints(),
  ])
  let error: string | null = null
  if (
    deliveriesResult.status === "rejected" &&
    endpointsResult.status === "rejected"
  ) {
    const r = deliveriesResult.reason
    error = r instanceof Error ? r.message : String(r)
  }
  return {
    deliveries:
      deliveriesResult.status === "fulfilled" ? deliveriesResult.value : null,
    endpoints:
      endpointsResult.status === "fulfilled" ? endpointsResult.value : null,
    error,
  }
}

export default async function Page() {
  const snap = await loadSnapshot()
  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Webhook Activity"
        description="Inbound and outbound webhook deliveries, with HMAC verification, dedup, and retry state."
      />
      {snap.error && !snap.deliveries && !snap.endpoints ? (
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ServerCrash />
            </EmptyMedia>
            <EmptyTitle>Webhooks unavailable</EmptyTitle>
            <EmptyDescription>
              The runtime returned:{" "}
              <code className="font-mono">{snap.error}</code>. Activity will
              appear here once the webhooks module is configured.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <WebhookActivityView
          initialDeliveries={snap.deliveries ?? { deliveries: [], total: 0, has_more: false }}
          initialEndpoints={snap.endpoints ?? { endpoints: [] }}
        />
      )}
    </div>
  )
}

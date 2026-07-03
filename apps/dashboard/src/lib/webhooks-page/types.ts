// SPDX-License-Identifier: Apache-2.0

import type {
  WebhookDelivery,
  WebhookDirection,
  WebhookEndpoint,
} from "@/lib/api"

export type WebhookDirectionFilter = WebhookDirection | "all"

export const DEFAULT_DIRECTION_FILTER: WebhookDirectionFilter = "all"

export interface WebhooksSnapshot {
  endpoints: WebhookEndpoint[]
  deliveries: WebhookDelivery[]
  total: number
  hasMore: boolean
  fetchedAt: string
  healthy: boolean
}

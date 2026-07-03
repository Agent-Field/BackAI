// SPDX-License-Identifier: Apache-2.0

import type { WebhookDelivery } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"

import type { WebhookDirectionFilter } from "./types"

// Status helpers ---------------------------------------------------------

export type DeliveryStatus = WebhookDelivery["status"]

// The wire enum is finer-grained than the operator story. Collapse to
// the three words the dashboard speaks: delivered / failed / pending.
// Dropped deliveries (dedup, bad signature) read as failed — the
// payload never reached a handler.
export function deliveryStatusLabel(status: DeliveryStatus): string {
  switch (status) {
    case "succeeded":
      return "delivered"
    case "failed":
      return "failed"
    case "dropped_duplicate":
      return "dropped (duplicate)"
    case "dropped_invalid_signature":
      return "dropped (bad signature)"
    case "queued":
    case "delivering":
      return "pending"
  }
}

export function classifyDeliveryStatus(status: DeliveryStatus): StatusState {
  switch (status) {
    case "succeeded":
      return "ok"
    case "failed":
    case "dropped_invalid_signature":
      return "act"
    case "dropped_duplicate":
      return "idle"
    case "queued":
    case "delivering":
      return "watch"
  }
}

export function isWebhookDirectionFilter(
  v: string,
): v is WebhookDirectionFilter {
  return v === "all" || v === "inbound" || v === "outbound"
}

// Retry only makes sense for terminal failures — the retry worker
// already owns queued/delivering rows.
export function isRetryableDelivery(delivery: WebhookDelivery): boolean {
  return delivery.status === "failed"
}

// The runtime serves inbound ingest at /webhooks/in/<slug>. Path only —
// the host depends on where the runtime is exposed, so operators paste
// it onto their public base URL.
export function ingestPath(slug: string): string {
  return `/webhooks/in/${slug}`
}

// Formatters -------------------------------------------------------------

export function formatDeliveryAge(iso: string | null): string {
  if (!iso) return "—"
  const ts = Date.parse(iso)
  if (Number.isNaN(ts)) return iso
  const diffMs = Math.max(0, Date.now() - ts)
  if (diffMs < 1000) return "now"
  const sec = Math.floor(diffMs / 1000)
  if (sec < 60) return `${sec}s ago`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  if (sec < 86_400) return `${Math.floor(sec / 3600)}h ago`
  return new Date(ts).toISOString().slice(5, 16).replace("T", " ")
}

export function formatResponse(delivery: WebhookDelivery): string {
  if (delivery.response_status === null) return "—"
  const ms =
    delivery.response_ms === null ? "" : ` · ${Math.round(delivery.response_ms)}ms`
  return `${delivery.response_status}${ms}`
}

export function shortDeliveryId(id: string): string {
  return id.length > 12 ? id.slice(0, 12) : id
}

// SPDX-License-Identifier: Apache-2.0

"use client"

import { ArrowUpRight, ChevronDown, ChevronRight, RotateCcw } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"

import { api } from "@/lib/api"
import type { WebhookDelivery } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"
import {
  classifyDeliveryStatus,
  deliveryStatusLabel,
  formatDeliveryAge,
  formatResponse,
  isRetryableDelivery,
  shortDeliveryId,
} from "@/lib/webhooks-page/derive"
import { cn } from "@/lib/utils"

// One row in the deliveries table. Status dot left, direction badge,
// cells across, chevron right. Clicking expands an inline detail panel
// with the body preview, response info, last error, and a Retry action
// for failed deliveries.

const DOT: Record<StatusState, string> = {
  ok: "bg-success",
  watch: "bg-warning",
  act: "bg-destructive",
  idle: "bg-muted-foreground/60",
}

interface DeliveryRowProps {
  delivery: WebhookDelivery
  expanded: boolean
  onToggle: (delivery: WebhookDelivery) => void
  onMutated: () => Promise<void> | void
}

export function DeliveryRow({
  delivery,
  expanded,
  onToggle,
  onMutated,
}: DeliveryRowProps) {
  const tone = classifyDeliveryStatus(delivery.status)
  const statusTone =
    tone === "ok"
      ? "text-muted-foreground"
      : tone === "watch"
        ? "text-warning font-medium"
        : tone === "act"
          ? "text-destructive font-medium"
          : "text-muted-foreground"
  const accent =
    tone === "act"
      ? "border-l-destructive"
      : tone === "watch"
        ? "border-l-warning"
        : "border-l-transparent"
  return (
    <div className="flex flex-col">
      <button
        type="button"
        onClick={() => onToggle(delivery)}
        aria-expanded={expanded}
        className={cn(
          "group grid w-full items-center gap-stack border-l-4 border-b px-row-x py-row-y text-left text-meta transition-colors",
          DELIVERY_ROW_COLUMNS,
          accent,
          expanded ? "bg-accent/30" : "hover:bg-accent/40",
        )}
      >
        <span
          aria-hidden
          className={`inline-block size-icon-dot rounded-pill ${DOT[tone]}`}
        />
        <span className="truncate font-mono tabular-nums text-muted-foreground">
          {formatDeliveryAge(delivery.created_at)}
        </span>
        <Badge variant={delivery.direction === "inbound" ? "secondary" : "outline"}>
          {delivery.direction}
        </Badge>
        <div className="flex min-w-0 flex-col gap-tile-tight">
          <span
            className="truncate font-mono text-body text-foreground"
            title={delivery.event_type}
          >
            {delivery.event_type}
          </span>
          {delivery.last_error ? (
            <span
              className="truncate text-meta text-destructive"
              title={delivery.last_error}
            >
              {delivery.last_error.length > 80
                ? `${delivery.last_error.slice(0, 78)}…`
                : delivery.last_error}
            </span>
          ) : (
            <span
              className="truncate font-mono text-meta text-muted-foreground"
              title={delivery.destination}
            >
              {delivery.destination}
            </span>
          )}
        </div>
        <span className="text-right font-mono tabular-nums text-foreground">
          {delivery.attempts}
        </span>
        <span className="truncate text-right font-mono tabular-nums text-muted-foreground">
          {formatResponse(delivery)}
        </span>
        <span className={`text-right ${statusTone}`}>
          {deliveryStatusLabel(delivery.status)}
        </span>
        {expanded ? (
          <ChevronDown aria-hidden className="size-3.5 text-muted-foreground" />
        ) : (
          <ChevronRight
            aria-hidden
            className="size-3.5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100"
          />
        )}
      </button>
      {expanded ? (
        <DeliveryDetail delivery={delivery} onMutated={onMutated} />
      ) : null}
    </div>
  )
}

// Inline detail panel — payload preview + response + last error +
// Retry. Sibling of the row button so interactive elements never nest.

function DeliveryDetail({
  delivery,
  onMutated,
}: {
  delivery: WebhookDelivery
  onMutated: () => Promise<void> | void
}) {
  const [retrying, setRetrying] = useState(false)

  const retry = async () => {
    if (retrying) return
    setRetrying(true)
    try {
      await api.webhooks.retry(delivery.id)
      toast.success("Delivery queued for retry", {
        description: `${delivery.event_type} · ${shortDeliveryId(delivery.id)}`,
      })
      await onMutated()
    } catch (err) {
      toast.error("Could not retry delivery", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setRetrying(false)
    }
  }

  return (
    <div className="flex flex-col gap-stack border-b bg-accent/10 px-row-x py-tile">
      <dl className="grid grid-cols-2 gap-stack text-meta md:grid-cols-4">
        <DetailMeta label="Delivery ID" value={delivery.id} mono />
        <DetailMeta label="Destination" value={delivery.destination} mono />
        <DetailMeta
          label="Scheduled"
          value={formatDeliveryAge(delivery.scheduled_at)}
        />
        <DetailMeta
          label="Delivered"
          value={formatDeliveryAge(delivery.delivered_at)}
        />
      </dl>
      <div className="flex flex-col gap-tile-tight">
        <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
          Payload preview
        </span>
        {delivery.body_preview ? (
          <pre className="max-h-48 overflow-auto rounded-md border bg-background px-row-x py-tile font-mono text-meta text-foreground">
            {delivery.body_preview}
          </pre>
        ) : (
          <p className="rounded-md border border-dashed border-muted-foreground/40 px-row-x py-tile text-meta text-muted-foreground">
            No body preview stored for this delivery.
          </p>
        )}
        {delivery.body_storage_url ? (
          <a
            href={delivery.body_storage_url}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-inline text-meta text-foreground underline hover:no-underline"
          >
            Full body in object storage
            <ArrowUpRight className="size-3" aria-hidden />
          </a>
        ) : null}
      </div>
      {delivery.last_error ? (
        <div className="rounded-md border border-l-4 border-l-destructive bg-destructive/5 px-row-x py-tile">
          <span className="text-eyebrow uppercase tracking-wide text-destructive">
            Last error
          </span>
          <pre className="mt-tile-tight max-h-48 overflow-auto whitespace-pre-wrap font-mono text-meta text-foreground">
            {delivery.last_error}
          </pre>
        </div>
      ) : null}
      {isRetryableDelivery(delivery) ? (
        <div className="flex justify-end">
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="gap-inline"
            disabled={retrying}
            onClick={retry}
          >
            <RotateCcw
              className={`size-3.5 ${retrying ? "animate-spin" : ""}`}
              aria-hidden
            />
            {retrying ? "Retrying…" : "Retry"}
          </Button>
        </div>
      ) : null}
    </div>
  )
}

function DetailMeta({
  label,
  value,
  mono,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div className="flex min-w-0 flex-col gap-tile-tight">
      <dt className="text-eyebrow uppercase tracking-wide text-muted-foreground">
        {label}
      </dt>
      <dd
        className={`truncate text-meta text-foreground ${mono ? "font-mono" : ""}`}
        title={value}
      >
        {value}
      </dd>
    </div>
  )
}

export const DELIVERY_ROW_COLUMNS =
  "grid-cols-[12px_72px_76px_minmax(0,1fr)_56px_88px_140px_18px]"

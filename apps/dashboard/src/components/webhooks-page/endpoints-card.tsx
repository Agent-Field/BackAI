// SPDX-License-Identifier: Apache-2.0

"use client"

import { Plus, Trash2 } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { CopyButton } from "@/components/ui/copy-button"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import { api } from "@/lib/api"
import type { WebhookEndpoint } from "@/lib/api"
import { ingestPath } from "@/lib/webhooks-page/derive"

import { EndpointCreateDialog } from "./endpoint-create-dialog"

// Endpoints zone — every inbound webhook endpoint the runtime knows
// about, with its ingest path (copyable), forward target, and active
// state. Create via dialog, delete with confirm.

interface EndpointsCardProps {
  endpoints: WebhookEndpoint[]
  healthy: boolean
  onMutated: () => Promise<void> | void
}

export function EndpointsCard({
  endpoints,
  healthy,
  onMutated,
}: EndpointsCardProps) {
  const [creating, setCreating] = useState(false)
  const [deleting, setDeleting] = useState<WebhookEndpoint | null>(null)

  return (
    <ZoneCard aria-labelledby="webhooks-endpoints">
      <ZoneCardHeader
        id="webhooks-endpoints"
        title="Endpoints"
        subtitle={healthy ? `${endpoints.length} configured` : "unavailable"}
        trailing={
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="h-7 gap-inline text-meta"
            onClick={() => setCreating(true)}
          >
            <Plus className="size-3.5" aria-hidden />
            New endpoint
          </Button>
        }
      />
      {!healthy ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          The runtime did not return webhook endpoints. Check the Health
          page, then retry.
        </p>
      ) : endpoints.length === 0 ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          No endpoints yet. Create one here, declare it in{" "}
          <code className="font-mono">gateway.yaml</code>, or use the
          SDK&apos;s <code className="font-mono">webhooks.endpoint()</code>.
        </p>
      ) : (
        <ul role="list" className="divide-y">
          {endpoints.map((ep) => (
            <li
              key={ep.id}
              className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto_auto] items-center gap-stack px-row-x py-row-y text-meta"
            >
              <div className="flex min-w-0 flex-col gap-tile-tight">
                <div className="flex min-w-0 items-center gap-inline">
                  <span className="truncate font-mono text-body text-foreground">
                    {ep.slug}
                  </span>
                  <Badge variant="outline">{ep.provider}</Badge>
                </div>
                <span className="flex min-w-0 items-center gap-inline font-mono text-meta text-muted-foreground">
                  <span className="truncate" title={ingestPath(ep.slug)}>
                    {ingestPath(ep.slug)}
                  </span>
                  <CopyButton
                    value={ingestPath(ep.slug)}
                    label="Copy ingest path"
                  />
                </span>
              </div>
              <div className="flex min-w-0 flex-col gap-tile-tight">
                <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
                  Forwards to
                </span>
                <span
                  className="truncate font-mono text-meta text-foreground"
                  title={ep.forward_to}
                >
                  {ep.forward_to}
                </span>
              </div>
              {ep.is_active ? (
                <Badge variant="secondary">active</Badge>
              ) : (
                <Badge variant="outline">inactive</Badge>
              )}
              <Button
                type="button"
                size="sm"
                variant="ghost"
                className="h-7 gap-inline text-meta text-muted-foreground hover:text-destructive"
                aria-label={`Delete endpoint ${ep.slug}`}
                onClick={() => setDeleting(ep)}
              >
                <Trash2 className="size-3.5" aria-hidden />
              </Button>
            </li>
          ))}
        </ul>
      )}
      <EndpointCreateDialog
        open={creating}
        onClose={() => setCreating(false)}
        onCreated={onMutated}
      />
      <DeleteConfirm
        endpoint={deleting}
        onClose={() => setDeleting(null)}
        onDeleted={onMutated}
      />
    </ZoneCard>
  )
}

function DeleteConfirm({
  endpoint,
  onClose,
  onDeleted,
}: {
  endpoint: WebhookEndpoint | null
  onClose: () => void
  onDeleted: () => Promise<void> | void
}) {
  const [submitting, setSubmitting] = useState(false)

  const onConfirm = async () => {
    if (!endpoint || submitting) return
    setSubmitting(true)
    try {
      await api.webhooks.deleteEndpoint(endpoint.id)
      toast.success("Endpoint deleted", { description: endpoint.slug })
      await onDeleted()
      onClose()
    } catch (err) {
      toast.error("Could not delete endpoint", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AlertDialog
      open={endpoint !== null}
      onOpenChange={(open) => !open && onClose()}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete endpoint?</AlertDialogTitle>
          <AlertDialogDescription>
            Providers posting to{" "}
            <code className="font-mono">
              {endpoint ? ingestPath(endpoint.slug) : ""}
            </code>{" "}
            will start receiving 404s. The delivery log is kept.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel size="sm" disabled={submitting}>
            Cancel
          </AlertDialogCancel>
          <AlertDialogAction
            size="sm"
            variant="destructive"
            disabled={submitting}
            onClick={onConfirm}
          >
            {submitting ? "Deleting…" : "Delete"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

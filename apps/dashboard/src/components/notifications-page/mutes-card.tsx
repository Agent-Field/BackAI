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
import { RelativeTime } from "@/components/ui/relative-time"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import { api } from "@/lib/api"
import type { NotificationMute } from "@/lib/api"
import { formatNotificationAge } from "@/lib/notifications-page/derive"

import { MuteCreateDialog } from "./mute-create-dialog"

// Mutes zone — the suppression patterns the worker consults before
// sending. Each row shows which dimensions it matches (blank = wildcard),
// its reason, and when it expires. Create via dialog, delete with
// confirm.

interface MutesCardProps {
  mutes: NotificationMute[]
  healthy: boolean
  onMutated: () => Promise<void> | void
}

export function MutesCard({ mutes, healthy, onMutated }: MutesCardProps) {
  const [creating, setCreating] = useState(false)
  const [deleting, setDeleting] = useState<NotificationMute | null>(null)

  return (
    <ZoneCard aria-labelledby="notifications-mutes">
      <ZoneCardHeader
        id="notifications-mutes"
        title="Mutes"
        subtitle={healthy ? `${mutes.length} active` : "unavailable"}
        trailing={
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="h-7 gap-inline text-meta"
            onClick={() => setCreating(true)}
          >
            <Plus className="size-3.5" aria-hidden />
            New mute
          </Button>
        }
      />
      {!healthy ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          The runtime did not return mutes. Check the Health page, then retry.
        </p>
      ) : mutes.length === 0 ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          No mutes. Matching notifications are suppressed and recorded as{" "}
          <code className="font-mono">skipped</code> rather than sent.
        </p>
      ) : (
        <ul role="list" className="divide-y">
          {mutes.map((mute) => (
            <li
              key={mute.id}
              className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto_auto] items-center gap-stack px-row-x py-row-y text-meta"
            >
              <div className="flex min-w-0 flex-wrap items-center gap-inline">
                <PatternChip label="kind" value={mute.pattern.kind} />
                <PatternChip label="to" value={mute.pattern.recipient} />
                <PatternChip label="template" value={mute.pattern.template} />
                <PatternChip label="category" value={mute.pattern.category} />
              </div>
              <span
                className="truncate text-meta text-muted-foreground"
                title={mute.reason ?? undefined}
              >
                {mute.reason ?? "—"}
              </span>
              <span className="whitespace-nowrap font-mono text-meta text-muted-foreground">
                {mute.expires_at ? (
                  <>
                    expires{" "}
                    <RelativeTime iso={mute.expires_at} format={formatNotificationAge} />
                  </>
                ) : (
                  "no expiry"
                )}
              </span>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                className="h-7 gap-inline text-meta text-muted-foreground hover:text-destructive"
                aria-label="Delete mute"
                onClick={() => setDeleting(mute)}
              >
                <Trash2 className="size-3.5" aria-hidden />
              </Button>
            </li>
          ))}
        </ul>
      )}
      <MuteCreateDialog open={creating} onClose={() => setCreating(false)} onCreated={onMutated} />
      <DeleteConfirm mute={deleting} onClose={() => setDeleting(null)} onDeleted={onMutated} />
    </ZoneCard>
  )
}

// A single dimension of a mute pattern. A blank matcher is a wildcard —
// render it as "any" so operators can read what the rule catches.
function PatternChip({ label, value }: { label: string; value: string }) {
  return (
    <span className="inline-flex items-center gap-inline">
      <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">{label}</span>
      {value.trim() ? (
        <Badge variant="outline" className="font-mono">
          {value}
        </Badge>
      ) : (
        <span className="font-mono text-meta text-muted-foreground/70">any</span>
      )}
    </span>
  )
}

function DeleteConfirm({
  mute,
  onClose,
  onDeleted,
}: {
  mute: NotificationMute | null
  onClose: () => void
  onDeleted: () => Promise<void> | void
}) {
  const [submitting, setSubmitting] = useState(false)

  const onConfirm = async () => {
    if (!mute || submitting) return
    setSubmitting(true)
    try {
      await api.notifications.mutes.delete(mute.id)
      toast.success("Mute deleted")
      await onDeleted()
      onClose()
    } catch (err) {
      toast.error("Could not delete mute", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AlertDialog open={mute !== null} onOpenChange={(open) => !open && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete mute?</AlertDialogTitle>
          <AlertDialogDescription>
            Notifications matching this pattern will start sending again.
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

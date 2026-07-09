// SPDX-License-Identifier: Apache-2.0

"use client"

import { useState } from "react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"

import { api } from "@/lib/api"

// Create-mute dialog. Persists via POST /api/v1/notifications/mutes
// (CreateNotificationMuteInput), surfaces success/error via Sonner.
// Parent owns the open state. Each pattern field is a free-form matcher:
// leave one blank to wildcard that dimension. At least one must be set so
// a mute can't silently swallow every notification.

interface MuteCreateDialogProps {
  open: boolean
  onClose: () => void
  onCreated: () => Promise<void> | void
}

export function MuteCreateDialog({ open, onClose, onCreated }: MuteCreateDialogProps) {
  const [kind, setKind] = useState("")
  const [recipient, setRecipient] = useState("")
  const [template, setTemplate] = useState("")
  const [category, setCategory] = useState("")
  const [reason, setReason] = useState("")
  const [expiresAt, setExpiresAt] = useState("")
  const [submitting, setSubmitting] = useState(false)

  const reset = () => {
    setKind("")
    setRecipient("")
    setTemplate("")
    setCategory("")
    setReason("")
    setExpiresAt("")
  }

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!kind.trim() && !recipient.trim() && !template.trim() && !category.trim()) {
      toast.error("Set at least one pattern field", {
        description: "A fully blank mute would swallow every notification.",
      })
      return
    }
    setSubmitting(true)
    try {
      await api.notifications.mutes.create({
        pattern: {
          kind: kind.trim(),
          recipient: recipient.trim(),
          template: template.trim(),
          category: category.trim(),
        },
        reason: reason.trim() ? reason.trim() : undefined,
        expires_at: expiresAt.trim() ? expiresAt.trim() : undefined,
      })
      toast.success("Mute created")
      reset()
      await onCreated()
      onClose()
    } catch (err) {
      toast.error("Could not create mute", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>New mute</DialogTitle>
          <DialogDescription>
            Matching notifications are recorded as <code className="font-mono">skipped</code>{" "}
            instead of being sent. Blank fields wildcard that dimension.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit} className="flex flex-col gap-stack px-4 pb-1">
          <div className="grid grid-cols-2 gap-stack">
            <Field label="Kind" hint="email / sms / push / log">
              <Input
                value={kind}
                onChange={(e) => setKind(e.target.value)}
                placeholder="email"
                className="font-mono"
              />
            </Field>
            <Field label="Category" hint="e.g. marketing, alerts">
              <Input
                value={category}
                onChange={(e) => setCategory(e.target.value)}
                placeholder="marketing"
                className="font-mono"
              />
            </Field>
          </div>
          <Field label="Recipient" hint="exact address to silence">
            <Input
              value={recipient}
              onChange={(e) => setRecipient(e.target.value)}
              placeholder="user@example.com"
              className="font-mono"
            />
          </Field>
          <Field label="Template" hint="template slug to silence">
            <Input
              value={template}
              onChange={(e) => setTemplate(e.target.value)}
              placeholder="budget-alert"
              className="font-mono"
            />
          </Field>
          <Field label="Reason" hint="optional — shown in the mute list">
            <Input
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder="Customer opted out"
            />
          </Field>
          <Field label="Expires at" hint="optional — ISO 8601, blank = never">
            <Input
              value={expiresAt}
              onChange={(e) => setExpiresAt(e.target.value)}
              placeholder="2026-12-31T00:00:00Z"
              className="font-mono"
            />
          </Field>
          <DialogFooter className="flex-row justify-end gap-inline px-0 pt-stack">
            <Button type="button" variant="ghost" size="sm" onClick={onClose} disabled={submitting}>
              Cancel
            </Button>
            <Button type="submit" size="sm" disabled={submitting}>
              {submitting ? "Creating…" : "Create mute"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function Field({
  label,
  hint,
  children,
}: {
  label: string
  hint?: string
  children: React.ReactNode
}) {
  return (
    <label className="flex flex-col gap-tile-tight">
      <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">{label}</span>
      {children}
      {hint ? <span className="text-meta text-muted-foreground">{hint}</span> : null}
    </label>
  )
}

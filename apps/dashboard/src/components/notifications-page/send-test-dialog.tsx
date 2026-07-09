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
import { FilterChip, FilterChipGroup } from "@/components/ui/filter-chip"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"

import { api } from "@/lib/api"
import type { NotificationKind } from "@/lib/api"
import { shortNotificationId } from "@/lib/notifications-page/derive"

// Send-test dialog. Enqueues a real notification via
// POST /api/v1/notifications so operators can prove the adapter is wired
// without leaving the console. Kind defaults to "log" — the dev-safe
// adapter that needs no provider credentials. The data field is optional
// free-form JSON handed to the template renderer.

const KIND_OPTIONS: { value: NotificationKind; label: string }[] = [
  { value: "log", label: "Log" },
  { value: "email", label: "Email" },
  { value: "sms", label: "SMS" },
  { value: "push", label: "Push" },
]

interface SendTestDialogProps {
  open: boolean
  onClose: () => void
  onSent: () => Promise<void> | void
}

export function SendTestDialog({ open, onClose, onSent }: SendTestDialogProps) {
  const [kind, setKind] = useState<NotificationKind>("log")
  const [template, setTemplate] = useState("test-notification")
  const [to, setTo] = useState("")
  const [subject, setSubject] = useState("")
  const [dataText, setDataText] = useState("")
  const [submitting, setSubmitting] = useState(false)

  const reset = () => {
    setKind("log")
    setTemplate("test-notification")
    setTo("")
    setSubject("")
    setDataText("")
  }

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!template.trim() || !to.trim()) {
      toast.error("Template and recipient are required")
      return
    }
    let data: Record<string, unknown> | undefined
    if (dataText.trim()) {
      try {
        const parsed: unknown = JSON.parse(dataText)
        if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
          throw new Error("Data must be a JSON object")
        }
        data = parsed as Record<string, unknown>
      } catch (err) {
        toast.error("Data must be valid JSON", {
          description: err instanceof Error ? err.message : String(err),
        })
        return
      }
    }
    setSubmitting(true)
    try {
      const notification = await api.notifications.send({
        kind,
        template: template.trim(),
        to: to.trim(),
        subject: subject.trim() ? subject.trim() : undefined,
        data,
      })
      toast.success("Notification enqueued", {
        description: `${template.trim()} · ${shortNotificationId(notification.id)}`,
      })
      reset()
      await onSent()
      onClose()
    } catch (err) {
      toast.error("Could not enqueue notification", {
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
          <DialogTitle>Send test notification</DialogTitle>
          <DialogDescription>
            Enqueues a row in the outbox for the background worker to drain through the configured
            adapter. Use <code className="font-mono">log</code> to prove wiring without a provider.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit} className="flex flex-col gap-stack px-4 pb-1">
          <Field label="Kind">
            <FilterChipGroup>
              {KIND_OPTIONS.map((opt) => (
                <FilterChip
                  key={opt.value}
                  label={opt.label}
                  active={kind === opt.value}
                  onSelect={() => setKind(opt.value)}
                />
              ))}
            </FilterChipGroup>
          </Field>
          <Field label="Template" hint="template slug the worker renders">
            <Input
              value={template}
              onChange={(e) => setTemplate(e.target.value)}
              placeholder="test-notification"
              className="font-mono"
              required
            />
          </Field>
          <Field label="Recipient" hint="email / phone / device token per kind">
            <Input
              value={to}
              onChange={(e) => setTo(e.target.value)}
              placeholder="operator@example.com"
              className="font-mono"
              required
            />
          </Field>
          <Field label="Subject" hint="optional — email subject line">
            <Input
              value={subject}
              onChange={(e) => setSubject(e.target.value)}
              placeholder="Test from the operator console"
            />
          </Field>
          <Field label="Data" hint="optional — JSON object for the template">
            <Textarea
              value={dataText}
              onChange={(e) => setDataText(e.target.value)}
              placeholder='{"name": "Ada"}'
              className="min-h-20 font-mono"
            />
          </Field>
          <DialogFooter className="flex-row justify-end gap-inline px-0 pt-stack">
            <Button type="button" variant="ghost" size="sm" onClick={onClose} disabled={submitting}>
              Cancel
            </Button>
            <Button type="submit" size="sm" disabled={submitting}>
              {submitting ? "Sending…" : "Send test"}
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

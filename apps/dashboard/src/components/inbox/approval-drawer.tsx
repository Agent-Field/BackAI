// SPDX-License-Identifier: Apache-2.0

"use client"

import { useEffect, useState } from "react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { Textarea } from "@/components/ui/textarea"
import type { InboxItem } from "@/lib/inbox/types"

// Approval detail drawer. Right-side sheet over the page; opens via URL
// state from the shell. The shell owns the decide mutation; this
// component is the form surface only.

type Decision = "approved" | "denied" | "cancelled"

interface ApprovalDrawerProps {
  item: (InboxItem & { kind: "approval" }) | null
  onClose: () => void
  onDecide: (itemId: string, decision: Decision, note?: string) => Promise<void>
}

export function ApprovalDrawer({
  item,
  onClose,
  onDecide,
}: ApprovalDrawerProps) {
  const [note, setNote] = useState("")
  const [pending, setPending] = useState<Decision | null>(null)

  // Reset state when the displayed approval changes — note from the
  // previous item must not leak into the next.
  useEffect(() => {
    setNote("")
    setPending(null)
  }, [item?.id])

  const decide = async (decision: Decision) => {
    if (!item || pending) return
    setPending(decision)
    try {
      await onDecide(item.id, decision, note.trim() || undefined)
    } finally {
      setPending(null)
    }
  }

  return (
    <Sheet
      open={item !== null}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <SheetContent side="right" className="w-full sm:max-w-md">
        {item ? (
          <>
            <SheetHeader>
              <SheetTitle>{item.title}</SheetTitle>
              <SheetDescription>{item.context}</SheetDescription>
            </SheetHeader>

            <div className="flex flex-1 flex-col gap-section overflow-y-auto px-4 pb-4">
              <ApprovalMeta item={item} />
              <ApprovalPayload payload={item.approval.payload} />
            </div>

            <div className="flex flex-col gap-stack border-t bg-card/40 p-4">
              <label
                htmlFor="approval-note"
                className="text-eyebrow uppercase tracking-wide text-muted-foreground"
              >
                Decision note (optional)
              </label>
              <Textarea
                id="approval-note"
                value={note}
                onChange={(e) => setNote(e.target.value)}
                rows={2}
                placeholder="Audit trail picks this up."
                className="text-meta"
              />
              <div className="flex flex-wrap items-center justify-end gap-inline">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => decide("cancelled")}
                  disabled={pending !== null}
                >
                  {pending === "cancelled" ? "Cancelling…" : "Cancel"}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => decide("denied")}
                  disabled={pending !== null}
                >
                  {pending === "denied" ? "Denying…" : "Deny"}
                </Button>
                <Button
                  type="button"
                  size="sm"
                  onClick={() => decide("approved")}
                  disabled={pending !== null}
                >
                  {pending === "approved" ? "Approving…" : "Approve"}
                </Button>
              </div>
            </div>
          </>
        ) : null}
      </SheetContent>
    </Sheet>
  )
}

function ApprovalMeta({ item }: { item: InboxItem & { kind: "approval" } }) {
  const a = item.approval
  return (
    <dl className="grid grid-cols-3 gap-stack text-meta">
      <MetaRow label="Kind" value={a.kind} />
      <MetaRow label="Tenant" value={a.tenant_id} mono />
      <MetaRow label="Requested by" value={a.requested_by ?? "—"} mono />
      <MetaRow label="Status" value={<Badge variant="outline">{a.status}</Badge>} />
      <MetaRow label="Created" value={formatAbsolute(a.created_at)} mono />
      <MetaRow label="Updated" value={formatAbsolute(a.updated_at)} mono />
    </dl>
  )
}

function MetaRow({
  label,
  value,
  mono,
}: {
  label: string
  value: React.ReactNode
  mono?: boolean
}) {
  return (
    <div className="flex flex-col gap-tile-tight">
      <dt className="text-eyebrow uppercase tracking-wide text-muted-foreground">
        {label}
      </dt>
      <dd
        className={`truncate text-meta text-foreground ${
          mono ? "font-mono" : ""
        }`}
        title={typeof value === "string" ? value : undefined}
      >
        {value}
      </dd>
    </div>
  )
}

function ApprovalPayload({ payload }: { payload: Record<string, unknown> }) {
  const json = JSON.stringify(payload, null, 2)
  return (
    <div className="flex flex-col gap-stack">
      <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
        Payload
      </span>
      <pre className="max-h-96 overflow-auto rounded-md border bg-background px-row-x py-row-y font-mono text-meta text-foreground">
        {json}
      </pre>
    </div>
  )
}

function formatAbsolute(iso: string): string {
  const ts = Date.parse(iso)
  if (Number.isNaN(ts)) return iso
  return new Date(ts).toISOString().replace("T", " ").slice(0, 19) + "Z"
}

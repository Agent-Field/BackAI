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
import type { User } from "@/lib/api"

// GDPR erase dialog. Double-confirm: the destructive button only arms
// once the operator types the user's email (same type-to-confirm
// convention as tenant delete in tenant-detail/settings-tab.tsx).
// Parent owns the open state via the `user` prop being non-null.

interface EraseUserDialogProps {
  user: User | null
  onClose: () => void
  onErased: () => Promise<void> | void
}

export function EraseUserDialog({ user, onClose, onErased }: EraseUserDialogProps) {
  const [confirm, setConfirm] = useState("")
  const [erasing, setErasing] = useState(false)

  const canErase = user !== null && confirm === user.email && !erasing

  const close = () => {
    setConfirm("")
    onClose()
  }

  const erase = async () => {
    if (!user || !canErase) return
    setErasing(true)
    try {
      const result = await api.admin.users.eraseData(user.id)
      const rows = Object.values(result.counts).reduce((a, b) => a + b, 0)
      toast.success("User data erased", {
        description: `${user.email}: ${rows} rows touched across ${Object.keys(result.counts).length} tables`,
      })
      await onErased()
      close()
    } catch (err) {
      toast.error("Could not erase user data", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setErasing(false)
    }
  }

  return (
    <Dialog open={user !== null} onOpenChange={(open) => !open && close()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Erase user data</DialogTitle>
          <DialogDescription>
            GDPR right-to-erasure. Personal data for{" "}
            <span className="font-medium text-foreground">{user?.email}</span>{" "}
            is redacted across the suite tables and the user is
            soft-deleted. This cannot be undone.
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-stack px-4 pb-1">
          <label className="flex flex-col gap-tile-tight">
            <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
              Type{" "}
              <span className="font-mono normal-case">{user?.email}</span> to
              confirm
            </span>
            <Input
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              placeholder={user?.email ?? ""}
              className="font-mono"
              autoComplete="off"
            />
          </label>
          <DialogFooter className="flex-row justify-end gap-inline px-0 pt-stack">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={close}
              disabled={erasing}
            >
              Cancel
            </Button>
            <Button
              type="button"
              variant="destructive"
              size="sm"
              disabled={!canErase}
              onClick={erase}
            >
              {erasing ? "Erasing…" : "Erase permanently"}
            </Button>
          </DialogFooter>
        </div>
      </DialogContent>
    </Dialog>
  )
}

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
import type { SecretMetadata } from "@/lib/api"

// Rotate dialog. A value-only form on a fixed key — the runtime's rotate
// endpoint swaps the stored value and clears the rotation deadline in one
// step, keeping the audit trail distinct from a plain edit. Parent owns
// open state via `secret`; onRotated fires after success so the shell can
// refresh the list.

interface RotateSecretDialogProps {
  secret: SecretMetadata | null
  onClose: () => void
  onRotated: () => Promise<void> | void
}

export function RotateSecretDialog({ secret, onClose, onRotated }: RotateSecretDialogProps) {
  return (
    <Dialog open={secret !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        {secret ? (
          <RotateSecretForm secret={secret} onClose={onClose} onRotated={onRotated} />
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

function RotateSecretForm({
  secret,
  onClose,
  onRotated,
}: {
  secret: SecretMetadata
  onClose: () => void
  onRotated: () => Promise<void> | void
}) {
  const [value, setValue] = useState("")
  const [submitting, setSubmitting] = useState(false)

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!value) {
      toast.error("A new value is required to rotate")
      return
    }
    setSubmitting(true)
    try {
      await api.secrets.rotate(secret.key, { value })
      toast.success("Secret rotated", { description: secret.key })
      await onRotated()
      onClose()
    } catch (err) {
      toast.error("Could not rotate secret", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>Rotate secret</DialogTitle>
        <DialogDescription>
          {`Swaps the stored value for “${secret.key}” and clears its rotation deadline.`}
        </DialogDescription>
      </DialogHeader>

      <form onSubmit={onSubmit} className="flex flex-col gap-stack px-4 pb-1">
        <label className="flex flex-col gap-tile-tight">
          <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
            New value
          </span>
          <Input
            type="password"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            required
            autoComplete="off"
            autoFocus
            placeholder="The replacement value"
            className="font-mono"
          />
          <span className="text-meta text-muted-foreground">
            Consumers reading `secret:{secret.key}` pick up the new value immediately.
          </span>
        </label>
        <DialogFooter className="flex-row justify-end gap-inline px-0 pt-stack">
          <Button type="button" variant="ghost" size="sm" onClick={onClose} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" size="sm" disabled={submitting}>
            {submitting ? "Rotating…" : "Rotate secret"}
          </Button>
        </DialogFooter>
      </form>
    </>
  )
}

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
import { toDatetimeLocal, toRotateAfterISO, validateSecretKey } from "@/lib/secrets-page/derive"

// Set-secret dialog. One form for two modes:
//   create — key is editable; the value is required.
//   edit   — key is locked (PUT is upsert-by-key); saving replaces the
//            stored value and metadata. Editing always re-supplies the
//            value because the runtime never echoes it back.
// The inner form is keyed by the target so it remounts (and re-seeds its
// state from props) whenever the shell switches create ↔ edit, keeping the
// state initialisation in the render path rather than an effect. Parent
// owns open state; onSaved fires after a successful PUT.

interface SetSecretDialogProps {
  open: boolean
  /** null → create a new secret; otherwise edit this one (key locked). */
  secret: SecretMetadata | null
  onClose: () => void
  onSaved: () => Promise<void> | void
}

export function SetSecretDialog({ open, secret, onClose, onSaved }: SetSecretDialogProps) {
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        <SetSecretForm
          key={secret?.key ?? "__new__"}
          secret={secret}
          onClose={onClose}
          onSaved={onSaved}
        />
      </DialogContent>
    </Dialog>
  )
}

function SetSecretForm({
  secret,
  onClose,
  onSaved,
}: {
  secret: SecretMetadata | null
  onClose: () => void
  onSaved: () => Promise<void> | void
}) {
  const editing = secret !== null

  const [key, setKey] = useState(secret?.key ?? "")
  const [value, setValue] = useState("")
  const [description, setDescription] = useState(secret?.description ?? "")
  const [rotateAfter, setRotateAfter] = useState(toDatetimeLocal(secret?.rotate_after ?? null))
  const [submitting, setSubmitting] = useState(false)

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    const keyErr = validateSecretKey(key)
    if (keyErr) {
      toast.error(keyErr)
      return
    }
    if (!value) {
      toast.error("Value is required")
      return
    }
    setSubmitting(true)
    try {
      await api.secrets.put(key.trim(), {
        value,
        description: description.trim() || undefined,
        rotate_after: toRotateAfterISO(rotateAfter),
      })
      toast.success(editing ? "Secret updated" : "Secret created", {
        description: key.trim(),
      })
      await onSaved()
      onClose()
    } catch (err) {
      toast.error(editing ? "Could not update secret" : "Could not create secret", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>{editing ? "Edit secret" : "New secret"}</DialogTitle>
        <DialogDescription>
          {editing
            ? "Replaces the stored value in the vault. The plaintext is never echoed back, so re-enter it to save."
            : "Stores a value in the vault. Reference it from agents and modules as `secret:<key>` — the plaintext stays server-side."}
        </DialogDescription>
      </DialogHeader>

      <form onSubmit={onSubmit} className="flex flex-col gap-stack px-4 pb-1">
        <Field
          label="Key"
          hint={editing ? undefined : "Letters, digits, and _ . - / — e.g. openai/api-key"}
        >
          <Input
            value={key}
            onChange={(e) => setKey(e.target.value)}
            readOnly={editing}
            required
            placeholder="e.g. stripe/secret-key"
            className="font-mono disabled:opacity-100 read-only:text-muted-foreground"
          />
        </Field>
        <Field
          label="Value"
          hint="Sensitive. Stored encrypted; only revealed via an explicit action."
        >
          <Input
            type="password"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            required
            autoComplete="off"
            placeholder={editing ? "Enter a new value" : "The secret value"}
            className="font-mono"
          />
        </Field>
        <Field label="Description" hint="Optional. Shown in the secrets list.">
          <Input
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="e.g. Live Stripe key for billing"
          />
        </Field>
        <Field
          label="Rotate after"
          hint="Optional deadline. Once it passes the list flags the secret as overdue for rotation."
        >
          <Input
            type="datetime-local"
            value={rotateAfter}
            onChange={(e) => setRotateAfter(e.target.value)}
            className="tabular-nums"
          />
        </Field>
        <DialogFooter className="flex-row justify-end gap-inline px-0 pt-stack">
          <Button type="button" variant="ghost" size="sm" onClick={onClose} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" size="sm" disabled={submitting}>
            {submitting
              ? editing
                ? "Saving…"
                : "Creating…"
              : editing
                ? "Save secret"
                : "Create secret"}
          </Button>
        </DialogFooter>
      </form>
    </>
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

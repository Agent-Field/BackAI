// SPDX-License-Identifier: Apache-2.0

"use client"

import { Eye, Pencil, RotateCw, Trash2 } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"

import { api } from "@/lib/api"
import type { SecretMetadata, SecretValue } from "@/lib/api"
import { rotationStatus } from "@/lib/secrets-page/derive"
import { formatAge } from "@/lib/tenant-detail/derive"

// Secrets table — sticky header + one row per secret. Reveal fetches the
// plaintext and hands it to the shell's reveal dialog; Rotate / Edit open
// shell-owned dialogs; Delete uses a two-step inline confirm. Degraded /
// empty states keep the column shell visible, mirroring the keys table.

const SECRET_ROW_COLUMNS =
  "grid-cols-[minmax(0,1.4fr)_minmax(0,1.2fr)_130px_110px_110px_minmax(220px,auto)]"

interface SecretsTableProps {
  secrets: SecretMetadata[]
  healthy: boolean
  onRevealed: (value: SecretValue) => void
  onEdit: (secret: SecretMetadata) => void
  onRotate: (secret: SecretMetadata) => void
  onMutated: () => Promise<void> | void
}

export function SecretsTable({
  secrets,
  healthy,
  onRevealed,
  onEdit,
  onRotate,
  onMutated,
}: SecretsTableProps) {
  return (
    <section aria-label="Secrets table" className="flex min-h-0 flex-col rounded-md border bg-card">
      <header
        className={`sticky top-0 z-10 grid items-center gap-stack border-b bg-card px-row-x py-tile-tight text-eyebrow uppercase tracking-wide text-muted-foreground ${SECRET_ROW_COLUMNS}`}
      >
        <span>Key</span>
        <span>Description</span>
        <span>Rotation</span>
        <span className="text-right">Updated</span>
        <span className="text-right">Created</span>
        <span className="text-right tabular-nums normal-case">{secrets.length}</span>
      </header>
      {!healthy ? (
        <DegradedRow />
      ) : secrets.length === 0 ? (
        <EmptyRow />
      ) : (
        <ul role="list" className="flex flex-col divide-y">
          {secrets.map((secret) => (
            <SecretRow
              key={secret.key}
              secret={secret}
              onRevealed={onRevealed}
              onEdit={onEdit}
              onRotate={onRotate}
              onMutated={onMutated}
            />
          ))}
        </ul>
      )}
    </section>
  )
}

function SecretRow({
  secret,
  onRevealed,
  onEdit,
  onRotate,
  onMutated,
}: {
  secret: SecretMetadata
  onRevealed: (value: SecretValue) => void
  onEdit: (secret: SecretMetadata) => void
  onRotate: (secret: SecretMetadata) => void
  onMutated: () => Promise<void> | void
}) {
  const [phase, setPhase] = useState<"idle" | "revealing" | "confirm-delete" | "deleting">("idle")
  const rotation = rotationStatus(secret.rotate_after)

  const reveal = async () => {
    if (phase !== "idle") return
    setPhase("revealing")
    try {
      const value = await api.secrets.reveal(secret.key)
      onRevealed(value)
    } catch (err) {
      toast.error("Could not reveal secret", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setPhase("idle")
    }
  }

  const remove = async () => {
    if (phase === "deleting") return
    setPhase("deleting")
    try {
      await api.secrets.delete(secret.key)
      toast.success("Secret deleted", { description: secret.key })
      await onMutated()
    } catch (err) {
      toast.error("Could not delete secret", {
        description: err instanceof Error ? err.message : String(err),
      })
      setPhase("idle")
    }
  }

  return (
    <li className={`grid items-center gap-stack px-row-x py-row-y text-meta ${SECRET_ROW_COLUMNS}`}>
      <span className="min-w-0 truncate font-mono text-body font-medium text-foreground">
        {secret.key}
      </span>
      <span className="min-w-0 truncate text-muted-foreground">
        {secret.description ? (
          secret.description
        ) : (
          <span className="text-muted-foreground/60">—</span>
        )}
      </span>
      <span className="flex min-w-0 items-center gap-inline">
        {rotation.overdue ? (
          <Badge variant="destructive">{rotation.label}</Badge>
        ) : (
          <span className="truncate text-muted-foreground" title={secret.rotate_after ?? undefined}>
            {rotation.label}
          </span>
        )}
      </span>
      <span
        className="text-right font-mono tabular-nums text-muted-foreground"
        title={secret.updated_at}
      >
        {formatAge(secret.updated_at)}
      </span>
      <span
        className="text-right font-mono tabular-nums text-muted-foreground"
        title={secret.created_at}
      >
        {formatAge(secret.created_at)}
      </span>
      <div className="flex items-center justify-end gap-inline">
        {phase === "confirm-delete" ? (
          <>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="h-7 text-meta"
              onClick={() => setPhase("idle")}
            >
              Cancel
            </Button>
            <Button
              type="button"
              size="sm"
              variant="destructive"
              className="h-7 text-meta"
              onClick={remove}
            >
              Confirm delete
            </Button>
          </>
        ) : (
          <>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="h-7 gap-inline text-meta"
              disabled={phase !== "idle"}
              onClick={reveal}
            >
              <Eye className="size-3" aria-hidden />
              {phase === "revealing" ? "Revealing…" : "Reveal"}
            </Button>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="h-7 gap-inline text-meta"
              disabled={phase !== "idle"}
              onClick={() => onRotate(secret)}
            >
              <RotateCw className="size-3" aria-hidden />
              Rotate
            </Button>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="h-7 gap-inline text-meta"
              disabled={phase !== "idle"}
              onClick={() => onEdit(secret)}
            >
              <Pencil className="size-3" aria-hidden />
              Edit
            </Button>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="h-7 gap-inline text-meta text-destructive hover:text-destructive"
              disabled={phase !== "idle"}
              onClick={() => setPhase("confirm-delete")}
            >
              <Trash2 className="size-3" aria-hidden />
              {phase === "deleting" ? "Deleting…" : "Delete"}
            </Button>
          </>
        )}
      </div>
    </li>
  )
}

function EmptyRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">No secrets stored yet.</p>
      <p className="max-w-md text-meta text-muted-foreground">
        Add one with the button above, then reference it from agents and modules as
        `secret:&lt;key&gt;` — the plaintext never leaves the vault.
      </p>
    </div>
  )
}

function DegradedRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">Secrets vault unavailable.</p>
      <p className="max-w-md text-meta text-muted-foreground">
        The runtime did not return a secrets list — the vault may not be configured (no database or
        KEK). Check the Health page, then retry.
      </p>
    </div>
  )
}

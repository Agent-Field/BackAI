// SPDX-License-Identifier: Apache-2.0

"use client"

import { Pencil, Plus, Trash2, X } from "lucide-react"
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
import { Switch } from "@/components/ui/switch"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import { api } from "@/lib/api"
import type { NotificationChannel, NotificationKind } from "@/lib/api"

// Channels zone — the delivery channels the runtime routes through (which
// adapter handles each kind, and where its config comes from). Operators
// manage db-backed channels here: create one, edit its config/enabled
// state, or delete it. Env-provisioned channels (source="env") show as
// read-only rows — they come from process config, not the database, so
// there's no db row to edit or remove; create a db override to change one.

const KIND_OPTIONS: { value: NotificationKind; label: string }[] = [
  { value: "email", label: "Email" },
  { value: "sms", label: "SMS" },
  { value: "push", label: "Push" },
  { value: "log", label: "Log" },
]

interface ChannelsCardProps {
  channels: NotificationChannel[]
  healthy: boolean
  onChanged: () => Promise<void> | void
}

// "new" opens the create dialog; a channel opens the edit dialog; null closes.
type EditTarget = NotificationChannel | "new" | null

export function ChannelsCard({ channels, healthy, onChanged }: ChannelsCardProps) {
  const [editing, setEditing] = useState<EditTarget>(null)
  const [deleting, setDeleting] = useState<NotificationChannel | null>(null)

  const dbKinds = new Set(channels.filter((ch) => ch.source === "db").map((ch) => ch.kind))

  return (
    <ZoneCard aria-labelledby="notifications-channels">
      <ZoneCardHeader
        id="notifications-channels"
        title="Channels"
        subtitle={healthy ? `${channels.length} configured` : "unavailable"}
        trailing={
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="h-7 gap-inline text-meta"
            onClick={() => setEditing("new")}
            disabled={!healthy}
          >
            <Plus className="size-3.5" aria-hidden />
            New channel
          </Button>
        }
      />
      {!healthy ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          The runtime did not return channels. Check the Health page, then retry.
        </p>
      ) : channels.length === 0 ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          No channels configured. Create one here, or set the default adapter via the{" "}
          <code className="font-mono">NOTIFICATIONS_ADAPTER</code> env var (defaults to{" "}
          <code className="font-mono">log</code> in dev).
        </p>
      ) : (
        <ul role="list" className="divide-y">
          {channels.map((ch) => {
            const editable = ch.source === "db"
            return (
              <li
                key={ch.id}
                className="grid grid-cols-[auto_minmax(0,1fr)_auto_auto_auto] items-center gap-stack px-row-x py-row-y text-meta"
              >
                <Badge variant="secondary">{ch.kind}</Badge>
                <span
                  className="truncate font-mono text-meta text-muted-foreground"
                  title={summariseConfig(ch.config_json)}
                >
                  {summariseConfig(ch.config_json)}
                </span>
                <Badge variant="outline">{ch.source}</Badge>
                {ch.enabled ? (
                  <Badge variant="secondary">enabled</Badge>
                ) : (
                  <Badge variant="outline">disabled</Badge>
                )}
                <div className="flex items-center gap-inline">
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    className="h-7 gap-inline text-meta text-muted-foreground"
                    aria-label={editable ? "Edit channel" : "Create db override"}
                    onClick={() => setEditing(editable ? ch : "new")}
                  >
                    <Pencil className="size-3.5" aria-hidden />
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    className="h-7 gap-inline text-meta text-muted-foreground hover:text-destructive disabled:opacity-40"
                    aria-label="Delete channel"
                    disabled={!editable}
                    title={
                      editable ? undefined : "Env channels can't be deleted — override them instead"
                    }
                    onClick={() => setDeleting(ch)}
                  >
                    <Trash2 className="size-3.5" aria-hidden />
                  </Button>
                </div>
              </li>
            )
          })}
        </ul>
      )}
      {/* key remounts the dialog with fresh initial state per target, so
          prefill happens in the state initializer — no set-state-in-effect. */}
      <ChannelDialog
        key={editing === "new" ? "new" : editing === null ? "closed" : editing.kind}
        open={editing !== null}
        channel={editing === "new" || editing === null ? null : editing}
        takenDbKinds={dbKinds}
        onClose={() => setEditing(null)}
        onSaved={onChanged}
      />
      <DeleteConfirm channel={deleting} onClose={() => setDeleting(null)} onDeleted={onChanged} />
    </ZoneCard>
  )
}

// The config blob is adapter-specific and may hold secrets-by-reference.
// Render the keys only — never the values — so a copy/paste of the row
// can't leak a token into a screenshot.
function summariseConfig(config: Record<string, unknown>): string {
  const keys = Object.keys(config)
  if (keys.length === 0) return "no config"
  return keys.join(", ")
}

// One editable config entry. `stored` marks a key that already had a value
// in the database; its value input starts blank (never rendering the secret)
// and, if left blank, the stored value is preserved on save.
interface ConfigRow {
  key: string
  value: string
  stored: boolean
}

function ChannelDialog({
  open,
  channel,
  takenDbKinds,
  onClose,
  onSaved,
}: {
  open: boolean
  channel: NotificationChannel | null
  takenDbKinds: Set<NotificationKind>
  onClose: () => void
  onSaved: () => Promise<void> | void
}) {
  const isEdit = channel !== null
  // Stored values are kept in a closure constant (never in rendered inputs)
  // so unchanged secrets survive the full-replace upsert without display.
  const storedValues: Record<string, unknown> = channel?.config_json ?? {}

  const [kind, setKind] = useState<NotificationKind>(channel?.kind ?? "email")
  const [enabled, setEnabled] = useState<boolean>(channel?.enabled ?? true)
  const [rows, setRows] = useState<ConfigRow[]>(
    channel
      ? Object.keys(channel.config_json).map((k) => ({ key: k, value: "", stored: true }))
      : [],
  )
  const [submitting, setSubmitting] = useState(false)

  const updateRow = (idx: number, patch: Partial<ConfigRow>) => {
    setRows((prev) => prev.map((r, i) => (i === idx ? { ...r, ...patch } : r)))
  }
  const addRow = () => setRows((prev) => [...prev, { key: "", value: "", stored: false }])
  const removeRow = (idx: number) => setRows((prev) => prev.filter((_, i) => i !== idx))

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (submitting) return

    // Assemble config_json. Keys are trimmed and de-duplicated (last wins).
    // A blank value on a stored key keeps the existing (unshown) secret; a
    // blank value on a fresh key is sent as an empty string.
    const config: Record<string, unknown> = {}
    for (const row of rows) {
      const k = row.key.trim()
      if (!k) continue
      if (row.value.trim()) {
        config[k] = row.value.trim()
      } else if (row.stored && k in storedValues) {
        config[k] = storedValues[k]
      } else {
        config[k] = ""
      }
    }

    setSubmitting(true)
    try {
      await api.notifications.channels.upsert({
        kind,
        config_json: config,
        enabled,
      })
      toast.success(isEdit ? "Channel updated" : "Channel created")
      await onSaved()
      onClose()
    } catch (err) {
      toast.error(isEdit ? "Could not update channel" : "Could not create channel", {
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
          <DialogTitle>{isEdit ? "Edit channel" : "New channel"}</DialogTitle>
          <DialogDescription>
            {isEdit ? (
              <>
                Update the config and enabled state for the{" "}
                <code className="font-mono">{channel.kind}</code> channel. Stored values are hidden
                — leave a field blank to keep its current value.
              </>
            ) : (
              <>
                Persist a channel in the database. Config values may reference secrets (e.g.{" "}
                <code className="font-mono">secret:resend-key</code>); they are never displayed
                after save.
              </>
            )}
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit} className="flex flex-col gap-stack px-4 pb-1">
          <Field label="Kind">
            {isEdit ? (
              <Badge variant="secondary" className="w-fit">
                {kind}
              </Badge>
            ) : (
              <FilterChipGroup>
                {KIND_OPTIONS.map((opt) => {
                  const taken = takenDbKinds.has(opt.value)
                  return (
                    <FilterChip
                      key={opt.value}
                      label={taken ? `${opt.label} (exists)` : opt.label}
                      active={kind === opt.value}
                      onSelect={() => setKind(opt.value)}
                    />
                  )
                })}
              </FilterChipGroup>
            )}
          </Field>

          <Field label="Config" hint="adapter-specific keys — values may be secret references">
            <div className="flex flex-col gap-tile-tight">
              {rows.length === 0 ? (
                <p className="text-meta text-muted-foreground">No config keys.</p>
              ) : (
                rows.map((row, idx) => (
                  <div key={idx} className="flex items-center gap-inline">
                    <Input
                      value={row.key}
                      onChange={(e) => updateRow(idx, { key: e.target.value })}
                      placeholder="key"
                      className="font-mono"
                      aria-label={`Config key ${idx + 1}`}
                    />
                    <Input
                      value={row.value}
                      onChange={(e) => updateRow(idx, { value: e.target.value })}
                      placeholder={row.stored ? "•••• leave blank to keep" : "value"}
                      className="font-mono"
                      type="password"
                      autoComplete="off"
                      aria-label={`Config value ${idx + 1}`}
                    />
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      className="h-8 text-muted-foreground hover:text-destructive"
                      aria-label={`Remove config key ${idx + 1}`}
                      onClick={() => removeRow(idx)}
                    >
                      <X className="size-3.5" aria-hidden />
                    </Button>
                  </div>
                ))
              )}
              <Button
                type="button"
                size="sm"
                variant="ghost"
                className="h-7 w-fit gap-inline text-meta text-muted-foreground"
                onClick={addRow}
              >
                <Plus className="size-3.5" aria-hidden />
                Add key
              </Button>
            </div>
          </Field>

          <label className="flex items-center justify-between gap-stack">
            <span className="flex flex-col gap-tile-tight">
              <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
                Enabled
              </span>
              <span className="text-meta text-muted-foreground">
                Disabled channels are skipped by the worker.
              </span>
            </span>
            <Switch checked={enabled} onCheckedChange={setEnabled} />
          </label>

          <DialogFooter className="flex-row justify-end gap-inline px-0 pt-stack">
            <Button type="button" variant="ghost" size="sm" onClick={onClose} disabled={submitting}>
              Cancel
            </Button>
            <Button type="submit" size="sm" disabled={submitting}>
              {submitting
                ? isEdit
                  ? "Saving…"
                  : "Creating…"
                : isEdit
                  ? "Save changes"
                  : "Create channel"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function DeleteConfirm({
  channel,
  onClose,
  onDeleted,
}: {
  channel: NotificationChannel | null
  onClose: () => void
  onDeleted: () => Promise<void> | void
}) {
  const [submitting, setSubmitting] = useState(false)

  const onConfirm = async () => {
    if (!channel || submitting) return
    setSubmitting(true)
    try {
      await api.notifications.channels.remove(channel.kind)
      toast.success("Channel deleted")
      await onDeleted()
      onClose()
    } catch (err) {
      toast.error("Could not delete channel", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AlertDialog open={channel !== null} onOpenChange={(open) => !open && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete channel?</AlertDialogTitle>
          <AlertDialogDescription>
            The <code className="font-mono">{channel?.kind}</code> channel config will be removed.
            Delivery falls back to the env-configured adapter, if any.
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

// SPDX-License-Identifier: Apache-2.0

"use client"

import { useState } from "react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import { api } from "@/lib/api"
import type { IntegrationSlot } from "@/lib/api"

// One card per adapter slot. Renders each credential field as a password
// input with a per-field "Clear stored value" action. Save semantics
// mirror the API: a field left blank is NOT sent (leave unchanged); the
// explicit Clear action sends "" (clear the stored value). That keeps
// "typed nothing" from silently wiping a stored secret. After any write we
// call onSaved(), which re-lists the whole snapshot server-side rather
// than trusting the bare PUT body.

// Human-readable labels for known field names. Falls back to a generic
// humanizer (with common acronyms upper-cased) for anything unmapped.
const FIELD_LABELS: Record<string, string> = {
  resend_api_key: "Resend API key",
  slack_webhook_url: "Slack webhook URL",
  twilio_account_sid: "Twilio Account SID",
  twilio_auth_token: "Twilio auth token",
  twilio_from_number: "Twilio from number",
  fcm_project_id: "FCM project ID",
  fcm_access_token: "FCM access token",
  remote_url: "Remote URL",
  remote_token: "Remote token",
  e2b_api_key: "E2B API key",
  e2b_base_url: "E2B base URL",
  browser_use_url: "Browser sidecar URL",
  steel_api_key: "Steel API key",
  browserbase_api_key: "Browserbase API key",
  browserbase_project_id: "Browserbase project ID",
  playwright_endpoint: "CDP / Playwright endpoint",
  allow_private: "Allow private addresses (true/false)",
}

const SLOT_LABELS: Record<string, string> = {
  notifications: "Notifications",
  storage: "Storage",
  secrets: "Secrets",
  llm: "LLM",
  sandbox: "Sandbox (code execution)",
  browser: "Browser tool",
}

const ACRONYMS = new Set(["api", "url", "sid", "id", "fcm", "sms", "llm", "smtp"])

function humanize(name: string): string {
  return name
    .split("_")
    .map((word) =>
      ACRONYMS.has(word) ? word.toUpperCase() : word.charAt(0).toUpperCase() + word.slice(1),
    )
    .join(" ")
}

function fieldLabel(name: string): string {
  return FIELD_LABELS[name] ?? humanize(name)
}

function slotLabel(slot: string): string {
  return SLOT_LABELS[slot] ?? humanize(slot)
}

interface IntegrationSlotCardProps {
  slot: IntegrationSlot
  onSaved: () => Promise<void> | void
}

export function IntegrationSlotCard({ slot, onSaved }: IntegrationSlotCardProps) {
  // Draft values keyed by field name. Only non-blank drafts are submitted.
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)

  const setDraft = (name: string, value: string) =>
    setDrafts((prev) => ({ ...prev, [name]: value }))

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    const credentials: Record<string, string> = {}
    for (const field of slot.fields) {
      const value = (drafts[field.name] ?? "").trim()
      if (value) credentials[field.name] = value
    }
    if (Object.keys(credentials).length === 0) {
      toast.error("Enter at least one value first")
      return
    }
    setBusy(true)
    try {
      await api.admin.integrations.update(slot.slot, { credentials })
      setDrafts({})
      toast.success(`${slotLabel(slot.slot)} credentials saved`)
      await onSaved()
    } catch (err) {
      toast.error("Could not save credentials", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setBusy(false)
    }
  }

  const clear = async (name: string) => {
    setBusy(true)
    try {
      await api.admin.integrations.update(slot.slot, {
        credentials: { [name]: "" },
      })
      setDrafts((prev) => {
        const next = { ...prev }
        delete next[name]
        return next
      })
      toast.success(`${fieldLabel(name)} cleared`)
      await onSaved()
    } catch (err) {
      toast.error("Could not clear the stored value", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setBusy(false)
    }
  }

  const configuredCount = slot.fields.filter((f) => f.set).length

  return (
    <ZoneCard>
      <ZoneCardHeader
        title={slotLabel(slot.slot)}
        subtitle={
          <span className="flex items-center gap-tile-tight">
            <code className="font-mono text-meta text-foreground">{slot.activeAdapter}</code>
            <span aria-hidden>·</span>
            <span>
              {configuredCount}/{slot.fields.length} set
            </span>
          </span>
        }
      />

      <form onSubmit={save} className="flex flex-col gap-stack px-row-x py-row-y">
        {slot.fields.map((field) => {
          const label = fieldLabel(field.name)
          const hint = field.set
            ? `Currently set${field.hint ? ` (${field.hint})` : ""}. Leave blank to keep it.`
            : "Not set."
          return (
            <Field
              key={field.name}
              label={label}
              hint={hint}
              onClear={field.set ? () => clear(field.name) : undefined}
              busy={busy}
            >
              <Input
                type={field.kind === "text" ? "text" : "password"}
                autoComplete="off"
                value={drafts[field.name] ?? ""}
                onChange={(e) => setDraft(field.name, e.target.value)}
                placeholder={field.set ? "unchanged" : ""}
                className="font-mono"
              />
            </Field>
          )
        })}
        <div className="flex items-center justify-end">
          <Button type="submit" size="sm" disabled={busy}>
            {busy ? "Saving…" : "Save credentials"}
          </Button>
        </div>
      </form>
    </ZoneCard>
  )
}

function Field({
  label,
  hint,
  onClear,
  busy,
  children,
}: {
  label: string
  hint?: string
  onClear?: () => void
  busy?: boolean
  children: React.ReactNode
}) {
  return (
    <div className="flex flex-col gap-tile-tight">
      <div className="flex items-center justify-between">
        <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">{label}</span>
        {onClear ? (
          <button
            type="button"
            onClick={onClear}
            disabled={busy}
            className="text-meta text-muted-foreground underline-offset-2 hover:text-destructive hover:underline disabled:opacity-50"
          >
            Clear stored value
          </button>
        ) : null}
      </div>
      {children}
      {hint ? <span className="text-meta text-muted-foreground">{hint}</span> : null}
    </div>
  )
}

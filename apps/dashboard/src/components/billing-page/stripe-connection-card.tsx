// SPDX-License-Identifier: Apache-2.0

"use client"

import { useState } from "react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { CopyButton } from "@/components/ui/copy-button"
import { Input } from "@/components/ui/input"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import { api } from "@/lib/api"
import type { BillingSettingsStatus } from "@/lib/api"

// Stripe connection card. Status row (adapter / mode / key source, masked
// key) + a paste-keys form + the copyable webhook endpoint hint. Saving
// hot-swaps the live Stripe client server-side — no restart needed.
//
// Save semantics mirror the API: a field left blank is NOT sent (leave
// unchanged); the explicit "Clear" action sends "" (clear the stored
// value). That keeps "typed nothing" from silently wiping a key.

// Same convention as home/quick-actions.tsx: the runtime's public URL is
// baked in via NEXT_PUBLIC_RUNTIME_URL, localhost fallback for dev.
const RUNTIME_URL =
  process.env.NEXT_PUBLIC_RUNTIME_URL ?? "http://localhost:8080"

const SOURCE_LABEL: Record<BillingSettingsStatus["source"], string> = {
  vault: "vault (DB + KMS)",
  env: "env vars",
  none: "not configured",
}

interface StripeConnectionCardProps {
  settings: BillingSettingsStatus
  onSaved: () => Promise<void> | void
}

export function StripeConnectionCard({
  settings,
  onSaved,
}: StripeConnectionCardProps) {
  const [secretKey, setSecretKey] = useState("")
  const [webhookSecret, setWebhookSecret] = useState("")
  const [busy, setBusy] = useState(false)

  const webhookEndpoint = `${RUNTIME_URL}${settings.webhook_path}`
  const maskedKey = settings.secret_key_set
    ? `•••• ${settings.secret_key_last4}`
    : "not set"

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    const key = secretKey.trim()
    const whsec = webhookSecret.trim()
    if (!key && !whsec) {
      toast.error("Paste a secret key and/or a webhook secret first")
      return
    }
    setBusy(true)
    try {
      await api.admin.billing.updateSettings({
        ...(key ? { stripe_secret_key: key } : {}),
        ...(whsec ? { stripe_webhook_secret: whsec } : {}),
      })
      setSecretKey("")
      setWebhookSecret("")
      toast.success("Stripe settings saved", {
        description: "The live billing client was hot-swapped — no restart needed.",
      })
      await onSaved()
    } catch (err) {
      toast.error("Could not save Stripe settings", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setBusy(false)
    }
  }

  const clear = async (field: "stripe_secret_key" | "stripe_webhook_secret") => {
    setBusy(true)
    try {
      await api.admin.billing.updateSettings({ [field]: "" })
      toast.success(
        field === "stripe_secret_key"
          ? "Stored secret key cleared"
          : "Stored webhook secret cleared",
      )
      await onSaved()
    } catch (err) {
      toast.error("Could not clear the stored value", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setBusy(false)
    }
  }

  return (
    <ZoneCard>
      <ZoneCardHeader
        title="Stripe connection"
        subtitle={
          settings.mode === "real" ? "live keys configured" : "dev / stub mode"
        }
      />

      {/* Status strip */}
      <dl className="grid grid-cols-2 gap-stack border-b px-row-x py-row-y sm:grid-cols-4">
        <StatusItem label="Adapter">
          <code className="font-mono text-meta text-foreground">
            {settings.adapter}
          </code>
        </StatusItem>
        <StatusItem label="Mode">
          <span className="flex items-center gap-tile-tight">
            <span
              aria-hidden
              className={`size-2 shrink-0 rounded-full ${
                settings.mode === "real" ? "bg-emerald-500" : "bg-amber-500"
              }`}
            />
            <span className="text-meta text-foreground">{settings.mode}</span>
          </span>
        </StatusItem>
        <StatusItem label="Key source">
          <span className="text-meta text-foreground">
            {SOURCE_LABEL[settings.source]}
          </span>
        </StatusItem>
        <StatusItem label="Secret key">
          <span className="font-mono text-meta text-foreground">{maskedKey}</span>
        </StatusItem>
      </dl>

      {settings.settings_writable ? (
        <form onSubmit={save} className="flex flex-col gap-stack px-row-x py-row-y">
          <Field
            label="Stripe secret key"
            hint={
              settings.secret_key_set
                ? `Currently ${maskedKey}. Leave blank to keep it.`
                : "sk_live_… or sk_test_… from the Stripe dashboard."
            }
            onClear={settings.secret_key_set ? () => clear("stripe_secret_key") : undefined}
            busy={busy}
          >
            <Input
              type="password"
              autoComplete="off"
              value={secretKey}
              onChange={(e) => setSecretKey(e.target.value)}
              placeholder={settings.secret_key_set ? "unchanged" : "sk_live_…"}
              className="font-mono"
            />
          </Field>
          <Field
            label="Webhook signing secret"
            hint={
              settings.webhook_secret_set
                ? "A signing secret is stored. Leave blank to keep it."
                : "whsec_… from the Stripe webhook endpoint config."
            }
            onClear={
              settings.webhook_secret_set
                ? () => clear("stripe_webhook_secret")
                : undefined
            }
            busy={busy}
          >
            <Input
              type="password"
              autoComplete="off"
              value={webhookSecret}
              onChange={(e) => setWebhookSecret(e.target.value)}
              placeholder={settings.webhook_secret_set ? "unchanged" : "whsec_…"}
              className="font-mono"
            />
          </Field>
          <div className="flex items-center justify-end">
            <Button type="submit" size="sm" disabled={busy}>
              {busy ? "Saving…" : "Save keys"}
            </Button>
          </div>
        </form>
      ) : (
        <p className="border-b px-row-x py-row-y text-meta text-muted-foreground">
          The billing settings store is unavailable (it needs the database and
          a KMS key). Keys can only be supplied via the{" "}
          <code className="font-mono">STRIPE_SECRET_KEY</code> and{" "}
          <code className="font-mono">STRIPE_WEBHOOK_SECRET</code> env vars
          until then.
        </p>
      )}

      {/* Webhook endpoint hint */}
      <div className="flex flex-wrap items-center gap-stack border-t px-row-x py-row-y">
        <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
          Webhook endpoint
        </span>
        <code className="min-w-0 truncate rounded bg-muted px-1.5 py-0.5 font-mono text-meta text-muted-foreground">
          {webhookEndpoint}
        </code>
        <CopyButton value={webhookEndpoint} label="Copy webhook endpoint" />
        <span className="basis-full text-meta text-muted-foreground">
          This is your <strong>runtime&apos;s</strong> URL — Stripe posts events
          here, not to this dashboard. Point a Stripe webhook at it (events:{" "}
          <code className="font-mono">checkout.session.completed</code>,{" "}
          <code className="font-mono">customer.subscription.*</code>) and paste
          its signing secret above. For local dev Stripe can&apos;t reach{" "}
          <code className="font-mono">localhost</code> — forward with{" "}
          <code className="font-mono">
            stripe listen --forward-to {webhookEndpoint}
          </code>
          .
        </span>
      </div>
    </ZoneCard>
  )
}

function StatusItem({
  label,
  children,
}: {
  label: string
  children: React.ReactNode
}) {
  return (
    <div className="flex min-w-0 flex-col gap-tile-tight">
      <dt className="text-eyebrow uppercase tracking-wide text-muted-foreground">
        {label}
      </dt>
      <dd className="min-w-0 truncate">{children}</dd>
    </div>
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
        <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
          {label}
        </span>
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
      {hint ? (
        <span className="text-meta text-muted-foreground">{hint}</span>
      ) : null}
    </div>
  )
}

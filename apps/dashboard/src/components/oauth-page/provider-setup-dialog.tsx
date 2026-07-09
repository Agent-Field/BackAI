// SPDX-License-Identifier: Apache-2.0

"use client"

import { useState } from "react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { CopyButton } from "@/components/ui/copy-button"
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
import type { IntegrationSlot, OAuthProvider } from "@/lib/api"
import { providerLabel } from "@/lib/oauth-page/derive"

// Provider setup dialog. Operators enter the OAuth client credentials the
// runtime stores in the vault under the `oauth_<provider>` integration slot
// (PUT /api/v1/admin/integrations/oauth_<provider>). The API never echoes
// stored secrets, so inputs are never prefilled — we surface the masked
// set-state from the integrations list instead. The dialog also shows the
// exact redirect URI to register on the provider side (copy-to-clipboard)
// plus short per-provider registration steps. On save we toast and ask the
// parent to refresh the snapshot; the runtime picks up new credentials
// without a restart, so the tile flips to configured/vault on refresh.

interface ProviderSetupDialogProps {
  provider: OAuthProvider | null
  // Best-effort masked status for the oauth_<provider> slot, fetched by the
  // parent when the dialog opens. Null while loading or if unavailable.
  slot: IntegrationSlot | null
  onClose: () => void
  onSaved: () => Promise<void> | void
}

export function ProviderSetupDialog({
  provider,
  slot,
  onClose,
  onSaved,
}: ProviderSetupDialogProps) {
  const [clientId, setClientId] = useState("")
  const [clientSecret, setClientSecret] = useState("")
  const [submitting, setSubmitting] = useState(false)

  const reset = () => {
    setClientId("")
    setClientSecret("")
  }

  const close = () => {
    reset()
    onClose()
  }

  const name = provider?.provider ?? ""
  const label = provider ? providerLabel(name) : ""
  const redirectUri = provider?.redirect_uri ?? null

  const clientIdSet = slot?.fields.find((f) => f.name === "client_id")?.set ?? false
  const clientSecretSet = slot?.fields.find((f) => f.name === "client_secret")?.set ?? false

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!provider || submitting) return
    // Match the integrations write contract: only non-blank fields are sent
    // (blank leaves a stored value unchanged). Require at least one value so
    // an empty submit can't silently no-op.
    const credentials: Record<string, string> = {}
    if (clientId.trim()) credentials.client_id = clientId.trim()
    if (clientSecret.trim()) credentials.client_secret = clientSecret.trim()
    if (Object.keys(credentials).length === 0) {
      toast.error("Enter the client ID and secret first")
      return
    }
    setSubmitting(true)
    try {
      await api.admin.integrations.update(`oauth_${name}`, { credentials })
      toast.success(`${label} credentials saved`, {
        description: "The provider is ready to connect.",
      })
      reset()
      await onSaved()
      onClose()
    } catch (err) {
      toast.error("Could not save credentials", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={provider !== null} onOpenChange={(next) => !next && close()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Configure {label}</DialogTitle>
          <DialogDescription>
            Register the redirect URI below in the provider&apos;s console, then paste the OAuth
            client credentials here. They&apos;re stored in the runtime vault and picked up without
            a restart.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-stack px-4 pb-1">
          <RedirectUri redirectUri={redirectUri} provider={name} />
          <SetupSteps provider={name} />

          <form onSubmit={onSubmit} className="flex flex-col gap-stack">
            <Field
              label="Client ID"
              hint={
                clientIdSet
                  ? "Currently set. Leave blank to keep it."
                  : "From the provider's OAuth application."
              }
            >
              <Input
                value={clientId}
                onChange={(e) => setClientId(e.target.value)}
                placeholder={clientIdSet ? "unchanged" : ""}
                autoComplete="off"
                className="font-mono"
              />
            </Field>
            <Field
              label="Client secret"
              hint={
                clientSecretSet
                  ? "Currently set. Leave blank to keep it."
                  : "Stored encrypted; never shown again after saving."
              }
            >
              <Input
                type="password"
                value={clientSecret}
                onChange={(e) => setClientSecret(e.target.value)}
                placeholder={clientSecretSet ? "unchanged" : ""}
                autoComplete="off"
                className="font-mono"
              />
            </Field>
            <DialogFooter className="flex-row justify-end gap-inline px-0 pt-stack">
              <Button type="button" variant="ghost" size="sm" onClick={close} disabled={submitting}>
                Cancel
              </Button>
              <Button type="submit" size="sm" disabled={submitting}>
                {submitting ? "Saving…" : "Save credentials"}
              </Button>
            </DialogFooter>
          </form>
        </div>
      </DialogContent>
    </Dialog>
  )
}

// Redirect URI block. When the runtime reports the exact callback URL we
// show it with a copy control. On older runtimes the field is absent — fall
// back to the path pattern and nudge the operator to set AF_STACK_PUBLIC_URL
// so the runtime can return the concrete URL.
function RedirectUri({ redirectUri, provider }: { redirectUri: string | null; provider: string }) {
  return (
    <div className="flex flex-col gap-tile-tight rounded-md border bg-muted/40 px-row-x py-tile">
      <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
        Redirect URI
      </span>
      {redirectUri ? (
        <div className="flex items-center gap-inline">
          <code className="min-w-0 flex-1 truncate font-mono text-meta text-foreground">
            {redirectUri}
          </code>
          <CopyButton value={redirectUri} label="Copy redirect URI" />
        </div>
      ) : (
        <>
          <code className="truncate font-mono text-meta text-foreground">
            &lt;your public URL&gt;/oauth/callback/{provider}
          </code>
          <span className="text-meta text-muted-foreground">
            Set <code className="font-mono">AF_STACK_PUBLIC_URL</code> so the runtime can report the
            exact callback URL to register.
          </span>
        </>
      )}
    </div>
  )
}

// Short, provider-specific registration steps. Falls back to a generic
// checklist for any provider we don't recognise.
function SetupSteps({ provider }: { provider: string }) {
  const steps = STEPS[provider.toLowerCase()] ?? GENERIC_STEPS
  return (
    <ol className="flex list-decimal flex-col gap-tile-tight pl-4 text-meta text-muted-foreground">
      {steps.map((step, i) => (
        <li key={i}>{step}</li>
      ))}
    </ol>
  )
}

const STEPS: Record<string, string[]> = {
  google: [
    "Google Cloud Console → APIs & Services → Credentials → Create credentials → OAuth client ID, type “Web application”.",
    "Add the redirect URI above under “Authorized redirect URIs” (exact match). Authorized JavaScript origins are not needed for this server-side flow.",
    "Configure the OAuth consent screen and scopes, then copy the client ID and secret here.",
  ],
  github: [
    "GitHub → Settings → Developer settings → OAuth Apps → New OAuth App.",
    "Paste the redirect URI above as the “Authorization callback URL”.",
    "Register the app, generate a client secret, then copy the client ID and secret here.",
  ],
}

const GENERIC_STEPS = [
  "Register a new OAuth application in the provider's developer console.",
  "Use the redirect URI above as the app's callback / redirect URL (exact match).",
  "Copy the resulting client ID and secret here.",
]

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

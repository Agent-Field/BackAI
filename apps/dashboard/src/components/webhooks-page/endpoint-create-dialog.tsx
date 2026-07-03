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

// Create-endpoint dialog. Persists via POST /api/v1/webhooks/endpoints
// (CreateEndpointInput), surfaces success/error via Sonner. Parent owns
// the open state. Only the three required fields plus the signature
// trio — dedup window keeps its server default.

interface EndpointCreateDialogProps {
  open: boolean
  onClose: () => void
  onCreated: () => Promise<void> | void
}

export function EndpointCreateDialog({
  open,
  onClose,
  onCreated,
}: EndpointCreateDialogProps) {
  const [slug, setSlug] = useState("")
  const [provider, setProvider] = useState("custom")
  const [forwardTo, setForwardTo] = useState("")
  const [secretKeyRef, setSecretKeyRef] = useState("")
  const [signatureAlgorithm, setSignatureAlgorithm] = useState("")
  const [signatureHeader, setSignatureHeader] = useState("")
  const [submitting, setSubmitting] = useState(false)

  const reset = () => {
    setSlug("")
    setProvider("custom")
    setForwardTo("")
    setSecretKeyRef("")
    setSignatureAlgorithm("")
    setSignatureHeader("")
  }

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!slug.trim() || !forwardTo.trim()) {
      toast.error("Slug and forward-to are required")
      return
    }
    setSubmitting(true)
    try {
      await api.webhooks.createEndpoint({
        slug: slug.trim(),
        provider: provider.trim() || "custom",
        forward_to: forwardTo.trim(),
        secret_key_ref: secretKeyRef.trim() ? secretKeyRef.trim() : undefined,
        signature_algorithm: signatureAlgorithm.trim() || undefined,
        signature_header: signatureHeader.trim() || undefined,
      })
      toast.success("Endpoint created", {
        description: `/webhooks/in/${slug.trim()}`,
      })
      reset()
      await onCreated()
      onClose()
    } catch (err) {
      toast.error("Could not create endpoint", {
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
          <DialogTitle>New inbound endpoint</DialogTitle>
          <DialogDescription>
            Providers POST to <code className="font-mono">/webhooks/in/&lt;slug&gt;</code>;
            verified payloads forward to an internal URL or agent.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit} className="flex flex-col gap-stack px-4 pb-1">
          <Field label="Slug" hint="lowercase letters, digits, dashes">
            <Input
              value={slug}
              onChange={(e) => setSlug(e.target.value)}
              placeholder="github-prs"
              className="font-mono"
              required
            />
          </Field>
          <Field label="Provider" hint="documentation hint only">
            <Input
              value={provider}
              onChange={(e) => setProvider(e.target.value)}
              placeholder="github / stripe / custom"
            />
          </Field>
          <Field
            label="Forward to"
            hint="HTTP URL or agent id (af://agents/<name>)"
          >
            <Input
              value={forwardTo}
              onChange={(e) => setForwardTo(e.target.value)}
              placeholder="http://internal-service/hooks"
              className="font-mono"
              required
            />
          </Field>
          <Field
            label="Secret key ref"
            hint="optional — key in the secrets vault used for HMAC verification"
          >
            <Input
              value={secretKeyRef}
              onChange={(e) => setSecretKeyRef(e.target.value)}
              placeholder="github-webhook-secret"
              className="font-mono"
            />
          </Field>
          <div className="grid grid-cols-2 gap-stack">
            <Field label="Signature algorithm">
              <Input
                value={signatureAlgorithm}
                onChange={(e) => setSignatureAlgorithm(e.target.value)}
                placeholder="sha256"
                className="font-mono"
              />
            </Field>
            <Field label="Signature header">
              <Input
                value={signatureHeader}
                onChange={(e) => setSignatureHeader(e.target.value)}
                placeholder="X-Hub-Signature-256"
                className="font-mono"
              />
            </Field>
          </div>
          <DialogFooter className="flex-row justify-end gap-inline px-0 pt-stack">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={onClose}
              disabled={submitting}
            >
              Cancel
            </Button>
            <Button type="submit" size="sm" disabled={submitting}>
              {submitting ? "Creating…" : "Create endpoint"}
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
      <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
        {label}
      </span>
      {children}
      {hint ? (
        <span className="text-meta text-muted-foreground">{hint}</span>
      ) : null}
    </label>
  )
}

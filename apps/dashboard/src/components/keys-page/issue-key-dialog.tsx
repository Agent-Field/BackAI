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
import type { IssuedAPIKey, Tenant } from "@/lib/api"
import { OPERATOR_TENANT_ID, parseScopes } from "@/lib/keys-page/derive"

// Issue-key dialog. Two phases:
//   form   — tenant picker + name + optional scopes / budget / rate limits
//   result — the one-time plaintext value, copyable, with a can't-miss
//            "you won't see this again" warning (same UX as the inline
//            quick-actions mint on Home).
// Parent owns the open state; onIssued fires after a successful mint so
// the shell can refresh the list.

import { KeyValueBlock } from "./key-reveal"

interface IssueKeyDialogProps {
  open: boolean
  tenants: Tenant[]
  /** Preselected tenant id (from the active ?tenant= filter). */
  defaultTenantId?: string
  onClose: () => void
  onIssued: () => Promise<void> | void
}

export function IssueKeyDialog({
  open,
  tenants,
  defaultTenantId,
  onClose,
  onIssued,
}: IssueKeyDialogProps) {
  const [tenantId, setTenantId] = useState(defaultTenantId ?? "")
  const [name, setName] = useState("")
  const [scopes, setScopes] = useState("")
  const [budget, setBudget] = useState("")
  const [rpm, setRpm] = useState("")
  const [tpm, setTpm] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [issued, setIssued] = useState<IssuedAPIKey | null>(null)

  const reset = () => {
    setTenantId(defaultTenantId ?? "")
    setName("")
    setScopes("")
    setBudget("")
    setRpm("")
    setTpm("")
    setIssued(null)
  }

  const close = () => {
    reset()
    onClose()
  }

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!tenantId) {
      toast.error("Pick a tenant for the key")
      return
    }
    const budgetNum = budget.trim() ? Number(budget) : null
    if (budgetNum !== null && (!Number.isFinite(budgetNum) || budgetNum <= 0)) {
      toast.error("Budget must be a positive number (or empty for unlimited)")
      return
    }
    const rpmNum = rpm.trim() ? Number(rpm) : null
    const tpmNum = tpm.trim() ? Number(tpm) : null
    if (
      (rpmNum !== null && (!Number.isInteger(rpmNum) || rpmNum <= 0)) ||
      (tpmNum !== null && (!Number.isInteger(tpmNum) || tpmNum <= 0))
    ) {
      toast.error("Rate limits must be positive integers (or empty)")
      return
    }
    setSubmitting(true)
    try {
      const result = await api.admin.keys.issue({
        tenant_id: tenantId,
        name: name.trim() || undefined,
        scopes: parseScopes(scopes),
        budget_max_usd: budgetNum,
        rate_limit_rpm: rpmNum,
        rate_limit_tpm: tpmNum,
      })
      setIssued(result)
      toast.success("Key issued", { description: result.prefix })
      await onIssued()
    } catch (err) {
      toast.error("Could not issue key", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !o && close()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{issued ? "Key issued" : "Issue API key"}</DialogTitle>
          <DialogDescription>
            {issued
              ? "The plaintext value below is shown exactly once."
              : "Mints a tenant-scoped key via the admin API. On the zero-uuid tenant with an operator scope it acts as an operator key for the af-stack CLI."}
          </DialogDescription>
        </DialogHeader>

        {issued ? (
          <>
            <div className="px-4 pb-1">
              <KeyValueBlock value={issued.value} />
            </div>
            <DialogFooter className="flex-row justify-end gap-inline pt-stack">
              <Button type="button" size="sm" onClick={close}>
                Done
              </Button>
            </DialogFooter>
          </>
        ) : (
          <form onSubmit={onSubmit} className="flex flex-col gap-stack px-4 pb-1">
            <Field label="Tenant">
              <select
                value={tenantId}
                onChange={(e) => setTenantId(e.target.value)}
                required
                className="h-8 w-full rounded-lg border border-input bg-transparent px-2.5 py-1 text-sm text-foreground outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30"
              >
                <option value="" disabled>
                  Pick a tenant…
                </option>
                <option value={OPERATOR_TENANT_ID}>
                  operator (zero-uuid tenant)
                </option>
                {tenants
                  .filter((t) => t.id !== OPERATOR_TENANT_ID)
                  .map((t) => (
                    <option key={t.id} value={t.id}>
                      {t.name || t.slug}
                    </option>
                  ))}
              </select>
            </Field>
            <Field label="Name">
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. ci-deploy"
              />
            </Field>
            <Field
              label="Scopes"
              hint="Comma-separated, optional. Use `operator` on the zero-uuid tenant for an af-stack CLI key."
            >
              <Input
                value={scopes}
                onChange={(e) => setScopes(e.target.value)}
                placeholder="e.g. operator, llm:read"
                className="font-mono"
              />
            </Field>
            <Field label="Budget cap (USD)" hint="Lifetime cap enforced by LiteLLM. Empty = unlimited.">
              <Input
                type="number"
                min="0"
                step="0.01"
                value={budget}
                onChange={(e) => setBudget(e.target.value)}
                className="font-mono tabular-nums"
                placeholder="unlimited"
              />
            </Field>
            <div className="grid grid-cols-2 gap-stack">
              <Field label="Rate limit (RPM)">
                <Input
                  type="number"
                  min="1"
                  step="1"
                  value={rpm}
                  onChange={(e) => setRpm(e.target.value)}
                  className="font-mono tabular-nums"
                  placeholder="default"
                />
              </Field>
              <Field label="Rate limit (TPM)">
                <Input
                  type="number"
                  min="1"
                  step="1"
                  value={tpm}
                  onChange={(e) => setTpm(e.target.value)}
                  className="font-mono tabular-nums"
                  placeholder="default"
                />
              </Field>
            </div>
            <DialogFooter className="flex-row justify-end gap-inline px-0 pt-stack">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={close}
                disabled={submitting}
              >
                Cancel
              </Button>
              <Button type="submit" size="sm" disabled={submitting}>
                {submitting ? "Issuing…" : "Issue key"}
              </Button>
            </DialogFooter>
          </form>
        )}
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

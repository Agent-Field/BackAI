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
import { Textarea } from "@/components/ui/textarea"

import { api } from "@/lib/api"
import type { BillingPlan } from "@/lib/api"

// Add/Edit plan dialog. One form, two modes: when `plan` is set we're
// editing (id locked — it's the upsert key), otherwise creating.
// Entitlements are edited as raw JSON with validation on submit; the
// LLM budget field maps empty ↔ null (unlimited) per the API contract.
//
// The inner <PlanForm> is mounted fresh per open (keyed by the target
// plan) so field state initializes from props at mount — no
// setState-in-effect re-seeding.

interface PlanFormDialogProps {
  open: boolean
  /** Plan being edited; null = create a new plan. */
  plan: BillingPlan | null
  onClose: () => void
  onSaved: () => Promise<void> | void
}

export function PlanFormDialog({ open, plan, onClose, onSaved }: PlanFormDialogProps) {
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{plan ? `Edit plan: ${plan.name}` : "Add plan"}</DialogTitle>
          <DialogDescription>
            {plan
              ? "Changes apply to future checkouts and entitlement reads immediately."
              : "Upserts into the catalog. Bind a Stripe Price id for real checkout; leave it empty in dev/stub mode."}
          </DialogDescription>
        </DialogHeader>
        {open ? (
          <PlanForm
            key={plan?.id ?? "__create__"}
            plan={plan}
            onClose={onClose}
            onSaved={onSaved}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

function PlanForm({
  plan,
  onClose,
  onSaved,
}: {
  plan: BillingPlan | null
  onClose: () => void
  onSaved: () => Promise<void> | void
}) {
  const [id, setId] = useState(plan?.id ?? "")
  const [name, setName] = useState(plan?.name ?? "")
  const [price, setPrice] = useState(plan ? String(plan.price_usd_month) : "")
  const [stripePriceId, setStripePriceId] = useState(plan?.stripe_price_id ?? "")
  const [llmBudget, setLlmBudget] = useState(
    plan?.llm_budget_usd != null ? String(plan.llm_budget_usd) : "",
  )
  const [entitlements, setEntitlements] = useState(() =>
    plan ? JSON.stringify(plan.entitlements, null, 2) : "{}",
  )
  const [isDefault, setIsDefault] = useState(plan?.is_default ?? false)
  const [submitting, setSubmitting] = useState(false)

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    const trimmedId = id.trim()
    const trimmedName = name.trim()
    if (!trimmedId || !trimmedName) {
      toast.error("Plan id and name are required")
      return
    }
    const priceNum = price.trim() ? Number(price) : 0
    if (!Number.isFinite(priceNum) || priceNum < 0) {
      toast.error("Price must be a non-negative number")
      return
    }
    const budgetNum = llmBudget.trim() ? Number(llmBudget) : null
    if (budgetNum !== null && (!Number.isFinite(budgetNum) || budgetNum <= 0)) {
      toast.error("LLM budget must be a positive number (or empty for unlimited)")
      return
    }
    let ents: Record<string, unknown>
    try {
      const parsed: unknown = JSON.parse(entitlements.trim() || "{}")
      if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
        throw new Error('must be a JSON object, e.g. {"seats": 5}')
      }
      ents = parsed as Record<string, unknown>
    } catch (err) {
      toast.error("Entitlements is not valid JSON", {
        description: err instanceof Error ? err.message : String(err),
      })
      return
    }
    setSubmitting(true)
    try {
      const saved = await api.admin.billing.upsertPlan({
        id: trimmedId,
        name: trimmedName,
        stripe_price_id: stripePriceId.trim() || null,
        price_usd_month: priceNum,
        llm_budget_usd: budgetNum,
        entitlements: ents,
        is_default: isDefault,
      })
      toast.success(plan ? "Plan updated" : "Plan created", {
        description: saved.id,
      })
      onClose()
      await onSaved()
    } catch (err) {
      toast.error("Could not save the plan", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-stack px-4 pb-1">
      <div className="grid grid-cols-2 gap-stack">
        <Field label="Plan id" hint={plan ? "Upsert key — locked." : "e.g. pro"}>
          <Input
            value={id}
            onChange={(e) => setId(e.target.value)}
            disabled={plan !== null}
            required
            placeholder="pro"
            className="font-mono"
          />
        </Field>
        <Field label="Name">
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            placeholder="Pro"
          />
        </Field>
      </div>
      <div className="grid grid-cols-2 gap-stack">
        <Field label="Price (USD / month)">
          <Input
            type="number"
            min="0"
            step="0.01"
            value={price}
            onChange={(e) => setPrice(e.target.value)}
            placeholder="0"
            className="font-mono tabular-nums"
          />
        </Field>
        <Field label="LLM budget (USD / month)" hint="Empty = unlimited.">
          <Input
            type="number"
            min="0"
            step="0.01"
            value={llmBudget}
            onChange={(e) => setLlmBudget(e.target.value)}
            placeholder="unlimited"
            className="font-mono tabular-nums"
          />
        </Field>
      </div>
      <Field
        label="Stripe price id"
        hint="From the Stripe product's pricing table. Empty = not bound (stub checkout only)."
      >
        <Input
          value={stripePriceId}
          onChange={(e) => setStripePriceId(e.target.value)}
          placeholder="price_…"
          className="font-mono"
        />
      </Field>
      <Field
        label="Entitlements (JSON)"
        hint='Freeform object surfaced via suite.billing.entitlements(), e.g. {"seats": 5, "priority_support": true}.'
      >
        <Textarea
          value={entitlements}
          onChange={(e) => setEntitlements(e.target.value)}
          rows={5}
          spellCheck={false}
          className="font-mono text-meta"
        />
      </Field>
      <label className="flex items-center gap-inline text-body text-foreground">
        <input
          type="checkbox"
          checked={isDefault}
          onChange={(e) => setIsDefault(e.target.checked)}
          disabled={plan?.is_default ?? false}
          className="size-4 accent-primary"
        />
        <span>Default plan</span>
        <span className="text-meta text-muted-foreground">
          {plan?.is_default
            ? "— set another plan as default to move it"
            : "— new tenants land here"}
        </span>
      </label>
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
          {submitting ? "Saving…" : plan ? "Save plan" : "Create plan"}
        </Button>
      </DialogFooter>
    </form>
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

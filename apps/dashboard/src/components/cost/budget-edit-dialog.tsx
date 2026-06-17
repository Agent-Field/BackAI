// SPDX-License-Identifier: Apache-2.0

"use client"

import { useEffect, useState } from "react"
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
import type { Budget } from "@/lib/api"
import { formatCurrency } from "@/lib/cost/derive"

// Edit-budget dialog. Persists via PUT /api/v1/admin/budgets, surfaces
// success/error via Sonner. Parent owns the open state via the `budget`
// prop being non-null.

interface BudgetEditDialogProps {
  budget: Budget | null
  tenantName: string
  onClose: () => void
  onSaved: () => Promise<void> | void
}

export function BudgetEditDialog({
  budget,
  tenantName,
  onClose,
  onSaved,
}: BudgetEditDialogProps) {
  const [monthlyUSD, setMonthlyUSD] = useState<string>("")
  const [thresholdPct, setThresholdPct] = useState<string>("80")
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (!budget) return
    setMonthlyUSD(budget.monthly_usd.toString())
    setThresholdPct(budget.alert_threshold_pct.toString())
  }, [budget])

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!budget) return
    const monthly = Number(monthlyUSD)
    const threshold = Number(thresholdPct)
    if (!Number.isFinite(monthly) || monthly <= 0) {
      toast.error("Budget must be a positive number")
      return
    }
    if (!Number.isFinite(threshold) || threshold < 0 || threshold > 100) {
      toast.error("Alert threshold must be 0–100%")
      return
    }
    setSubmitting(true)
    try {
      await api.budgets.set({
        tenant_id: budget.tenant_id,
        monthly_usd: monthly,
        alert_threshold_pct: threshold,
      })
      toast.success("Budget updated", {
        description: `${tenantName}: ${formatCurrency(monthly)} cap, alert at ${threshold}%`,
      })
      await onSaved()
    } catch (err) {
      toast.error("Could not save budget", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={budget !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Set budget</DialogTitle>
          <DialogDescription>
            {tenantName ? `Adjust monthly cap and alert threshold for ${tenantName}.` : ""}
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit} className="flex flex-col gap-stack px-4 pb-1">
          <label className="flex flex-col gap-tile-tight">
            <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
              Monthly cap (USD)
            </span>
            <Input
              type="number"
              min="1"
              step="1"
              value={monthlyUSD}
              onChange={(e) => setMonthlyUSD(e.target.value)}
              className="font-mono tabular-nums"
              required
            />
          </label>
          <label className="flex flex-col gap-tile-tight">
            <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
              Alert at (%)
            </span>
            <Input
              type="number"
              min="0"
              max="100"
              step="5"
              value={thresholdPct}
              onChange={(e) => setThresholdPct(e.target.value)}
              className="font-mono tabular-nums"
              required
            />
            <span className="text-meta text-muted-foreground">
              Inbox surfaces an alert once spending crosses this share of the cap.
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
              {submitting ? "Saving…" : "Save"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

"use client"

// Set-budget dialog.
//
// Driven by react-hook-form + zod. Picks the active tenant from the
// tenants list (when multi-tenancy is enabled) or defaults to the
// implicit default tenant id. On submit we PUT through
// api.budgets.set(...), toast, then router.refresh() so the page picks
// up the new alert state without a full reload.

import * as React from "react"
import { useRouter } from "next/navigation"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { Coins, Plus, Settings2 } from "lucide-react"
import { toast } from "sonner"

import { api, ApiError, type Budget, type Tenant } from "@/lib/api"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"

const DEFAULT_TENANT_ID = "default"

// Local form schema. Mirrors SetBudgetInputSchema but loosens types so
// the number inputs can carry empty strings while the user is typing.
const FormSchema = z.object({
  tenant_id: z.string().min(1, "Pick a tenant"),
  monthly_usd: z
    .number({ message: "Enter a monthly budget" })
    .positive("Must be greater than zero"),
  alert_threshold_pct: z
    .number({ message: "Enter a threshold percent" })
    .min(0, "Must be 0 or more")
    .max(100, "Cannot exceed 100"),
})
type FormValues = z.infer<typeof FormSchema>

type CostBudgetDialogProps = {
  tenants: Tenant[]
  multiTenancyEnabled: boolean
  existingBudgets: Budget[]
  /** Optional trigger override; defaults to the standard "Set budget" button. */
  trigger?: React.ReactNode
}

export function CostBudgetDialog({
  tenants,
  multiTenancyEnabled,
  existingBudgets,
  trigger,
}: CostBudgetDialogProps) {
  const router = useRouter()
  const [open, setOpen] = React.useState(false)
  const [submitting, setSubmitting] = React.useState(false)

  // Default tenant selection: first existing budget, else first tenant,
  // else the implicit single-tenant id.
  const defaultTenantId = React.useMemo(() => {
    if (existingBudgets[0]) return existingBudgets[0].tenant_id
    if (tenants[0]) return tenants[0].id
    return DEFAULT_TENANT_ID
  }, [existingBudgets, tenants])

  const form = useForm<FormValues>({
    resolver: zodResolver(FormSchema),
    defaultValues: {
      tenant_id: defaultTenantId,
      monthly_usd: 100,
      alert_threshold_pct: 80,
    },
    mode: "onBlur",
  })

  // When the dialog opens, pre-fill from the existing budget (if any)
  // for the chosen tenant. Lets the dialog double as an "edit" affordance.
  React.useEffect(() => {
    if (!open) return
    const tenantId = form.getValues("tenant_id") || defaultTenantId
    const existing = existingBudgets.find((b) => b.tenant_id === tenantId)
    form.reset({
      tenant_id: tenantId,
      monthly_usd: existing?.monthly_usd ?? 100,
      alert_threshold_pct: existing?.alert_threshold_pct ?? 80,
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const watchedTenant = form.watch("tenant_id")
  React.useEffect(() => {
    if (!open) return
    const existing = existingBudgets.find((b) => b.tenant_id === watchedTenant)
    if (existing) {
      form.setValue("monthly_usd", existing.monthly_usd)
      form.setValue("alert_threshold_pct", existing.alert_threshold_pct)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [watchedTenant, open])

  const handleSubmit = async (values: FormValues) => {
    setSubmitting(true)
    try {
      await api.budgets.set({
        tenant_id: values.tenant_id,
        monthly_usd: values.monthly_usd,
        alert_threshold_pct: values.alert_threshold_pct,
      })
      toast.success("Budget saved", {
        description: `${values.tenant_id} · $${values.monthly_usd.toFixed(0)}/mo`,
      })
      setOpen(false)
      router.refresh()
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to set budget"
      toast.error("Could not save budget", { description: message })
    } finally {
      setSubmitting(false)
    }
  }

  const hasExisting = existingBudgets.length > 0

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger
        render={
          trigger ? (
            (trigger as React.ReactElement)
          ) : (
            <Button size="sm" variant={hasExisting ? "outline" : "default"}>
              {hasExisting ? <Settings2 /> : <Plus />}
              {hasExisting ? "Manage budgets" : "Set budget"}
            </Button>
          )
        }
      />
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Coins className="size-4" />
            {hasExisting ? "Update budget" : "Set monthly budget"}
          </DialogTitle>
          <DialogDescription>
            Caps spend per tenant per month. Crossing the threshold raises an
            alert here; hitting 100% can also block further LLM calls if the
            runtime is configured to enforce.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={form.handleSubmit(handleSubmit)}>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="budget-tenant">Tenant</FieldLabel>
              {multiTenancyEnabled && tenants.length > 0 ? (
                <Select
                  value={form.watch("tenant_id")}
                  onValueChange={(v) =>
                    form.setValue("tenant_id", v ?? defaultTenantId)
                  }
                >
                  <SelectTrigger id="budget-tenant">
                    <SelectValue placeholder="Pick a tenant" />
                  </SelectTrigger>
                  <SelectContent>
                    {tenants.map((t) => (
                      <SelectItem key={t.id} value={t.id}>
                        {t.name}
                        <span className="text-muted-foreground ml-1 font-mono text-[10px]">
                          {t.slug}
                        </span>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : (
                <Input
                  id="budget-tenant"
                  value={form.watch("tenant_id")}
                  readOnly
                  className="font-mono text-xs"
                />
              )}
              <FieldDescription>
                {multiTenancyEnabled
                  ? "Budgets are scoped per tenant. Each can have its own cap."
                  : "Multi-tenancy is off — the budget applies to the default tenant."}
              </FieldDescription>
            </Field>

            <Field
              data-invalid={
                form.formState.errors.monthly_usd ? true : undefined
              }
            >
              <FieldLabel htmlFor="budget-monthly">Monthly budget</FieldLabel>
              <Input
                id="budget-monthly"
                type="number"
                inputMode="decimal"
                step="1"
                min="0"
                placeholder="100"
                aria-invalid={
                  form.formState.errors.monthly_usd ? true : undefined
                }
                {...form.register("monthly_usd", { valueAsNumber: true })}
              />
              <FieldDescription>
                {form.formState.errors.monthly_usd?.message ??
                  "USD limit for the current calendar month. Resets at month start."}
              </FieldDescription>
            </Field>

            <Field
              data-invalid={
                form.formState.errors.alert_threshold_pct ? true : undefined
              }
            >
              <FieldLabel htmlFor="budget-threshold">
                Alert threshold (%)
              </FieldLabel>
              <Input
                id="budget-threshold"
                type="number"
                inputMode="numeric"
                step="1"
                min="0"
                max="100"
                placeholder="80"
                aria-invalid={
                  form.formState.errors.alert_threshold_pct ? true : undefined
                }
                {...form.register("alert_threshold_pct", {
                  valueAsNumber: true,
                })}
              />
              <FieldDescription>
                {form.formState.errors.alert_threshold_pct?.message ??
                  "Raises an alert once this percentage of the monthly budget is spent."}
              </FieldDescription>
            </Field>
          </FieldGroup>

          <DialogFooter className="mt-4">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setOpen(false)}
              disabled={submitting}
            >
              Cancel
            </Button>
            <Button type="submit" size="sm" disabled={submitting}>
              {submitting ? "Saving…" : "Save budget"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

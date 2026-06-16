// SPDX-License-Identifier: Apache-2.0

"use client"

// APIKeysView — list, issue, and revoke per-tenant API keys.
//
// The issue flow is two-stage:
//   1. Issue dialog — pick tenant, name, scopes, optional expiry.
//   2. Reveal dialog — shows the secret value EXACTLY ONCE. After the
//      reveal dialog closes, the value is gone forever (the list/get
//      endpoints never return it).

import * as React from "react"
import { Controller, useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table"
import {
  AlertTriangle,
  Copy,
  KeyRound,
  MoreHorizontal,
  Plus,
  Trash2,
} from "lucide-react"
import { toast } from "sonner"

import {
  api,
  ApiError,
  type APIKey,
  type IssuedAPIKey,
  type Tenant,
} from "@/lib/api"
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
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { DateTimePicker } from "@/components/ui/datetime-picker"
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
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { cn } from "@/lib/utils"

import { formatRelative } from "../../../operate/_lib/format"

const TENANT_ALL = "all"

// Local form shape — keeps RHF + zodResolver happy by avoiding `.default()`
// (which would split input/output types).
//
// budget_max_usd / rate_limit_rpm / rate_limit_tpm are item-#22 LiteLLM
// virtual-key knobs. Empty string = unlimited (forwarded as null on the
// wire so LiteLLM treats the field as absent).
const IssueKeyFormSchema = z.object({
  tenant_id: z.string().min(1, "Pick a tenant"),
  name: z.string(),
  scopes: z.array(z.string()),
  expires_at: z.string(),
  budget_max_usd: z.string(),
  rate_limit_rpm: z.string(),
  rate_limit_tpm: z.string(),
})
type IssueKeyFormValues = z.infer<typeof IssueKeyFormSchema>

const SCOPE_OPTIONS = [
  { value: "agents:invoke", label: "agents:invoke" },
  { value: "agents:read", label: "agents:read" },
  { value: "runs:read", label: "runs:read" },
  { value: "storage:read", label: "storage:read" },
  { value: "storage:write", label: "storage:write" },
  { value: "secrets:read", label: "secrets:read" },
  { value: "admin", label: "admin" },
] as const

export function APIKeysView() {
  const [tenants, setTenants] = React.useState<Tenant[]>([])
  const [tenantFilter, setTenantFilter] = React.useState<string>(TENANT_ALL)
  const [keys, setKeys] = React.useState<APIKey[]>([])
  const [loading, setLoading] = React.useState(true)
  const [error, setError] = React.useState<string | null>(null)
  const [openIssue, setOpenIssue] = React.useState(false)
  const [revokeTarget, setRevokeTarget] = React.useState<APIKey | null>(null)
  const [revealed, setRevealed] = React.useState<IssuedAPIKey | null>(null)

  React.useEffect(() => {
    void api.admin.tenants
      .list()
      .then((r) => setTenants(r.tenants))
      .catch(() => setTenants([]))
  }, [])

  const fetchKeys = React.useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const list = await api.admin.keys.list({
        tenant: tenantFilter === TENANT_ALL ? undefined : tenantFilter,
      })
      setKeys(list.keys)
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to load API keys"
      setError(message)
      setKeys([])
    } finally {
      setLoading(false)
    }
  }, [tenantFilter])

  React.useEffect(() => {
    void fetchKeys()
  }, [fetchKeys])

  const tenantNameById = React.useMemo(() => {
    const map = new Map<string, string>()
    for (const t of tenants) map.set(t.id, t.name)
    return map
  }, [tenants])

  const columns = React.useMemo<ColumnDef<APIKey>[]>(
    () => [
      {
        id: "prefix",
        header: "Prefix",
        cell: ({ row }) => (
          <span className="font-mono text-xs">{row.original.prefix}…</span>
        ),
      },
      {
        id: "name",
        header: "Name",
        cell: ({ row }) => (
          <span className="text-sm">{row.original.name ?? "—"}</span>
        ),
      },
      {
        id: "tenant",
        header: "Tenant",
        cell: ({ row }) => (
          <span className="text-muted-foreground text-xs">
            {tenantNameById.get(row.original.tenant_id) ??
              row.original.tenant_id}
          </span>
        ),
      },
      {
        id: "scopes",
        header: "Scopes",
        cell: ({ row }) => (
          <div className="flex flex-wrap gap-1">
            {row.original.scopes.length === 0 ? (
              <span className="text-muted-foreground text-xs">—</span>
            ) : (
              row.original.scopes.map((s) => (
                <Badge key={s} variant="outline" className="font-mono text-[10px]">
                  {s}
                </Badge>
              ))
            )}
          </div>
        ),
      },
      {
        id: "budget",
        header: "Budget",
        cell: ({ row }) => <BudgetCell apiKey={row.original} />,
      },
      {
        id: "rate_limit_rpm",
        header: "RPM",
        cell: ({ row }) => (
          <span className="text-muted-foreground text-xs">
            {row.original.rate_limit_rpm != null
              ? row.original.rate_limit_rpm.toLocaleString()
              : "—"}
          </span>
        ),
      },
      {
        id: "created_at",
        header: "Created",
        cell: ({ row }) => (
          <span className="text-muted-foreground text-xs">
            {formatRelative(row.original.created_at)}
          </span>
        ),
      },
      {
        id: "last_used_at",
        header: "Last used",
        cell: ({ row }) => (
          <span className="text-muted-foreground text-xs">
            {row.original.last_used_at
              ? formatRelative(row.original.last_used_at)
              : "Never"}
          </span>
        ),
      },
      {
        id: "status",
        header: "Status",
        cell: ({ row }) =>
          row.original.revoked_at ? (
            <Badge variant="destructive">Revoked</Badge>
          ) : (
            <Badge variant="secondary">Active</Badge>
          ),
      },
      {
        id: "actions",
        header: "",
        cell: ({ row }) => {
          if (row.original.revoked_at) return null
          return (
            <div className="flex justify-end">
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button variant="ghost" size="icon-sm" aria-label="Actions">
                      <MoreHorizontal />
                    </Button>
                  }
                />
                <DropdownMenuContent align="end">
                  <DropdownMenuItem
                    variant="destructive"
                    onClick={() => setRevokeTarget(row.original)}
                  >
                    <Trash2 />
                    Revoke
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          )
        },
      },
    ],
    [tenantNameById],
  )

  const table = useReactTable({
    data: keys,
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <Select
          value={tenantFilter}
          onValueChange={(v) => setTenantFilter(v ?? TENANT_ALL)}
        >
          <SelectTrigger className="w-56">
            <SelectValue placeholder="All tenants" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={TENANT_ALL}>All tenants</SelectItem>
            {tenants.map((t) => (
              <SelectItem key={t.id} value={t.id}>
                {t.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <span className="text-muted-foreground text-xs">
          {loading
            ? "Loading…"
            : keys.length === 0
              ? "No keys"
              : `${keys.length} key${keys.length === 1 ? "" : "s"}`}
        </span>
        <div className="ml-auto">
          <Button
            size="sm"
            onClick={() => setOpenIssue(true)}
            disabled={tenants.length === 0}
          >
            <Plus />
            Issue key
          </Button>
        </div>
      </div>

      <div className="rounded-xl border bg-card">
        <Table>
          <TableHeader>
            {table.getHeaderGroups().map((hg) => (
              <TableRow key={hg.id} className="hover:bg-transparent">
                {hg.headers.map((header) => (
                  <TableHead
                    key={header.id}
                    className="text-muted-foreground text-xs font-medium uppercase tracking-wide"
                  >
                    {header.isPlaceholder
                      ? null
                      : flexRender(
                          header.column.columnDef.header,
                          header.getContext(),
                        )}
                  </TableHead>
                ))}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {loading && keys.length === 0 ? (
              <SkeletonRows columns={columns.length} />
            ) : keys.length === 0 ? (
              <TableRow className="hover:bg-transparent">
                <TableCell colSpan={columns.length} className="p-0">
                  <KeysEmpty
                    error={error}
                    canIssue={tenants.length > 0}
                    onIssue={() => setOpenIssue(true)}
                  />
                </TableCell>
              </TableRow>
            ) : (
              table.getRowModel().rows.map((row) => (
                <TableRow key={row.id} className="hover:bg-muted/30">
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id}>
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </TableCell>
                  ))}
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>

      <IssueKeyDialog
        open={openIssue}
        onOpenChange={setOpenIssue}
        tenants={tenants}
        defaultTenant={tenantFilter !== TENANT_ALL ? tenantFilter : undefined}
        onIssued={(issued) => {
          setOpenIssue(false)
          setRevealed(issued)
          void fetchKeys()
        }}
      />

      <RevealKeyDialog
        issued={revealed}
        onOpenChange={(open) => {
          if (!open) setRevealed(null)
        }}
      />

      <RevokeKeyDialog
        target={revokeTarget}
        onOpenChange={(open) => !open && setRevokeTarget(null)}
        onRevoked={fetchKeys}
      />
    </div>
  )
}

// parsePositiveNumber maps an optional string input to a positive
// number, undefined (empty), or an Error (invalid). Used by the issue
// dialog to validate budget/rate-limit inputs before round-trip.
function parsePositiveNumber(v: string): number | undefined | Error {
  const trimmed = v.trim()
  if (trimmed === "") return undefined
  const n = Number(trimmed)
  if (!Number.isFinite(n) || n <= 0) return new Error("invalid")
  return n
}

function parsePositiveInt(v: string): number | undefined | Error {
  const result = parsePositiveNumber(v)
  if (result === undefined || result instanceof Error) return result
  if (!Number.isInteger(result)) return new Error("invalid")
  return result
}

// BudgetCell renders the "$spent / $cap (X% remaining)" widget for one
// row. When neither budget nor live spend is known the cell renders an
// em-dash — legacy keys without LiteLLM mapping and degraded modes
// (LiteLLM unreachable) take this branch.
//
// Source of truth: live_spend_usd + budget_max_usd both come from
// LiteLLM via the runtime's admin endpoint — suite_cost_events is
// audit-only (#22).
function BudgetCell({ apiKey }: { apiKey: APIKey }) {
  const budget = apiKey.budget_max_usd ?? null
  const spent = apiKey.live_spend_usd ?? null
  if (budget == null && spent == null) {
    return <span className="text-muted-foreground text-xs">—</span>
  }
  if (budget == null) {
    // No cap set — show running spend only.
    return (
      <span className="font-mono text-xs">
        {spent != null ? `$${spent.toFixed(2)}` : "—"}
      </span>
    )
  }
  const pct =
    budget > 0
      ? Math.min(100, Math.max(0, ((spent ?? 0) / budget) * 100))
      : 0
  const remainingPct = Math.max(0, 100 - pct)
  return (
    <div className="flex flex-col gap-1 min-w-[140px]">
      <span className="font-mono text-xs">
        ${(spent ?? 0).toFixed(2)} / ${budget.toFixed(2)}
      </span>
      <div
        className="h-1 w-full overflow-hidden rounded-full bg-muted"
        role="progressbar"
        aria-valuenow={Math.round(pct)}
        aria-valuemin={0}
        aria-valuemax={100}
      >
        <div
          className={cn(
            "h-full transition-all",
            pct >= 90 ? "bg-destructive" : pct >= 75 ? "bg-amber-500" : "bg-primary",
          )}
          style={{ width: `${pct}%` }}
        />
      </div>
      <span className="text-muted-foreground text-[10px]">
        {Math.round(remainingPct)}% remaining
      </span>
    </div>
  )
}

function SkeletonRows({ columns }: { columns: number }) {
  return (
    <>
      {Array.from({ length: 4 }).map((_, i) => (
        <TableRow key={i} className="hover:bg-transparent">
          {Array.from({ length: columns }).map((__, j) => (
            <TableCell key={j}>
              <Skeleton className="h-4 w-24" />
            </TableCell>
          ))}
        </TableRow>
      ))}
    </>
  )
}

function KeysEmpty({
  error,
  canIssue,
  onIssue,
}: {
  error: string | null
  canIssue: boolean
  onIssue: () => void
}) {
  return (
    <Empty className="border-0">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <KeyRound />
        </EmptyMedia>
        <EmptyTitle>No API keys yet</EmptyTitle>
        <EmptyDescription>
          {error ? (
            <>
              The runtime returned:{" "}
              <code className="font-mono">{error}</code>.
            </>
          ) : canIssue ? (
            "Issue an API key to let a tenant call the runtime programmatically."
          ) : (
            "Create a tenant first, then come back to issue keys for it."
          )}
        </EmptyDescription>
      </EmptyHeader>
      {canIssue ? (
        <div className="mt-4 flex justify-center">
          <Button size="sm" onClick={onIssue}>
            <Plus />
            Issue key
          </Button>
        </div>
      ) : null}
    </Empty>
  )
}

function IssueKeyDialog({
  open,
  onOpenChange,
  tenants,
  defaultTenant,
  onIssued,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  tenants: Tenant[]
  defaultTenant?: string
  onIssued: (issued: IssuedAPIKey) => void
}) {
  const [submitting, setSubmitting] = React.useState(false)
  const form = useForm<IssueKeyFormValues>({
    resolver: zodResolver(IssueKeyFormSchema),
    defaultValues: {
      tenant_id: defaultTenant ?? tenants[0]?.id ?? "",
      name: "",
      scopes: [],
      expires_at: "",
      budget_max_usd: "",
      rate_limit_rpm: "",
      rate_limit_tpm: "",
    },
    mode: "onBlur",
  })

  React.useEffect(() => {
    if (open) {
      form.reset({
        tenant_id: defaultTenant ?? tenants[0]?.id ?? "",
        name: "",
        scopes: [],
        expires_at: "",
        budget_max_usd: "",
        rate_limit_rpm: "",
        rate_limit_tpm: "",
      })
    }
  }, [open, form, tenants, defaultTenant])

  const handleSubmit = async (values: IssueKeyFormValues) => {
    setSubmitting(true)
    try {
      // Parse the optional numeric fields. Empty string -> undefined
      // (omitted on the wire); invalid -> reject before round-tripping
      // so the customer sees the error immediately.
      const budget = parsePositiveNumber(values.budget_max_usd)
      const rpm = parsePositiveInt(values.rate_limit_rpm)
      const tpm = parsePositiveInt(values.rate_limit_tpm)
      if (
        budget instanceof Error ||
        rpm instanceof Error ||
        tpm instanceof Error
      ) {
        const which =
          budget instanceof Error
            ? "Budget"
            : rpm instanceof Error
              ? "Rate limit RPM"
              : "Rate limit TPM"
        toast.error("Invalid input", { description: `${which} must be a positive number.` })
        setSubmitting(false)
        return
      }
      const issued = await api.admin.keys.issue({
        tenant_id: values.tenant_id,
        name: values.name || undefined,
        scopes: values.scopes,
        // datetime-local omits TZ — leave it as the runtime received it.
        expires_at: values.expires_at || undefined,
        budget_max_usd: budget,
        rate_limit_rpm: rpm,
        rate_limit_tpm: tpm,
      })
      toast.success("API key issued", { description: issued.prefix })
      onIssued(issued)
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to issue key"
      toast.error("Could not issue key", { description: message })
    } finally {
      setSubmitting(false)
    }
  }

  const scopes = form.watch("scopes") ?? []

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Issue API key</DialogTitle>
          <DialogDescription>
            Issued keys are shown once. After the next dialog closes the value
            can no longer be retrieved — copy it before then.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={form.handleSubmit(handleSubmit)}>
          <FieldGroup>
            <Field
              data-invalid={
                form.formState.errors.tenant_id ? true : undefined
              }
            >
              <FieldLabel htmlFor="issue-tenant">Tenant</FieldLabel>
              <Select
                value={form.watch("tenant_id")}
                onValueChange={(v) => form.setValue("tenant_id", v ?? "")}
              >
                <SelectTrigger id="issue-tenant">
                  <SelectValue placeholder="Pick a tenant" />
                </SelectTrigger>
                <SelectContent>
                  {tenants.map((t) => (
                    <SelectItem key={t.id} value={t.id}>
                      {t.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {form.formState.errors.tenant_id ? (
                <FieldDescription>
                  {form.formState.errors.tenant_id.message}
                </FieldDescription>
              ) : null}
            </Field>
            <Field>
              <FieldLabel htmlFor="issue-name">Name</FieldLabel>
              <Input
                id="issue-name"
                placeholder="e.g. production worker"
                {...form.register("name")}
              />
              <FieldDescription>
                Optional. Helps you identify the key in audit logs.
              </FieldDescription>
            </Field>
            <Field>
              <FieldLabel>Scopes</FieldLabel>
              <ToggleGroup
                multiple
                value={scopes}
                onValueChange={(v) => form.setValue("scopes", v)}
                className="flex-wrap justify-start gap-1"
              >
                {SCOPE_OPTIONS.map((s) => (
                  <ToggleGroupItem
                    key={s.value}
                    value={s.value}
                    className="font-mono text-[11px]"
                  >
                    {s.label}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
              <FieldDescription>
                Empty grants no permissions. Pick the minimum needed.
              </FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="issue-expires">Expires at</FieldLabel>
              <Controller
                control={form.control}
                name="expires_at"
                render={({ field }) => (
                  <DateTimePicker
                    value={field.value ?? ""}
                    onChange={field.onChange}
                    placeholder="Never expires"
                  />
                )}
              />
              <FieldDescription>
                Optional. Leave blank for a non-expiring key.
              </FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="issue-budget">Budget max (USD)</FieldLabel>
              <Input
                id="issue-budget"
                type="number"
                inputMode="decimal"
                step="0.01"
                placeholder="Unlimited"
                {...form.register("budget_max_usd")}
              />
              <FieldDescription>
                LiteLLM enforces upstream. Leave blank for no cap.
              </FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="issue-rpm">Rate limit (RPM)</FieldLabel>
              <Input
                id="issue-rpm"
                type="number"
                inputMode="numeric"
                step="1"
                placeholder="Unlimited"
                {...form.register("rate_limit_rpm")}
              />
              <FieldDescription>
                Requests per minute. Empty = no cap.
              </FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="issue-tpm">Rate limit (TPM)</FieldLabel>
              <Input
                id="issue-tpm"
                type="number"
                inputMode="numeric"
                step="1"
                placeholder="Unlimited"
                {...form.register("rate_limit_tpm")}
              />
              <FieldDescription>
                Tokens per minute. Empty = no cap.
              </FieldDescription>
            </Field>
          </FieldGroup>
          <DialogFooter className="mt-4">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => onOpenChange(false)}
              disabled={submitting}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              size="sm"
              disabled={submitting || !form.watch("tenant_id")}
            >
              {submitting ? "Issuing…" : "Issue key"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function RevealKeyDialog({
  issued,
  onOpenChange,
}: {
  issued: IssuedAPIKey | null
  onOpenChange: (open: boolean) => void
}) {
  const handleCopy = async () => {
    if (!issued) return
    try {
      await navigator.clipboard.writeText(issued.value)
      toast.success("Copied to clipboard")
    } catch {
      toast.error("Could not copy to clipboard")
    }
  }

  return (
    <Dialog
      open={issued !== null}
      onOpenChange={(open) => {
        if (!open) onOpenChange(false)
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>API key issued</DialogTitle>
          <DialogDescription>
            <span className="font-mono">{issued?.prefix}…</span>
            {issued?.name ? <> · {issued.name}</> : null}
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <Alert variant="destructive">
            <AlertTriangle />
            <AlertTitle>Store this now. You won&apos;t see it again.</AlertTitle>
            <AlertDescription>
              This is the only time the secret value is shown. Copy it into your
              secrets manager before closing this dialog.
            </AlertDescription>
          </Alert>
          {issued ? (
            <pre className={cn(
              "bg-muted max-h-48 overflow-auto rounded-md p-3",
              "font-mono text-xs break-all whitespace-pre-wrap",
            )}>
              {issued.value}
            </pre>
          ) : null}
        </div>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => onOpenChange(false)}
          >
            Done
          </Button>
          <Button type="button" size="sm" onClick={handleCopy}>
            <Copy />
            Copy value
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function RevokeKeyDialog({
  target,
  onOpenChange,
  onRevoked,
}: {
  target: APIKey | null
  onOpenChange: (open: boolean) => void
  onRevoked: () => void
}) {
  const [submitting, setSubmitting] = React.useState(false)

  const handleRevoke = async () => {
    if (!target) return
    setSubmitting(true)
    try {
      await api.admin.keys.revoke(target.id)
      toast.success("Key revoked", { description: target.prefix })
      onOpenChange(false)
      onRevoked()
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to revoke"
      toast.error("Could not revoke key", { description: message })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AlertDialog
      open={target !== null}
      onOpenChange={(open) => {
        if (!open) onOpenChange(false)
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Revoke API key?</AlertDialogTitle>
          <AlertDialogDescription>
            <span className="font-mono">{target?.prefix}…</span> will stop
            authenticating immediately. Any caller using it will receive 401.
            This cannot be undone.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={submitting}>Cancel</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            onClick={handleRevoke}
            disabled={submitting}
          >
            {submitting ? "Revoking…" : "Revoke"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
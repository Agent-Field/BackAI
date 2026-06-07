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
import { useForm } from "react-hook-form"
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
const IssueKeyFormSchema = z.object({
  tenant_id: z.string().min(1, "Pick a tenant"),
  name: z.string(),
  scopes: z.array(z.string()),
  expires_at: z.string(),
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
      })
    }
  }, [open, form, tenants, defaultTenant])

  const handleSubmit = async (values: IssueKeyFormValues) => {
    setSubmitting(true)
    try {
      const issued = await api.admin.keys.issue({
        tenant_id: values.tenant_id,
        name: values.name || undefined,
        scopes: values.scopes,
        // datetime-local omits TZ — leave it as the runtime received it.
        expires_at: values.expires_at || undefined,
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
              <Input
                id="issue-expires"
                type="datetime-local"
                {...form.register("expires_at")}
              />
              <FieldDescription>
                Optional. Leave blank for a non-expiring key.
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
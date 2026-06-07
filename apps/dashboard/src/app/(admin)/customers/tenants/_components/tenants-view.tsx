"use client"

// TenantsView — list + CRUD over multi-tenant orgs.
//
// Rows are tappable; clicking a row navigates to the Phase 12.1 drilldown
// page at /customers/tenants/<id>. A side-Sheet "Quick view" remains
// available from the row dropdown for operators who don't want to leave
// the list.
//
// All fetches go through `lib/api.ts`. Detail data is loaded lazily when
// the Sheet opens — that keeps the table render path fast.

import * as React from "react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table"
import {
  ExternalLink,
  Eye,
  Globe,
  KeyRound,
  MoreHorizontal,
  Pencil,
  Plus,
  Trash2,
  Users,
} from "lucide-react"
import { toast } from "sonner"

import {
  api,
  ApiError,
  CreateTenantInputSchema,
  UpdateTenantInputSchema,
  type AuditEntry,
  type CreateTenantInput,
  type Tenant,
  type TenantDetail,
  type UpdateTenantInput,
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
import { Badge } from "@/components/ui/badge"
import { Button, buttonVariants } from "@/components/ui/button"
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
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

import { formatRelative } from "../../../operate/_lib/format"

const PLAN_OPTIONS = ["free", "pro", "enterprise"] as const

function planBadgeVariant(
  plan: string,
): "default" | "secondary" | "outline" {
  switch (plan) {
    case "enterprise":
      return "default"
    case "pro":
      return "secondary"
    default:
      return "outline"
  }
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.min(
    Math.floor(Math.log(bytes) / Math.log(1024)),
    units.length - 1,
  )
  const value = bytes / Math.pow(1024, i)
  return `${value.toFixed(value >= 100 || i === 0 ? 0 : 1)} ${units[i]}`
}

type TenantRow = Tenant & { members_count: number }

export function TenantsView() {
  const router = useRouter()
  const [rows, setRows] = React.useState<TenantRow[]>([])
  const [loading, setLoading] = React.useState(true)
  const [error, setError] = React.useState<string | null>(null)
  const [openNew, setOpenNew] = React.useState(false)
  const [editTarget, setEditTarget] = React.useState<Tenant | null>(null)
  const [deleteTarget, setDeleteTarget] = React.useState<Tenant | null>(null)
  const [detailTarget, setDetailTarget] = React.useState<Tenant | null>(null)

  const fetchTenants = React.useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const list = await api.admin.tenants.list()
      // For the members count we ask the backend per tenant via
      // memberships.list — done in parallel so the table fills out quickly.
      const memberships = await Promise.all(
        list.tenants.map((t) =>
          api.admin.memberships
            .list({ tenant: t.id })
            .then((r) => r.memberships.length)
            .catch(() => 0),
        ),
      )
      setRows(
        list.tenants.map((t, i) => ({
          ...t,
          members_count: memberships[i] ?? 0,
        })),
      )
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to load tenants"
      setError(message)
      setRows([])
    } finally {
      setLoading(false)
    }
  }, [])

  React.useEffect(() => {
    void fetchTenants()
  }, [fetchTenants])

  const columns = React.useMemo<ColumnDef<TenantRow>[]>(
    () => [
      {
        id: "slug",
        header: "Slug",
        cell: ({ row }) => (
          <span className="font-mono text-xs">{row.original.slug}</span>
        ),
      },
      {
        id: "name",
        header: "Name",
        cell: ({ row }) => (
          <span className="text-sm">{row.original.name}</span>
        ),
      },
      {
        id: "plan",
        header: "Plan",
        cell: ({ row }) => (
          <Badge variant={planBadgeVariant(row.original.plan)}>
            {row.original.plan}
          </Badge>
        ),
      },
      {
        id: "members",
        header: "Members",
        cell: ({ row }) => (
          <span className="tabular-nums text-xs">
            {row.original.members_count}
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
        id: "actions",
        header: "",
        cell: ({ row }) => (
          <div
            className="flex items-center justify-end gap-1"
            onClick={(e) => e.stopPropagation()}
          >
            <Link
              href={`/customers/tenants/${row.original.id}`}
              aria-label="Open detail"
              className={buttonVariants({ variant: "ghost", size: "icon-sm" })}
            >
              <ExternalLink />
            </Link>
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
                  onClick={() => setDetailTarget(row.original)}
                >
                  <Eye />
                  Quick view
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => setEditTarget(row.original)}>
                  <Pencil />
                  Edit
                </DropdownMenuItem>
                <DropdownMenuItem
                  variant="destructive"
                  onClick={() => setDeleteTarget(row.original)}
                >
                  <Trash2 />
                  Delete
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        ),
      },
    ],
    [],
  )

  const table = useReactTable({
    data: rows,
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <span className="text-muted-foreground text-xs">
          {loading
            ? "Loading…"
            : rows.length === 0
              ? "No tenants"
              : `${rows.length} tenant${rows.length === 1 ? "" : "s"}`}
        </span>
        <Button size="sm" onClick={() => setOpenNew(true)}>
          <Plus />
          New tenant
        </Button>
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
            {loading && rows.length === 0 ? (
              <SkeletonRows columns={columns.length} />
            ) : rows.length === 0 ? (
              <TableRow className="hover:bg-transparent">
                <TableCell colSpan={columns.length} className="p-0">
                  <TenantsEmpty
                    error={error}
                    onCreate={() => setOpenNew(true)}
                  />
                </TableCell>
              </TableRow>
            ) : (
              table.getRowModel().rows.map((row) => (
                <TableRow
                  key={row.id}
                  className="cursor-pointer hover:bg-muted/30"
                  onClick={() =>
                    router.push(`/customers/tenants/${row.original.id}`)
                  }
                >
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

      <NewTenantDialog
        open={openNew}
        onOpenChange={setOpenNew}
        onSaved={fetchTenants}
      />
      <EditTenantDialog
        target={editTarget}
        onOpenChange={(open) => !open && setEditTarget(null)}
        onSaved={fetchTenants}
      />
      <DeleteTenantDialog
        target={deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        onDeleted={fetchTenants}
      />
      <TenantDetailSheet
        tenant={detailTarget}
        onOpenChange={(open) => {
          if (!open) setDetailTarget(null)
        }}
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

function TenantsEmpty({
  error,
  onCreate,
}: {
  error: string | null
  onCreate: () => void
}) {
  return (
    <Empty className="border-0">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <Globe />
        </EmptyMedia>
        <EmptyTitle>No tenants yet</EmptyTitle>
        <EmptyDescription>
          {error ? (
            <>
              The runtime returned:{" "}
              <code className="font-mono">{error}</code>.
            </>
          ) : (
            "Create your first tenant to start isolating data, secrets, and quotas per customer."
          )}
        </EmptyDescription>
      </EmptyHeader>
      <div className="mt-4 flex justify-center">
        <Button size="sm" onClick={onCreate}>
          <Plus />
          New tenant
        </Button>
      </div>
    </Empty>
  )
}

function NewTenantDialog({
  open,
  onOpenChange,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  const [submitting, setSubmitting] = React.useState(false)
  const form = useForm<CreateTenantInput>({
    resolver: zodResolver(CreateTenantInputSchema),
    defaultValues: { slug: "", name: "", plan: "free" },
    mode: "onBlur",
  })

  React.useEffect(() => {
    if (open) form.reset({ slug: "", name: "", plan: "free" })
  }, [open, form])

  const handleSubmit = async (values: CreateTenantInput) => {
    setSubmitting(true)
    try {
      await api.admin.tenants.create(values)
      toast.success("Tenant created", { description: values.slug })
      onOpenChange(false)
      onSaved()
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to create tenant"
      toast.error("Could not create tenant", { description: message })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>New tenant</DialogTitle>
          <DialogDescription>
            Tenants isolate data, secrets, and quotas. The slug becomes part of
            the tenant URL and cannot be changed after creation.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={form.handleSubmit(handleSubmit)}>
          <FieldGroup>
            <Field
              data-invalid={form.formState.errors.slug ? true : undefined}
            >
              <FieldLabel htmlFor="new-tenant-slug">Slug</FieldLabel>
              <Input
                id="new-tenant-slug"
                placeholder="acme"
                autoCapitalize="none"
                autoCorrect="off"
                spellCheck={false}
                aria-invalid={
                  form.formState.errors.slug ? true : undefined
                }
                {...form.register("slug")}
              />
              {form.formState.errors.slug ? (
                <FieldDescription>
                  {form.formState.errors.slug.message}
                </FieldDescription>
              ) : (
                <FieldDescription>
                  Lowercase letters, digits, hyphens. Cannot be changed later.
                </FieldDescription>
              )}
            </Field>
            <Field
              data-invalid={form.formState.errors.name ? true : undefined}
            >
              <FieldLabel htmlFor="new-tenant-name">Name</FieldLabel>
              <Input
                id="new-tenant-name"
                placeholder="Acme Inc."
                aria-invalid={
                  form.formState.errors.name ? true : undefined
                }
                {...form.register("name")}
              />
              {form.formState.errors.name ? (
                <FieldDescription>
                  {form.formState.errors.name.message}
                </FieldDescription>
              ) : null}
            </Field>
            <Field>
              <FieldLabel htmlFor="new-tenant-plan">Plan</FieldLabel>
              <Select
                value={form.watch("plan") ?? "free"}
                onValueChange={(v) => form.setValue("plan", v ?? "free")}
              >
                <SelectTrigger id="new-tenant-plan">
                  <SelectValue placeholder="Pick a plan" />
                </SelectTrigger>
                <SelectContent>
                  {PLAN_OPTIONS.map((p) => (
                    <SelectItem key={p} value={p}>
                      {p}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
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
            <Button type="submit" size="sm" disabled={submitting}>
              {submitting ? "Creating…" : "Create tenant"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function EditTenantDialog({
  target,
  onOpenChange,
  onSaved,
}: {
  target: Tenant | null
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  return (
    <Dialog open={target !== null} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Edit tenant</DialogTitle>
          <DialogDescription>
            Update the name or plan for{" "}
            <span className="font-mono">{target?.slug}</span>.
          </DialogDescription>
        </DialogHeader>
        {target ? (
          <EditTenantBody
            key={target.id}
            target={target}
            onClose={() => onOpenChange(false)}
            onSaved={onSaved}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

function EditTenantBody({
  target,
  onClose,
  onSaved,
}: {
  target: Tenant
  onClose: () => void
  onSaved: () => void
}) {
  const [submitting, setSubmitting] = React.useState(false)
  const form = useForm<UpdateTenantInput>({
    resolver: zodResolver(UpdateTenantInputSchema),
    defaultValues: { name: target.name, plan: target.plan },
    mode: "onBlur",
  })

  const handleSubmit = async (values: UpdateTenantInput) => {
    setSubmitting(true)
    try {
      await api.admin.tenants.update(target.id, {
        name: values.name,
        plan: values.plan,
      })
      toast.success("Tenant updated", { description: target.slug })
      onClose()
      onSaved()
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to update tenant"
      toast.error("Could not update tenant", { description: message })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form onSubmit={form.handleSubmit(handleSubmit)}>
      <FieldGroup>
        <Field data-invalid={form.formState.errors.name ? true : undefined}>
          <FieldLabel htmlFor="edit-tenant-name">Name</FieldLabel>
          <Input
            id="edit-tenant-name"
            aria-invalid={form.formState.errors.name ? true : undefined}
            {...form.register("name")}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="edit-tenant-plan">Plan</FieldLabel>
          <Select
            value={form.watch("plan") ?? target.plan}
            onValueChange={(v) => form.setValue("plan", v ?? target.plan)}
          >
            <SelectTrigger id="edit-tenant-plan">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {PLAN_OPTIONS.map((p) => (
                <SelectItem key={p} value={p}>
                  {p}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
      </FieldGroup>
      <DialogFooter className="mt-4">
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={onClose}
          disabled={submitting}
        >
          Cancel
        </Button>
        <Button type="submit" size="sm" disabled={submitting}>
          {submitting ? "Saving…" : "Save changes"}
        </Button>
      </DialogFooter>
    </form>
  )
}

function DeleteTenantDialog({
  target,
  onOpenChange,
  onDeleted,
}: {
  target: Tenant | null
  onOpenChange: (open: boolean) => void
  onDeleted: () => void
}) {
  const [submitting, setSubmitting] = React.useState(false)

  const handleDelete = async () => {
    if (!target) return
    setSubmitting(true)
    try {
      await api.admin.tenants.delete(target.id)
      toast.success("Tenant deleted", { description: target.slug })
      onOpenChange(false)
      onDeleted()
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to delete tenant"
      toast.error("Could not delete tenant", { description: message })
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
          <AlertDialogTitle>Delete tenant?</AlertDialogTitle>
          <AlertDialogDescription>
            <span className="font-mono">{target?.slug}</span> will be soft-
            deleted. Members lose access immediately. This cannot be undone
            from the dashboard.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={submitting}>Cancel</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            onClick={handleDelete}
            disabled={submitting}
          >
            {submitting ? "Deleting…" : "Delete"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

function TenantDetailSheet({
  tenant,
  onOpenChange,
}: {
  tenant: Tenant | null
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Sheet open={tenant !== null} onOpenChange={onOpenChange}>
      <SheetContent className="flex w-full flex-col gap-0 p-0 sm:max-w-2xl">
        {tenant ? <TenantDetailBody key={tenant.id} tenant={tenant} /> : null}
      </SheetContent>
    </Sheet>
  )
}

type DetailState =
  | { kind: "loading" }
  | { kind: "loaded"; detail: TenantDetail; audit: AuditEntry[] }
  | { kind: "error"; message: string }

function TenantDetailBody({ tenant }: { tenant: Tenant }) {
  const [state, setState] = React.useState<DetailState>({ kind: "loading" })

  React.useEffect(() => {
    let cancelled = false
    void Promise.all([
      api.admin.tenants.get(tenant.id),
      api.admin.audit
        .list({ tenant: tenant.id, limit: 10 })
        .catch(() => ({ entries: [], total: 0, has_more: false })),
    ])
      .then(([detail, audit]) => {
        if (cancelled) return
        setState({
          kind: "loaded",
          detail,
          audit: audit.entries,
        })
      })
      .catch((e: unknown) => {
        if (cancelled) return
        const message =
          e instanceof ApiError
            ? `${e.code}: ${e.message}`
            : e instanceof Error
              ? e.message
              : "Failed to load tenant detail"
        setState({ kind: "error", message })
      })
    return () => {
      cancelled = true
    }
  }, [tenant.id])

  return (
    <>
      <SheetHeader className="border-b">
        <div className="flex flex-col gap-1">
          <SheetTitle className="flex items-center gap-2">
            <span>{tenant.name}</span>
            <Badge variant={planBadgeVariant(tenant.plan)}>{tenant.plan}</Badge>
          </SheetTitle>
          <SheetDescription className="font-mono text-xs">
            {tenant.slug} · {tenant.id}
          </SheetDescription>
        </div>
      </SheetHeader>
      <ScrollArea className="flex-1">
        <div className="flex flex-col gap-6 p-4">
          {state.kind === "loading" ? (
            <DetailSkeleton />
          ) : state.kind === "error" ? (
            <div className="bg-destructive/10 text-destructive rounded-md p-3 font-mono text-xs">
              {state.message}
            </div>
          ) : (
            <DetailLoaded detail={state.detail} audit={state.audit} />
          )}
        </div>
      </ScrollArea>
    </>
  )
}

function DetailSkeleton() {
  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-16 w-full" />
        ))}
      </div>
      <Skeleton className="h-24 w-full" />
      <Skeleton className="h-24 w-full" />
    </div>
  )
}

function DetailLoaded({
  detail,
  audit,
}: {
  detail: TenantDetail
  audit: AuditEntry[]
}) {
  return (
    <>
      <section className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <Stat
          label="Requests 30d"
          value={detail.usage.requests_30d.toLocaleString()}
        />
        <Stat
          label="Cost 30d"
          value={`$${detail.usage.cost_usd_30d.toFixed(2)}`}
        />
        <Stat
          label="Storage"
          value={formatBytes(detail.usage.storage_bytes)}
        />
        <Stat
          label="Secrets"
          value={detail.usage.secrets_count.toLocaleString()}
        />
      </section>

      <section className="flex flex-col gap-2">
        <div className="flex items-center gap-2">
          <Users className="size-3.5 text-muted-foreground" />
          <SectionLabel>Members ({detail.members.length})</SectionLabel>
        </div>
        {detail.members.length === 0 ? (
          <p className="text-muted-foreground text-xs">No members yet.</p>
        ) : (
          <ul className="flex flex-col gap-1.5">
            {detail.members.map((m) => (
              <li
                key={m.user.id}
                className="flex items-center justify-between rounded-md border bg-card px-3 py-2 text-xs"
              >
                <div className="flex flex-col">
                  <span className="font-medium">
                    {m.user.name ?? m.user.email}
                  </span>
                  <span className="text-muted-foreground font-mono">
                    {m.user.email}
                  </span>
                </div>
                <Badge variant="outline">{m.role}</Badge>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="flex flex-col gap-2">
        <div className="flex items-center gap-2">
          <KeyRound className="size-3.5 text-muted-foreground" />
          <SectionLabel>API keys ({detail.api_keys.length})</SectionLabel>
        </div>
        {detail.api_keys.length === 0 ? (
          <p className="text-muted-foreground text-xs">No keys issued.</p>
        ) : (
          <ul className="flex flex-col gap-1.5">
            {detail.api_keys.map((k) => (
              <li
                key={k.id}
                className="flex items-center justify-between rounded-md border bg-card px-3 py-2 text-xs"
              >
                <div className="flex flex-col">
                  <span className="font-mono">{k.prefix}…</span>
                  <span className="text-muted-foreground">
                    {k.name ?? "Unnamed"}
                  </span>
                </div>
                <Badge variant={k.revoked_at ? "destructive" : "secondary"}>
                  {k.revoked_at ? "Revoked" : "Active"}
                </Badge>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="flex flex-col gap-2">
        <SectionLabel>Recent audit ({audit.length})</SectionLabel>
        {audit.length === 0 ? (
          <p className="text-muted-foreground text-xs">
            No audit entries for this tenant yet.
          </p>
        ) : (
          <ul className="flex flex-col gap-1.5">
            {audit.map((entry) => (
              <li
                key={entry.id}
                className="flex items-center justify-between rounded-md border bg-card px-3 py-2 text-xs"
              >
                <div className="flex flex-col">
                  <Badge variant="outline" className="w-fit font-mono">
                    {entry.action}
                  </Badge>
                  <span className="text-muted-foreground mt-1 text-[11px]">
                    {entry.resource_type
                      ? `${entry.resource_type}${entry.resource_id ? `:${entry.resource_id}` : ""}`
                      : "—"}
                  </span>
                </div>
                <span className="text-muted-foreground font-mono">
                  {formatRelative(entry.occurred_at)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-0.5 rounded-md border bg-card p-2">
      <span className="text-muted-foreground text-[10px] font-medium uppercase tracking-wide">
        {label}
      </span>
      <span className="font-mono text-xs">{value}</span>
    </div>
  )
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <span className="text-muted-foreground text-[10px] font-medium uppercase tracking-wide">
      {children}
    </span>
  )
}

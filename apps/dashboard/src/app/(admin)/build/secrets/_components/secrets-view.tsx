"use client"

// SecretsView — list + CRUD over the secrets vault.
//
// Strict rules:
//   - The list endpoint never returns values.
//   - Values are only displayed via the Reveal dialog, on explicit click.
//   - Copy-to-clipboard is the only way values leave the dialog.

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
  Copy,
  Eye,
  Lock,
  MoreHorizontal,
  Plus,
  RotateCw,
  Trash2,
} from "lucide-react"
import { toast } from "sonner"

import {
  api,
  ApiError,
  type SecretMetadata,
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

const KEY_PATTERN = /^[A-Z0-9_]+$/

const NewSecretSchema = z.object({
  key: z
    .string()
    .min(1, "Required")
    .max(120, "Too long")
    .refine((v) => KEY_PATTERN.test(v), {
      message: "Use UPPER_SNAKE_CASE (A–Z, 0–9, _)",
    }),
  value: z.string().min(1, "Required"),
  description: z.string().optional(),
  rotate_after: z.string().optional(),
})
type NewSecretValues = z.infer<typeof NewSecretSchema>

const RotateSchema = z.object({
  value: z.string().min(1, "Required"),
})
type RotateValues = z.infer<typeof RotateSchema>

export function SecretsView() {
  const [secrets, setSecrets] = React.useState<SecretMetadata[]>([])
  const [loading, setLoading] = React.useState(true)
  const [error, setError] = React.useState<string | null>(null)
  const [openNew, setOpenNew] = React.useState(false)
  const [revealTarget, setRevealTarget] = React.useState<SecretMetadata | null>(
    null,
  )
  const [rotateTarget, setRotateTarget] = React.useState<SecretMetadata | null>(
    null,
  )
  const [deleteTarget, setDeleteTarget] = React.useState<SecretMetadata | null>(
    null,
  )

  const fetchSecrets = React.useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const list = await api.secrets.list()
      setSecrets(list.secrets)
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to load secrets"
      setError(message)
      setSecrets([])
    } finally {
      setLoading(false)
    }
  }, [])

  React.useEffect(() => {
    void fetchSecrets()
  }, [fetchSecrets])

  const columns = React.useMemo<ColumnDef<SecretMetadata>[]>(
    () => [
      {
        id: "key",
        header: "Key",
        cell: ({ row }) => (
          <span className="font-mono text-xs">{row.original.key}</span>
        ),
      },
      {
        id: "description",
        header: "Description",
        cell: ({ row }) => (
          <span className="text-muted-foreground text-xs">
            {row.original.description || "—"}
          </span>
        ),
      },
      {
        id: "rotate_after",
        header: "Rotate after",
        cell: ({ row }) => (
          <span className="text-muted-foreground text-xs">
            {row.original.rotate_after || "—"}
          </span>
        ),
      },
      {
        id: "updated_at",
        header: "Updated",
        cell: ({ row }) => (
          <span className="text-muted-foreground text-xs">
            {formatRelative(row.original.updated_at)}
          </span>
        ),
      },
      {
        id: "actions",
        header: "",
        cell: ({ row }) => (
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
                <DropdownMenuItem onClick={() => setRevealTarget(row.original)}>
                  <Eye />
                  Reveal value
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => setRotateTarget(row.original)}>
                  <RotateCw />
                  Rotate
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
    data: secrets,
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <span className="text-muted-foreground text-xs">
          {loading
            ? "Loading…"
            : secrets.length === 0
              ? "No secrets stored"
              : `${secrets.length} secret${secrets.length === 1 ? "" : "s"}`}
        </span>
        <Button size="sm" onClick={() => setOpenNew(true)}>
          <Plus />
          New secret
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
            {loading && secrets.length === 0 ? (
              <SkeletonRows columns={columns.length} />
            ) : secrets.length === 0 ? (
              <TableRow className="hover:bg-transparent">
                <TableCell colSpan={columns.length} className="p-0">
                  <SecretsEmpty
                    error={error}
                    onCreate={() => setOpenNew(true)}
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

      <NewSecretDialog
        open={openNew}
        onOpenChange={setOpenNew}
        onSaved={fetchSecrets}
      />
      <RevealSecretDialog
        target={revealTarget}
        onOpenChange={(open) => !open && setRevealTarget(null)}
      />
      <RotateSecretDialog
        target={rotateTarget}
        onOpenChange={(open) => !open && setRotateTarget(null)}
        onSaved={fetchSecrets}
      />
      <DeleteSecretDialog
        target={deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        onDeleted={fetchSecrets}
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

function SecretsEmpty({
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
          <Lock />
        </EmptyMedia>
        <EmptyTitle>No secrets yet</EmptyTitle>
        <EmptyDescription>
          {error ? (
            <>
              The runtime returned:{" "}
              <code className="font-mono">{error}</code>. Once the vault is
              available you can add secrets here.
            </>
          ) : (
            "Store API keys, tokens, and other credentials here. Values are encrypted at rest."
          )}
        </EmptyDescription>
      </EmptyHeader>
      <div className="mt-4 flex justify-center">
        <Button size="sm" onClick={onCreate}>
          <Plus />
          New secret
        </Button>
      </div>
    </Empty>
  )
}

function NewSecretDialog({
  open,
  onOpenChange,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  const [submitting, setSubmitting] = React.useState(false)
  const form = useForm<NewSecretValues>({
    resolver: zodResolver(NewSecretSchema),
    defaultValues: { key: "", value: "", description: "", rotate_after: "" },
    mode: "onBlur",
  })

  React.useEffect(() => {
    if (open) {
      form.reset({ key: "", value: "", description: "", rotate_after: "" })
    }
  }, [open, form])

  const handleSubmit = async (values: NewSecretValues) => {
    setSubmitting(true)
    try {
      await api.secrets.put(values.key, {
        value: values.value,
        description: values.description || undefined,
        rotate_after: values.rotate_after || undefined,
      })
      toast.success("Secret saved", {
        description: `${values.key} is in the vault.`,
      })
      onOpenChange(false)
      onSaved()
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to save secret"
      toast.error("Could not save secret", { description: message })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>New secret</DialogTitle>
          <DialogDescription>
            Values are stored encrypted. The key is the identifier your
            handlers will reference.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={form.handleSubmit(handleSubmit)}>
          <FieldGroup>
            <Field
              data-invalid={form.formState.errors.key ? true : undefined}
            >
              <FieldLabel htmlFor="new-secret-key">Key</FieldLabel>
              <Input
                id="new-secret-key"
                placeholder="OPENAI_API_KEY"
                autoCapitalize="characters"
                autoCorrect="off"
                spellCheck={false}
                aria-invalid={form.formState.errors.key ? true : undefined}
                {...form.register("key")}
              />
              {form.formState.errors.key ? (
                <FieldDescription>
                  {form.formState.errors.key.message}
                </FieldDescription>
              ) : (
                <FieldDescription>
                  UPPER_SNAKE_CASE. Cannot be changed after creation.
                </FieldDescription>
              )}
            </Field>
            <Field
              data-invalid={form.formState.errors.value ? true : undefined}
            >
              <FieldLabel htmlFor="new-secret-value">Value</FieldLabel>
              <Input
                id="new-secret-value"
                type="password"
                autoComplete="off"
                aria-invalid={form.formState.errors.value ? true : undefined}
                {...form.register("value")}
              />
              {form.formState.errors.value ? (
                <FieldDescription>
                  {form.formState.errors.value.message}
                </FieldDescription>
              ) : (
                <FieldDescription>
                  Encrypted at rest. Only revealed on explicit click.
                </FieldDescription>
              )}
            </Field>
            <Field>
              <FieldLabel htmlFor="new-secret-description">
                Description
              </FieldLabel>
              <Input
                id="new-secret-description"
                placeholder="What this is used for"
                {...form.register("description")}
              />
              <FieldDescription>Optional.</FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="new-secret-rotate">Rotate after</FieldLabel>
              <Input
                id="new-secret-rotate"
                placeholder="e.g. 90d"
                {...form.register("rotate_after")}
              />
              <FieldDescription>
                Optional rotation reminder window.
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
            <Button type="submit" size="sm" disabled={submitting}>
              {submitting ? "Saving…" : "Save secret"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function RevealSecretDialog({
  target,
  onOpenChange,
}: {
  target: SecretMetadata | null
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Dialog open={target !== null} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            <span className="font-mono">{target?.key}</span>
          </DialogTitle>
          <DialogDescription>
            Treat the value like a credential. Close this dialog as soon as
            you&apos;re done.
          </DialogDescription>
        </DialogHeader>
        {target ? (
          <RevealSecretBody
            key={target.key}
            target={target}
            onClose={() => onOpenChange(false)}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

type RevealState =
  | { kind: "loading" }
  | { kind: "loaded"; value: string }
  | { kind: "error"; message: string }

function RevealSecretBody({
  target,
  onClose,
}: {
  target: SecretMetadata
  onClose: () => void
}) {
  const [state, setState] = React.useState<RevealState>({ kind: "loading" })

  React.useEffect(() => {
    let cancelled = false
    void api.secrets
      .reveal(target.key)
      .then((res) => {
        if (cancelled) return
        setState({ kind: "loaded", value: res.value })
      })
      .catch((e: unknown) => {
        if (cancelled) return
        const message =
          e instanceof ApiError
            ? `${e.code}: ${e.message}`
            : e instanceof Error
              ? e.message
              : "Failed to reveal"
        setState({ kind: "error", message })
      })
    return () => {
      cancelled = true
    }
  }, [target.key])

  const handleCopy = async () => {
    if (state.kind !== "loaded") return
    try {
      await navigator.clipboard.writeText(state.value)
      toast.success("Copied to clipboard")
    } catch {
      toast.error("Could not copy to clipboard")
    }
  }

  return (
    <>
      <div className="flex flex-col gap-2">
        {state.kind === "loading" ? (
          <Skeleton className="h-9 w-full" />
        ) : state.kind === "error" ? (
          <div className="bg-destructive/10 text-destructive rounded-md p-3 font-mono text-xs">
            {state.message}
          </div>
        ) : (
          <pre className="bg-muted max-h-48 overflow-auto rounded-md p-3 font-mono text-xs break-all whitespace-pre-wrap">
            {state.value}
          </pre>
        )}
      </div>
      <DialogFooter>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={onClose}
        >
          Close
        </Button>
        <Button
          type="button"
          size="sm"
          onClick={handleCopy}
          disabled={state.kind !== "loaded"}
        >
          <Copy />
          Copy
        </Button>
      </DialogFooter>
    </>
  )
}

function RotateSecretDialog({
  target,
  onOpenChange,
  onSaved,
}: {
  target: SecretMetadata | null
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  return (
    <Dialog open={target !== null} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Rotate secret</DialogTitle>
          <DialogDescription>
            Replace the stored value for{" "}
            <span className="font-mono">{target?.key}</span>. Existing readers
            will pick up the new value on next fetch.
          </DialogDescription>
        </DialogHeader>
        {target ? (
          <RotateSecretBody
            key={target.key}
            target={target}
            onClose={() => onOpenChange(false)}
            onSaved={onSaved}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

function RotateSecretBody({
  target,
  onClose,
  onSaved,
}: {
  target: SecretMetadata
  onClose: () => void
  onSaved: () => void
}) {
  const [submitting, setSubmitting] = React.useState(false)
  const form = useForm<RotateValues>({
    resolver: zodResolver(RotateSchema),
    defaultValues: { value: "" },
    mode: "onBlur",
  })

  const handleSubmit = async (values: RotateValues) => {
    setSubmitting(true)
    try {
      await api.secrets.rotate(target.key, { value: values.value })
      toast.success("Secret rotated", { description: target.key })
      onClose()
      onSaved()
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to rotate secret"
      toast.error("Could not rotate", { description: message })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form onSubmit={form.handleSubmit(handleSubmit)}>
      <FieldGroup>
        <Field data-invalid={form.formState.errors.value ? true : undefined}>
          <FieldLabel htmlFor="rotate-value">New value</FieldLabel>
          <Input
            id="rotate-value"
            type="password"
            autoComplete="off"
            aria-invalid={form.formState.errors.value ? true : undefined}
            {...form.register("value")}
          />
          {form.formState.errors.value ? (
            <FieldDescription>
              {form.formState.errors.value.message}
            </FieldDescription>
          ) : null}
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
          {submitting ? "Rotating…" : "Rotate"}
        </Button>
      </DialogFooter>
    </form>
  )
}

function DeleteSecretDialog({
  target,
  onOpenChange,
  onDeleted,
}: {
  target: SecretMetadata | null
  onOpenChange: (open: boolean) => void
  onDeleted: () => void
}) {
  const [submitting, setSubmitting] = React.useState(false)

  const handleDelete = async () => {
    if (!target) return
    setSubmitting(true)
    try {
      await api.secrets.delete(target.key)
      toast.success("Secret deleted", { description: target.key })
      onOpenChange(false)
      onDeleted()
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to delete"
      toast.error("Could not delete secret", { description: message })
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
          <AlertDialogTitle>Delete secret?</AlertDialogTitle>
          <AlertDialogDescription>
            <span className="font-mono">{target?.key}</span> will be removed
            from the vault. Any handler that reads it will fail until you
            re-create it. This cannot be undone.
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

"use client"

// MemoryTab — browse, add, view, delete, and vector-search memory entries.
//
// Memory is scoped (global/tenant/agent/session/run). Listing supports a
// debounced prefix filter so operators can quickly find a key without
// scrolling. Vector search lives in an expandable card at the bottom so
// it doesn't dominate the page when not in use.

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
  Brain,
  ChevronDown,
  Eye,
  MoreHorizontal,
  Plus,
  RotateCw,
  Search,
  Sparkles,
  Trash2,
} from "lucide-react"
import { toast } from "sonner"

import {
  api,
  ApiError,
  type MemoryEntry,
  type MemoryScope,
  type MemorySearchHit,
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
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
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
import { Progress } from "@/components/ui/progress"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Slider } from "@/components/ui/slider"
import { Switch } from "@/components/ui/switch"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import { cn } from "@/lib/utils"

import { formatRelative } from "../../../operate/_lib/format"
import { stringifyValue, truncate } from "../_lib/format"

const SCOPE_OPTIONS: { value: MemoryScope | "all"; label: string }[] = [
  { value: "all", label: "All scopes" },
  { value: "global", label: "Global" },
  { value: "tenant", label: "Tenant" },
  { value: "agent", label: "Agent" },
  { value: "session", label: "Session" },
  { value: "run", label: "Run" },
]

export function MemoryTab() {
  const [scope, setScope] = React.useState<MemoryScope | "all">("all")
  const [scopeId, setScopeId] = React.useState("")
  const [prefix, setPrefix] = React.useState("")
  const [debouncedScopeId, setDebouncedScopeId] = React.useState("")
  const [debouncedPrefix, setDebouncedPrefix] = React.useState("")

  const [entries, setEntries] = React.useState<MemoryEntry[]>([])
  const [loading, setLoading] = React.useState(true)
  const [error, setError] = React.useState<string | null>(null)

  const [viewTarget, setViewTarget] = React.useState<MemoryEntry | null>(null)
  const [deleteTarget, setDeleteTarget] = React.useState<MemoryEntry | null>(
    null,
  )
  const [openNew, setOpenNew] = React.useState(false)

  React.useEffect(() => {
    const t = setTimeout(() => setDebouncedScopeId(scopeId.trim()), 300)
    return () => clearTimeout(t)
  }, [scopeId])
  React.useEffect(() => {
    const t = setTimeout(() => setDebouncedPrefix(prefix.trim()), 300)
    return () => clearTimeout(t)
  }, [prefix])

  const fetchEntries = React.useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const list = await api.memory.list({
        scope: scope === "all" ? undefined : scope,
        scope_id: debouncedScopeId || undefined,
        prefix: debouncedPrefix || undefined,
      })
      setEntries(list.entries)
    } catch (e) {
      setError(
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to load memory",
      )
      setEntries([])
    } finally {
      setLoading(false)
    }
  }, [scope, debouncedScopeId, debouncedPrefix])

  React.useEffect(() => {
    void fetchEntries()
  }, [fetchEntries])

  const columns = React.useMemo<ColumnDef<MemoryEntry>[]>(
    () => [
      {
        id: "key",
        header: "Key",
        cell: ({ row }) => (
          <span className="font-mono text-xs">{row.original.key}</span>
        ),
      },
      {
        id: "scope",
        header: "Scope",
        cell: ({ row }) => (
          <div className="flex flex-col gap-0.5">
            <Badge variant="outline" className="w-fit font-mono">
              {row.original.scope}
            </Badge>
            {row.original.scope_id ? (
              <span
                className="text-muted-foreground max-w-[140px] truncate font-mono text-[10px]"
                title={row.original.scope_id}
              >
                {row.original.scope_id}
              </span>
            ) : null}
          </div>
        ),
      },
      {
        id: "value",
        header: "Value preview",
        cell: ({ row }) => {
          const text = stringifyValue(row.original.value)
          return (
            <span className="text-muted-foreground font-mono text-xs" title={text}>
              {truncate(text, 80)}
            </span>
          )
        },
      },
      {
        id: "embedding",
        header: "Embedding",
        cell: ({ row }) =>
          row.original.has_embedding ? (
            <Badge variant="secondary">
              <Sparkles />
              embedded
            </Badge>
          ) : (
            <Badge variant="ghost" className="text-muted-foreground">
              none
            </Badge>
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
                <DropdownMenuItem onClick={() => setViewTarget(row.original)}>
                  <Eye />
                  View
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

  const tableInstance = useReactTable({
    data: entries,
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  return (
    <div className="flex flex-col gap-4">
      <FiltersBar
        scope={scope}
        scopeId={scopeId}
        prefix={prefix}
        loading={loading}
        onScopeChange={setScope}
        onScopeIdChange={setScopeId}
        onPrefixChange={setPrefix}
        onRefresh={() => void fetchEntries()}
        onCreate={() => setOpenNew(true)}
      />

      <div className="bg-card rounded-xl border">
        <Table>
          <TableHeader>
            {tableInstance.getHeaderGroups().map((hg) => (
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
            {loading && entries.length === 0 ? (
              <SkeletonRows columns={columns.length} />
            ) : entries.length === 0 ? (
              <TableRow className="hover:bg-transparent">
                <TableCell colSpan={columns.length} className="p-0">
                  <MemoryEmpty error={error} onCreate={() => setOpenNew(true)} />
                </TableCell>
              </TableRow>
            ) : (
              tableInstance.getRowModel().rows.map((row) => (
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

      <VectorSearchSection scope={scope} scopeId={debouncedScopeId} />

      <ViewMemoryDialog
        target={viewTarget}
        onOpenChange={(open) => !open && setViewTarget(null)}
      />
      <DeleteMemoryDialog
        target={deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        onDeleted={fetchEntries}
      />
      <NewMemoryDialog
        open={openNew}
        onOpenChange={setOpenNew}
        onSaved={fetchEntries}
      />
    </div>
  )
}

function SkeletonRows({ columns }: { columns: number }) {
  return (
    <>
      {Array.from({ length: 5 }).map((_, i) => (
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

function MemoryEmpty({
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
          <Brain />
        </EmptyMedia>
        <EmptyTitle>
          {error ? "Couldn't load memory" : "No memory entries"}
        </EmptyTitle>
        <EmptyDescription>
          {error ? (
            <span className="font-mono text-xs">{error}</span>
          ) : (
            "Memory is the durable key-value store agents use for facts, summaries, and embeddings."
          )}
        </EmptyDescription>
      </EmptyHeader>
      {!error ? (
        <div className="mt-4 flex justify-center">
          <Button size="sm" onClick={onCreate}>
            <Plus />
            Add memory
          </Button>
        </div>
      ) : null}
    </Empty>
  )
}

type FiltersBarProps = {
  scope: MemoryScope | "all"
  scopeId: string
  prefix: string
  loading: boolean
  onScopeChange: (s: MemoryScope | "all") => void
  onScopeIdChange: (v: string) => void
  onPrefixChange: (v: string) => void
  onRefresh: () => void
  onCreate: () => void
}

function FiltersBar({
  scope,
  scopeId,
  prefix,
  loading,
  onScopeChange,
  onScopeIdChange,
  onPrefixChange,
  onRefresh,
  onCreate,
}: FiltersBarProps) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Select
        value={scope}
        onValueChange={(v) => onScopeChange(v as MemoryScope | "all")}
      >
        <SelectTrigger className="w-40">
          <SelectValue placeholder="Scope" />
        </SelectTrigger>
        <SelectContent>
          {SCOPE_OPTIONS.map((s) => (
            <SelectItem key={s.value} value={s.value}>
              {s.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Input
        placeholder="Scope ID…"
        value={scopeId}
        onChange={(e) => onScopeIdChange(e.target.value)}
        className="w-48"
      />
      <Input
        placeholder="Key prefix…"
        value={prefix}
        onChange={(e) => onPrefixChange(e.target.value)}
        className="w-48 font-mono"
      />
      <div className="ml-auto flex items-center gap-2">
        <Button
          variant="outline"
          size="sm"
          onClick={onRefresh}
          disabled={loading}
        >
          <RotateCw className={cn(loading && "animate-spin")} />
          Refresh
        </Button>
        <Button size="sm" onClick={onCreate}>
          <Plus />
          Add memory
        </Button>
      </div>
    </div>
  )
}

// ─── View ────────────────────────────────────────────────────────────────

function ViewMemoryDialog({
  target,
  onOpenChange,
}: {
  target: MemoryEntry | null
  onOpenChange: (open: boolean) => void
}) {
  let formatted = ""
  if (target) {
    try {
      formatted = JSON.stringify(target.value, null, 2)
    } catch {
      formatted = String(target.value)
    }
  }
  let metadataFormatted = ""
  if (target) {
    try {
      metadataFormatted = JSON.stringify(target.metadata, null, 2)
    } catch {
      metadataFormatted = String(target.metadata)
    }
  }
  return (
    <Dialog open={target !== null} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="font-mono">{target?.key}</DialogTitle>
          <DialogDescription className="flex flex-wrap items-center gap-1.5">
            <Badge variant="outline" className="font-mono">
              {target?.scope}
            </Badge>
            {target?.scope_id ? (
              <Badge variant="ghost" className="font-mono">
                {target.scope_id}
              </Badge>
            ) : null}
            {target?.has_embedding ? (
              <Badge variant="secondary">
                <Sparkles />
                embedded
              </Badge>
            ) : null}
          </DialogDescription>
        </DialogHeader>
        {target ? (
          <div className="flex flex-col gap-3">
            <div className="flex flex-col gap-1">
              <span className="text-muted-foreground text-[10px] font-medium uppercase tracking-wide">
                Value
              </span>
              <ScrollArea className="max-h-72">
                <pre className="bg-muted/40 overflow-auto rounded-md p-3 font-mono text-xs">
                  {formatted}
                </pre>
              </ScrollArea>
            </div>
            <div className="flex flex-col gap-1">
              <span className="text-muted-foreground text-[10px] font-medium uppercase tracking-wide">
                Metadata
              </span>
              <pre className="bg-muted/40 max-h-32 overflow-auto rounded-md p-3 font-mono text-xs">
                {metadataFormatted}
              </pre>
            </div>
            <div className="text-muted-foreground flex gap-4 text-xs">
              <span>Created: {formatRelative(target.created_at)}</span>
              <span>Updated: {formatRelative(target.updated_at)}</span>
            </div>
          </div>
        ) : null}
        <DialogFooter>
          <Button
            variant="outline"
            size="sm"
            onClick={() => onOpenChange(false)}
          >
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ─── Delete ──────────────────────────────────────────────────────────────

function DeleteMemoryDialog({
  target,
  onOpenChange,
  onDeleted,
}: {
  target: MemoryEntry | null
  onOpenChange: (open: boolean) => void
  onDeleted: () => void
}) {
  const [submitting, setSubmitting] = React.useState(false)

  const handleDelete = async () => {
    if (!target) return
    setSubmitting(true)
    try {
      await api.memory.delete(
        target.scope,
        target.key,
        target.scope_id ?? undefined,
      )
      toast.success("Memory deleted", { description: target.key })
      onOpenChange(false)
      onDeleted()
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to delete"
      toast.error("Could not delete memory", { description: message })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AlertDialog
      open={target !== null}
      onOpenChange={(open) => !open && onOpenChange(false)}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete memory entry?</AlertDialogTitle>
          <AlertDialogDescription>
            <span className="font-mono">{target?.key}</span> in scope{" "}
            <span className="font-mono">{target?.scope}</span> will be removed.
            This cannot be undone.
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

// ─── Add ─────────────────────────────────────────────────────────────────

const SCOPE_VALUES = ["global", "tenant", "agent", "session", "run"] as const

const NewMemorySchema = z.object({
  scope: z.enum(SCOPE_VALUES),
  scope_id: z.string().optional(),
  key: z.string().min(1, "Required").max(255, "Too long"),
  value: z
    .string()
    .min(1, "Required")
    .refine(
      (v) => {
        try {
          JSON.parse(v)
          return true
        } catch {
          return false
        }
      },
      { message: "Must be valid JSON" },
    ),
  metadata: z
    .string()
    .optional()
    .refine(
      (v) => {
        if (!v || !v.trim()) return true
        try {
          const parsed = JSON.parse(v)
          return (
            typeof parsed === "object" &&
            parsed !== null &&
            !Array.isArray(parsed)
          )
        } catch {
          return false
        }
      },
      { message: "Must be a JSON object" },
    ),
  embed: z.boolean(),
})
type NewMemoryValues = z.infer<typeof NewMemorySchema>

function NewMemoryDialog({
  open,
  onOpenChange,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  const [submitting, setSubmitting] = React.useState(false)
  const form = useForm<NewMemoryValues>({
    resolver: zodResolver(NewMemorySchema),
    defaultValues: {
      scope: "tenant",
      scope_id: "",
      key: "",
      value: "",
      metadata: "",
      embed: false,
    },
    mode: "onBlur",
  })

  React.useEffect(() => {
    if (open) {
      form.reset({
        scope: "tenant",
        scope_id: "",
        key: "",
        value: "",
        metadata: "",
        embed: false,
      })
    }
  }, [open, form])

  const handleSubmit = async (values: NewMemoryValues) => {
    setSubmitting(true)
    try {
      const value = JSON.parse(values.value)
      const metadata = values.metadata?.trim()
        ? (JSON.parse(values.metadata) as Record<string, unknown>)
        : undefined
      await api.memory.put({
        scope: values.scope,
        scope_id: values.scope_id?.trim() || undefined,
        key: values.key,
        value,
        metadata,
        embed: values.embed,
      })
      toast.success("Memory saved", { description: values.key })
      onOpenChange(false)
      onSaved()
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to save memory"
      toast.error("Could not save memory", { description: message })
    } finally {
      setSubmitting(false)
    }
  }

  const scope = form.watch("scope")
  const embed = form.watch("embed")

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Add memory entry</DialogTitle>
          <DialogDescription>
            Memory entries are scoped key/value pairs that agents and reasoners
            can read and write.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={form.handleSubmit(handleSubmit)}>
          <FieldGroup>
            <div className="grid grid-cols-2 gap-3">
              <Field
                data-invalid={form.formState.errors.scope ? true : undefined}
              >
                <FieldLabel htmlFor="new-memory-scope">Scope</FieldLabel>
                <Select
                  value={scope}
                  onValueChange={(v) =>
                    form.setValue("scope", v as MemoryScope, {
                      shouldValidate: true,
                    })
                  }
                >
                  <SelectTrigger id="new-memory-scope">
                    <SelectValue placeholder="Scope" />
                  </SelectTrigger>
                  <SelectContent>
                    {SCOPE_VALUES.map((s) => (
                      <SelectItem key={s} value={s}>
                        {s}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <Field>
                <FieldLabel htmlFor="new-memory-scope-id">Scope ID</FieldLabel>
                <Input
                  id="new-memory-scope-id"
                  placeholder={
                    scope === "global" ? "Not required for global" : "Optional"
                  }
                  disabled={scope === "global"}
                  {...form.register("scope_id")}
                />
              </Field>
            </div>
            <Field
              data-invalid={form.formState.errors.key ? true : undefined}
            >
              <FieldLabel htmlFor="new-memory-key">Key</FieldLabel>
              <Input
                id="new-memory-key"
                placeholder="user.preferences.theme"
                spellCheck={false}
                autoCapitalize="off"
                autoCorrect="off"
                className="font-mono"
                aria-invalid={
                  form.formState.errors.key ? true : undefined
                }
                {...form.register("key")}
              />
              {form.formState.errors.key ? (
                <FieldDescription>
                  {form.formState.errors.key.message}
                </FieldDescription>
              ) : null}
            </Field>
            <Field
              data-invalid={form.formState.errors.value ? true : undefined}
            >
              <FieldLabel htmlFor="new-memory-value">Value (JSON)</FieldLabel>
              <Textarea
                id="new-memory-value"
                placeholder={'{"dark": true}'}
                spellCheck={false}
                rows={5}
                className="font-mono text-xs"
                aria-invalid={
                  form.formState.errors.value ? true : undefined
                }
                {...form.register("value")}
              />
              {form.formState.errors.value ? (
                <FieldDescription>
                  {form.formState.errors.value.message}
                </FieldDescription>
              ) : (
                <FieldDescription>
                  Any valid JSON value: object, array, string, number, boolean,
                  or null.
                </FieldDescription>
              )}
            </Field>
            <Field
              data-invalid={form.formState.errors.metadata ? true : undefined}
            >
              <FieldLabel htmlFor="new-memory-metadata">
                Metadata (JSON object)
              </FieldLabel>
              <Textarea
                id="new-memory-metadata"
                placeholder='{"source": "user"}'
                spellCheck={false}
                rows={3}
                className="font-mono text-xs"
                aria-invalid={
                  form.formState.errors.metadata ? true : undefined
                }
                {...form.register("metadata")}
              />
              {form.formState.errors.metadata ? (
                <FieldDescription>
                  {form.formState.errors.metadata.message}
                </FieldDescription>
              ) : (
                <FieldDescription>Optional.</FieldDescription>
              )}
            </Field>
            <Field orientation="horizontal">
              <Switch
                id="new-memory-embed"
                checked={embed}
                onCheckedChange={(v) => form.setValue("embed", v)}
              />
              <FieldLabel htmlFor="new-memory-embed">
                Generate embedding
              </FieldLabel>
            </Field>
            <FieldDescription>
              When enabled, the value is embedded for vector search.
            </FieldDescription>
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
              {submitting ? "Saving…" : "Save"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// ─── Vector search ───────────────────────────────────────────────────────

function VectorSearchSection({
  scope,
  scopeId,
}: {
  scope: MemoryScope | "all"
  scopeId: string
}) {
  const [open, setOpen] = React.useState(false)
  const [query, setQuery] = React.useState("")
  const [topK, setTopK] = React.useState(10)
  const [threshold, setThreshold] = React.useState(0.5)
  const [running, setRunning] = React.useState(false)
  const [hits, setHits] = React.useState<MemorySearchHit[] | null>(null)
  const [duration, setDuration] = React.useState<number | null>(null)
  const [error, setError] = React.useState<string | null>(null)

  const handleSearch = async () => {
    if (!query.trim()) {
      toast.warning("Enter a search query")
      return
    }
    setRunning(true)
    setError(null)
    try {
      const result = await api.memory.search({
        query: query.trim(),
        scope: scope === "all" ? undefined : scope,
        scope_id: scopeId || undefined,
        top_k: topK,
        threshold,
      })
      setHits(result.hits)
      setDuration(result.duration_ms)
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to search"
      setError(message)
      setHits([])
    } finally {
      setRunning(false)
    }
  }

  return (
    <Card className="gap-0">
      <Collapsible open={open} onOpenChange={setOpen}>
        <CardHeader className={cn(open && "border-b pb-4")}>
          <CollapsibleTrigger
            render={
              <button
                type="button"
                className="flex w-full items-center justify-between gap-2 text-left"
              >
                <div className="flex flex-col gap-0.5">
                  <CardTitle className="flex items-center gap-2">
                    <Search className="size-4" />
                    Vector search
                  </CardTitle>
                  <CardDescription>
                    Semantic lookup over memory entries with embeddings.
                  </CardDescription>
                </div>
                <ChevronDown
                  className={cn(
                    "size-4 transition-transform",
                    open && "rotate-180",
                  )}
                />
              </button>
            }
          />
        </CardHeader>
        <CollapsibleContent>
          <CardContent className="flex flex-col gap-4 pt-4">
            <Field>
              <FieldLabel htmlFor="vector-query">Query</FieldLabel>
              <Textarea
                id="vector-query"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="What is the user's preferred color scheme?"
                rows={2}
                className="text-sm"
              />
            </Field>
            <div className="grid grid-cols-2 gap-4">
              <Field>
                <FieldLabel htmlFor="vector-topk">
                  Top K
                  <Badge
                    variant="outline"
                    className="ml-1 tabular-nums font-mono"
                  >
                    {topK}
                  </Badge>
                </FieldLabel>
                <Slider
                  id="vector-topk"
                  value={[topK]}
                  onValueChange={(v) => {
                    if (Array.isArray(v) && typeof v[0] === "number") {
                      setTopK(v[0])
                    }
                  }}
                  min={1}
                  max={50}
                  step={1}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="vector-threshold">
                  Threshold
                  <Badge
                    variant="outline"
                    className="ml-1 tabular-nums font-mono"
                  >
                    {threshold.toFixed(2)}
                  </Badge>
                </FieldLabel>
                <Slider
                  id="vector-threshold"
                  value={[threshold]}
                  onValueChange={(v) => {
                    if (Array.isArray(v) && typeof v[0] === "number") {
                      setThreshold(v[0])
                    }
                  }}
                  min={0}
                  max={1}
                  step={0.05}
                />
              </Field>
            </div>
            <div className="flex items-center justify-between gap-2">
              <span className="text-muted-foreground text-xs">
                Scoped by the filters above. Only entries with embeddings match.
              </span>
              <Button size="sm" onClick={() => void handleSearch()} disabled={running}>
                <Search />
                {running ? "Searching…" : "Search"}
              </Button>
            </div>
            <VectorSearchResults
              running={running}
              hits={hits}
              duration={duration}
              error={error}
            />
          </CardContent>
        </CollapsibleContent>
      </Collapsible>
    </Card>
  )
}

function VectorSearchResults({
  running,
  hits,
  duration,
  error,
}: {
  running: boolean
  hits: MemorySearchHit[] | null
  duration: number | null
  error: string | null
}) {
  if (running && !hits) {
    return (
      <div className="flex flex-col gap-2">
        <Skeleton className="h-16 w-full" />
        <Skeleton className="h-16 w-full" />
      </div>
    )
  }
  if (error) {
    return (
      <div className="bg-destructive/10 text-destructive rounded-md p-3 font-mono text-xs">
        {error}
      </div>
    )
  }
  if (hits === null) return null
  if (hits.length === 0) {
    return (
      <Empty className="border-dashed">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <Search />
          </EmptyMedia>
          <EmptyTitle>No matches</EmptyTitle>
          <EmptyDescription>
            Try lowering the threshold or removing scope filters.
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }
  return (
    <div className="flex flex-col gap-2">
      <div className="text-muted-foreground text-xs tabular-nums">
        {hits.length} hit{hits.length === 1 ? "" : "s"}
        {duration !== null ? ` · ${duration}ms` : ""}
      </div>
      <div className="flex flex-col gap-2">
        {hits.map((hit, i) => (
          <HitCard key={`${hit.entry.scope}.${hit.entry.key}.${i}`} hit={hit} />
        ))}
      </div>
    </div>
  )
}

function HitCard({ hit }: { hit: MemorySearchHit }) {
  const pct = Math.max(0, Math.min(1, hit.similarity)) * 100
  const preview = truncate(stringifyValue(hit.entry.value), 120)
  return (
    <Card size="sm" className="gap-2">
      <CardContent className="flex flex-col gap-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-1.5">
            <span className="font-mono text-xs">{hit.entry.key}</span>
            <Badge variant="outline" className="font-mono">
              {hit.entry.scope}
            </Badge>
            {hit.entry.scope_id ? (
              <Badge variant="ghost" className="font-mono">
                {hit.entry.scope_id}
              </Badge>
            ) : null}
          </div>
          <span className="text-muted-foreground font-mono text-xs tabular-nums">
            similarity {hit.similarity.toFixed(3)}
          </span>
        </div>
        <Progress value={pct} max={100} />
        <p
          className="text-muted-foreground font-mono text-xs"
          title={stringifyValue(hit.entry.value)}
        >
          {preview}
        </p>
      </CardContent>
    </Card>
  )
}

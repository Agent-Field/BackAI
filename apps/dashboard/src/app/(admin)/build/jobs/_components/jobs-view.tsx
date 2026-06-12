"use client"

// JobsView — registered job definitions + enqueue sheet.
//
// Client component because it owns:
//   - the Enqueue Sheet open state
//   - the react-hook-form + zod submit flow
//   - the post-submit refresh via `router.refresh()`
//
// Source data (`definitions`) comes from the parent server component.

import * as React from "react"
import { useRouter } from "next/navigation"
import { Controller, useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table"
import { Plus, Timer } from "lucide-react"
import { toast } from "sonner"

import { api, ApiError, type JobDefinition } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
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
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"

const LANGUAGE_LABEL: Record<JobDefinition["language"], string> = {
  python: "Python",
  typescript: "TypeScript",
  go: "Go",
}

const EnqueueSchema = z.object({
  name: z.string().min(1, "Required"),
  args: z
    .string()
    .min(1, "Required")
    .refine(
      (raw) => {
        try {
          JSON.parse(raw)
          return true
        } catch {
          return false
        }
      },
      { message: "Must be valid JSON" },
    ),
  max_attempts: z
    .string()
    .optional()
    .refine(
      (raw) => {
        if (!raw) return true
        const n = Number(raw)
        return Number.isInteger(n) && n >= 1 && n <= 50
      },
      { message: "Must be an integer between 1 and 50" },
    ),
  scheduled_at: z.string().optional(),
})

type EnqueueValues = z.infer<typeof EnqueueSchema>

type JobsViewProps = {
  definitions: JobDefinition[]
  loadError: string | null
}

export function JobsView({ definitions, loadError }: JobsViewProps) {
  const [open, setOpen] = React.useState(false)

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <span className="text-muted-foreground text-xs">
          {definitions.length === 0
            ? "No jobs registered"
            : `${definitions.length} job${definitions.length === 1 ? "" : "s"}`}
        </span>
        <Button
          size="sm"
          onClick={() => setOpen(true)}
          disabled={definitions.length === 0}
        >
          <Plus />
          Enqueue job
        </Button>
      </div>

      <JobsTable definitions={definitions} loadError={loadError} />

      <EnqueueSheet
        definitions={definitions}
        open={open}
        onOpenChange={setOpen}
      />
    </div>
  )
}

function JobsTable({
  definitions,
  loadError,
}: {
  definitions: JobDefinition[]
  loadError: string | null
}) {
  const columns = React.useMemo<ColumnDef<JobDefinition>[]>(
    () => [
      {
        id: "name",
        header: "Name",
        cell: ({ row }) => (
          <SourcePathTooltip path={row.original.source_path}>
            <span className="font-mono text-xs">{row.original.name}</span>
          </SourcePathTooltip>
        ),
      },
      {
        id: "language",
        header: "Language",
        cell: ({ row }) => (
          <Badge variant="outline" className="font-normal">
            {LANGUAGE_LABEL[row.original.language]}
          </Badge>
        ),
      },
      {
        id: "cron",
        header: "Schedule",
        cell: ({ row }) =>
          row.original.cron ? (
            <span className="font-mono text-xs">{row.original.cron}</span>
          ) : (
            <span className="text-muted-foreground text-xs">Manual</span>
          ),
      },
      {
        id: "succeeded",
        header: "Succeeded",
        cell: ({ row }) => (
          <span className="tabular-nums text-xs">
            {row.original.recent.succeeded}
          </span>
        ),
      },
      {
        id: "failed",
        header: "Failed",
        cell: ({ row }) => {
          const n = row.original.recent.failed
          return (
            <span
              className={
                n > 0
                  ? "text-destructive tabular-nums text-xs"
                  : "text-muted-foreground tabular-nums text-xs"
              }
            >
              {n}
            </span>
          )
        },
      },
      {
        id: "running",
        header: "Running",
        cell: ({ row }) => (
          <span className="tabular-nums text-xs">
            {row.original.recent.running}
          </span>
        ),
      },
    ],
    [],
  )

  const table = useReactTable({
    data: definitions,
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  if (definitions.length === 0) {
    return (
      <div className="bg-card rounded-xl border">
        <Empty className="border-0">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Timer />
            </EmptyMedia>
            <EmptyTitle>No jobs registered yet</EmptyTitle>
            <EmptyDescription>
              {loadError ? (
                <>
                  The runtime returned:{" "}
                  <code className="font-mono">{loadError}</code>. Once the
                  definitions endpoint is live, jobs defined in{" "}
                  <code className="font-mono">
                    apps/backend/jobs/&lt;name&gt;.{`{py,ts}`}
                  </code>{" "}
                  will appear here.
                </>
              ) : (
                <>
                  Define one in{" "}
                  <code className="font-mono">
                    apps/backend/jobs/&lt;name&gt;.{`{py,ts}`}
                  </code>{" "}
                  to see it here.
                </>
              )}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </div>
    )
  }

  return (
    <div className="rounded-xl border bg-card">
      <TooltipProvider>
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
            {table.getRowModel().rows.map((row) => (
              <TableRow key={row.id} className="hover:bg-muted/30">
                {row.getVisibleCells().map((cell) => (
                  <TableCell key={cell.id}>
                    {flexRender(cell.column.columnDef.cell, cell.getContext())}
                  </TableCell>
                ))}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TooltipProvider>
    </div>
  )
}

function SourcePathTooltip({
  path,
  children,
}: {
  path: string | null
  children: React.ReactNode
}) {
  if (!path) return <>{children}</>
  return (
    <Tooltip>
      <TooltipTrigger render={<span className="cursor-help">{children}</span>} />
      <TooltipContent>
        <span className="font-mono">{path}</span>
      </TooltipContent>
    </Tooltip>
  )
}

function EnqueueSheet({
  definitions,
  open,
  onOpenChange,
}: {
  definitions: JobDefinition[]
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const router = useRouter()
  const [submitting, setSubmitting] = React.useState(false)

  const form = useForm<EnqueueValues>({
    resolver: zodResolver(EnqueueSchema),
    defaultValues: {
      name: definitions[0]?.name ?? "",
      args: "{}",
      max_attempts: "",
      scheduled_at: "",
    },
    mode: "onBlur",
  })

  // Reset form whenever the sheet opens, so a stale submission isn't carried
  // over from a previous attempt.
  React.useEffect(() => {
    if (open) {
      form.reset({
        name: definitions[0]?.name ?? "",
        args: "{}",
        max_attempts: "",
        scheduled_at: "",
      })
    }
  }, [open, definitions, form])

  const nameValue = form.watch("name")

  const handleSubmit = async (values: EnqueueValues) => {
    setSubmitting(true)
    try {
      const args = JSON.parse(values.args) as unknown
      const payload: {
        name: string
        args: unknown
        max_attempts?: number
        scheduled_at?: string
      } = {
        name: values.name,
        args,
      }
      if (values.max_attempts) {
        payload.max_attempts = Number(values.max_attempts)
      }
      if (values.scheduled_at) {
        // datetime-local gives `YYYY-MM-DDTHH:mm` — convert to ISO via Date.
        const iso = new Date(values.scheduled_at).toISOString()
        payload.scheduled_at = iso
      }
      const job = await api.jobs.enqueue(payload)
      toast.success("Job enqueued", {
        description: `${job.name} (${job.id}) is in the queue.`,
      })
      onOpenChange(false)
      router.refresh()
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to enqueue job"
      toast.error("Could not enqueue job", { description: message })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="flex w-full flex-col gap-0 p-0 sm:max-w-md">
        <SheetHeader className="border-b">
          <SheetTitle>Enqueue job</SheetTitle>
          <SheetDescription>
            The runtime will pick this up on its next poll.
          </SheetDescription>
        </SheetHeader>
        <form
          onSubmit={form.handleSubmit(handleSubmit)}
          className="flex flex-1 flex-col"
        >
          <div className="flex-1 overflow-auto p-4">
            <FieldGroup>
              <Field
                data-invalid={form.formState.errors.name ? true : undefined}
              >
                <FieldLabel htmlFor="enqueue-name">Job</FieldLabel>
                <Select
                  value={nameValue}
                  onValueChange={(v) =>
                    form.setValue("name", v ?? "", { shouldValidate: true })
                  }
                >
                  <SelectTrigger id="enqueue-name" className="w-full">
                    <SelectValue placeholder="Pick a job" />
                  </SelectTrigger>
                  <SelectContent>
                    {definitions.map((def) => (
                      <SelectItem key={def.name} value={def.name}>
                        <span className="font-mono text-xs">{def.name}</span>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {form.formState.errors.name ? (
                  <FieldDescription>
                    {form.formState.errors.name.message}
                  </FieldDescription>
                ) : (
                  <FieldDescription>
                    Choose a registered job handler.
                  </FieldDescription>
                )}
              </Field>

              <Field
                data-invalid={form.formState.errors.args ? true : undefined}
              >
                <FieldLabel htmlFor="enqueue-args">Arguments</FieldLabel>
                <Textarea
                  id="enqueue-args"
                  rows={6}
                  className="font-mono text-xs"
                  aria-invalid={
                    form.formState.errors.args ? true : undefined
                  }
                  {...form.register("args")}
                />
                {form.formState.errors.args ? (
                  <FieldDescription>
                    {form.formState.errors.args.message}
                  </FieldDescription>
                ) : (
                  <FieldDescription>
                    JSON object passed to the handler.
                  </FieldDescription>
                )}
              </Field>

              <Field
                data-invalid={
                  form.formState.errors.max_attempts ? true : undefined
                }
              >
                <FieldLabel htmlFor="enqueue-max-attempts">
                  Max attempts
                </FieldLabel>
                <Input
                  id="enqueue-max-attempts"
                  type="number"
                  min={1}
                  max={50}
                  placeholder="Use runtime default"
                  aria-invalid={
                    form.formState.errors.max_attempts ? true : undefined
                  }
                  {...form.register("max_attempts")}
                />
                {form.formState.errors.max_attempts ? (
                  <FieldDescription>
                    {form.formState.errors.max_attempts.message}
                  </FieldDescription>
                ) : (
                  <FieldDescription>
                    Optional. Overrides the runtime&apos;s retry cap.
                  </FieldDescription>
                )}
              </Field>

              <Field>
                <FieldLabel htmlFor="enqueue-scheduled-at">
                  Schedule for
                </FieldLabel>
                <Controller
                  control={form.control}
                  name="scheduled_at"
                  render={({ field }) => (
                    <DateTimePicker
                      value={field.value ?? ""}
                      onChange={field.onChange}
                      placeholder="Run immediately"
                    />
                  )}
                />
                <FieldDescription>
                  Optional. Leave blank to run immediately.
                </FieldDescription>
              </Field>
            </FieldGroup>
          </div>

          <SheetFooter className="border-t">
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
              {submitting ? "Enqueuing…" : "Enqueue"}
            </Button>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  )
}

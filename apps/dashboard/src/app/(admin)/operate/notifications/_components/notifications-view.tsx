// SPDX-License-Identifier: Apache-2.0

"use client"

// NotificationsView — client-side wrapper for the notifications tab.
//
// Responsibilities:
//   - Keep the stats + list fresh (5s polling, single in-flight controller,
//     pause/resume button, tab-hidden gate).
//   - Drive filtering (tenant, status, kind) via list query params.
//   - Surface a "Send notification" Dialog that POSTs through the
//     dashboard's typed api client.

import * as React from "react"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table"
import {
  Bell,
  CircleDot,
  Pause,
  Play,
  RotateCw,
  Send,
} from "lucide-react"
import { useForm } from "react-hook-form"
import { toast } from "sonner"

import {
  api,
  ApiError,
  type Notification,
  type NotificationKind,
  type NotificationList,
  type NotificationStats,
  type NotificationStatus,
  type SendNotificationInput,
} from "@/lib/api"
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
  Pagination,
  PaginationContent,
  PaginationItem,
  PaginationNext,
  PaginationPrevious,
} from "@/components/ui/pagination"
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
import { Textarea } from "@/components/ui/textarea"
import { cn } from "@/lib/utils"

import { formatRelative } from "../../_lib/format"
import { NotificationsKpiStrip } from "./notifications-kpi-strip"

const PAGE_SIZE = 25
const REFRESH_INTERVAL_MS = 5000

type StatusFilter = "all" | NotificationStatus
type KindFilter = "all" | NotificationKind

const STATUSES: { value: StatusFilter; label: string }[] = [
  { value: "all", label: "All statuses" },
  { value: "queued", label: "Queued" },
  { value: "sending", label: "Sending" },
  { value: "sent", label: "Sent" },
  { value: "failed", label: "Failed" },
  { value: "skipped", label: "Skipped" },
]

const KINDS: { value: KindFilter; label: string }[] = [
  { value: "all", label: "All kinds" },
  { value: "email", label: "Email" },
  { value: "sms", label: "SMS" },
  { value: "push", label: "Push" },
  { value: "log", label: "Log" },
]

const STATUS_VARIANTS: Record<
  NotificationStatus,
  React.ComponentProps<typeof Badge>["variant"]
> = {
  queued: "secondary",
  sending: "default",
  sent: "outline",
  failed: "destructive",
  skipped: "secondary",
}

const STATUS_LABELS: Record<NotificationStatus, string> = {
  queued: "Queued",
  sending: "Sending",
  sent: "Sent",
  failed: "Failed",
  skipped: "Skipped",
}

type Props = {
  initialStats: NotificationStats | null
  initialList: NotificationList | null
  initialError: string | null
}

export function NotificationsView({
  initialStats,
  initialList,
  initialError,
}: Props) {
  const [stats, setStats] = React.useState<NotificationStats | null>(initialStats)
  const [data, setData] = React.useState<NotificationList | null>(initialList)
  const [tenant, setTenant] = React.useState("")
  const [status, setStatus] = React.useState<StatusFilter>("all")
  const [kind, setKind] = React.useState<KindFilter>("all")
  const [offset, setOffset] = React.useState(0)
  const [loading, setLoading] = React.useState(initialList === null)
  const [error, setError] = React.useState<string | null>(initialError)
  const [paused, setPaused] = React.useState(false)
  const [sendOpen, setSendOpen] = React.useState(false)

  const [debouncedTenant, setDebouncedTenant] = React.useState("")
  React.useEffect(() => {
    const t = setTimeout(() => setDebouncedTenant(tenant.trim()), 300)
    return () => clearTimeout(t)
  }, [tenant])

  // Reset to first page when filters change.
  React.useEffect(() => {
    setOffset(0)
  }, [debouncedTenant, status, kind])

  const abortRef = React.useRef<AbortController | null>(null)
  const inFlight = React.useRef(false)

  const fetchOnce = React.useCallback(
    async ({ silent }: { silent: boolean } = { silent: false }) => {
      if (inFlight.current) return
      inFlight.current = true
      abortRef.current?.abort()
      const controller = new AbortController()
      abortRef.current = controller
      if (!silent) setLoading(true)
      try {
        const [statsResult, listResult] = await Promise.allSettled([
          api.notifications.stats(),
          api.notifications.list({
            tenant: debouncedTenant || undefined,
            status: status === "all" ? undefined : status,
            kind: kind === "all" ? undefined : kind,
            limit: PAGE_SIZE,
            offset,
          }),
        ])
        if (controller.signal.aborted) return
        if (statsResult.status === "fulfilled") {
          setStats(statsResult.value)
        }
        if (listResult.status === "fulfilled") {
          setData(listResult.value)
          setError(null)
        } else {
          const reason = listResult.reason
          const message =
            reason instanceof ApiError
              ? `${reason.code}: ${reason.message}`
              : reason instanceof Error
                ? reason.message
                : "Failed to load notifications"
          setError(message)
          setData({ notifications: [], total: 0, has_more: false })
        }
      } finally {
        inFlight.current = false
        if (!silent) setLoading(false)
      }
    },
    [debouncedTenant, status, kind, offset],
  )

  // Refetch when filters/pagination change.
  React.useEffect(() => {
    void fetchOnce()
  }, [fetchOnce])

  // Polling loop. Pauses on tab hidden + on explicit pause.
  React.useEffect(() => {
    if (paused) return
    const id = setInterval(() => {
      if (typeof document !== "undefined" && document.hidden) return
      void fetchOnce({ silent: true })
    }, REFRESH_INTERVAL_MS)
    return () => clearInterval(id)
  }, [paused, fetchOnce])

  // Abort any in-flight request on unmount.
  React.useEffect(() => {
    return () => abortRef.current?.abort()
  }, [])

  const columns = React.useMemo<ColumnDef<Notification>[]>(
    () => [
      {
        id: "status",
        header: "Status",
        cell: ({ row }) => {
          const s = row.original.status
          return <Badge variant={STATUS_VARIANTS[s]}>{STATUS_LABELS[s]}</Badge>
        },
      },
      {
        id: "kind",
        header: "Kind",
        cell: ({ row }) => (
          <Badge variant="outline" className="font-mono">
            {row.original.kind}
          </Badge>
        ),
      },
      {
        id: "adapter",
        header: "Adapter",
        cell: ({ row }) => {
          const a = row.original.adapter
          if (!a) {
            return (
              <span className="text-muted-foreground font-mono text-[11px]">
                —
              </span>
            )
          }
          return (
            <Badge variant="ghost" className="font-mono">
              {a}
            </Badge>
          )
        },
      },
      {
        id: "to",
        header: "To",
        cell: ({ row }) => (
          <span className="font-mono text-xs" title={row.original.to}>
            {truncate(row.original.to, 40)}
          </span>
        ),
      },
      {
        id: "template",
        header: "Template",
        cell: ({ row }) => (
          <span
            className="text-muted-foreground font-mono text-xs"
            title={row.original.template}
          >
            {truncate(row.original.template, 24)}
          </span>
        ),
      },
      {
        id: "subject",
        header: "Subject",
        cell: ({ row }) => {
          const s = row.original.subject
          if (!s) {
            return (
              <span className="text-muted-foreground text-[11px]">—</span>
            )
          }
          return (
            <span className="text-xs" title={s}>
              {truncate(s, 40)}
            </span>
          )
        },
      },
      {
        id: "attempts",
        header: "Attempts",
        cell: ({ row }) => (
          <span className="tabular-nums text-xs">{row.original.attempts}</span>
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
    ],
    [],
  )

  const table = useReactTable({
    data: data?.notifications ?? [],
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  const total = data?.total ?? 0
  const hasMore = data?.has_more ?? false
  const page = Math.floor(offset / PAGE_SIZE) + 1
  const totalPages = total > 0 ? Math.ceil(total / PAGE_SIZE) : 1
  const startIdx = total === 0 ? 0 : offset + 1
  const endIdx = Math.min(offset + PAGE_SIZE, total)
  const rowsForView = table.getRowModel().rows
  const liveSending =
    data?.notifications.some((n) => n.status === "sending") ?? false

  return (
    <div className="flex flex-col gap-6">
      {stats ? <NotificationsKpiStrip stats={stats} /> : <KpiSkeleton />}

      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-col gap-0.5">
            <div className="flex items-center gap-2">
              <h2 className="font-heading text-base font-medium">
                Deliveries
              </h2>
              {!paused && liveSending ? (
                <CircleDot
                  className="size-3 animate-pulse text-emerald-500"
                  aria-hidden
                />
              ) : null}
            </div>
            <p className="text-muted-foreground text-xs">
              {paused
                ? "Paused. Resume to keep this table fresh every 5s."
                : "Auto-refreshing every 5s."}
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => setPaused((p) => !p)}
            >
              {paused ? <Play /> : <Pause />}
              {paused ? "Resume" : "Pause"}
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void fetchOnce()}
              disabled={loading}
            >
              <RotateCw className={cn(loading && "animate-spin")} />
              Refresh
            </Button>
            <Button size="sm" onClick={() => setSendOpen(true)}>
              <Send />
              Send notification
            </Button>
          </div>
        </div>

        <FiltersBar
          tenant={tenant}
          status={status}
          kind={kind}
          onTenantChange={setTenant}
          onStatusChange={setStatus}
          onKindChange={setKind}
        />

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
              {loading && (data?.notifications.length ?? 0) === 0 ? (
                <SkeletonRows columns={columns.length} />
              ) : rowsForView.length === 0 ? (
                <TableRow className="hover:bg-transparent">
                  <TableCell colSpan={columns.length} className="p-0">
                    <NotificationsEmptyState error={error} />
                  </TableCell>
                </TableRow>
              ) : (
                rowsForView.map((row) => (
                  <TableRow key={row.id}>
                    {row.getVisibleCells().map((cell) => (
                      <TableCell key={cell.id}>
                        {flexRender(
                          cell.column.columnDef.cell,
                          cell.getContext(),
                        )}
                      </TableCell>
                    ))}
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>

        {rowsForView.length > 0 ? (
          <div className="flex items-center justify-between gap-2 px-1">
            <p className="text-muted-foreground text-xs">
              Showing {startIdx}–{endIdx} of {total} ·{" "}
              <span className="tabular-nums">
                page {page} of {totalPages}
              </span>
            </p>
            <Pagination className="m-0 w-fit justify-end">
              <PaginationContent>
                <PaginationItem>
                  <PaginationPrevious
                    href="#"
                    className={cn(
                      offset === 0 && "pointer-events-none opacity-50",
                    )}
                    onClick={(e) => {
                      e.preventDefault()
                      if (offset === 0) return
                      setOffset(Math.max(0, offset - PAGE_SIZE))
                    }}
                  />
                </PaginationItem>
                <PaginationItem>
                  <PaginationNext
                    href="#"
                    className={cn(
                      !hasMore && "pointer-events-none opacity-50",
                    )}
                    onClick={(e) => {
                      e.preventDefault()
                      if (!hasMore) return
                      setOffset(offset + PAGE_SIZE)
                    }}
                  />
                </PaginationItem>
              </PaginationContent>
            </Pagination>
          </div>
        ) : null}
      </div>

      <SendNotificationDialog
        open={sendOpen}
        onOpenChange={setSendOpen}
        onSent={() => {
          setSendOpen(false)
          void fetchOnce({ silent: true })
        }}
      />
    </div>
  )
}

// ─── FiltersBar ───────────────────────────────────────────────────────────

function FiltersBar({
  tenant,
  status,
  kind,
  onTenantChange,
  onStatusChange,
  onKindChange,
}: {
  tenant: string
  status: StatusFilter
  kind: KindFilter
  onTenantChange: (v: string) => void
  onStatusChange: (v: StatusFilter) => void
  onKindChange: (v: KindFilter) => void
}) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Input
        placeholder="Filter by tenant…"
        value={tenant}
        onChange={(e) => onTenantChange(e.target.value)}
        className="w-56"
      />
      <Select
        value={status}
        onValueChange={(v) => onStatusChange(v as StatusFilter)}
      >
        <SelectTrigger className="w-44">
          <SelectValue placeholder="Status" />
        </SelectTrigger>
        <SelectContent>
          {STATUSES.map((s) => (
            <SelectItem key={s.value} value={s.value}>
              {s.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Select
        value={kind}
        onValueChange={(v) => onKindChange(v as KindFilter)}
      >
        <SelectTrigger className="w-40">
          <SelectValue placeholder="Kind" />
        </SelectTrigger>
        <SelectContent>
          {KINDS.map((k) => (
            <SelectItem key={k.value} value={k.value}>
              {k.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}

// ─── Send dialog ──────────────────────────────────────────────────────────

type SendFormValues = {
  kind: NotificationKind
  to: string
  subject: string
  from: string
  template: string
  body: string
}

function SendNotificationDialog({
  open,
  onOpenChange,
  onSent,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSent: () => void
}) {
  const [submitting, setSubmitting] = React.useState(false)
  const form = useForm<SendFormValues>({
    defaultValues: {
      kind: "email",
      to: "",
      subject: "",
      from: "",
      template: "default",
      body: "",
    },
  })

  React.useEffect(() => {
    if (!open) {
      form.reset({
        kind: "email",
        to: "",
        subject: "",
        from: "",
        template: "default",
        body: "",
      })
    }
  }, [open, form])

  const handleSubmit = async (values: SendFormValues) => {
    if (!values.to.trim()) {
      toast.error("Recipient is required")
      return
    }
    setSubmitting(true)
    try {
      const payload: SendNotificationInput = {
        kind: values.kind,
        template: values.template || "default",
        to: values.to.trim(),
      }
      if (values.subject.trim()) payload.subject = values.subject.trim()
      if (values.from.trim()) payload.from = values.from.trim()
      if (values.body.trim()) payload.data = { body: values.body.trim() }
      await api.notifications.send(payload)
      toast.success("Notification queued", {
        description: `to=${values.to}`,
      })
      onSent()
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to send"
      toast.error("Could not send notification", { description: message })
    } finally {
      setSubmitting(false)
    }
  }

  const watchKind = form.watch("kind")

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Send notification</DialogTitle>
          <DialogDescription>
            Queues a row in the outbox. The background worker dispatches it
            through the configured adapter on the next tick.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={form.handleSubmit(handleSubmit)}>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="send-kind">Kind</FieldLabel>
              <Select
                value={watchKind}
                onValueChange={(v) =>
                  form.setValue("kind", v as NotificationKind)
                }
              >
                <SelectTrigger id="send-kind">
                  <SelectValue placeholder="Pick a channel" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="email">Email</SelectItem>
                  <SelectItem value="sms">SMS</SelectItem>
                  <SelectItem value="push">Push</SelectItem>
                  <SelectItem value="log">Log (dev only)</SelectItem>
                </SelectContent>
              </Select>
              <FieldDescription>
                Email is the only channel with a production adapter today.
              </FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="send-to">To</FieldLabel>
              <Input
                id="send-to"
                placeholder={
                  watchKind === "email"
                    ? "user@example.com"
                    : "recipient identifier"
                }
                {...form.register("to")}
              />
              <FieldDescription>
                Required. Email address for email, phone for sms, etc.
              </FieldDescription>
            </Field>
            {watchKind === "email" ? (
              <Field>
                <FieldLabel htmlFor="send-subject">Subject</FieldLabel>
                <Input
                  id="send-subject"
                  placeholder="Welcome to AF Stack"
                  {...form.register("subject")}
                />
              </Field>
            ) : null}
            <Field>
              <FieldLabel htmlFor="send-from">From (optional)</FieldLabel>
              <Input
                id="send-from"
                placeholder="hello@af-stack.local"
                {...form.register("from")}
              />
              <FieldDescription>
                Overrides the adapter&apos;s default sender address.
              </FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="send-template">Template</FieldLabel>
              <Input
                id="send-template"
                placeholder="default"
                {...form.register("template")}
              />
              <FieldDescription>
                Slug only — the worker resolves the body. Leave empty for the
                default template that just renders <code>body</code>.
              </FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="send-body">Body</FieldLabel>
              <Textarea
                id="send-body"
                rows={4}
                placeholder="Hello {{name}}, welcome to the platform."
                {...form.register("body")}
              />
              <FieldDescription>
                Folded into <code>data.body</code>. <code>{"{{token}}"}</code>{" "}
                substitutions resolve against the data payload.
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
              {submitting ? "Queueing…" : "Queue notification"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// ─── Skeletons + empty states ─────────────────────────────────────────────

function KpiSkeleton() {
  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4">
      {Array.from({ length: 4 }).map((_, i) => (
        <div
          key={i}
          className="rounded-xl bg-card p-4 ring-1 ring-foreground/10"
        >
          <Skeleton className="h-3 w-20" />
          <Skeleton className="mt-2 h-9 w-32" />
          <Skeleton className="mt-3 h-3 w-40" />
        </div>
      ))}
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

function NotificationsEmptyState({ error }: { error: string | null }) {
  if (error) {
    return (
      <Empty className="border-0">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <Bell />
          </EmptyMedia>
          <EmptyTitle>Couldn&apos;t load notifications</EmptyTitle>
          <EmptyDescription>
            The runtime returned:{" "}
            <code className="font-mono">{error}</code>. Check that the
            notifications module is wired in <code className="font-mono">
              main.go
            </code>{" "}
            and an adapter is configured.
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }
  return (
    <Empty className="border-0">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <Bell />
        </EmptyMedia>
        <EmptyTitle>No notifications yet</EmptyTitle>
        <EmptyDescription>
          Queue one with the &quot;Send notification&quot; button or call{" "}
          <code className="font-mono">suite.notifications.send()</code> from
          an agent.
        </EmptyDescription>
      </EmptyHeader>
    </Empty>
  )
}

// ─── Helpers ──────────────────────────────────────────────────────────────

function truncate(value: string, max: number): string {
  if (value.length <= max) return value
  return `${value.slice(0, max)}…`
}
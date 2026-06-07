// SPDX-License-Identifier: Apache-2.0

"use client"

// CronsView — table of cron schedules with create / toggle / delete.
//
// Each row shows:
//   - name (with optional tenant scope badge)
//   - schedule (mono font; cron expressions deserve to render plainly)
//   - job_name (mono)
//   - next_run (relative; tooltip with absolute ISO)
//   - last_run (relative; "—" when never)
//   - is_active toggle (Switch)
//   - actions dropdown (Delete confirm via AlertDialog)
//
// The list endpoint returns { crons: [] } when no DB / no rows; we
// surface that as the empty state with an explanation rather than an
// error.

import * as React from "react"
import { Clock, MoreHorizontal, Plus, RefreshCw, Trash2 } from "lucide-react"
import { toast } from "sonner"

import {
  api,
  ApiError,
  type Cron,
  type CronList,
  type JobDefinitionList,
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
import { Switch } from "@/components/ui/switch"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

import { formatRelative } from "../../_lib/format"

import { CreateCronDialog } from "./create-cron-dialog"

type CronsViewProps = {
  initial: CronList
  definitions: JobDefinitionList
}

export function CronsView({ initial, definitions }: CronsViewProps) {
  const [crons, setCrons] = React.useState<Cron[]>(initial.crons)
  const [loading, setLoading] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)
  const [openCreate, setOpenCreate] = React.useState(false)
  const [deleteTarget, setDeleteTarget] = React.useState<Cron | null>(null)

  const fetchCrons = React.useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const list = await api.crons.list()
      setCrons(list.crons)
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to load crons"
      setError(message)
    } finally {
      setLoading(false)
    }
  }, [])

  const handleSetActive = async (cron: Cron, active: boolean) => {
    // Optimistic update so the toggle feels instant.
    setCrons((prev) =>
      prev.map((c) => (c.id === cron.id ? { ...c, is_active: active } : c)),
    )
    try {
      const updated = await api.crons.setActive(cron.id, active)
      setCrons((prev) => prev.map((c) => (c.id === cron.id ? updated : c)))
    } catch (e) {
      // Rollback on failure.
      setCrons((prev) =>
        prev.map((c) =>
          c.id === cron.id ? { ...c, is_active: !active } : c,
        ),
      )
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Could not toggle cron"
      toast.error("Toggle failed", { description: message })
    }
  }

  const handleDelete = async (cron: Cron) => {
    try {
      await api.crons.delete(cron.id)
      setCrons((prev) => prev.filter((c) => c.id !== cron.id))
      toast.success("Cron deleted", { description: cron.name })
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Could not delete cron"
      toast.error("Delete failed", { description: message })
    } finally {
      setDeleteTarget(null)
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-2">
        <Button
          variant="outline"
          size="sm"
          onClick={fetchCrons}
          disabled={loading}
        >
          <RefreshCw className={loading ? "animate-spin" : undefined} />
          Refresh
        </Button>
        <span className="text-muted-foreground text-xs">
          {crons.length} cron{crons.length === 1 ? "" : "s"}
        </span>
        <div className="ml-auto">
          <Button size="sm" onClick={() => setOpenCreate(true)}>
            <Plus />
            Create cron
          </Button>
        </div>
      </div>

      {error ? (
        <div className="bg-card text-muted-foreground rounded-xl border border-dashed p-4 text-sm">
          <p className="text-foreground mb-1 font-medium">
            Couldn&apos;t load crons
          </p>
          <p>
            The runtime returned:{" "}
            <code className="bg-muted rounded px-1 font-mono text-xs">
              {error}
            </code>
            . The dashboard will keep trying — hit{" "}
            <em>Refresh</em> once the runtime is back.
          </p>
        </div>
      ) : null}

      {crons.length === 0 && !error ? (
        <div className="bg-card rounded-xl border">
          <Empty className="border-0">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <Clock />
              </EmptyMedia>
              <EmptyTitle>No crons yet</EmptyTitle>
              <EmptyDescription>
                A cron schedule fires a background job on a fixed cadence —{" "}
                <code className="font-mono">@hourly</code>,{" "}
                <code className="font-mono">@daily</code>, or any standard
                5-field cron expression. The scheduler ticks once a minute and
                dispatches everything that&apos;s due.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        </div>
      ) : (
        <div className="bg-card rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Name
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Schedule
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Job
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Next run
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Last run
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Active
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  {""}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {crons.map((cron) => (
                <TableRow key={cron.id} className="hover:bg-muted/30">
                  <TableCell>
                    <div className="flex flex-col gap-1">
                      <span className="font-medium">{cron.name}</span>
                      {cron.tenant_id ? (
                        <Badge variant="outline" className="w-fit text-[10px]">
                          tenant: {cron.tenant_id.slice(0, 8)}
                        </Badge>
                      ) : null}
                    </div>
                  </TableCell>
                  <TableCell>
                    <code className="bg-muted rounded px-1 font-mono text-xs">
                      {cron.schedule}
                    </code>
                  </TableCell>
                  <TableCell>
                    <code className="font-mono text-xs">{cron.job_name}</code>
                  </TableCell>
                  <TableCell>
                    <span
                      className="text-muted-foreground text-xs"
                      title={cron.next_run_at}
                    >
                      {formatRelative(cron.next_run_at)}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span
                      className="text-muted-foreground text-xs"
                      title={cron.last_run_at ?? undefined}
                    >
                      {formatRelative(cron.last_run_at)}
                    </span>
                  </TableCell>
                  <TableCell>
                    <Switch
                      checked={cron.is_active}
                      onCheckedChange={(checked) =>
                        handleSetActive(cron, checked)
                      }
                      aria-label={
                        cron.is_active ? "Disable cron" : "Enable cron"
                      }
                    />
                  </TableCell>
                  <TableCell>
                    <DropdownMenu>
                      <DropdownMenuTrigger
                        render={
                          <Button
                            variant="ghost"
                            size="icon"
                            aria-label="Actions"
                          >
                            <MoreHorizontal />
                          </Button>
                        }
                      />
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem
                          onSelect={() => setDeleteTarget(cron)}
                          className="text-destructive focus:text-destructive"
                        >
                          <Trash2 />
                          Delete
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <CreateCronDialog
        open={openCreate}
        onOpenChange={setOpenCreate}
        onCreated={(cron) => {
          setCrons((prev) => [cron, ...prev])
        }}
        definitions={definitions.definitions}
      />

      <AlertDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              Delete &ldquo;{deleteTarget?.name}&rdquo;?
            </AlertDialogTitle>
            <AlertDialogDescription>
              The cron will stop firing immediately. Any jobs already enqueued
              will still run.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              onClick={() => {
                if (deleteTarget) handleDelete(deleteTarget)
              }}
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
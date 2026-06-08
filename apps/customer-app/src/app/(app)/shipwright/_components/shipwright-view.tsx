// SPDX-License-Identifier: Apache-2.0

"use client"

import { useCallback, useEffect, useState } from "react"

import Link from "next/link"
import {
  AlertCircle,
  CheckCircle2,
  Clock,
  Loader2,
  Plus,
  XCircle,
} from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
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
import { Textarea } from "@/components/ui/textarea"
import { toast } from "sonner"

type TaskSummary = {
  id: string
  title: string
  issue_url: string
  status: "queued" | "running" | "completed" | "failed" | "cancelled"
  created_at: string
  started_at: string | null
  finished_at: string | null
}

const POLL_MS = 1500
const STATUS_LABEL: Record<TaskSummary["status"], string> = {
  queued: "Queued",
  running: "Running",
  completed: "Completed",
  failed: "Failed",
  cancelled: "Cancelled",
}

function StatusBadge({ status }: { status: TaskSummary["status"] }) {
  if (status === "queued") {
    return (
      <Badge variant="outline" className="gap-1.5">
        <Clock className="size-3" />
        {STATUS_LABEL[status]}
      </Badge>
    )
  }
  if (status === "running") {
    return (
      <Badge variant="outline" className="gap-1.5 border-blue-300 text-blue-700 dark:text-blue-300">
        <Loader2 className="size-3 animate-spin" />
        {STATUS_LABEL[status]}
      </Badge>
    )
  }
  if (status === "completed") {
    return (
      <Badge variant="outline" className="gap-1.5 border-emerald-300 text-emerald-700 dark:text-emerald-300">
        <CheckCircle2 className="size-3" />
        {STATUS_LABEL[status]}
      </Badge>
    )
  }
  if (status === "failed") {
    return (
      <Badge variant="outline" className="gap-1.5 border-red-300 text-red-700 dark:text-red-300">
        <AlertCircle className="size-3" />
        {STATUS_LABEL[status]}
      </Badge>
    )
  }
  return (
    <Badge variant="outline" className="gap-1.5 text-muted-foreground">
      <XCircle className="size-3" />
      {STATUS_LABEL[status]}
    </Badge>
  )
}

function formatRelative(when: string | null | undefined): string {
  if (!when) return "—"
  const d = new Date(when)
  const diff = Date.now() - d.getTime()
  if (diff < 5_000) return "just now"
  if (diff < 60_000) return `${Math.floor(diff / 1000)}s ago`
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`
  return d.toLocaleDateString()
}

function durationMs(task: TaskSummary): string {
  if (!task.started_at) return "—"
  const end = task.finished_at ? new Date(task.finished_at).getTime() : Date.now()
  const ms = Math.max(0, end - new Date(task.started_at).getTime())
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`
  return `${(ms / 60_000).toFixed(1)}m`
}

export function ShipwrightView() {
  const [tasks, setTasks] = useState<TaskSummary[] | null>(null)
  const [creating, setCreating] = useState(false)
  const [issueUrl, setIssueUrl] = useState("")
  const [title, setTitle] = useState("")
  const [description, setDescription] = useState("")

  const refresh = useCallback(async () => {
    try {
      const res = await fetch("/api/customer/shipwright/tasks", {
        cache: "no-store",
      })
      if (!res.ok) return
      const data = (await res.json()) as TaskSummary[]
      setTasks(data)
    } catch {
      // swallow; next tick will retry
    }
  }, [])

  useEffect(() => {
    refresh()
    const id = setInterval(refresh, POLL_MS)
    return () => clearInterval(id)
  }, [refresh])

  const submit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault()
      if (!issueUrl.trim() || !title.trim()) return
      setCreating(true)
      try {
        const res = await fetch("/api/customer/shipwright/tasks", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({
            issue_url: issueUrl.trim(),
            title: title.trim(),
            description: description.trim(),
          }),
        })
        if (!res.ok) {
          const detail = await res.text()
          toast.error("Could not queue task", { description: detail.slice(0, 120) })
          return
        }
        setIssueUrl("")
        setTitle("")
        setDescription("")
        toast.success("Task queued")
        refresh()
      } finally {
        setCreating(false)
      }
    },
    [issueUrl, title, description, refresh],
  )

  return (
    <div className="grid gap-6 lg:grid-cols-[1fr_360px]">
      <Card>
        <CardHeader className="flex flex-row items-center justify-between gap-4">
          <CardTitle className="text-base font-medium">
            Queued tasks
            {tasks ? (
              <span className="ml-2 text-xs text-muted-foreground">
                {tasks.length}
              </span>
            ) : null}
          </CardTitle>
        </CardHeader>
        <CardContent className="px-0">
          {!tasks ? (
            <div className="space-y-2 px-6 pb-4">
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
            </div>
          ) : tasks.length === 0 ? (
            <div className="px-6 py-10 text-center text-sm text-muted-foreground">
              No tasks yet. Submit a GitHub issue on the right to get started.
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Title</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Duration</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="sr-only">Open</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {tasks.map((t) => (
                  <TableRow
                    key={t.id}
                    className="cursor-pointer transition-colors hover:bg-muted/40"
                  >
                    <TableCell className="font-medium">
                      <Link
                        href={`/shipwright/${t.id}`}
                        className="block max-w-md truncate"
                      >
                        {t.title}
                      </Link>
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={t.status} />
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground tabular-nums">
                      {durationMs(t)}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {formatRelative(t.created_at)}
                    </TableCell>
                    <TableCell>
                      <Link href={`/shipwright/${t.id}`}>
                        <Button size="sm" variant="ghost">
                          Open
                        </Button>
                      </Link>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base font-medium">New task</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={submit}>
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="issue_url">GitHub issue URL</FieldLabel>
                <Input
                  id="issue_url"
                  placeholder="https://github.com/owner/repo/issues/42"
                  value={issueUrl}
                  onChange={(e) => setIssueUrl(e.target.value)}
                  required
                />
                <FieldDescription>
                  Public issue, or one in a repo your token can read.
                </FieldDescription>
              </Field>
              <Field>
                <FieldLabel htmlFor="title">Title</FieldLabel>
                <Input
                  id="title"
                  placeholder="Fix nil pointer in handler"
                  value={title}
                  onChange={(e) => setTitle(e.target.value)}
                  required
                  maxLength={200}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="description">Notes (optional)</FieldLabel>
                <Textarea
                  id="description"
                  placeholder="Anything the agent should know — coding conventions, scope, etc."
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  rows={4}
                  maxLength={4000}
                />
              </Field>
              <Button type="submit" disabled={creating} className="mt-1">
                {creating ? (
                  <>
                    <Loader2 className="size-4 animate-spin" />
                    Queueing
                  </>
                ) : (
                  <>
                    <Plus className="size-4" />
                    Queue task
                  </>
                )}
              </Button>
            </FieldGroup>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}

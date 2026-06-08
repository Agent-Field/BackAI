// SPDX-License-Identifier: Apache-2.0

"use client"

import { useCallback, useEffect, useState } from "react"

import Link from "next/link"
import {
  AlertCircle,
  ArrowLeft,
  CheckCircle2,
  Clock,
  ExternalLink,
  Hammer,
  Loader2,
  XCircle,
} from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"

type Step = {
  idx: number
  title: string
  status: "pending" | "running" | "completed" | "failed"
  detail: string | null
  started_at: string | null
  finished_at: string | null
}

type TaskDetail = {
  id: string
  title: string
  issue_url: string
  description: string
  status: "queued" | "running" | "completed" | "failed" | "cancelled"
  result_summary: string | null
  diff_preview: string | null
  error: string | null
  created_at: string
  started_at: string | null
  finished_at: string | null
  steps: Step[]
}

const POLL_MS = 1000

function StatusBadge({ status }: { status: TaskDetail["status"] }) {
  if (status === "queued") {
    return (
      <Badge variant="outline" className="gap-1.5">
        <Clock className="size-3" />
        Queued
      </Badge>
    )
  }
  if (status === "running") {
    return (
      <Badge variant="outline" className="gap-1.5 border-blue-300 text-blue-700 dark:text-blue-300">
        <Loader2 className="size-3 animate-spin" />
        Running
      </Badge>
    )
  }
  if (status === "completed") {
    return (
      <Badge variant="outline" className="gap-1.5 border-emerald-300 text-emerald-700 dark:text-emerald-300">
        <CheckCircle2 className="size-3" />
        Completed
      </Badge>
    )
  }
  if (status === "failed") {
    return (
      <Badge variant="outline" className="gap-1.5 border-red-300 text-red-700 dark:text-red-300">
        <AlertCircle className="size-3" />
        Failed
      </Badge>
    )
  }
  return (
    <Badge variant="outline" className="gap-1.5 text-muted-foreground">
      <XCircle className="size-3" />
      Cancelled
    </Badge>
  )
}

function StepIcon({ status }: { status: Step["status"] }) {
  if (status === "running")
    return <Loader2 className="size-4 animate-spin text-blue-500" />
  if (status === "completed")
    return <CheckCircle2 className="size-4 text-emerald-500" />
  if (status === "failed") return <AlertCircle className="size-4 text-red-500" />
  return <Clock className="size-4 text-muted-foreground" />
}

function duration(task: TaskDetail): string {
  if (!task.started_at) return "—"
  const end = task.finished_at ? new Date(task.finished_at).getTime() : Date.now()
  const ms = Math.max(0, end - new Date(task.started_at).getTime())
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`
  return `${(ms / 60_000).toFixed(1)}m`
}

export function TaskDetailView({ taskId }: { taskId: string }) {
  const [task, setTask] = useState<TaskDetail | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [cancelling, setCancelling] = useState(false)

  const refresh = useCallback(async () => {
    try {
      const res = await fetch(`/api/customer/shipwright/tasks/${taskId}`, {
        cache: "no-store",
      })
      if (res.status === 404) {
        setError("Task not found")
        return
      }
      if (!res.ok) {
        setError(`Server returned ${res.status}`)
        return
      }
      const data = (await res.json()) as TaskDetail
      setTask(data)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Network error")
    }
  }, [taskId])

  useEffect(() => {
    refresh()
    const id = setInterval(() => {
      // Stop polling once we reach a terminal state.
      setTask((current) => {
        const done =
          current?.status === "completed" ||
          current?.status === "failed" ||
          current?.status === "cancelled"
        if (!done) {
          refresh()
        }
        return current
      })
    }, POLL_MS)
    return () => clearInterval(id)
  }, [refresh])

  const cancelTask = useCallback(async () => {
    setCancelling(true)
    try {
      const res = await fetch(`/api/customer/shipwright/tasks/${taskId}/cancel`, {
        method: "POST",
      })
      if (res.ok) refresh()
    } finally {
      setCancelling(false)
    }
  }, [taskId, refresh])

  if (error) {
    return (
      <Card>
        <CardContent className="py-12 text-center text-sm text-muted-foreground">
          <AlertCircle className="mx-auto mb-3 size-6 text-red-500" />
          {error}
          <div className="mt-4">
            <Link href="/shipwright">
              <Button variant="outline" size="sm">
                <ArrowLeft className="size-4" />
                Back to tasks
              </Button>
            </Link>
          </div>
        </CardContent>
      </Card>
    )
  }

  if (!task) {
    return <Skeleton className="h-96 w-full" />
  }

  const canCancel = task.status === "queued" || task.status === "running"

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-4">
        <Link href="/shipwright">
          <Button variant="ghost" size="sm">
            <ArrowLeft className="size-4" />
            All tasks
          </Button>
        </Link>
        <div className="flex items-center gap-2">
          <StatusBadge status={task.status} />
          {canCancel ? (
            <Button
              variant="outline"
              size="sm"
              onClick={cancelTask}
              disabled={cancelling}
            >
              {cancelling ? <Loader2 className="size-4 animate-spin" /> : null}
              Cancel
            </Button>
          ) : null}
        </div>
      </div>

      <Card>
        <CardHeader className="space-y-1.5">
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Hammer className="size-4" />
            Shipwright task
          </div>
          <CardTitle className="text-xl font-semibold tracking-tight">
            {task.title}
          </CardTitle>
          <a
            href={task.issue_url}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
          >
            <ExternalLink className="size-3.5" />
            <span className="truncate">{task.issue_url}</span>
          </a>
        </CardHeader>
        {task.description ? (
          <CardContent>
            <p className="text-sm whitespace-pre-wrap text-muted-foreground">
              {task.description}
            </p>
          </CardContent>
        ) : null}
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base font-medium">Progress</CardTitle>
        </CardHeader>
        <CardContent>
          {task.steps.length === 0 ? (
            <div className="py-8 text-center text-sm text-muted-foreground">
              {task.status === "queued" ? (
                <>
                  <Clock className="mx-auto mb-2 size-5" />
                  Waiting for an agent to pick this up…
                </>
              ) : (
                <>
                  <Loader2 className="mx-auto mb-2 size-5 animate-spin" />
                  Agent is working — first step coming soon.
                </>
              )}
            </div>
          ) : (
            <ol className="space-y-3">
              {task.steps.map((step, i) => (
                <li key={step.idx} className="flex gap-3">
                  <div className="mt-0.5 flex flex-col items-center">
                    <StepIcon status={step.status} />
                    {i < task.steps.length - 1 ? (
                      <div className="mt-1 h-full w-px bg-border" />
                    ) : null}
                  </div>
                  <div className="min-w-0 flex-1 pb-2">
                    <div className="flex items-center justify-between gap-3">
                      <p className="text-sm font-medium leading-tight">
                        {step.title}
                      </p>
                      <span className="text-xs text-muted-foreground tabular-nums shrink-0">
                        Step {step.idx}
                      </span>
                    </div>
                    {step.detail ? (
                      <p className="mt-1 text-xs text-muted-foreground leading-snug">
                        {step.detail}
                      </p>
                    ) : null}
                  </div>
                </li>
              ))}
            </ol>
          )}
        </CardContent>
      </Card>

      {task.result_summary ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-base font-medium">Summary</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm leading-relaxed">{task.result_summary}</p>
            <Separator className="my-4" />
            <div className="grid grid-cols-3 gap-4 text-xs text-muted-foreground">
              <div>
                <div className="text-muted-foreground">Duration</div>
                <div className="text-foreground font-medium tabular-nums">
                  {duration(task)}
                </div>
              </div>
              <div>
                <div className="text-muted-foreground">Steps</div>
                <div className="text-foreground font-medium tabular-nums">
                  {task.steps.length}
                </div>
              </div>
              <div>
                <div className="text-muted-foreground">Status</div>
                <div className="text-foreground font-medium">{task.status}</div>
              </div>
            </div>
          </CardContent>
        </Card>
      ) : null}

      {task.diff_preview ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-base font-medium">Diff preview</CardTitle>
          </CardHeader>
          <CardContent>
            <ScrollArea className="h-80 rounded-md border bg-muted/40 font-mono text-xs">
              <pre className="p-4 whitespace-pre-wrap leading-relaxed">
                {task.diff_preview}
              </pre>
            </ScrollArea>
          </CardContent>
        </Card>
      ) : null}

      {task.error ? (
        <Card className="border-red-200">
          <CardHeader>
            <CardTitle className="text-base font-medium text-red-700 dark:text-red-300">
              Error
            </CardTitle>
          </CardHeader>
          <CardContent>
            <pre className="whitespace-pre-wrap text-xs text-red-700 dark:text-red-300">
              {task.error}
            </pre>
          </CardContent>
        </Card>
      ) : null}
    </div>
  )
}

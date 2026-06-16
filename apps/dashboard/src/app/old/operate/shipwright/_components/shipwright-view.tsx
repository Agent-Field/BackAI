// SPDX-License-Identifier: Apache-2.0

"use client"

import * as React from "react"
import { ExternalLink, GitPullRequest, RefreshCw, Send } from "lucide-react"
import { toast } from "sonner"

import {
  api,
  ApiError,
  type ShipwrightStatus,
  type ShipwrightTask,
  type ShipwrightTaskList,
} from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"

import { formatRelative } from "../../_lib/format"

const PAGE_SIZE = 25

type StatusFilter = "all" | ShipwrightStatus

const STATUSES: { value: StatusFilter; label: string }[] = [
  { value: "all", label: "All statuses" },
  { value: "queued", label: "Queued" },
  { value: "running", label: "Running" },
  { value: "succeeded", label: "Succeeded" },
  { value: "failed", label: "Failed" },
  { value: "cancelled", label: "Cancelled" },
]

const HARNESS_PROVIDERS = ["codex", "claude-code", "gemini", "opencode"]

type ShipwrightViewProps = {
  initial: ShipwrightTaskList
}

export function ShipwrightView({ initial }: ShipwrightViewProps) {
  const [data, setData] = React.useState<ShipwrightTaskList>(initial)
  const [status, setStatus] = React.useState<StatusFilter>("all")
  const [offset, setOffset] = React.useState(0)
  const [loading, setLoading] = React.useState(false)
  const [creating, setCreating] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)
  const [selected, setSelected] = React.useState<ShipwrightTask | null>(null)
  const [patchUrl, setPatchUrl] = React.useState<string | null>(null)

  const [title, setTitle] = React.useState("")
  const [repoUrl, setRepoUrl] = React.useState("")
  const [description, setDescription] = React.useState("")
  const [harness, setHarness] = React.useState("codex")
  const [model, setModel] = React.useState("")

  React.useEffect(() => {
    setOffset(0)
  }, [status])

  const fetchTasks = React.useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const list = await api.shipwright.list({
        status: status === "all" ? undefined : status,
        limit: PAGE_SIZE,
        offset,
      })
      setData(list)
    } catch (e) {
      const message = errorMessage(e, "Failed to load Shipwright tasks")
      setError(message)
      setData({ tasks: [], total: 0, has_more: false })
    } finally {
      setLoading(false)
    }
  }, [offset, status])

  React.useEffect(() => {
    void fetchTasks()
  }, [fetchTasks])

  const createTask = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setCreating(true)
    try {
      const resp = await api.shipwright.create({
        title,
        repo_url: repoUrl,
        description,
        harness_provider: harness,
        model: model.trim() || undefined,
      })
      toast.success("Shipwright task queued", {
        description: resp.task.run_id
          ? `AgentField run ${resp.task.run_id.slice(0, 8)}`
          : resp.task.id,
      })
      setTitle("")
      setRepoUrl("")
      setDescription("")
      setSelected(resp.task)
      setPatchUrl(null)
      await fetchTasks()
    } catch (e) {
      toast.error("Could not create task", { description: errorMessage(e, "Create failed") })
    } finally {
      setCreating(false)
    }
  }

  const openTask = async (task: ShipwrightTask) => {
    setSelected(task)
    setPatchUrl(null)
    try {
      const resp = await api.shipwright.get(task.id)
      setSelected(resp.task)
      setPatchUrl(resp.patches?.[0]?.diff_url ?? null)
    } catch (e) {
      toast.error("Could not load patches", { description: errorMessage(e, "Get failed") })
    }
  }

  return (
    <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
      <div className="flex flex-col gap-4">
        <div className="bg-card rounded-lg border p-4">
          <form className="grid gap-4" onSubmit={createTask}>
            <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
              <Field label="Title">
                <Input
                  value={title}
                  onChange={(e) => setTitle(e.target.value)}
                  required
                  placeholder="Add usage-based billing meter"
                />
              </Field>
              <Field label="Repository URL">
                <Input
                  value={repoUrl}
                  onChange={(e) => setRepoUrl(e.target.value)}
                  required
                  placeholder="https://github.com/acme/product.git"
                />
              </Field>
            </div>
            <Field label="Description">
              <Textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                required
                className="min-h-28"
                placeholder="Describe the change, constraints, and tests Shipwright should run."
              />
            </Field>
            <div className="grid gap-3 md:grid-cols-[180px_minmax(0,1fr)_auto]">
              <Field label="Harness">
                <Select value={harness} onValueChange={(v) => v && setHarness(v)}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {HARNESS_PROVIDERS.map((provider) => (
                      <SelectItem key={provider} value={provider}>
                        {provider}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <Field label="Model">
                <Input
                  value={model}
                  onChange={(e) => setModel(e.target.value)}
                  placeholder="openrouter/google/gemini-2.5-flash"
                />
              </Field>
              <div className="flex items-end">
                <Button type="submit" disabled={creating}>
                  <Send />
                  {creating ? "Queuing" : "Queue task"}
                </Button>
              </div>
            </div>
          </form>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Select value={status} onValueChange={(v) => v && setStatus(v as StatusFilter)}>
            <SelectTrigger className="w-[180px]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {STATUSES.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button variant="outline" size="sm" onClick={fetchTasks} disabled={loading}>
            <RefreshCw className={loading ? "animate-spin" : undefined} />
            Refresh
          </Button>
          <span className="text-muted-foreground text-xs">
            {data.total} task{data.total === 1 ? "" : "s"}
          </span>
        </div>

        {error ? (
          <div className="bg-card text-muted-foreground rounded-lg border border-dashed p-4 text-sm">
            <p className="text-foreground mb-1 font-medium">Couldn&apos;t load Shipwright tasks</p>
            <p>{error}</p>
          </div>
        ) : null}

        <div className="bg-card overflow-hidden rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead>Status</TableHead>
                <TableHead>Task</TableHead>
                <TableHead>Repository</TableHead>
                <TableHead>Run</TableHead>
                <TableHead>Updated</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.tasks.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={5} className="text-muted-foreground h-28 text-center">
                    No Shipwright tasks yet.
                  </TableCell>
                </TableRow>
              ) : (
                data.tasks.map((task) => (
                  <TableRow
                    key={task.id}
                    className="cursor-pointer"
                    onClick={() => void openTask(task)}
                  >
                    <TableCell>
                      <StatusBadge status={task.status} />
                    </TableCell>
                    <TableCell>
                      <div className="max-w-[320px] truncate font-medium">{task.title}</div>
                      <div className="text-muted-foreground font-mono text-xs">
                        {task.id.slice(0, 8)}
                      </div>
                    </TableCell>
                    <TableCell>
                      <span className="text-muted-foreground max-w-[280px] truncate font-mono text-xs">
                        {task.repo_url}
                      </span>
                    </TableCell>
                    <TableCell>
                      {task.run_id ? (
                        <a
                          href={api.traceUrl(task.run_id)}
                          target="_blank"
                          rel="noopener noreferrer"
                          onClick={(e) => e.stopPropagation()}
                          className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1 font-mono text-xs"
                        >
                          {task.run_id.slice(0, 8)}
                          <ExternalLink className="size-3" />
                        </a>
                      ) : (
                        <span className="text-muted-foreground text-xs">—</span>
                      )}
                    </TableCell>
                    <TableCell className="text-muted-foreground text-xs">
                      {formatRelative(task.updated_at)}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
      </div>

      <aside className="bg-card h-fit rounded-lg border p-4">
        <div className="mb-4 flex items-center gap-2">
          <GitPullRequest className="size-4" />
          <h2 className="text-sm font-semibold">Selected task</h2>
        </div>
        {selected ? (
          <div className="flex flex-col gap-3 text-sm">
            <div>
              <div className="font-medium">{selected.title}</div>
              <div className="text-muted-foreground font-mono text-xs">{selected.id}</div>
            </div>
            <StatusBadge status={selected.status} />
            <p className="text-muted-foreground line-clamp-5">{selected.description}</p>
            {selected.run_id ? (
              <Button
                variant="outline"
                size="sm"
                render={
                  <a href={api.traceUrl(selected.run_id)} target="_blank" rel="noopener noreferrer">
                    <ExternalLink />
                    Open AgentField run
                  </a>
                }
              />
            ) : null}
            {patchUrl ? (
              <Button
                variant="outline"
                size="sm"
                render={
                  <a href={patchUrl} target="_blank" rel="noopener noreferrer">
                    <ExternalLink />
                    Open patch / PR
                  </a>
                }
              />
            ) : (
              <p className="text-muted-foreground text-xs">
                Patch or PR link appears after the AgentField run completes.
              </p>
            )}
          </div>
        ) : (
          <p className="text-muted-foreground text-sm">
            Select a task to inspect its AgentField run and patch pointer.
          </p>
        )}
      </aside>
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid gap-1.5">
      <Label>{label}</Label>
      {children}
    </div>
  )
}

function StatusBadge({ status }: { status: ShipwrightStatus }) {
  const variant =
    status === "succeeded"
      ? "default"
      : status === "failed" || status === "cancelled"
        ? "destructive"
        : "secondary"
  return <Badge variant={variant}>{status}</Badge>
}

function errorMessage(error: unknown, fallback: string) {
  if (error instanceof ApiError) return `${error.code}: ${error.message}`
  if (error instanceof Error) return error.message
  return fallback
}

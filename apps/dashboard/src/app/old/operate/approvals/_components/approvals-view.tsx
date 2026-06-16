// SPDX-License-Identifier: Apache-2.0

"use client"

import * as React from "react"
import { CheckCheck, RefreshCw, Send, XCircle } from "lucide-react"
import { toast } from "sonner"

import {
  api,
  ApiError,
  type Approval,
  type ApprovalList,
  type ApprovalStatus,
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
type StatusFilter = "all" | ApprovalStatus

const STATUSES: { value: StatusFilter; label: string }[] = [
  { value: "pending", label: "Pending" },
  { value: "all", label: "All statuses" },
  { value: "approved", label: "Approved" },
  { value: "denied", label: "Denied" },
  { value: "cancelled", label: "Cancelled" },
]

export function ApprovalsView({ initial }: { initial: ApprovalList }) {
  const [data, setData] = React.useState<ApprovalList>(initial)
  const [status, setStatus] = React.useState<StatusFilter>("pending")
  const [offset, setOffset] = React.useState(0)
  const [kindFilter, setKindFilter] = React.useState("")
  const [loading, setLoading] = React.useState(false)
  const [creating, setCreating] = React.useState(false)
  const [selected, setSelected] = React.useState<Approval | null>(initial.approvals[0] ?? null)

  const [kind, setKind] = React.useState("")
  const [payload, setPayload] = React.useState("{\n  \n}")
  const [decisionNote, setDecisionNote] = React.useState("")

  React.useEffect(() => {
    setOffset(0)
  }, [status, kindFilter])

  const fetchApprovals = React.useCallback(async () => {
    setLoading(true)
    try {
      const list = await api.approvals.list({
        status: status === "all" ? undefined : status,
        kind: kindFilter.trim() || undefined,
        limit: PAGE_SIZE,
        offset,
      })
      setData(list)
      if (selected && !list.approvals.some((a) => a.id === selected.id)) {
        setSelected(list.approvals[0] ?? null)
      }
    } catch (e) {
      toast.error("Could not load approvals", { description: errorMessage(e) })
      setData({ approvals: [], total: 0, has_more: false })
    } finally {
      setLoading(false)
    }
  }, [kindFilter, offset, selected, status])

  React.useEffect(() => {
    void fetchApprovals()
  }, [fetchApprovals])

  const createApproval = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setCreating(true)
    try {
      const created = await api.approvals.request({
        kind,
        payload: parsePayload(payload),
      })
      toast.success("Approval requested", { description: created.kind })
      setKind("")
      setPayload("{\n  \n}")
      setSelected(created)
      await fetchApprovals()
    } catch (e) {
      toast.error("Could not request approval", { description: errorMessage(e) })
    } finally {
      setCreating(false)
    }
  }

  const decide = async (approval: Approval, decision: "approved" | "denied" | "cancelled") => {
    try {
      const updated = await api.approvals.decide(approval.id, {
        status: decision,
        decision_note: decisionNote.trim() || undefined,
      })
      toast.success(`Approval ${decision}`, { description: approval.kind })
      setDecisionNote("")
      setSelected(updated)
      await fetchApprovals()
    } catch (e) {
      toast.error("Decision failed", { description: errorMessage(e) })
    }
  }

  return (
    <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
      <div className="flex flex-col gap-4">
        <div className="bg-card rounded-lg border p-4">
          <form className="grid gap-4" onSubmit={createApproval}>
            <Field label="Kind">
              <Input
                value={kind}
                onChange={(e) => setKind(e.target.value)}
                required
                placeholder="deploy_to_prod"
              />
            </Field>
            <Field label="Payload JSON">
              <Textarea
                value={payload}
                onChange={(e) => setPayload(e.target.value)}
                className="min-h-28 font-mono text-xs"
              />
            </Field>
            <div>
              <Button type="submit" disabled={creating}>
                <Send />
                {creating ? "Requesting" : "Request approval"}
              </Button>
            </div>
          </form>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Select value={status} onValueChange={(v) => v && setStatus(v as StatusFilter)}>
            <SelectTrigger className="w-[170px]">
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
          <Input
            className="w-[220px]"
            value={kindFilter}
            onChange={(e) => setKindFilter(e.target.value)}
            placeholder="Filter kind"
          />
          <Button variant="outline" size="sm" onClick={fetchApprovals} disabled={loading}>
            <RefreshCw className={loading ? "animate-spin" : undefined} />
            Refresh
          </Button>
          <span className="text-muted-foreground text-xs">
            {data.total} request{data.total === 1 ? "" : "s"}
          </span>
        </div>

        <div className="bg-card overflow-hidden rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead>Status</TableHead>
                <TableHead>Kind</TableHead>
                <TableHead>Requested</TableHead>
                <TableHead>Decided</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.approvals.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={4} className="text-muted-foreground h-28 text-center">
                    No approval requests.
                  </TableCell>
                </TableRow>
              ) : (
                data.approvals.map((approval) => (
                  <TableRow
                    key={approval.id}
                    className="cursor-pointer"
                    onClick={() => setSelected(approval)}
                  >
                    <TableCell>
                      <StatusBadge status={approval.status} />
                    </TableCell>
                    <TableCell>
                      <div className="font-medium">{approval.kind}</div>
                      <div className="text-muted-foreground font-mono text-xs">
                        {approval.id.slice(0, 8)}
                      </div>
                    </TableCell>
                    <TableCell className="text-muted-foreground text-xs">
                      {formatRelative(approval.created_at)}
                    </TableCell>
                    <TableCell className="text-muted-foreground text-xs">
                      {approval.decided_at ? formatRelative(approval.decided_at) : "—"}
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
          <CheckCheck className="size-4" />
          <h2 className="text-sm font-semibold">Decision</h2>
        </div>
        {selected ? (
          <div className="flex flex-col gap-3 text-sm">
            <div>
              <div className="font-medium">{selected.kind}</div>
              <div className="text-muted-foreground font-mono text-xs">{selected.id}</div>
            </div>
            <StatusBadge status={selected.status} />
            <pre className="bg-muted max-h-56 overflow-auto rounded-md p-3 text-xs">
              {JSON.stringify(selected.payload, null, 2)}
            </pre>
            <Textarea
              value={decisionNote}
              onChange={(e) => setDecisionNote(e.target.value)}
              placeholder="Decision note"
            />
            <div className="grid grid-cols-2 gap-2">
              <Button
                size="sm"
                disabled={selected.status !== "pending"}
                onClick={() => void decide(selected, "approved")}
              >
                <CheckCheck />
                Approve
              </Button>
              <Button
                size="sm"
                variant="destructive"
                disabled={selected.status !== "pending"}
                onClick={() => void decide(selected, "denied")}
              >
                <XCircle />
                Deny
              </Button>
            </div>
            <Button
              size="sm"
              variant="outline"
              disabled={selected.status !== "pending"}
              onClick={() => void decide(selected, "cancelled")}
            >
              Cancel request
            </Button>
          </div>
        ) : (
          <p className="text-muted-foreground text-sm">
            Select a request to inspect its payload and decide it.
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

function StatusBadge({ status }: { status: ApprovalStatus }) {
  const variant =
    status === "approved"
      ? "default"
      : status === "denied" || status === "cancelled"
        ? "destructive"
        : "secondary"
  return <Badge variant={variant}>{status}</Badge>
}

function parsePayload(raw: string) {
  const trimmed = raw.trim()
  if (!trimmed) return {}
  const parsed = JSON.parse(trimmed)
  if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("payload must be a JSON object")
  }
  return parsed as Record<string, unknown>
}

function errorMessage(error: unknown) {
  if (error instanceof ApiError) return `${error.code}: ${error.message}`
  if (error instanceof Error) return error.message
  return "Unknown error"
}

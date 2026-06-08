// SPDX-License-Identifier: Apache-2.0

// suite.approvals.* — general human decision gates for AF Stack workflows.
//
// This is business workflow metadata in AF Stack, not AgentField run state.

import { z } from "zod"
import { request, type HttpOptions } from "./_http.js"

export const ApprovalStatusSchema = z.enum(["pending", "approved", "denied", "cancelled"])
export type ApprovalStatus = z.infer<typeof ApprovalStatusSchema>

export const ApprovalSchema = z.object({
  id: z.string(),
  tenantId: z.string(),
  requestedBy: z.string().nullable(),
  kind: z.string(),
  payload: z.record(z.string(), z.unknown()),
  status: ApprovalStatusSchema,
  decidedBy: z.string().nullable(),
  decidedAt: z.string().nullable(),
  decisionNote: z.string().nullable(),
  createdAt: z.string(),
  updatedAt: z.string(),
})
export type Approval = z.infer<typeof ApprovalSchema>

export const ApprovalListSchema = z.object({
  approvals: z.array(ApprovalSchema),
  total: z.number(),
  hasMore: z.boolean(),
})
export type ApprovalList = z.infer<typeof ApprovalListSchema>

export interface RequestApprovalInput extends HttpOptions {
  kind: string
  payload?: Record<string, unknown>
}

export interface ListApprovalsOptions extends HttpOptions {
  status?: ApprovalStatus | string
  kind?: string
  limit?: number
  offset?: number
}

export interface DecideApprovalInput extends HttpOptions {
  status: Extract<ApprovalStatus, "approved" | "denied" | "cancelled">
  decisionNote?: string
}

export async function requestApproval(input: RequestApprovalInput): Promise<Approval> {
  const { kind, payload, ...http } = input
  if (kind.trim() === "") throw new Error("approval kind must be a non-empty string")
  const raw = await request<unknown>(
    "POST",
    "/approvals",
    { kind, payload: payload ?? {} },
    http,
  )
  return ApprovalSchema.parse(camelizeApproval(raw))
}

export async function get(id: string, opts: HttpOptions = {}): Promise<Approval> {
  if (id.trim() === "") throw new Error("approval id must be a non-empty string")
  const raw = await request<unknown>(
    "GET",
    `/approvals/${encodeURIComponent(id)}`,
    null,
    opts,
  )
  return ApprovalSchema.parse(camelizeApproval(raw))
}

export async function list(opts: ListApprovalsOptions = {}): Promise<ApprovalList> {
  const { status, kind, limit = 25, offset = 0, ...http } = opts
  const qs = new URLSearchParams()
  if (status !== undefined && status !== "") qs.set("status", status)
  if (kind !== undefined && kind !== "") qs.set("kind", kind)
  qs.set("limit", String(limit))
  qs.set("offset", String(offset))
  const raw = await request<unknown>(
    "GET",
    `/approvals?${qs.toString()}`,
    null,
    http,
  )
  return ApprovalListSchema.parse(camelizeApprovalList(raw))
}

export async function decide(
  id: string,
  input: DecideApprovalInput,
): Promise<Approval> {
  if (id.trim() === "") throw new Error("approval id must be a non-empty string")
  const { status, decisionNote, ...http } = input
  const raw = await request<unknown>(
    "POST",
    `/approvals/${encodeURIComponent(id)}/decide`,
    { status, decision_note: decisionNote },
    http,
  )
  return ApprovalSchema.parse(camelizeApproval(raw))
}

function camelizeApprovalList(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    approvals: Array.isArray(src.approvals) ? src.approvals.map(camelizeApproval) : src.approvals,
    total: src.total,
    hasMore: src.has_more ?? src.hasMore,
  }
}

function camelizeApproval(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    id: src.id,
    tenantId: src.tenant_id ?? src.tenantId,
    requestedBy: src.requested_by ?? src.requestedBy ?? null,
    kind: src.kind,
    payload: src.payload ?? {},
    status: src.status,
    decidedBy: src.decided_by ?? src.decidedBy ?? null,
    decidedAt: src.decided_at ?? src.decidedAt ?? null,
    decisionNote: src.decision_note ?? src.decisionNote ?? null,
    createdAt: src.created_at ?? src.createdAt,
    updatedAt: src.updated_at ?? src.updatedAt,
  }
}

export const approvals = {
  request: requestApproval,
  get,
  list,
  decide,
} as const

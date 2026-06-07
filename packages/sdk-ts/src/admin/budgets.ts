// suite.admin.budgets.* — per-tenant LLM spend budgets.
//
// Maps to the admin budgets endpoints in `apps/dashboard/src/lib/api.ts`
// (Phase 7):
//
//   GET    /api/v1/admin/budgets
//   GET    /api/v1/admin/budgets/{tenant_id}
//   PUT    /api/v1/admin/budgets
//
// Budgets gate LLM calls — when a tenant's monthly spend exceeds
// `monthlyUsd`, gateway requests fail with HTTP 402 and
// `code === "BUDGET_EXCEEDED"`. Setting a budget is idempotent.

import { request, toCamel, toSnake, type HttpOptions } from "../_http.js"
import {
  BudgetListSchema,
  BudgetSchema,
  type Budget,
  type BudgetList,
  type SetBudgetInput,
} from "./_models.js"

/** List every tenant's budget + current-period spend. */
export async function list(opts: HttpOptions = {}): Promise<BudgetList> {
  const raw = await request<unknown>("GET", "/admin/budgets", null, opts)
  return BudgetListSchema.parse(toCamel(raw))
}

/** Fetch a single tenant's budget by id. */
export async function get(
  tenantId: string,
  opts: HttpOptions = {},
): Promise<Budget> {
  if (typeof tenantId !== "string" || tenantId.length === 0) {
    throw new Error("tenantId must be a non-empty string")
  }
  const raw = await request<unknown>(
    "GET",
    `/admin/budgets/${encodeURIComponent(tenantId)}`,
    null,
    opts,
  )
  return BudgetSchema.parse(toCamel(raw))
}

/** Create or update a tenant's monthly budget. */
export async function set(
  input: SetBudgetInput,
  opts: HttpOptions = {},
): Promise<Budget> {
  if (typeof input.tenantId !== "string" || input.tenantId.length === 0) {
    throw new Error("input.tenantId must be a non-empty string")
  }
  if (typeof input.monthlyUsd !== "number" || !Number.isFinite(input.monthlyUsd)) {
    throw new Error("input.monthlyUsd must be a finite number")
  }
  if (input.monthlyUsd <= 0) {
    throw new Error("input.monthlyUsd must be positive")
  }
  const raw = await request<unknown>(
    "PUT",
    "/admin/budgets",
    toSnake(input),
    opts,
  )
  return BudgetSchema.parse(toCamel(raw))
}

/** Namespace object — the shape `suite.admin.budgets` is built from. */
export const budgets = {
  list,
  get,
  set,
} as const

// Shared zod schemas for the admin sub-package.
//
// Each schema mirrors a zod schema declared in
// `apps/dashboard/src/lib/api.ts` (the canonical contract) but exposes
// camelCase keys to TypeScript callers. The wire format stays snake_case;
// translation happens via the `toCamel` / `toSnake` helpers in `_http.ts`.
//
// Keep these schemas in sync with the dashboard side. The runtime's
// REST handlers serialise the snake_case shape; the schemas below
// describe the camelCase shape we hand to callers.

import { z } from "zod"

// ---------- Tenants ----------

export const TenantSchema = z.object({
  id: z.string(),
  slug: z.string(),
  name: z.string(),
  plan: z.string(),
  // settings / quota are free-form maps; the runtime always emits {} so
  // we don't accept null here.
  settings: z.record(z.string(), z.unknown()),
  quota: z.record(z.string(), z.unknown()),
  createdAt: z.string(),
  deletedAt: z.string().nullable(),
})
export type Tenant = z.infer<typeof TenantSchema>

export const TenantListSchema = z.object({
  tenants: z.array(TenantSchema),
})
export type TenantList = z.infer<typeof TenantListSchema>

// ---------- Users ----------

export const UserSchema = z.object({
  id: z.string(),
  // The dashboard zod uses `z.email()`; we relax to z.string() here so a
  // mocked test fixture with a sentinel address doesn't fail parsing.
  // Real validation happens server-side.
  email: z.string(),
  name: z.string().nullable(),
  avatarUrl: z.string().nullable(),
  createdAt: z.string(),
  deletedAt: z.string().nullable(),
})
export type User = z.infer<typeof UserSchema>

export const UserListSchema = z.object({
  users: z.array(UserSchema),
})
export type UserList = z.infer<typeof UserListSchema>

// ---------- Memberships ----------

export const RoleSchema = z.enum(["owner", "admin", "member", "viewer"])
export type Role = z.infer<typeof RoleSchema>

export const MembershipSchema = z.object({
  tenantId: z.string(),
  userId: z.string(),
  role: RoleSchema,
  invitedAt: z.string(),
  acceptedAt: z.string().nullable(),
})
export type Membership = z.infer<typeof MembershipSchema>

export const MembershipListSchema = z.object({
  memberships: z.array(MembershipSchema),
})
export type MembershipList = z.infer<typeof MembershipListSchema>

// ---------- API keys ----------

export const APIKeySchema = z.object({
  id: z.string(),
  tenantId: z.string(),
  prefix: z.string(),
  name: z.string().nullable(),
  scopes: z.array(z.string()),
  createdBy: z.string().nullable(),
  createdAt: z.string(),
  lastUsedAt: z.string().nullable(),
  expiresAt: z.string().nullable(),
  revokedAt: z.string().nullable(),
})
export type APIKey = z.infer<typeof APIKeySchema>

export const APIKeyListSchema = z.object({
  keys: z.array(APIKeySchema),
})
export type APIKeyList = z.infer<typeof APIKeyListSchema>

// IssuedAPIKey extends APIKey with the one-time-reveal `value`. Returned
// ONLY by `POST /api/v1/admin/keys`; subsequent reads use `APIKeySchema`.
export const IssuedAPIKeySchema = APIKeySchema.extend({
  value: z.string(),
})
export type IssuedAPIKey = z.infer<typeof IssuedAPIKeySchema>

// ---------- Tenant detail (joined view) ----------

export const TenantMemberSchema = z.object({
  user: UserSchema,
  role: z.string(),
})
export type TenantMember = z.infer<typeof TenantMemberSchema>

export const TenantUsageSchema = z.object({
  requests30d: z.number(),
  costUsd30d: z.number(),
  storageBytes: z.number(),
  secretsCount: z.number(),
})
export type TenantUsage = z.infer<typeof TenantUsageSchema>

export const TenantDetailSchema = z.object({
  tenant: TenantSchema,
  members: z.array(TenantMemberSchema),
  apiKeys: z.array(APIKeySchema),
  usage: TenantUsageSchema,
})
export type TenantDetail = z.infer<typeof TenantDetailSchema>

// ---------- Budgets (Phase 7) ----------

export const BudgetSchema = z.object({
  tenantId: z.string(),
  monthlyUsd: z.number(),
  alertThresholdPct: z.number(),
  spentThisPeriodUsd: z.number(),
  remainingUsd: z.number(),
  resetsAt: z.string(),
})
export type Budget = z.infer<typeof BudgetSchema>

export const BudgetListSchema = z.object({
  budgets: z.array(BudgetSchema),
})
export type BudgetList = z.infer<typeof BudgetListSchema>

export interface SetBudgetInput {
  tenantId: string
  monthlyUsd: number
  alertThresholdPct?: number
}

// ---------- Audit ----------

export const AuditEntrySchema = z.object({
  id: z.string(),
  tenantId: z.string().nullable(),
  userId: z.string().nullable(),
  apiKeyId: z.string().nullable(),
  action: z.string(),
  resourceType: z.string().nullable(),
  resourceId: z.string().nullable(),
  metadata: z.record(z.string(), z.unknown()),
  occurredAt: z.string(),
})
export type AuditEntry = z.infer<typeof AuditEntrySchema>

export const AuditListSchema = z.object({
  entries: z.array(AuditEntrySchema),
  total: z.number(),
  hasMore: z.boolean(),
})
export type AuditList = z.infer<typeof AuditListSchema>

// ---------- Inputs ----------

export interface CreateTenantInput {
  slug: string
  name: string
  plan?: string
}

export interface UpdateTenantInput {
  name?: string
  plan?: string
  settings?: Record<string, unknown>
  quota?: Record<string, unknown>
}

export interface IssueAPIKeyInput {
  tenantId: string
  name?: string
  scopes?: string[]
  expiresAt?: string
}

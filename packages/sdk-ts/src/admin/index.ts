// suite.admin.* — multi-tenancy admin operations.
//
// Re-exports the per-namespace modules so callers can write either:
//
//   import { suite } from "@af-stack/sdk"
//   await suite.admin.tenants.list()
//
// or the flat form:
//
//   import { tenants as adminTenants } from "@af-stack/sdk/admin"
//   await adminTenants.list()
//
// All operations are gated by the runtime's `multi-tenancy` module
// flag — calls against a disabled runtime surface as `SuiteError` with
// `code === "MT_DISABLED"`.

import { tenants } from "./tenants.js"
import { users } from "./users.js"
import { memberships } from "./memberships.js"
import { keys } from "./keys.js"
import { audit } from "./audit.js"
import { budgets } from "./budgets.js"

export { tenants, users, memberships, keys, audit, budgets }

export type {
  CreateTenantInput,
  UpdateTenantInput,
  IssueAPIKeyInput,
  Tenant,
  TenantList,
  TenantDetail,
  TenantMember,
  TenantUsage,
  User,
  UserList,
  Membership,
  MembershipList,
  Role,
  APIKey,
  APIKeyList,
  IssuedAPIKey,
  AuditEntry,
  AuditList,
  Budget,
  BudgetList,
  SetBudgetInput,
} from "./_models.js"

export {
  TenantSchema,
  TenantListSchema,
  UserSchema,
  UserListSchema,
  MembershipSchema,
  MembershipListSchema,
  RoleSchema,
  APIKeySchema,
  APIKeyListSchema,
  IssuedAPIKeySchema,
  AuditEntrySchema,
  AuditListSchema,
  TenantDetailSchema,
  TenantMemberSchema,
  TenantUsageSchema,
  BudgetSchema,
  BudgetListSchema,
} from "./_models.js"

/** Combined namespace: `suite.admin.{tenants,users,memberships,keys,audit,budgets}`. */
export const admin = {
  tenants,
  users,
  memberships,
  keys,
  audit,
  budgets,
} as const

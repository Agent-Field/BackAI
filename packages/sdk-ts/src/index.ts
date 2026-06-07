// AF Stack — Suite SDK for TypeScript
//
// Two equivalent import styles:
//
//   import { suite, ctx } from "@af-stack/sdk"
//   await suite.agents.call("notable-ai.summarize", { text: "..." })
//
//   import { agents, ctx } from "@af-stack/sdk"
//   await agents.call("notable-ai.summarize", { text: "..." })
//
// Inside an AgentField agent process, use the AgentField SDK to *define*
// agents. Use this suite SDK to *call* them and to use suite infrastructure.
//
// All model calls route through AgentField — this SDK never reaches a model
// provider directly. See docs/sdk-strategy.md.

export const VERSION = "0.0.1"

export { ctx, withCtx, type CtxValues } from "./ctx.js"
export {
  agents,
  call,
  callAsync,
  stream,
  status,
  cancel,
  approve,
  deny,
  pendingApprovals,
  type CallResult,
  type AsyncCallResult,
  type ExecutionStatus,
  type PendingApproval,
  type CallOptions,
  type CallAsyncOptions,
  type ApproveOptions,
  type DenyOptions,
} from "./agents.js"
export { SuiteError, type HttpOptions, type SseEvent } from "./_http.js"
export {
  jobs,
  enqueue as enqueueJob,
  get as getJob,
  retry as retryJob,
  list as listJobs,
  JobSchema,
  JobListSchema,
  JobStateSchema,
  type Job,
  type JobList,
  type JobState,
  type EnqueueJobOptions,
  type ListJobsOptions,
} from "./jobs.js"
export { secrets, get as getSecret } from "./secrets.js"
export {
  storage,
  upload as uploadObject,
  download as downloadObject,
  signedURL as signedObjectURL,
  delete as deleteObject,
  list as listObjects,
  StorageObjectSchema,
  StorageListSchema,
  SignedURLSchema,
  type StorageObject,
  type StorageList,
  type SignedURL,
  type UploadOptions,
  type ListStorageOptions,
} from "./storage.js"

// ─── Admin sub-package ────────────────────────────────────────────────────
//
// Admin verbs live under their own namespace so operational and
// administrative SDKs stay visibly separated (see docs/sdk-strategy.md).
// All admin endpoints are gated by the runtime's `multi-tenancy` module
// flag — calls against a disabled runtime surface as `SuiteError` with
// `code === "MT_DISABLED"`.

export {
  admin,
  tenants as adminTenants,
  users as adminUsers,
  memberships as adminMemberships,
  keys as adminKeys,
  audit as adminAudit,
  budgets as adminBudgets,
  TenantSchema,
  TenantListSchema,
  TenantDetailSchema,
  TenantMemberSchema,
  TenantUsageSchema,
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
  BudgetSchema,
  BudgetListSchema,
  type Tenant,
  type TenantList,
  type TenantDetail,
  type TenantMember,
  type TenantUsage,
  type CreateTenantInput,
  type UpdateTenantInput,
  type User,
  type UserList,
  type Membership,
  type MembershipList,
  type Role,
  type APIKey,
  type APIKeyList,
  type IssuedAPIKey,
  type IssueAPIKeyInput,
  type AuditEntry,
  type AuditList,
  type Budget,
  type BudgetList,
  type SetBudgetInput,
} from "./admin/index.js"

// ─── LLM gateway + cost (Phase 7) ─────────────────────────────────────────

export {
  llm,
  chat,
  embed,
  models as llmModels,
  cacheStats as llmCacheStats,
  LLMModelSchema,
  LLMModelListSchema,
  CacheStatsSchema,
  type LLMModel,
  type LLMModelList,
  type CacheStats,
  type ChatMessage,
  type ChatChunk,
  type ChatOptions,
  type EmbedOptions,
} from "./llm.js"

export {
  cost,
  events as costEvents,
  CostEventSchema,
  CostEventListSchema,
  type CostEvent,
  type CostEventList,
  type ListCostEventsOptions,
} from "./cost.js"

import { agents } from "./agents.js"
import { jobs } from "./jobs.js"
import { secrets } from "./secrets.js"
import { storage } from "./storage.js"
import { admin } from "./admin/index.js"
import { llm } from "./llm.js"
import { cost } from "./cost.js"

/** Top-level namespace: `suite.agents.*`, `suite.jobs.*`, `suite.secrets.*`, `suite.storage.*`, `suite.llm.*`, `suite.cost.*`, `suite.admin.*`. */
export const suite = {
  agents,
  jobs,
  secrets,
  storage,
  llm,
  cost,
  admin,
} as const

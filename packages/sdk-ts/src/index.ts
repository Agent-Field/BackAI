// SPDX-License-Identifier: Apache-2.0

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
  realtime,
  subscribe as subscribeRealtime,
  type RealtimeEvent,
  type RealtimeFilter,
  type SubscribeOptions as RealtimeSubscribeOptions,
} from "./realtime.js"
export {
  runs,
  subscribe as subscribeRuns,
  subscribeById as subscribeRunEvents,
  type AgentRunEvent,
  type RunEvent,
  type RunEventType,
  type RunsSubscribeFilter,
  type SubscribeRunOptions,
} from "./runs.js"
export {
  searchIndex,
  search,
  upsert as upsertSearchDocument,
  delete as deleteSearchDocument,
  SearchModeSchema,
  SearchDocumentSchema,
  SearchHitSchema,
  SearchResultSchema,
  type SearchMode,
  type SearchDocument,
  type SearchHit,
  type SearchResult,
  type SearchOptions,
  type UpsertSearchDocumentOptions,
  type DeleteSearchDocumentOptions,
} from "./search.js"
export {
  activity,
  log as logActivity,
  list as listActivity,
  ActivityActorTypeSchema,
  ActivityEntrySchema,
  ActivityListSchema,
  type ActivityActorType,
  type ActivityEntry,
  type ActivityList,
  type LogActivityOptions,
  type ListActivityOptions,
} from "./activity.js"
export {
  flags,
  list as listFeatureFlags,
  set as setFeatureFlag,
  isEnabled as isFeatureFlagEnabled,
  FeatureFlagSchema,
  FeatureFlagListSchema,
  type FeatureFlag,
  type FeatureFlagList,
  type SetFeatureFlagOptions,
} from "./flags.js"
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
  type DownloadStorageOptions,
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
  image as llmImage,
  speech as llmSpeech,
  transcribe as llmTranscribe,
  models as llmModels,
  cacheStats as llmCacheStats,
  audio,
  images,
  audioTranslate,
  imageEdit,
  imageVariations,
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
  type ImageOptions,
  type SpeechOptions,
  type TranscriptionOptions,
  type ImageEditOptions,
  type ImageVariationOptions,
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

// ─── Memory (Phase 8.3) ───────────────────────────────────────────────────

export {
  memory,
  get as getMemory,
  put as putMemory,
  delete as deleteMemory,
  list as listMemory,
  search as searchMemory,
  MemoryScopeSchema,
  MemoryEntrySchema,
  MemoryListSchema,
  MemorySearchHitSchema,
  MemorySearchResultSchema,
  type MemoryScope,
  type MemoryEntry,
  type MemoryList,
  type MemorySearchHit,
  type MemorySearchResult,
  type PutMemoryOptions,
  type ListMemoryOptions,
  type SearchMemoryOptions,
} from "./memory.js"

// ─── Sandbox (Phase 9.3) ──────────────────────────────────────────────────

export {
  sandbox,
  run as runSandbox,
  list as listSandboxRuns,
  get as getSandboxRun,
  stop as stopSandboxRun,
  pool as sandboxPool,
  SandboxStatusSchema,
  SandboxNetworkSchema,
  SandboxCapabilitiesSchema,
  SandboxRunSchema,
  SandboxRunListSchema,
  SandboxPoolStatsSchema,
  type SandboxStatus,
  type SandboxNetwork,
  type SandboxCapabilities,
  type SandboxRun,
  type SandboxRunList,
  type SandboxPoolStats,
  type RunSandboxOptions,
  type ListSandboxRunsOptions,
} from "./sandbox.js"

// ─── Billing (Phase 10.4) ─────────────────────────────────────────────────

export {
  billing,
  customers as billingCustomers,
  customer as billingCustomer,
  meters as billingMeters,
  portalLink as billingPortalLink,
  meter as billingMeter,
  hasBudget as billingHasBudget,
  plans as billingPlans,
  checkout as billingCheckout,
  entitlements as billingEntitlements,
  BillingCustomerSchema,
  BillingCustomerListSchema,
  UsageMeterSchema,
  UsageMeterListSchema,
  PortalLinkSchema,
  BillingPlanSchema,
  BillingPlanListSchema,
  CheckoutResultSchema,
  TenantEntitlementsSchema,
  type BillingCustomer,
  type BillingCustomerList,
  type UsageMeter,
  type UsageMeterList,
  type PortalLink,
  type BillingPlan,
  type BillingPlanList,
  type CheckoutResult,
  type TenantEntitlements,
  type Bucket as BillingBucket,
  type ListMetersOptions as ListBillingMetersOptions,
  type PortalLinkOptions,
  type MeterOptions as BillingMeterOptions,
  type CheckoutOptions as BillingCheckoutOptions,
  type EntitlementsOptions as BillingEntitlementsOptions,
} from "./billing.js"

// ─── Auth identity ────────────────────────────────────────────────────────

export { auth, whoami as authWhoami, type WhoAmI } from "./auth.js"

// ─── Notifications (Phase 10.1) ───────────────────────────────────────────

export {
  notifications,
  send as sendNotification,
  email as sendEmail,
  list as listNotifications,
  get as getNotification,
  stats as notificationStats,
  NotificationKindSchema,
  NotificationStatusSchema,
  NotificationSchema,
  NotificationListSchema,
  NotificationStatsSchema,
  AdapterCountSchema,
  type NotificationKind,
  type NotificationStatus,
  type Notification,
  type NotificationList,
  type NotificationStats,
  type AdapterCount,
  type SendNotificationOptions,
  type EmailNotificationOptions,
  type ListNotificationsOptions,
} from "./notifications.js"

// ─── Webhooks (Phase 10.2 + 10.3) ────────────────────────────────────────

export {
  webhooks,
  send as sendWebhook,
  list as listWebhookDeliveries,
  get as getWebhookDelivery,
  retry as retryWebhookDelivery,
  WebhookDirectionSchema,
  WebhookStatusSchema,
  WebhookDeliverySchema,
  WebhookDeliveryListSchema,
  type WebhookDirection,
  type WebhookStatus,
  type WebhookDelivery,
  type WebhookDeliveryList,
  type SendWebhookOptions,
  type ListWebhookDeliveriesOptions,
} from "./webhooks.js"

// ─── OAuth-on-behalf-of-user ─────────────────────────────────────────────

export {
  oauth,
  authorizeUrl as oauthAuthorizeUrl,
  connected as oauthConnected,
  token as oauthToken,
  disconnect as oauthDisconnect,
  ConnectionSchema as OAuthConnectionSchema,
  ConnectionListSchema as OAuthConnectionListSchema,
  AccessTokenSchema as OAuthAccessTokenSchema,
  type Connection as OAuthConnection,
  type ConnectionList as OAuthConnectionList,
  type AccessToken as OAuthAccessToken,
  type AuthorizeUrlOptions as OAuthAuthorizeUrlOptions,
  type TokenOptions as OAuthTokenOptions,
  type DisconnectOptions as OAuthDisconnectOptions,
} from "./oauth.js"

// ─── Skills (Phase 11.3) ─────────────────────────────────────────────────

export {
  skills,
  list as listSkills,
  install as installSkill,
  uninstall as uninstallSkill,
  attach as attachSkill,
  SkillSchema,
  SkillListSchema,
  type Skill,
  type SkillList,
  type InstallSkillInput,
  type AttachSkillInput,
  type ListSkillsOptions,
} from "./skills.js"

// ─── Harnesses (Phase 11.4) ──────────────────────────────────────────────

export {
  harnesses,
  list as listHarnesses,
  get as getHarness,
  probe as probeHarness,
  HarnessProviderSchema,
  HarnessStatusSchema,
  HarnessSchema,
  HarnessListSchema,
  type HarnessProvider,
  type HarnessStatus,
  type Harness,
  type HarnessList,
} from "./harnesses.js"

// ─── Tools / MCP (Phase 11.2) ────────────────────────────────────────────

export {
  tools,
  listMcpServers,
  addMcpServer,
  removeMcpServer,
  enableMcpServer,
  listMcpTools,
  callMcp,
  listAdapters as listToolAdapters,
  setAdapterEnabled as setToolAdapterEnabled,
  callAdapter as callToolAdapter,
  ToolAdapterIdSchema,
  BuiltinToolDefinitionSchema,
  ToolAdapterSchema,
  ToolAdapterListSchema,
  ToolAdapterCallResultSchema,
  MCPTransportSchema,
  MCPServerStatusSchema,
  MCPServerSchema,
  MCPServerListSchema,
  MCPToolSchema,
  MCPToolListSchema,
  MCPCallResultSchema,
  type MCPTransport,
  type MCPServerStatus,
  type MCPServer,
  type MCPServerList,
  type MCPTool,
  type MCPToolList,
  type MCPCallResult,
  type ToolAdapterId,
  type BuiltinToolDefinition,
  type ToolAdapter,
  type ToolAdapterList,
  type ToolAdapterCallResult,
  type AddMCPServerOptions,
  type ListMCPToolsOptions,
  type SetToolAdapterEnabledOptions,
  type CallToolAdapterOptions,
} from "./tools.js"
export {
  approvals,
  ApprovalStatusSchema,
  ApprovalSchema,
  ApprovalListSchema,
  type ApprovalStatus,
  type Approval,
  type ApprovalList,
  type RequestApprovalInput,
  type ListApprovalsOptions,
  type DecideApprovalInput,
} from "./approvals.js"
export {
  shipwright,
  ShipwrightStatusSchema,
  ShipwrightTaskSchema,
  ShipwrightPatchSchema,
  ShipwrightTaskResponseSchema,
  ShipwrightTaskListSchema,
  type ShipwrightStatus,
  type ShipwrightTask,
  type ShipwrightPatch,
  type ShipwrightTaskResponse,
  type ShipwrightTaskList,
  type CreateShipwrightTaskInput,
  type ListShipwrightTasksOptions,
  type CompleteShipwrightTaskInput,
} from "./shipwright.js"

import { agents } from "./agents.js"
import { jobs } from "./jobs.js"
import { secrets } from "./secrets.js"
import { storage } from "./storage.js"
import { admin } from "./admin/index.js"
import { llm, audio, images } from "./llm.js"
import { cost } from "./cost.js"
import { memory } from "./memory.js"
import { search, searchIndex } from "./search.js"
import { activity } from "./activity.js"
import { flags } from "./flags.js"
import { runs } from "./runs.js"
import { sandbox } from "./sandbox.js"
import { notifications } from "./notifications.js"
import { billing } from "./billing.js"
import { webhooks } from "./webhooks.js"
import { tools } from "./tools.js"
import { harnesses } from "./harnesses.js"
import { oauth } from "./oauth.js"
import { shipwright } from "./shipwright.js"
import { approvals } from "./approvals.js"
import { auth } from "./auth.js"

/** Top-level namespace: `suite.agents.*`, `suite.approvals.*`, `suite.shipwright.*`, `suite.search(...)`, `suite.searchIndex.*`, `suite.activity.*`, `suite.flags.*`, `suite.runs.*`, `suite.jobs.*`, `suite.secrets.*`, `suite.storage.*`, `suite.llm.*`, `suite.cost.*`, `suite.memory.*`, `suite.sandbox.*`, `suite.notifications.*`, `suite.webhooks.*`, `suite.billing.*`, `suite.tools.*`, `suite.admin.*`. */
export const suite = {
	agents,
	approvals,
	auth,
	shipwright,
	search,
  searchIndex,
  activity,
  flags,
  runs,
  jobs,
  secrets,
  storage,
  llm,
  audio,
  images,
  cost,
  memory,
  sandbox,
  notifications,
  webhooks,
  billing,
  tools,
  harnesses,
  oauth,
  admin,
} as const

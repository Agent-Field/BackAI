// Single typed client for the suite runtime REST API.
//
// Every fetch from the dashboard goes through `api`. Adding a new endpoint
// means editing exactly one file. zod schemas validate the responses at the
// network boundary so the rest of the app works with safe types.
//
// On the server, calls go directly to the runtime (env: RUNTIME_URL).
// On the client, calls go through Next.js relative paths so they pick up
// the dashboard's auth cookie.

import { z } from "zod"

const isServer = typeof window === "undefined"

function baseUrl(): string {
  if (isServer) {
    // On the server, prefer internal Docker DNS / loopback.
    return process.env.RUNTIME_URL ?? "http://localhost:8080"
  }
  // In the browser, go through the dashboard's same-origin proxy
  // (see next.config.ts rewrites). This avoids CORS and keeps the
  // runtime URL invisible to the client.
  return ""
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly details?: unknown
  constructor(message: string, status: number, code: string, details?: unknown) {
    super(message)
    this.status = status
    this.code = code
    this.details = details
  }
}

type RequestInitWithJson = Omit<RequestInit, "body"> & { json?: unknown }

async function request<T>(
  path: string,
  init: RequestInitWithJson | undefined,
  schema: z.ZodType<T>,
): Promise<T> {
  const headers = new Headers(init?.headers)
  let body: BodyInit | undefined
  if (init?.json !== undefined) {
    headers.set("Content-Type", "application/json")
    body = JSON.stringify(init.json)
  }
  const res = await fetch(`${baseUrl()}${path}`, {
    ...init,
    headers,
    body,
    credentials: "include",
    cache: "no-store",
  })
  const text = await res.text()
  let parsed: unknown
  if (text.length > 0) {
    try {
      parsed = JSON.parse(text)
    } catch {
      throw new ApiError(`Non-JSON response from ${path}`, res.status, "BAD_RESPONSE")
    }
  }
  if (!res.ok) {
    const envelope = parsed as { error?: { code?: string; message?: string; details?: unknown } }
    throw new ApiError(
      envelope?.error?.message ?? `HTTP ${res.status}`,
      res.status,
      envelope?.error?.code ?? "HTTP_ERROR",
      envelope?.error?.details,
    )
  }
  const validated = schema.safeParse(parsed)
  if (!validated.success) {
    throw new ApiError(
      `Response from ${path} did not match schema: ${validated.error.message}`,
      res.status,
      "SCHEMA_MISMATCH",
      validated.error.issues,
    )
  }
  return validated.data
}

// ─── Schemas ──────────────────────────────────────────────────────────────

export const HealthSchema = z.object({
  status: z.string(),
  started_at: z.string().optional(),
  uptime_s: z.number().optional(),
  checks: z.record(z.string(), z.unknown()).optional(),
})
export type Health = z.infer<typeof HealthSchema>

export const AgentInfoSchema = z.object({
  node_id: z.string(),
  version: z.string().optional(),
  tags: z.array(z.string()).optional(),
  reasoners: z.array(z.string()).optional(),
})
export type AgentInfo = z.infer<typeof AgentInfoSchema>

export const AgentListSchema = z.object({
  agents: z.array(AgentInfoSchema),
})
export type AgentList = z.infer<typeof AgentListSchema>

export const RunSchema = z.object({
  id: z.string(),
  agent: z.string(),
  status: z.enum(["queued", "running", "succeeded", "failed", "cancelled"]),
  tenant_id: z.string().optional(),
  started_at: z.string(),
  duration_ms: z.number().optional(),
  cost_usd: z.number().optional(),
  input: z.unknown().optional(),
  output: z.unknown().optional(),
  error: z.string().optional(),
})
export type Run = z.infer<typeof RunSchema>

export const RunListSchema = z.object({
  runs: z.array(RunSchema),
  total: z.number(),
  has_more: z.boolean(),
})
export type RunList = z.infer<typeof RunListSchema>

export const CostPointSchema = z.object({
  date: z.string(),
  cost_usd: z.number(),
})
export type CostPoint = z.infer<typeof CostPointSchema>

export const CostSummarySchema = z.object({
  period_total_usd: z.number(),
  previous_total_usd: z.number(),
  budget_usd: z.number().nullable(),
  forecast_usd: z.number(),
  by_day: z.array(CostPointSchema),
  by_model: z.array(
    z.object({
      model: z.string(),
      cost_usd: z.number(),
    }),
  ),
  by_agent: z.array(
    z.object({
      agent: z.string(),
      cost_usd: z.number(),
    }),
  ),
  by_tenant: z.array(
    z.object({
      tenant_id: z.string(),
      tenant_name: z.string().nullable(),
      cost_usd: z.number(),
    }),
  ),
})
export type CostSummary = z.infer<typeof CostSummarySchema>

export const HomeOverviewSchema = z.object({
  requests_per_minute: z.number(),
  error_rate: z.number(),
  cost_today_usd: z.number(),
  queue_depth: z.number(),
  request_sparkline: z.array(z.number()),
  error_sparkline: z.array(z.number()),
  cost_sparkline: z.array(z.number()),
  queue_sparkline: z.array(z.number()),
  recent_runs: z.array(RunSchema),
  recent_webhook_deliveries: z.array(
    z.object({
      id: z.string(),
      url: z.string(),
      direction: z.enum(["in", "out"]),
      status: z.enum(["delivered", "failed", "pending"]),
      occurred_at: z.string(),
    }),
  ),
  alerts: z.array(
    z.object({
      id: z.string(),
      severity: z.enum(["info", "warning", "critical"]),
      title: z.string(),
      description: z.string().optional(),
    }),
  ),
})
export type HomeOverview = z.infer<typeof HomeOverviewSchema>

export const LogLineSchema = z.object({
  ts: z.string(),
  level: z.enum(["debug", "info", "warn", "error"]),
  service: z.string(),
  msg: z.string(),
  request_id: z.string().optional(),
  tenant_id: z.string().optional(),
  agent: z.string().optional(),
})
export type LogLine = z.infer<typeof LogLineSchema>

export const QueueSummarySchema = z.object({
  pending: z.number(),
  running: z.number(),
  failed: z.number(),
  succeeded_today: z.number(),
  recent: z.array(
    z.object({
      id: z.string(),
      name: z.string(),
      status: z.enum(["pending", "running", "succeeded", "failed", "cancelled"]),
      enqueued_at: z.string(),
      attempts: z.number(),
      last_error: z.string().nullable(),
    }),
  ),
})
export type QueueSummary = z.infer<typeof QueueSummarySchema>

// ─── Jobs (Phase 5) ───────────────────────────────────────────────────────

export const JobSchema = z.object({
  id: z.string(),
  name: z.string(),
  args: z.unknown(),
  state: z.enum([
    "available",
    "running",
    "completed",
    "discarded",
    "cancelled",
    "retryable",
    "scheduled",
    "pending",
  ]),
  tenant_id: z.string().nullable(),
  attempt: z.number(),
  max_attempts: z.number(),
  scheduled_at: z.string(),
  enqueued_at: z.string(),
  attempted_at: z.string().nullable(),
  finalized_at: z.string().nullable(),
  errors: z
    .array(
      z.object({
        at: z.string(),
        attempt: z.number(),
        error: z.string(),
      }),
    )
    .nullable(),
})
export type Job = z.infer<typeof JobSchema>

export const JobListSchema = z.object({
  jobs: z.array(JobSchema),
  total: z.number(),
  has_more: z.boolean(),
})
export type JobList = z.infer<typeof JobListSchema>

export const JobDefinitionSchema = z.object({
  name: z.string(),
  language: z.enum(["python", "typescript", "go"]),
  source_path: z.string().nullable(),
  description: z.string().nullable(),
  cron: z.string().nullable(),
  recent: z.object({
    succeeded: z.number(),
    failed: z.number(),
    running: z.number(),
  }),
})
export type JobDefinition = z.infer<typeof JobDefinitionSchema>

export const JobDefinitionListSchema = z.object({
  definitions: z.array(JobDefinitionSchema),
})
export type JobDefinitionList = z.infer<typeof JobDefinitionListSchema>

export const EnqueueJobInputSchema = z.object({
  name: z.string(),
  args: z.unknown(),
  scheduled_at: z.string().optional(),
  max_attempts: z.number().int().min(1).max(50).optional(),
})
export type EnqueueJobInput = z.infer<typeof EnqueueJobInputSchema>

// ─── Secrets (Phase 5) ────────────────────────────────────────────────────

export const SecretMetadataSchema = z.object({
  key: z.string(),
  tenant_id: z.string().nullable(),
  description: z.string().nullable(),
  rotate_after: z.string().nullable(),
  created_at: z.string(),
  updated_at: z.string(),
  // value is NEVER returned in list/show responses; only via /secrets/{key}/reveal
})
export type SecretMetadata = z.infer<typeof SecretMetadataSchema>

export const SecretListSchema = z.object({
  secrets: z.array(SecretMetadataSchema),
})
export type SecretList = z.infer<typeof SecretListSchema>

export const SecretValueSchema = z.object({
  key: z.string(),
  value: z.string(),
})
export type SecretValue = z.infer<typeof SecretValueSchema>

export const PutSecretInputSchema = z.object({
  value: z.string(),
  description: z.string().optional(),
  rotate_after: z.string().optional(),
})
export type PutSecretInput = z.infer<typeof PutSecretInputSchema>

// ─── Storage (Phase 5) ────────────────────────────────────────────────────

export const StorageObjectSchema = z.object({
  key: z.string(),
  size: z.number(),
  content_type: z.string().nullable(),
  last_modified: z.string(),
  etag: z.string().nullable(),
})
export type StorageObject = z.infer<typeof StorageObjectSchema>

export const StorageListSchema = z.object({
  objects: z.array(StorageObjectSchema),
  prefix: z.string(),
  next_token: z.string().nullable(),
})
export type StorageList = z.infer<typeof StorageListSchema>

export const SignedURLSchema = z.object({
  key: z.string(),
  url: z.string(),
  expires_at: z.string(),
})
export type SignedURL = z.infer<typeof SignedURLSchema>

// ─── Sandboxes (Phase 9) ──────────────────────────────────────────────────

export const SandboxCapabilitiesSchema = z.object({
  max_timeout_s: z.number(),
  supports_gpu: z.boolean(),
  supports_network: z.boolean(),
  supports_mounts: z.boolean(),
  cold_start_ms: z.number(),
  adapter: z.string(),
})
export type SandboxCapabilities = z.infer<typeof SandboxCapabilitiesSchema>

export const SandboxRunSchema = z.object({
  id: z.string(),
  tenant_id: z.string().nullable(),
  workspace_id: z.string().nullable(),
  adapter: z.string(),
  image: z.string(),
  command: z.array(z.string()),
  status: z.enum([
    "queued",
    "running",
    "done",
    "failed",
    "timeout",
    "killed",
  ]),
  exit_code: z.number().nullable(),
  duration_s: z.number().nullable(),
  cpu_seconds: z.number().nullable(),
  memory_peak_mb: z.number().nullable(),
  network_bytes_in: z.number().nullable(),
  network_bytes_out: z.number().nullable(),
  cost_usd: z.number().nullable(),
  stdout_url: z.string().nullable(),
  stderr_url: z.string().nullable(),
  artifacts_url: z.string().nullable(),
  started_at: z.string().nullable(),
  ended_at: z.string().nullable(),
  created_at: z.string(),
})
export type SandboxRun = z.infer<typeof SandboxRunSchema>

export const SandboxRunListSchema = z.object({
  runs: z.array(SandboxRunSchema),
  total: z.number(),
  has_more: z.boolean(),
})
export type SandboxRunList = z.infer<typeof SandboxRunListSchema>

export const SandboxRunInputSchema = z.object({
  image: z.string(),
  command: z.array(z.string()),
  files: z.record(z.string(), z.string()).optional(),
  env: z.record(z.string(), z.string()).optional(),
  timeout_s: z.number().int().min(1).max(86400).default(300),
  cpu: z.number().int().min(1).max(32).default(2),
  memory_gb: z.number().int().min(1).max(64).default(4),
  network: z.enum(["open", "restricted", "isolated"]).default("restricted"),
  allow_egress: z.array(z.string()).optional(),
  workspace_id: z.string().optional(),
})
export type SandboxRunInput = z.infer<typeof SandboxRunInputSchema>

export const SandboxPoolStatsSchema = z.object({
  adapter: z.string(),
  warm: z.number(),
  active: z.number(),
  queued: z.number(),
  total_runs_today: z.number(),
  cpu_seconds_today: z.number(),
  cost_usd_today: z.number(),
  capabilities: SandboxCapabilitiesSchema,
})
export type SandboxPoolStats = z.infer<typeof SandboxPoolStatsSchema>

// ─── Database studio + memory (Phase 8) ──────────────────────────────────

export const DBTableSchema = z.object({
  schema: z.string(),
  name: z.string(),
  kind: z.enum(["table", "view", "matview"]),
  estimated_rows: z.number(),
  size_bytes: z.number(),
  has_rls: z.boolean(),
})
export type DBTable = z.infer<typeof DBTableSchema>

export const DBTableListSchema = z.object({
  tables: z.array(DBTableSchema),
})
export type DBTableList = z.infer<typeof DBTableListSchema>

export const DBColumnSchema = z.object({
  name: z.string(),
  data_type: z.string(),
  is_nullable: z.boolean(),
  default_value: z.string().nullable(),
  is_primary_key: z.boolean(),
  is_unique: z.boolean(),
})
export type DBColumn = z.infer<typeof DBColumnSchema>

export const DBIndexSchema = z.object({
  name: z.string(),
  definition: z.string(),
  is_unique: z.boolean(),
  is_primary: z.boolean(),
})
export type DBIndex = z.infer<typeof DBIndexSchema>

export const RLSPolicySchema = z.object({
  name: z.string(),
  cmd: z.enum(["SELECT", "INSERT", "UPDATE", "DELETE", "ALL"]),
  permissive: z.boolean(),
  roles: z.array(z.string()),
  using_expression: z.string().nullable(),
  with_check_expression: z.string().nullable(),
})
export type RLSPolicy = z.infer<typeof RLSPolicySchema>

export const DBTableDetailSchema = z.object({
  schema: z.string(),
  name: z.string(),
  kind: z.string(),
  columns: z.array(DBColumnSchema),
  indexes: z.array(DBIndexSchema),
  rls_enabled: z.boolean(),
  rls_forced: z.boolean(),
  policies: z.array(RLSPolicySchema),
})
export type DBTableDetail = z.infer<typeof DBTableDetailSchema>

export const SQLRunRequestSchema = z.object({
  statement: z.string().min(1),
  // safety: read-only when true (caller asserts; runtime double-checks)
  read_only: z.boolean().default(true),
  limit: z.number().int().min(1).max(10000).default(500),
})
export type SQLRunRequest = z.infer<typeof SQLRunRequestSchema>

export const SQLRunResultSchema = z.object({
  columns: z.array(z.string()),
  rows: z.array(z.array(z.unknown())),
  row_count: z.number(),
  truncated: z.boolean(),
  duration_ms: z.number(),
})
export type SQLRunResult = z.infer<typeof SQLRunResultSchema>

export const TableRowsRequestSchema = z.object({
  schema: z.string().default("public"),
  table: z.string(),
  limit: z.number().int().min(1).max(1000).default(100),
  offset: z.number().int().min(0).default(0),
})
export type TableRowsRequest = z.infer<typeof TableRowsRequestSchema>

// Memory primitives (proxied to AgentField or stored in PG)
export const MemoryScopeEnum = z.enum([
  "global",
  "tenant",
  "agent",
  "session",
  "run",
])
export type MemoryScope = z.infer<typeof MemoryScopeEnum>

export const MemoryEntrySchema = z.object({
  scope: MemoryScopeEnum,
  scope_id: z.string().nullable(),
  key: z.string(),
  value: z.unknown(),
  metadata: z.record(z.string(), z.unknown()),
  has_embedding: z.boolean(),
  created_at: z.string(),
  updated_at: z.string(),
})
export type MemoryEntry = z.infer<typeof MemoryEntrySchema>

export const MemoryListSchema = z.object({
  entries: z.array(MemoryEntrySchema),
  total: z.number(),
  has_more: z.boolean(),
})
export type MemoryList = z.infer<typeof MemoryListSchema>

export const MemoryPutInputSchema = z.object({
  scope: MemoryScopeEnum,
  scope_id: z.string().optional(),
  key: z.string().min(1),
  value: z.unknown(),
  metadata: z.record(z.string(), z.unknown()).optional(),
  embed: z.boolean().default(false),
})
export type MemoryPutInput = z.infer<typeof MemoryPutInputSchema>

export const MemorySearchRequestSchema = z.object({
  query: z.string().min(1),
  scope: MemoryScopeEnum.optional(),
  scope_id: z.string().optional(),
  top_k: z.number().int().min(1).max(50).default(10),
  threshold: z.number().min(0).max(1).optional(),
})
export type MemorySearchRequest = z.infer<typeof MemorySearchRequestSchema>

export const MemorySearchHitSchema = z.object({
  entry: MemoryEntrySchema,
  similarity: z.number(),
  distance: z.number(),
})
export type MemorySearchHit = z.infer<typeof MemorySearchHitSchema>

export const MemorySearchResultSchema = z.object({
  hits: z.array(MemorySearchHitSchema),
  duration_ms: z.number(),
})
export type MemorySearchResult = z.infer<typeof MemorySearchResultSchema>

// ─── LLM Gateway + Cost (Phase 7) ─────────────────────────────────────────

export const CostEventSchema = z.object({
  id: z.string(),
  tenant_id: z.string().nullable(),
  api_key_id: z.string().nullable(),
  model: z.string(),
  provider: z.string(),
  agent: z.string().nullable(),
  prompt_tokens: z.number(),
  completion_tokens: z.number(),
  total_tokens: z.number(),
  cost_usd: z.number(),
  cached: z.boolean(),
  latency_ms: z.number(),
  occurred_at: z.string(),
})
export type CostEvent = z.infer<typeof CostEventSchema>

export const CostEventListSchema = z.object({
  events: z.array(CostEventSchema),
  total: z.number(),
  has_more: z.boolean(),
})
export type CostEventList = z.infer<typeof CostEventListSchema>

export const BudgetSchema = z.object({
  tenant_id: z.string(),
  monthly_usd: z.number(),
  alert_threshold_pct: z.number(),
  spent_this_period_usd: z.number(),
  remaining_usd: z.number(),
  resets_at: z.string(),
})
export type Budget = z.infer<typeof BudgetSchema>

export const BudgetListSchema = z.object({
  budgets: z.array(BudgetSchema),
})
export type BudgetList = z.infer<typeof BudgetListSchema>

export const SetBudgetInputSchema = z.object({
  tenant_id: z.string(),
  monthly_usd: z.number().positive(),
  alert_threshold_pct: z.number().min(0).max(100).default(80),
})
export type SetBudgetInput = z.infer<typeof SetBudgetInputSchema>

export const LLMModelSchema = z.object({
  id: z.string(),
  display_name: z.string(),
  provider: z.string(),
  prompt_usd_per_1m: z.number(),
  completion_usd_per_1m: z.number(),
  supports_streaming: z.boolean(),
  supports_tools: z.boolean(),
})
export type LLMModel = z.infer<typeof LLMModelSchema>

export const LLMModelListSchema = z.object({
  models: z.array(LLMModelSchema),
})
export type LLMModelList = z.infer<typeof LLMModelListSchema>

export const CacheStatsSchema = z.object({
  total_calls: z.number(),
  cache_hits: z.number(),
  cache_misses: z.number(),
  hit_rate: z.number(),
  savings_usd: z.number(),
  entries: z.number(),
})
export type CacheStats = z.infer<typeof CacheStatsSchema>

// ─── Tenancy (Phase 6) ────────────────────────────────────────────────────

export const TenantSchema = z.object({
  id: z.string(),
  slug: z.string(),
  name: z.string(),
  plan: z.string(),
  settings: z.record(z.string(), z.unknown()),
  quota: z.record(z.string(), z.unknown()),
  created_at: z.string(),
  deleted_at: z.string().nullable(),
})
export type Tenant = z.infer<typeof TenantSchema>

export const TenantListSchema = z.object({
  tenants: z.array(TenantSchema),
})
export type TenantList = z.infer<typeof TenantListSchema>

export const CreateTenantInputSchema = z.object({
  slug: z
    .string()
    .min(1)
    .max(64)
    .regex(/^[a-z0-9][a-z0-9-]*$/, "lowercase letters, digits, hyphens"),
  name: z.string().min(1).max(128),
  plan: z.string().optional(),
})
export type CreateTenantInput = z.infer<typeof CreateTenantInputSchema>

export const UpdateTenantInputSchema = z.object({
  name: z.string().optional(),
  plan: z.string().optional(),
  settings: z.record(z.string(), z.unknown()).optional(),
  quota: z.record(z.string(), z.unknown()).optional(),
})
export type UpdateTenantInput = z.infer<typeof UpdateTenantInputSchema>

export const UserSchema = z.object({
  id: z.string(),
  email: z.email(),
  name: z.string().nullable(),
  avatar_url: z.string().nullable(),
  created_at: z.string(),
  deleted_at: z.string().nullable(),
})
export type User = z.infer<typeof UserSchema>

export const UserListSchema = z.object({
  users: z.array(UserSchema),
})
export type UserList = z.infer<typeof UserListSchema>

export const MembershipSchema = z.object({
  tenant_id: z.string(),
  user_id: z.string(),
  role: z.enum(["owner", "admin", "member", "viewer"]),
  invited_at: z.string(),
  accepted_at: z.string().nullable(),
})
export type Membership = z.infer<typeof MembershipSchema>

export const MembershipListSchema = z.object({
  memberships: z.array(MembershipSchema),
})
export type MembershipList = z.infer<typeof MembershipListSchema>

export const APIKeySchema = z.object({
  id: z.string(),
  tenant_id: z.string(),
  prefix: z.string(),
  name: z.string().nullable(),
  scopes: z.array(z.string()),
  created_by: z.string().nullable(),
  created_at: z.string(),
  last_used_at: z.string().nullable(),
  expires_at: z.string().nullable(),
  revoked_at: z.string().nullable(),
})
export type APIKey = z.infer<typeof APIKeySchema>

export const APIKeyListSchema = z.object({
  keys: z.array(APIKeySchema),
})
export type APIKeyList = z.infer<typeof APIKeyListSchema>

// Returned by POST /api/v1/admin/keys ONCE; the `value` is never shown again.
export const IssuedAPIKeySchema = APIKeySchema.extend({
  value: z.string(),
})
export type IssuedAPIKey = z.infer<typeof IssuedAPIKeySchema>

export const IssueAPIKeyInputSchema = z.object({
  tenant_id: z.string(),
  name: z.string().optional(),
  scopes: z.array(z.string()).default([]),
  expires_at: z.string().optional(),
})
export type IssueAPIKeyInput = z.infer<typeof IssueAPIKeyInputSchema>

export const TenantDetailSchema = z.object({
  tenant: TenantSchema,
  members: z.array(
    z.object({
      user: UserSchema,
      role: z.string(),
    }),
  ),
  api_keys: z.array(APIKeySchema),
  usage: z.object({
    requests_30d: z.number(),
    cost_usd_30d: z.number(),
    storage_bytes: z.number(),
    secrets_count: z.number(),
  }),
})
export type TenantDetail = z.infer<typeof TenantDetailSchema>

export const AuditEntrySchema = z.object({
  id: z.string(),
  tenant_id: z.string().nullable(),
  user_id: z.string().nullable(),
  api_key_id: z.string().nullable(),
  action: z.string(),
  resource_type: z.string().nullable(),
  resource_id: z.string().nullable(),
  metadata: z.record(z.string(), z.unknown()),
  occurred_at: z.string(),
})
export type AuditEntry = z.infer<typeof AuditEntrySchema>

export const AuditListSchema = z.object({
  entries: z.array(AuditEntrySchema),
  total: z.number(),
  has_more: z.boolean(),
})
export type AuditList = z.infer<typeof AuditListSchema>

export const ModulesStateSchema = z.object({
  modules: z.array(
    z.object({
      id: z.string(),
      name: z.string(),
      enabled: z.boolean(),
      adapter: z.string().optional(),
      version: z.string().optional(),
    }),
  ),
  workload_modules: z.array(z.string()),
  multi_tenancy_enabled: z.boolean(),
})
export type ModulesState = z.infer<typeof ModulesStateSchema>

// ─── Client ───────────────────────────────────────────────────────────────

export const api = {
  health: () => request("/health", undefined, HealthSchema),
  agents: () => request("/api/v1/agents", undefined, AgentListSchema),
  runs: (params?: {
    agent?: string
    tenant?: string
    status?: string
    limit?: number
    offset?: number
  }) => {
    const qs = new URLSearchParams()
    if (params?.agent) qs.set("agent", params.agent)
    if (params?.tenant) qs.set("tenant", params.tenant)
    if (params?.status) qs.set("status", params.status)
    if (params?.limit !== undefined) qs.set("limit", String(params.limit))
    if (params?.offset !== undefined) qs.set("offset", String(params.offset))
    const q = qs.toString()
    return request(`/api/v1/runs${q ? "?" + q : ""}`, undefined, RunListSchema)
  },
  cost: (params?: { from?: string; to?: string }) => {
    const qs = new URLSearchParams()
    if (params?.from) qs.set("from", params.from)
    if (params?.to) qs.set("to", params.to)
    const q = qs.toString()
    return request(`/api/v1/cost${q ? "?" + q : ""}`, undefined, CostSummarySchema)
  },
  home: () => request("/api/v1/home/overview", undefined, HomeOverviewSchema),
  modulesState: () => request("/api/v1/modules", undefined, ModulesStateSchema),
  queue: () => request("/api/v1/queues/summary", undefined, QueueSummarySchema),

  // ─── Jobs ───
  jobs: {
    list: (params?: {
      name?: string
      state?: string
      tenant?: string
      limit?: number
      offset?: number
    }) => {
      const qs = new URLSearchParams()
      if (params?.name) qs.set("name", params.name)
      if (params?.state) qs.set("state", params.state)
      if (params?.tenant) qs.set("tenant", params.tenant)
      if (params?.limit !== undefined) qs.set("limit", String(params.limit))
      if (params?.offset !== undefined) qs.set("offset", String(params.offset))
      const q = qs.toString()
      return request(`/api/v1/jobs${q ? "?" + q : ""}`, undefined, JobListSchema)
    },
    get: (id: string) => request(`/api/v1/jobs/${id}`, undefined, JobSchema),
    enqueue: (input: EnqueueJobInput) =>
      request("/api/v1/jobs", { method: "POST", json: input }, JobSchema),
    retry: (id: string) =>
      request(`/api/v1/jobs/${id}/retry`, { method: "POST" }, JobSchema),
    definitions: () =>
      request("/api/v1/jobs/definitions", undefined, JobDefinitionListSchema),
  },

  // ─── Secrets ───
  secrets: {
    list: () => request("/api/v1/secrets", undefined, SecretListSchema),
    get: (key: string) =>
      request(
        `/api/v1/secrets/${encodeURIComponent(key)}`,
        undefined,
        SecretMetadataSchema,
      ),
    reveal: (key: string) =>
      request(
        `/api/v1/secrets/${encodeURIComponent(key)}/reveal`,
        { method: "POST" },
        SecretValueSchema,
      ),
    put: (key: string, input: PutSecretInput) =>
      request(
        `/api/v1/secrets/${encodeURIComponent(key)}`,
        { method: "PUT", json: input },
        SecretMetadataSchema,
      ),
    delete: (key: string) =>
      request(
        `/api/v1/secrets/${encodeURIComponent(key)}`,
        { method: "DELETE" },
        z.object({ deleted: z.boolean() }),
      ),
    rotate: (key: string, input: { value: string }) =>
      request(
        `/api/v1/secrets/${encodeURIComponent(key)}/rotate`,
        { method: "POST", json: input },
        SecretMetadataSchema,
      ),
  },

  // ─── Sandboxes (Phase 9) ───
  sandbox: {
    pool: () =>
      request("/api/v1/sandbox/pool", undefined, SandboxPoolStatsSchema),
    list: (params?: {
      tenant?: string
      status?: string
      limit?: number
      offset?: number
    }) => {
      const qs = new URLSearchParams()
      if (params?.tenant) qs.set("tenant", params.tenant)
      if (params?.status) qs.set("status", params.status)
      if (params?.limit !== undefined) qs.set("limit", String(params.limit))
      if (params?.offset !== undefined) qs.set("offset", String(params.offset))
      const q = qs.toString()
      return request(
        `/api/v1/sandbox/runs${q ? "?" + q : ""}`,
        undefined,
        SandboxRunListSchema,
      )
    },
    get: (id: string) =>
      request(`/api/v1/sandbox/runs/${id}`, undefined, SandboxRunSchema),
    run: (input: SandboxRunInput) =>
      request(
        "/api/v1/sandbox/run",
        { method: "POST", json: input },
        SandboxRunSchema,
      ),
    stop: (id: string) =>
      request(
        `/api/v1/sandbox/runs/${id}`,
        { method: "DELETE" },
        z.object({ stopped: z.boolean() }),
      ),
  },

  // ─── Database studio + memory (Phase 8) ───
  db: {
    tables: () => request("/api/v1/db/tables", undefined, DBTableListSchema),
    table: (schema: string, name: string) =>
      request(
        `/api/v1/db/tables/${encodeURIComponent(schema)}/${encodeURIComponent(name)}`,
        undefined,
        DBTableDetailSchema,
      ),
    rows: (input: TableRowsRequest) => {
      const qs = new URLSearchParams({
        schema: input.schema,
        table: input.table,
        limit: String(input.limit),
        offset: String(input.offset),
      })
      return request(`/api/v1/db/rows?${qs}`, undefined, SQLRunResultSchema)
    },
    sql: (input: SQLRunRequest) =>
      request("/api/v1/db/sql", { method: "POST", json: input }, SQLRunResultSchema),
  },
  memory: {
    list: (params?: {
      scope?: string
      scope_id?: string
      prefix?: string
      limit?: number
      offset?: number
    }) => {
      const qs = new URLSearchParams()
      if (params?.scope) qs.set("scope", params.scope)
      if (params?.scope_id) qs.set("scope_id", params.scope_id)
      if (params?.prefix) qs.set("prefix", params.prefix)
      if (params?.limit !== undefined) qs.set("limit", String(params.limit))
      if (params?.offset !== undefined) qs.set("offset", String(params.offset))
      const q = qs.toString()
      return request(
        `/api/v1/memory${q ? "?" + q : ""}`,
        undefined,
        MemoryListSchema,
      )
    },
    get: (scope: string, key: string, scopeId?: string) => {
      const qs = new URLSearchParams({ scope, key })
      if (scopeId) qs.set("scope_id", scopeId)
      return request(`/api/v1/memory/get?${qs}`, undefined, MemoryEntrySchema)
    },
    put: (input: MemoryPutInput) =>
      request(
        "/api/v1/memory",
        { method: "PUT", json: input },
        MemoryEntrySchema,
      ),
    delete: (scope: string, key: string, scopeId?: string) => {
      const qs = new URLSearchParams({ scope, key })
      if (scopeId) qs.set("scope_id", scopeId)
      return request(
        `/api/v1/memory?${qs}`,
        { method: "DELETE" },
        z.object({ deleted: z.boolean() }),
      )
    },
    search: (input: MemorySearchRequest) =>
      request(
        "/api/v1/memory/search",
        { method: "POST", json: input },
        MemorySearchResultSchema,
      ),
  },

  // ─── LLM gateway + cost (Phase 7) ───
  llm: {
    models: () => request("/api/v1/llm/models", undefined, LLMModelListSchema),
    cacheStats: () =>
      request("/api/v1/llm/cache/stats", undefined, CacheStatsSchema),
  },
  costEvents: (params?: {
    tenant?: string
    model?: string
    from?: string
    to?: string
    limit?: number
    offset?: number
  }) => {
    const qs = new URLSearchParams()
    if (params?.tenant) qs.set("tenant", params.tenant)
    if (params?.model) qs.set("model", params.model)
    if (params?.from) qs.set("from", params.from)
    if (params?.to) qs.set("to", params.to)
    if (params?.limit !== undefined) qs.set("limit", String(params.limit))
    if (params?.offset !== undefined) qs.set("offset", String(params.offset))
    const q = qs.toString()
    return request(
      `/api/v1/cost/events${q ? "?" + q : ""}`,
      undefined,
      CostEventListSchema,
    )
  },
  budgets: {
    list: () => request("/api/v1/admin/budgets", undefined, BudgetListSchema),
    get: (tenantId: string) =>
      request(
        `/api/v1/admin/budgets/${tenantId}`,
        undefined,
        BudgetSchema,
      ),
    set: (input: SetBudgetInput) =>
      request(
        "/api/v1/admin/budgets",
        { method: "PUT", json: input },
        BudgetSchema,
      ),
  },

  // ─── Tenancy (admin) ───
  admin: {
    tenants: {
      list: () =>
        request("/api/v1/admin/tenants", undefined, TenantListSchema),
      get: (id: string) =>
        request(`/api/v1/admin/tenants/${id}`, undefined, TenantDetailSchema),
      create: (input: CreateTenantInput) =>
        request(
          "/api/v1/admin/tenants",
          { method: "POST", json: input },
          TenantSchema,
        ),
      update: (id: string, input: UpdateTenantInput) =>
        request(
          `/api/v1/admin/tenants/${id}`,
          { method: "PATCH", json: input },
          TenantSchema,
        ),
      delete: (id: string) =>
        request(
          `/api/v1/admin/tenants/${id}`,
          { method: "DELETE" },
          z.object({ deleted: z.boolean() }),
        ),
    },
    users: {
      list: (params?: { tenant?: string; search?: string }) => {
        const qs = new URLSearchParams()
        if (params?.tenant) qs.set("tenant", params.tenant)
        if (params?.search) qs.set("search", params.search)
        const q = qs.toString()
        return request(
          `/api/v1/admin/users${q ? "?" + q : ""}`,
          undefined,
          UserListSchema,
        )
      },
    },
    memberships: {
      list: (params?: { tenant?: string; user?: string }) => {
        const qs = new URLSearchParams()
        if (params?.tenant) qs.set("tenant", params.tenant)
        if (params?.user) qs.set("user", params.user)
        const q = qs.toString()
        return request(
          `/api/v1/admin/memberships${q ? "?" + q : ""}`,
          undefined,
          MembershipListSchema,
        )
      },
      add: (input: { tenant_id: string; user_id: string; role: string }) =>
        request(
          "/api/v1/admin/memberships",
          { method: "POST", json: input },
          MembershipSchema,
        ),
      remove: (tenantId: string, userId: string) =>
        request(
          `/api/v1/admin/memberships/${tenantId}/${userId}`,
          { method: "DELETE" },
          z.object({ deleted: z.boolean() }),
        ),
    },
    keys: {
      list: (params?: { tenant?: string }) => {
        const qs = new URLSearchParams()
        if (params?.tenant) qs.set("tenant", params.tenant)
        const q = qs.toString()
        return request(
          `/api/v1/admin/keys${q ? "?" + q : ""}`,
          undefined,
          APIKeyListSchema,
        )
      },
      issue: (input: IssueAPIKeyInput) =>
        request(
          "/api/v1/admin/keys",
          { method: "POST", json: input },
          IssuedAPIKeySchema,
        ),
      revoke: (id: string) =>
        request(
          `/api/v1/admin/keys/${id}`,
          { method: "DELETE" },
          z.object({ revoked: z.boolean() }),
        ),
    },
    audit: {
      list: (params?: {
        tenant?: string
        action?: string
        from?: string
        to?: string
        limit?: number
        offset?: number
      }) => {
        const qs = new URLSearchParams()
        if (params?.tenant) qs.set("tenant", params.tenant)
        if (params?.action) qs.set("action", params.action)
        if (params?.from) qs.set("from", params.from)
        if (params?.to) qs.set("to", params.to)
        if (params?.limit !== undefined) qs.set("limit", String(params.limit))
        if (params?.offset !== undefined) qs.set("offset", String(params.offset))
        const q = qs.toString()
        return request(
          `/api/v1/admin/audit${q ? "?" + q : ""}`,
          undefined,
          AuditListSchema,
        )
      },
    },
  },

  // ─── Storage ───
  storage: {
    list: (params?: { prefix?: string; next_token?: string; limit?: number }) => {
      const qs = new URLSearchParams()
      if (params?.prefix) qs.set("prefix", params.prefix)
      if (params?.next_token) qs.set("next_token", params.next_token)
      if (params?.limit !== undefined) qs.set("limit", String(params.limit))
      const q = qs.toString()
      return request(
        `/api/v1/storage${q ? "?" + q : ""}`,
        undefined,
        StorageListSchema,
      )
    },
    signedURL: (key: string, ttlSeconds?: number) => {
      const qs = new URLSearchParams({ key })
      if (ttlSeconds !== undefined) qs.set("ttl", String(ttlSeconds))
      return request(
        `/api/v1/storage/signed-url?${qs.toString()}`,
        undefined,
        SignedURLSchema,
      )
    },
    delete: (key: string) =>
      request(
        `/api/v1/storage/${encodeURIComponent(key)}`,
        { method: "DELETE" },
        z.object({ deleted: z.boolean() }),
      ),
    // Upload uses a raw fetch (multipart body); not run through `request()`
    // because the schema layer doesn't help with FormData.
    upload: async (key: string, file: File | Blob): Promise<StorageObject> => {
      const form = new FormData()
      form.set("key", key)
      form.set("file", file)
      const res = await fetch("/api/v1/storage/upload", {
        method: "POST",
        body: form,
        credentials: "include",
      })
      if (!res.ok) {
        const text = await res.text()
        throw new ApiError(`upload failed: ${text}`, res.status, "UPLOAD_FAILED")
      }
      return StorageObjectSchema.parse(await res.json())
    },
  },
  // Build URL pattern for "View full trace" link-out (to runtime UI).
  traceUrl(executionId: string): string {
    const runtimeUI = process.env.NEXT_PUBLIC_RUNTIME_UI_URL ?? "http://localhost:8081"
    return `${runtimeUI}/?execution=${encodeURIComponent(executionId)}`
  },
}

// ─── Multi-tenancy helpers ────────────────────────────────────────────────

/**
 * Whether the multi-tenancy module is enabled. Read via `api.modulesState`.
 * Used to decide whether `Customers/*` tabs render real content or the
 * "Enable multi-tenancy" empty state.
 */
export async function isMultiTenancyEnabled(): Promise<boolean> {
  try {
    const state = await api.modulesState()
    return state.multi_tenancy_enabled
  } catch {
    return false
  }
}

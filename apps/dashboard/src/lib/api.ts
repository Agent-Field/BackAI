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

// Server-side, attach the incoming request's cookie header so the runtime
// can resolve the operator's better-auth session into a tenant. Without
// this every server-rendered page would 401 once multi-tenancy is on.
async function serverCookieHeader(): Promise<string | undefined> {
  if (!isServer) return undefined
  try {
    const { cookies } = await import("next/headers")
    const store = await cookies()
    return store
      .getAll()
      .map((c) => `${c.name}=${c.value}`)
      .join("; ")
  } catch {
    // Outside a request scope (e.g. boot-time render) — no cookies.
    return undefined
  }
}

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
  const cookieHeader = await serverCookieHeader()
  if (cookieHeader && !headers.has("cookie")) {
    headers.set("cookie", cookieHeader)
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

// ─── Notifications (Phase 10.1) ──────────────────────────────────────────
//
// Outbox-style notifications. `notifications.send()` enqueues a row in
// suite_notifications which a background worker drains via the configured
// adapter (log-stub for dev, Resend for prod). The dashboard reads from
// the outbox + status table so operators can see what's been sent,
// queued, or failed.

export const NotificationKindSchema = z.enum(["email", "sms", "push", "log"])
export type NotificationKind = z.infer<typeof NotificationKindSchema>

export const NotificationStatusSchema = z.enum([
  "queued",
  "sending",
  "sent",
  "failed",
  "skipped",
])
export type NotificationStatus = z.infer<typeof NotificationStatusSchema>

export const NotificationSchema = z.object({
  id: z.string(),
  tenant_id: z.string().nullable(),
  kind: NotificationKindSchema,
  // Adapter that handled (or will handle) this notification: "log",
  // "resend", etc. nullable until the worker picks it up.
  adapter: z.string().nullable(),
  // Free-form template slug — e.g. "welcome-email", "budget-alert". The
  // template is rendered by the worker, NOT the dashboard.
  template: z.string(),
  // Recipient address. Email for kind=email, phone for sms, etc.
  to: z.string(),
  // Optional from address override. When null the adapter's default applies.
  from: z.string().nullable(),
  subject: z.string().nullable(),
  // Structured payload passed into the template renderer. Kept opaque
  // here — the worker validates against the template's expected shape.
  data: z.record(z.string(), z.unknown()),
  status: NotificationStatusSchema,
  // Adapter-side message id (e.g. Resend's message id). Useful for
  // deep-linking from the dashboard into the provider's UI.
  provider_message_id: z.string().nullable(),
  attempts: z.number(),
  last_error: z.string().nullable(),
  scheduled_at: z.string(),
  sent_at: z.string().nullable(),
  created_at: z.string(),
})
export type Notification = z.infer<typeof NotificationSchema>

export const NotificationListSchema = z.object({
  notifications: z.array(NotificationSchema),
  total: z.number(),
  has_more: z.boolean(),
})
export type NotificationList = z.infer<typeof NotificationListSchema>

export const SendNotificationInputSchema = z.object({
  kind: NotificationKindSchema.default("email"),
  template: z.string(),
  to: z.string(),
  from: z.string().optional(),
  subject: z.string().optional(),
  data: z.record(z.string(), z.unknown()).optional(),
  // ISO 8601 schedule time. Omit = send immediately.
  scheduled_at: z.string().optional(),
})
export type SendNotificationInput = z.infer<typeof SendNotificationInputSchema>

export const NotificationStatsSchema = z.object({
  // Per-status counts for the active range (default last 24h).
  by_status: z.record(NotificationStatusSchema, z.number()),
  // Per-adapter counts so the dashboard can show a stacked bar.
  by_adapter: z.array(z.object({ adapter: z.string(), count: z.number() })),
  // sent_today + failed_today are convenience aggregates surfaced as KPI tiles.
  sent_today: z.number(),
  failed_today: z.number(),
})
export type NotificationStats = z.infer<typeof NotificationStatsSchema>

// ─── Webhooks (Phase 10.2 + 10.3) ────────────────────────────────────────
//
// Two surfaces:
//   1. INBOUND  — endpoints declared in `gateway.yaml` (or via the
//      `webhooks.endpoint()` SDK). The runtime stores raw payloads,
//      verifies HMAC, and forwards to a handler. The dashboard shows the
//      delivery log.
//   2. OUTBOUND — `webhooks.send()` enqueues a delivery in the PG outbox
//      which a retry worker drains. We expose both halves under the same
//      `webhooks.*` API so the dashboard can show a single unified feed
//      with a `direction` filter.

export const WebhookDirectionSchema = z.enum(["inbound", "outbound"])
export type WebhookDirection = z.infer<typeof WebhookDirectionSchema>

export const WebhookEndpointSchema = z.object({
  id: z.string(),
  // Slug used in URLs: /webhooks/in/<slug>. Globally unique.
  slug: z.string(),
  tenant_id: z.string().nullable(),
  // Provider hint for documentation only ("github", "stripe", "custom").
  provider: z.string(),
  // HMAC verification settings. We store the secret reference (a key in
  // the secrets vault) NOT the raw secret here.
  secret_key_ref: z.string().nullable(),
  // Algorithm name as the provider documents it ("sha256", "sha1", etc).
  signature_algorithm: z.string().nullable(),
  // Header name the provider puts its signature in (e.g.
  // "X-Hub-Signature-256" for GitHub).
  signature_header: z.string().nullable(),
  // Where to forward the verified payload. Either an HTTP URL (proxied
  // through to an internal service) or an agent id ("af://agents/<name>").
  forward_to: z.string(),
  // Number of seconds the runtime keeps dedup tokens for.
  dedup_window_s: z.number(),
  is_active: z.boolean(),
  created_at: z.string(),
})
export type WebhookEndpoint = z.infer<typeof WebhookEndpointSchema>

export const WebhookEndpointListSchema = z.object({
  endpoints: z.array(WebhookEndpointSchema),
})
export type WebhookEndpointList = z.infer<typeof WebhookEndpointListSchema>

export const WebhookDeliverySchema = z.object({
  id: z.string(),
  endpoint_id: z.string().nullable(),
  // direction = inbound for received-from-provider, outbound for
  // emitted-by-app deliveries.
  direction: WebhookDirectionSchema,
  // Provider/Subscriber URL (outbound) or remote IP + path (inbound).
  destination: z.string(),
  event_type: z.string(),
  status: z.enum([
    "queued",
    "delivering",
    "succeeded",
    "failed",
    "dropped_duplicate",
    "dropped_invalid_signature",
  ]),
  attempts: z.number(),
  // HTTP status the upstream returned (outbound), or the status we
  // returned (inbound). nullable when never attempted.
  response_status: z.number().nullable(),
  response_ms: z.number().nullable(),
  // Truncated request body preview for the dashboard list view. Full
  // body lives in object storage referenced by body_storage_url.
  body_preview: z.string().nullable(),
  body_storage_url: z.string().nullable(),
  last_error: z.string().nullable(),
  scheduled_at: z.string(),
  delivered_at: z.string().nullable(),
  created_at: z.string(),
})
export type WebhookDelivery = z.infer<typeof WebhookDeliverySchema>

export const WebhookDeliveryListSchema = z.object({
  deliveries: z.array(WebhookDeliverySchema),
  total: z.number(),
  has_more: z.boolean(),
})
export type WebhookDeliveryList = z.infer<typeof WebhookDeliveryListSchema>

export const CreateEndpointInputSchema = z.object({
  slug: z.string(),
  provider: z.string(),
  forward_to: z.string(),
  secret_key_ref: z.string().nullable().optional(),
  signature_algorithm: z.string().optional(),
  signature_header: z.string().optional(),
  dedup_window_s: z.number().optional(),
})
export type CreateEndpointInput = z.infer<typeof CreateEndpointInputSchema>

export const SendWebhookInputSchema = z.object({
  // Destination URL. We trust the caller — outbound dispatch is intended
  // for tenant-owned services, not arbitrary internet hosts. Egress
  // policy enforcement is the operator's responsibility.
  url: z.string(),
  event_type: z.string(),
  body: z.unknown(),
  headers: z.record(z.string(), z.string()).optional(),
  // When omitted, "queued" — when set, "delivering" right away.
  immediate: z.boolean().optional(),
})
export type SendWebhookInput = z.infer<typeof SendWebhookInputSchema>

// ─── Billing (Phase 10.4) ────────────────────────────────────────────────
//
// Stripe-first billing. The runtime mirrors customers/subscriptions
// locally so we don't make an API call per page load, and aggregates
// usage events into a meter table. The dashboard shows per-tenant
// usage + a Stripe portal link.

export const BillingCustomerSchema = z.object({
  tenant_id: z.string(),
  // Stripe customer id. nullable when MT is on but the tenant hasn't
  // been provisioned in Stripe yet.
  stripe_customer_id: z.string().nullable(),
  email: z.string().nullable(),
  // Active subscription plan ("free", "pro", "enterprise"). Free is the
  // implicit default and is NOT stored in Stripe.
  plan: z.string(),
  // ISO 8601 trial expiry. nullable when no trial.
  trial_ends_at: z.string().nullable(),
  // When the next invoice is expected (subscription period end).
  current_period_end: z.string().nullable(),
  // Stripe subscription status as Stripe sends it ("active", "past_due",
  // "canceled", "trialing", etc).
  subscription_status: z.string().nullable(),
  created_at: z.string(),
  updated_at: z.string(),
})
export type BillingCustomer = z.infer<typeof BillingCustomerSchema>

export const BillingCustomerListSchema = z.object({
  customers: z.array(BillingCustomerSchema),
})
export type BillingCustomerList = z.infer<typeof BillingCustomerListSchema>

export const UsageMeterSchema = z.object({
  // Meter name as defined in code ("llm_tokens", "sandbox_seconds", etc).
  meter: z.string(),
  tenant_id: z.string(),
  // Aggregation period — defaults to current month, can be a specific
  // ISO date for a daily slice when queried with ?bucket=day.
  period_start: z.string(),
  period_end: z.string(),
  // Total quantity in the meter's native units.
  quantity: z.number(),
  // Computed cost in USD when the meter has a price attached. nullable
  // when the meter is purely informational.
  cost_usd: z.number().nullable(),
  // Stripe meter id if synced; nullable until first sync.
  stripe_meter_id: z.string().nullable(),
  // When the value was last pushed to Stripe.
  last_synced_at: z.string().nullable(),
})
export type UsageMeter = z.infer<typeof UsageMeterSchema>

export const UsageMeterListSchema = z.object({
  meters: z.array(UsageMeterSchema),
  // Sum of cost_usd across all meters for the period. Convenience so the
  // dashboard doesn't have to do it.
  total_cost_usd: z.number(),
})
export type UsageMeterList = z.infer<typeof UsageMeterListSchema>

export const PortalLinkSchema = z.object({
  // Stripe Customer Portal URL. Short-lived (~24h).
  url: z.string(),
  expires_at: z.string(),
})
export type PortalLink = z.infer<typeof PortalLinkSchema>

// ─── MCP (Phase 11.1) ────────────────────────────────────────────────────
//
// Model Context Protocol servers are external processes (or HTTP/SSE
// endpoints) that expose tools to agents. The suite runtime hosts a
// pool of connections — one per configured server — and proxies tool
// calls. Per-tenant isolation: when MT is on, each tenant can opt
// servers in/out via the dashboard.
//
// Transport modes:
//   - "stdio" — spawn a child process and talk JSON-RPC over its
//     stdin/stdout. The canonical local-dev path.
//   - "sse"   — long-lived HTTP+SSE stream. The canonical remote /
//     hosted-MCP path.

export const MCPTransportSchema = z.enum(["stdio", "sse"])
export type MCPTransport = z.infer<typeof MCPTransportSchema>

export const MCPServerStatusSchema = z.enum([
  "connecting",
  "ready",
  "errored",
  "disabled",
])
export type MCPServerStatus = z.infer<typeof MCPServerStatusSchema>

export const MCPServerSchema = z.object({
  // Stable slug used in URLs and tool calls: `tools.call_mcp("github",
  // "search_repos", ...)`. Lowercased letters, digits, dashes.
  name: z.string(),
  transport: MCPTransportSchema,
  // stdio: ["uvx", "mcp-server-github"]; sse: ignored.
  command: z.array(z.string()),
  // sse only: e.g. "https://mcp.acme.com/sse". stdio: null.
  url: z.string().nullable(),
  // Environment passed to stdio servers. Values come from the secrets
  // vault when prefixed with "secret:<key>" — never raw secret strings.
  env: z.record(z.string(), z.string()),
  // Per-tenant scoping. null = available to every tenant; a uuid means
  // only that tenant sees it.
  tenant_id: z.string().nullable(),
  description: z.string(),
  is_enabled: z.boolean(),
  status: MCPServerStatusSchema,
  // Most-recent error message when status="errored". Truncated to ~500
  // chars so the dashboard isn't drowned in stack traces.
  last_error: z.string().nullable(),
  // Last successful tool list (refreshed on connect + every 5min).
  tools_count: z.number(),
  installed_at: z.string(),
  last_connected_at: z.string().nullable(),
})
export type MCPServer = z.infer<typeof MCPServerSchema>

export const MCPServerListSchema = z.object({
  servers: z.array(MCPServerSchema),
})
export type MCPServerList = z.infer<typeof MCPServerListSchema>

export const MCPToolSchema = z.object({
  // Composite id `<server>/<tool>`. Used in agent tool-use planning.
  id: z.string(),
  server: z.string(),
  name: z.string(),
  description: z.string().nullable(),
  // JSON Schema for the tool's arguments. The runtime forwards as-is
  // so agents can use it for tool-use prompting.
  input_schema: z.record(z.string(), z.unknown()),
})
export type MCPTool = z.infer<typeof MCPToolSchema>

export const MCPToolListSchema = z.object({
  tools: z.array(MCPToolSchema),
})
export type MCPToolList = z.infer<typeof MCPToolListSchema>

export const CreateMCPServerInputSchema = z.object({
  name: z.string(),
  transport: MCPTransportSchema,
  command: z.array(z.string()).optional(),
  url: z.string().optional(),
  env: z.record(z.string(), z.string()).optional(),
  description: z.string().optional(),
  tenant_id: z.string().optional(),
})
export type CreateMCPServerInput = z.infer<typeof CreateMCPServerInputSchema>

export const CallMCPInputSchema = z.object({
  server: z.string(),
  tool: z.string(),
  arguments: z.record(z.string(), z.unknown()).optional(),
})
export type CallMCPInput = z.infer<typeof CallMCPInputSchema>

export const CallMCPResultSchema = z.object({
  // MCP returns a `content` array of typed parts. We pass it through
  // unchanged so the SDK can pull text / image / json parts as it
  // pleases.
  content: z.array(z.record(z.string(), z.unknown())),
  is_error: z.boolean(),
  // Round-trip duration of the MCP call, useful for the dashboard
  // tool-call inspector and for cost rollups later.
  duration_ms: z.number(),
})
export type CallMCPResult = z.infer<typeof CallMCPResultSchema>

// ─── Skills (Phase 11.3) ─────────────────────────────────────────────────
//
// Skills are AF skillkit bundles installed into the runtime. Each one
// declares prompt overlays + optional tool bindings; agents attach them
// when needed. Persisted in suite_skills.

export const SkillSchema = z.object({
  // Bundle id ("af-skill://acme/security-audit@1.2.0").
  id: z.string(),
  name: z.string(),
  version: z.string(),
  // Where the bundle came from — registry URL, local path, or "embedded"
  // for the built-in defaults.
  source: z.string(),
  description: z.string().nullable(),
  // List of harnesses this skill targets ("claude-code", "codex", "any").
  harnesses: z.array(z.string()),
  // Tags from the skill manifest — surface for the dashboard filter.
  tags: z.array(z.string()),
  tenant_id: z.string().nullable(),
  installed_at: z.string(),
})
export type Skill = z.infer<typeof SkillSchema>

export const SkillListSchema = z.object({
  skills: z.array(SkillSchema),
})
export type SkillList = z.infer<typeof SkillListSchema>

export const InstallSkillInputSchema = z.object({
  // Either an id ("af-skill://acme/x@1") or a local path. The runtime
  // figures out which.
  source: z.string(),
  tenant_id: z.string().optional(),
})
export type InstallSkillInput = z.infer<typeof InstallSkillInputSchema>

export const AttachSkillInputSchema = z.object({
  skill_id: z.string(),
  // Agent (slug) or harness id to bind this skill onto.
  agent: z.string(),
})
export type AttachSkillInput = z.infer<typeof AttachSkillInputSchema>

// ─── Harnesses (Phase 11.4) ──────────────────────────────────────────────
//
// Harnesses are CLI tools agents shell out to (Claude Code, Codex,
// Gemini, OpenCode). We don't manage their state inside the runtime
// beyond knowing whether they're installed and reachable.

export const HarnessProviderSchema = z.enum([
  "claude-code",
  "codex",
  "gemini",
  "opencode",
])
export type HarnessProvider = z.infer<typeof HarnessProviderSchema>

export const HarnessSchema = z.object({
  provider: HarnessProviderSchema,
  is_installed: z.boolean(),
  // Resolved binary path when installed; nullable otherwise.
  binary_path: z.string().nullable(),
  version: z.string().nullable(),
  // Result of `--help` / `--version` health probe. ready = usable from
  // app.harness(); needs_auth = installed but no API key configured.
  status: z.enum(["ready", "needs_auth", "missing", "errored"]),
  last_error: z.string().nullable(),
  // Env vars the harness needs (e.g. ANTHROPIC_API_KEY for claude-code).
  // Dashboard surfaces these so the operator knows what's missing.
  required_env: z.array(z.string()),
  install_doc_url: z.string().nullable(),
})
export type Harness = z.infer<typeof HarnessSchema>

export const HarnessListSchema = z.object({
  harnesses: z.array(HarnessSchema),
})
export type HarnessList = z.infer<typeof HarnessListSchema>

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

// Phase 12.1 — the "everything about this tenant" drilldown payload.
// TenantDetail above is the legacy compact shape used elsewhere; this
// schema is what `/customers/tenants/[id]` renders against. We keep it
// as a superset so existing callers don't break.
export const TenantDrilldownSchema = z.object({
  tenant: TenantSchema,
  // Same shape as TenantDetail.members, repeated for self-containment.
  members: z.array(
    z.object({
      user: UserSchema,
      role: z.string(),
      // ISO 8601 timestamp of last seen activity (last_used_at on any
      // of the user's API keys, or session activity, whichever wins).
      // Nullable when the user has never made a request.
      last_active_at: z.string().nullable(),
    }),
  ),
  api_keys: z.array(APIKeySchema),
  // Aggregated usage rollups across the active billing period.
  usage: z.object({
    requests_30d: z.number(),
    cost_usd_30d: z.number(),
    storage_bytes: z.number(),
    secrets_count: z.number(),
    // 24-bucket sparkline of cost in USD — same format as HomeOverview's
    // cost_sparkline. The dashboard renders it inline so the operator
    // can see spend shape without a chart roundtrip.
    cost_sparkline: z.array(z.number()).length(24),
    // Same for requests.
    request_sparkline: z.array(z.number()).length(24),
  }),
  // Most-recent runs across all agents for this tenant. Capped at 10.
  recent_runs: z.array(
    z.object({
      id: z.string(),
      agent: z.string(),
      status: z.string(),
      started_at: z.string(),
      duration_ms: z.number(),
      cost_usd: z.number(),
    }),
  ),
  // Most-recent inbound + outbound webhook deliveries. Capped at 10.
  recent_webhooks: z.array(
    z.object({
      id: z.string(),
      direction: z.enum(["inbound", "outbound"]),
      event_type: z.string(),
      status: z.string(),
      created_at: z.string(),
    }),
  ),
  // Billing snapshot when the billing module is wired; null otherwise.
  billing: z
    .object({
      plan: z.string(),
      subscription_status: z.string().nullable(),
      current_period_end: z.string().nullable(),
      trial_ends_at: z.string().nullable(),
    })
    .nullable(),
})
export type TenantDrilldown = z.infer<typeof TenantDrilldownSchema>

// ─── Cron jobs (Phase 12.2) ──────────────────────────────────────────────
//
// Cron is a thin convenience layer over the existing jobs queue. Each
// cron row spawns one job at its scheduled time; the runtime tracks
// last_run + next_run separately from the job itself so the dashboard
// doesn't have to JOIN every render.

export const CronSchema = z.object({
  id: z.string(),
  tenant_id: z.string().nullable(),
  // Free-form name shown in the dashboard.
  name: z.string(),
  // Job kind (from suite_jobs_definitions) the cron enqueues.
  job_name: z.string(),
  // Standard cron expression ("0 * * * *" = top of each hour). The
  // runtime stores in 5-field crontab format; "@daily" / "@hourly"
  // shortcuts expand server-side.
  schedule: z.string(),
  // JSON args forwarded to the job on each tick.
  args: z.record(z.string(), z.unknown()),
  is_active: z.boolean(),
  // Nullable when the cron has never fired.
  last_run_at: z.string().nullable(),
  next_run_at: z.string(),
  created_at: z.string(),
})
export type Cron = z.infer<typeof CronSchema>

export const CronListSchema = z.object({
  crons: z.array(CronSchema),
})
export type CronList = z.infer<typeof CronListSchema>

export const CreateCronInputSchema = z.object({
  name: z.string(),
  job_name: z.string(),
  schedule: z.string(),
  args: z.record(z.string(), z.unknown()).optional(),
  is_active: z.boolean().optional(),
})
export type CreateCronInput = z.infer<typeof CreateCronInputSchema>

// ─── Metrics surface (Phase 12.2) ────────────────────────────────────────
//
// We don't try to mirror Prometheus's full surface — the operator can
// open the Prometheus / Grafana side panel for that. This schema is the
// minimal at-a-glance summary the dashboard's /operate/metrics tab
// renders so the operator doesn't have to leave the dashboard to know
// the runtime is healthy.

export const MetricsSummarySchema = z.object({
  // Total HTTP requests handled since boot.
  http_requests_total: z.number(),
  // 95p latency in milliseconds across the last 5 minutes.
  http_p95_ms: z.number(),
  // Active goroutines.
  goroutines: z.number(),
  // Heap allocated (bytes).
  heap_alloc_bytes: z.number(),
  // Uptime in seconds since boot.
  uptime_seconds: z.number(),
  // Runtime version string ("v0.12.0+abcd").
  version: z.string(),
  // List of per-endpoint rollups (top 10 by request count).
  by_route: z.array(
    z.object({
      route: z.string(),
      requests: z.number(),
      avg_ms: z.number(),
      error_count: z.number(),
    }),
  ),
})
export type MetricsSummary = z.infer<typeof MetricsSummarySchema>

// ─── Plugin manifest (Phase 12.3) ────────────────────────────────────────
//
// Dashboard plugins are TS files dropped into apps/dashboard/plugins/.
// The dashboard scans them at build time and exposes the discovered
// entries via this endpoint so the sidebar nav can render extra
// entries without a code edit.

export const PluginSchema = z.object({
  // Plugin id — taken from the file name (without extension).
  id: z.string(),
  name: z.string(),
  description: z.string(),
  // Where the plugin's main tab lives — relative path under /(admin).
  route: z.string(),
  // Lucide icon name. Resolved at render time.
  icon: z.string(),
  // Sidebar group ("Build" | "Operate" | "Customers" | "Plugins").
  group: z.string(),
  // Plugin version from its manifest.
  version: z.string(),
})
export type Plugin = z.infer<typeof PluginSchema>

export const PluginListSchema = z.object({
  plugins: z.array(PluginSchema),
})
export type PluginList = z.infer<typeof PluginListSchema>

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

  // ─── Notifications (Phase 10.1) ───
  notifications: {
    stats: () =>
      request(
        "/api/v1/notifications/stats",
        undefined,
        NotificationStatsSchema,
      ),
    list: (params?: {
      tenant?: string
      status?: NotificationStatus
      kind?: NotificationKind
      limit?: number
      offset?: number
    }) => {
      const qs = new URLSearchParams()
      if (params?.tenant) qs.set("tenant", params.tenant)
      if (params?.status) qs.set("status", params.status)
      if (params?.kind) qs.set("kind", params.kind)
      if (params?.limit !== undefined) qs.set("limit", String(params.limit))
      if (params?.offset !== undefined) qs.set("offset", String(params.offset))
      const q = qs.toString()
      return request(
        `/api/v1/notifications${q ? "?" + q : ""}`,
        undefined,
        NotificationListSchema,
      )
    },
    get: (id: string) =>
      request(
        `/api/v1/notifications/${id}`,
        undefined,
        NotificationSchema,
      ),
    send: (input: SendNotificationInput) =>
      request(
        "/api/v1/notifications",
        { method: "POST", json: input },
        NotificationSchema,
      ),
  },

  // ─── Webhooks (Phase 10.2 + 10.3) ───
  webhooks: {
    endpoints: () =>
      request(
        "/api/v1/webhooks/endpoints",
        undefined,
        WebhookEndpointListSchema,
      ),
    createEndpoint: (input: CreateEndpointInput) =>
      request(
        "/api/v1/webhooks/endpoints",
        { method: "POST", json: input },
        WebhookEndpointSchema,
      ),
    deleteEndpoint: (id: string) =>
      request(
        `/api/v1/webhooks/endpoints/${id}`,
        { method: "DELETE" },
        z.object({ deleted: z.boolean() }),
      ),
    deliveries: (params?: {
      direction?: WebhookDirection
      endpoint_id?: string
      tenant?: string
      status?: string
      limit?: number
      offset?: number
    }) => {
      const qs = new URLSearchParams()
      if (params?.direction) qs.set("direction", params.direction)
      if (params?.endpoint_id) qs.set("endpoint_id", params.endpoint_id)
      if (params?.tenant) qs.set("tenant", params.tenant)
      if (params?.status) qs.set("status", params.status)
      if (params?.limit !== undefined) qs.set("limit", String(params.limit))
      if (params?.offset !== undefined) qs.set("offset", String(params.offset))
      const q = qs.toString()
      return request(
        `/api/v1/webhooks/deliveries${q ? "?" + q : ""}`,
        undefined,
        WebhookDeliveryListSchema,
      )
    },
    delivery: (id: string) =>
      request(
        `/api/v1/webhooks/deliveries/${id}`,
        undefined,
        WebhookDeliverySchema,
      ),
    send: (input: SendWebhookInput) =>
      request(
        "/api/v1/webhooks/send",
        { method: "POST", json: input },
        WebhookDeliverySchema,
      ),
    retry: (id: string) =>
      request(
        `/api/v1/webhooks/deliveries/${id}/retry`,
        { method: "POST" },
        WebhookDeliverySchema,
      ),
  },

  // ─── Billing (Phase 10.4) ───
  billing: {
    customers: () =>
      request(
        "/api/v1/billing/customers",
        undefined,
        BillingCustomerListSchema,
      ),
    customer: (tenantId: string) =>
      request(
        `/api/v1/billing/customers/${encodeURIComponent(tenantId)}`,
        undefined,
        BillingCustomerSchema,
      ),
    meters: (params?: {
      tenant?: string
      // ISO date — defaults to current month.
      period_start?: string
      bucket?: "month" | "day"
    }) => {
      const qs = new URLSearchParams()
      if (params?.tenant) qs.set("tenant", params.tenant)
      if (params?.period_start) qs.set("period_start", params.period_start)
      if (params?.bucket) qs.set("bucket", params.bucket)
      const q = qs.toString()
      return request(
        `/api/v1/billing/meters${q ? "?" + q : ""}`,
        undefined,
        UsageMeterListSchema,
      )
    },
    portalLink: (tenantId: string) =>
      request(
        `/api/v1/billing/customers/${encodeURIComponent(tenantId)}/portal`,
        { method: "POST" },
        PortalLinkSchema,
      ),
  },

  // ─── MCP servers + tools (Phase 11.1) ───
  mcp: {
    servers: () =>
      request("/api/v1/mcp/servers", undefined, MCPServerListSchema),
    server: (name: string) =>
      request(
        `/api/v1/mcp/servers/${encodeURIComponent(name)}`,
        undefined,
        MCPServerSchema,
      ),
    addServer: (input: CreateMCPServerInput) =>
      request(
        "/api/v1/mcp/servers",
        { method: "POST", json: input },
        MCPServerSchema,
      ),
    removeServer: (name: string) =>
      request(
        `/api/v1/mcp/servers/${encodeURIComponent(name)}`,
        { method: "DELETE" },
        z.object({ deleted: z.boolean() }),
      ),
    enableServer: (name: string, enabled: boolean) =>
      request(
        `/api/v1/mcp/servers/${encodeURIComponent(name)}/enabled`,
        { method: "PUT", json: { enabled } },
        MCPServerSchema,
      ),
    tools: (server?: string) => {
      const qs = server ? `?server=${encodeURIComponent(server)}` : ""
      return request(`/api/v1/mcp/tools${qs}`, undefined, MCPToolListSchema)
    },
    call: (input: CallMCPInput) =>
      request(
        "/api/v1/mcp/call",
        { method: "POST", json: input },
        CallMCPResultSchema,
      ),
  },

  // ─── Skills (Phase 11.3) ───
  skills: {
    list: (params?: { tenant?: string; harness?: string }) => {
      const qs = new URLSearchParams()
      if (params?.tenant) qs.set("tenant", params.tenant)
      if (params?.harness) qs.set("harness", params.harness)
      const q = qs.toString()
      return request(
        `/api/v1/skills${q ? "?" + q : ""}`,
        undefined,
        SkillListSchema,
      )
    },
    install: (input: InstallSkillInput) =>
      request(
        "/api/v1/skills",
        { method: "POST", json: input },
        SkillSchema,
      ),
    uninstall: (id: string) =>
      request(
        `/api/v1/skills/${encodeURIComponent(id)}`,
        { method: "DELETE" },
        z.object({ deleted: z.boolean() }),
      ),
    attach: (input: AttachSkillInput) =>
      request(
        "/api/v1/skills/attach",
        { method: "POST", json: input },
        z.object({ attached: z.boolean() }),
      ),
  },

  // ─── Harnesses (Phase 11.4) ───
  harnesses: {
    list: () =>
      request("/api/v1/harnesses", undefined, HarnessListSchema),
    get: (provider: HarnessProvider) =>
      request(
        `/api/v1/harnesses/${encodeURIComponent(provider)}`,
        undefined,
        HarnessSchema,
      ),
    // Re-probes the harness binary and returns the refreshed status.
    probe: (provider: HarnessProvider) =>
      request(
        `/api/v1/harnesses/${encodeURIComponent(provider)}/probe`,
        { method: "POST" },
        HarnessSchema,
      ),
  },

  // ─── Tenant drilldown (Phase 12.1) ───
  // Superset of the legacy /admin/tenants/{id} payload. Renders the
  // per-tenant page at /customers/tenants/[id].
  tenantDrilldown: (id: string) =>
    request(
      `/api/v1/admin/tenants/${encodeURIComponent(id)}/drilldown`,
      undefined,
      TenantDrilldownSchema,
    ),

  // ─── Crons (Phase 12.2) ───
  crons: {
    list: (params?: { tenant?: string }) => {
      const qs = params?.tenant
        ? `?tenant=${encodeURIComponent(params.tenant)}`
        : ""
      return request(`/api/v1/crons${qs}`, undefined, CronListSchema)
    },
    get: (id: string) =>
      request(`/api/v1/crons/${encodeURIComponent(id)}`, undefined, CronSchema),
    create: (input: CreateCronInput) =>
      request("/api/v1/crons", { method: "POST", json: input }, CronSchema),
    setActive: (id: string, isActive: boolean) =>
      request(
        `/api/v1/crons/${encodeURIComponent(id)}/active`,
        { method: "PUT", json: { is_active: isActive } },
        CronSchema,
      ),
    delete: (id: string) =>
      request(
        `/api/v1/crons/${encodeURIComponent(id)}`,
        { method: "DELETE" },
        z.object({ deleted: z.boolean() }),
      ),
  },

  // ─── Metrics (Phase 12.2) ───
  metrics: () =>
    request("/api/v1/metrics/summary", undefined, MetricsSummarySchema),

  // ─── Plugins (Phase 12.3) ───
  plugins: () => request("/api/v1/plugins", undefined, PluginListSchema),

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
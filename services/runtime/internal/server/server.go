// SPDX-License-Identifier: Apache-2.0

// Package server wires HTTP handlers and lifecycle for the suite runtime.
package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/activity"
	adapterregistry "github.com/Agent-Field/backai/services/runtime/internal/adapters/registry"
	"github.com/Agent-Field/backai/services/runtime/internal/agentfield"
	"github.com/Agent-Field/backai/services/runtime/internal/appmetrics"
	"github.com/Agent-Field/backai/services/runtime/internal/audit"
	"github.com/Agent-Field/backai/services/runtime/internal/billing"
	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/cost"
	"github.com/Agent-Field/backai/services/runtime/internal/crons"
	"github.com/Agent-Field/backai/services/runtime/internal/db"
	"github.com/Agent-Field/backai/services/runtime/internal/dbstudio"
	"github.com/Agent-Field/backai/services/runtime/internal/featureflags"
	"github.com/Agent-Field/backai/services/runtime/internal/guardrails"
	"github.com/Agent-Field/backai/services/runtime/internal/harnesses"
	"github.com/Agent-Field/backai/services/runtime/internal/hooks"
	"github.com/Agent-Field/backai/services/runtime/internal/idempotency"
	"github.com/Agent-Field/backai/services/runtime/internal/jobs"
	"github.com/Agent-Field/backai/services/runtime/internal/llmcache"
	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway"
	"github.com/Agent-Field/backai/services/runtime/internal/logger"
	"github.com/Agent-Field/backai/services/runtime/internal/mcp"
	"github.com/Agent-Field/backai/services/runtime/internal/memory"
	"github.com/Agent-Field/backai/services/runtime/internal/notifications"
	"github.com/Agent-Field/backai/services/runtime/internal/oauth"
	"github.com/Agent-Field/backai/services/runtime/internal/observability"
	obserrors "github.com/Agent-Field/backai/services/runtime/internal/observability/errors"
	"github.com/Agent-Field/backai/services/runtime/internal/observability/logs"
	"github.com/Agent-Field/backai/services/runtime/internal/observability/metrics"
	"github.com/Agent-Field/backai/services/runtime/internal/observability/traces"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/probe"
	"github.com/Agent-Field/backai/services/runtime/internal/rbac"
	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
	"github.com/Agent-Field/backai/services/runtime/internal/search"
	"github.com/Agent-Field/backai/services/runtime/internal/secrets"
	"github.com/Agent-Field/backai/services/runtime/internal/skills"
	"github.com/Agent-Field/backai/services/runtime/internal/storage"
	"github.com/Agent-Field/backai/services/runtime/internal/tenancy"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
	"github.com/Agent-Field/backai/services/runtime/internal/tooladapters"
	"github.com/Agent-Field/backai/services/runtime/internal/tools"
	"github.com/Agent-Field/backai/services/runtime/internal/webhooks"
)

// Server holds the HTTP server and shared dependencies.
type Server struct {
	cfg config.Config
	log *slog.Logger
	mux *http.ServeMux
	srv *http.Server
	// metricsSrv serves the Prometheus scrape endpoint on a dedicated
	// listener (cfg.Server.MetricsAddr, default :9090) so /metrics is NOT
	// exposed on the public API port. The metrics carry per-tenant cost and
	// usage labels; keeping them on an internal port (Helm restricts :9090 via
	// a NetworkPolicy) avoids leaking tenant metadata to unauthenticated
	// callers. Nil when metrics share the main port (MetricsAddr empty or equal
	// to HTTPAddr — a single-port local-dev convenience).
	metricsSrv *http.Server
	health     *Health
	// drain is the Phase 14.3 graceful-shutdown controller. Always
	// constructed in New(); main.go calls drain.Start() from its
	// SIGTERM handler. The drain middleware (outermost in the chain)
	// shares it so probes can report draining state and new requests
	// are rejected once drain begins.
	drain *Drain
	// ready is set to true by MarkReady() once main.go has finished
	// startup (migrations applied, workers spawned). Until then
	// /ready returns 503 {"status":"booting"}.
	ready         atomic.Bool
	db            *db.DB
	af            *agentfield.Client
	tel           *observability.Telemetry
	hooks         *hooks.Engine
	storage       storage.Storage
	storagePrefix string // tenant key-prefix when multi-tenancy is on; "" otherwise
	secrets       *secrets.Vault
	jobs          *jobs.Manager
	tenancy       *tenancy.Manager
	openapi       *openapi.Builder
	llmCache      *llmcache.Cache
	llmGateway    *llmgateway.Gateway
	// guardrails applies gateway-local PII redaction and moderation to
	// LLM request/response text. It does not own AgentField state.
	guardrails *guardrails.Service
	// memory is the Phase 8.2 suite memory store. nil when no DB is
	// wired; the /api/v1/memory/* endpoints return 503 in that case.
	memory *memory.Store
	// search indexes tenant-scoped app/workload records. nil when no
	// DB is wired; /api/v1/search returns 503 SEARCH_NOT_CONFIGURED.
	search *search.Store
	// activity records tenant-scoped customer/product events. nil when
	// no DB is wired; /api/v1/activity returns 503 ACTIVITY_NOT_CONFIGURED.
	activity *activity.Store
	// featureFlags stores tenant-scoped runtime flags. nil when no DB
	// is wired; GET serves defaults and mutations return 503.
	featureFlags *featureflags.Store
	// dbStudio powers the Phase 8.1 DB studio (/api/v1/db/*). nil when
	// no DB is wired; endpoints return 503 DB_STUDIO_NOT_CONFIGURED.
	dbStudio *dbstudio.Studio
	// costAgg powers /api/v1/cost + /api/v1/cost/events. nil when no
	// DB is wired; handlers return empty/zero responses.
	costAgg *cost.Aggregate
	// budgets powers /api/v1/admin/budgets (Get/Set/List) and is the
	// authority for the cost summary's BudgetUSD field. nil disables
	// budget endpoints (they return 503).
	budgets *cost.Budgets
	// rbac gates cross-tenant operator/admin actions before handlers
	// opt into app.bypass_rls. nil means admin calls are denied once
	// the module/store gates pass.
	rbac *rbac.Enforcer
	// operatorResolver resolves the operator principal from a request's
	// better-auth session. It defaults to s.resolveOperatorPrincipal; tests
	// inject a fake to exercise the operator-gated surfaces without a DB.
	operatorResolver func(context.Context, *http.Request) (operatorPrincipal, error)
	// sandbox powers /api/v1/sandbox/*. nil = endpoints either return
	// 503 SANDBOX_NOT_CONFIGURED (mutating) or tolerant empty
	// responses (reads).
	sandbox *sandbox.Service
	// billingSettings stores operator-panel Stripe keys (KMS-encrypted).
	billingSettings *billing.SettingsStore
	// billing powers /api/v1/billing/* + the /webhooks/in/stripe
	// Stripe-webhook receiver. nil = the read endpoints serve empty
	// pages and synthesised "free plan" rows; the mutating portal-link
	// endpoint returns 503 BILLING_NOT_CONFIGURED.
	billing *billing.Service
	// notifications powers /api/v1/notifications/*. nil = endpoints
	// either return 503 NOTIFICATIONS_NOT_CONFIGURED (POST) or
	// tolerant empty responses (GET).
	notifications *notifications.Service
	// webhooks powers /api/v1/webhooks/* + /webhooks/in/{slug}. nil =
	// mutating endpoints return 503 WEBHOOKS_NOT_CONFIGURED; reads
	// degrade to empty pages. Shared facade between Phase 10.2
	// (inbound) and Phase 10.3 (outbound).
	webhooks *webhooks.Service
	// skills powers /api/v1/skills/*. nil = mutating endpoints return
	// 503 SKILLS_NOT_CONFIGURED; the list endpoint serves an empty
	// page (so the dashboard renders the empty state). skillsInstaller
	// is the manifest reader used by the install handler — kept on the
	// Server so tests can swap in a stub installer.
	skills          *skills.Store
	skillsInstaller *skills.Installer
	// harnesses powers /api/v1/harnesses/* (Phase 11.4). Probe-only —
	// the runtime detects whether each CLI harness is installed and
	// reachable but never installs them. nil = the list endpoint
	// returns an empty list; get / probe return 404 / 503.
	harnesses *harnesses.Service
	// mcp powers /api/v1/mcp/* (Phase 11.1). The Pool owns the
	// long-lived JSON-RPC connections to external MCP servers and
	// proxies tool calls. mcpStore is the persistence layer the
	// dashboard mutates through; mutating endpoints write via Store
	// then call Pool.Reconcile() so the in-memory adapter set catches
	// up. nil pool = MCP_NOT_CONFIGURED on mutating endpoints; GETs
	// return empty pages.
	mcp      *mcp.Pool
	mcpStore *mcp.Store
	// crons is the Phase 12.2 cron scheduling store. nil pool ⇒
	// mutating endpoints return 503 CRONS_NOT_CONFIGURED; reads serve
	// an empty list so the dashboard renders.
	crons *crons.Store
	// toolAdapters powers built-in adapter enablement and stateless
	// calls. AgentField owns agent tool-call spans/traces.
	toolAdapters *tooladapters.Service
	// adapterRegistry is the runtime-owned inventory of backend adapter
	// slots surfaced to the operator dashboard.
	adapterRegistry *adapterregistry.Registry
	probeRegistry   *probe.Registry
	featureConfig   config.FeatureConfig
	featureWarnings []config.ValidationError
	// toolsRegistry is the strict-interface native tool surface
	// (internal/tools). Construction in main.go via tools.BuildRegistry
	// at boot; nil ⇒ list returns empty + mutations/invokes return 503.
	toolsRegistry *tools.Registry
	// oauthManager stores user-granted third-party OAuth token refs and
	// retrieves plaintext from the secrets vault for backend agents.
	oauthManager *oauth.Manager
	oauthFactory *oauth.Factory
	// shipwright stores only task/patch metadata for the Shipwright
	// coding-agent factory. AgentField owns the execution graph, harness
	// calls, logs, spans, traces, and memory.
	shipwright ShipwrightStore
	// approvals stores tenant-scoped human decision gates for general
	// AF Stack workflows. AgentField run approvals remain in AgentField.
	approvals ApprovalsStore
	// metricsRing collects HTTP request durations + per-route counters
	// for the /api/v1/metrics/summary handler. Always constructed in
	// New().
	metricsRing *metricsRing
	// logRing is the in-memory log buffer the /api/v1/logs endpoint
	// reads from. nil ⇒ the endpoint serves an empty list. main.go
	// constructs one at boot and tees the structured logger into it.
	logRing *logger.Ring
	// logsStore is the active logs adapter slot. It defaults to the ring
	// adapter and may be Loki or remote when configured.
	logsStore logs.Store
	// tracesStore is the active traces adapter slot. It defaults to the empty
	// adapter and may be Tempo or remote when configured.
	tracesStore traces.Store
	// metricsStore is the active metrics adapter slot. It defaults to the none
	// adapter and may be Prometheus or remote when configured.
	metricsStore metrics.Store
	// errorsStore is the active errors adapter slot. It defaults to logfilter
	// and may be GlitchTip or remote when configured.
	errorsStore obserrors.Store
	// version is the runtime version label surfaced in
	// /api/v1/metrics/summary. Defaults to "dev" when empty.
	version string
	// audit writes append-only rows into suite_audit_log for every
	// admin mutation. Always non-nil; falls back to no-op when no DB
	// pool is wired.
	audit *audit.Writer
	// idem backs the inbound idempotency middleware (PRD R1). nil ⇒ the
	// middleware is not installed and every request passes through
	// unchanged. Constructed from the DB pool at New() time, or injected
	// via Deps.Idempotency for tests.
	idem idempotency.Store
}

// Health aggregates dependency health for the /health endpoint.
type Health struct {
	StartedAt time.Time
}

// Deps groups the runtime dependencies the server uses. nil entries are
// gracefully tolerated (the health check reports them as not-configured).
//
// StoragePrefix is the tenant key-prefix applied to every storage key (e.g.
// "tenants/default"). Phase 5 multi-tenancy is off by default; leave empty
// and keys pass through untouched. When MT lands, this will be derived
// per-request from the tenant context.
type Deps struct {
	DB            *db.DB
	AF            *agentfield.Client
	Telemetry     *observability.Telemetry
	Hooks         *hooks.Engine
	Storage       storage.Storage
	StoragePrefix string
	// Secrets is the AES-GCM-encrypted secrets vault. nil means the
	// endpoints return 503; main.go constructs one when both DB and a
	// KEK (env AF_STACK_KMS_KEY or dev fallback) are available.
	Secrets *secrets.Vault
	// Jobs is the River-backed jobs manager. nil means the endpoints
	// return tolerant empty responses (Phase 4 boot mode).
	Jobs *jobs.Manager
	// Tenancy implements the multi-tenancy admin operations. nil means
	// the /api/v1/admin/* endpoints return 503 even if the module flag
	// is on (Phase 6.1 owns the wiring).
	Tenancy *tenancy.Manager
	// OpenAPIBuilder lets callers pre-populate the /openapi.json spec
	// (e.g. tests that want a stable snapshot). nil means New()
	// constructs a fresh Builder with the standard AF Stack title.
	OpenAPIBuilder *openapi.Builder
	// LLMCache is the Phase 7.3 LLM response cache. nil means the
	// Phase 7.1 gateway should treat every call as a miss; the cache
	// is an optional accelerator, never a correctness dependency.
	LLMCache *llmcache.Cache
	// LLMGateway is the Phase 7.1 OpenAI-compatible LLM gateway shim.
	// nil means /api/v1/llm/* return 503 GATEWAY_NOT_CONFIGURED.
	// main.go constructs one from the runtime's provider env keys
	// (OPENROUTER_API_KEY etc.).
	LLMGateway *llmgateway.Gateway
	// Guardrails is the gateway-local PII/moderation policy layer. nil
	// disables redaction/moderation while preserving normal LLM gateway
	// behavior.
	Guardrails *guardrails.Service
	// Memory is the Phase 8.2 suite memory store (pgvector-backed
	// key/value with optional embeddings). nil means /api/v1/memory/*
	// endpoints return 503 MEMORY_NOT_CONFIGURED.
	Memory *memory.Store
	// Search is the app-data search index. It is distinct from AgentField
	// memory/runs/traces and from suite memory; apps/workload modules
	// index product records here for FTS/vector/hybrid lookup.
	Search *search.Store
	// Activity is the customer/user activity log store. It is separate
	// from admin audit and AgentField state.
	Activity *activity.Store
	// FeatureFlags stores durable runtime feature flags.
	FeatureFlags *featureflags.Store
	// DBStudio is the Phase 8.1 read-mostly DB introspection + SQL
	// runner. nil means /api/v1/db/* endpoints return 503
	// DB_STUDIO_NOT_CONFIGURED. main.go constructs one wrapped around
	// the same pgxpool when database != nil.
	DBStudio *dbstudio.Studio
	// CostAggregate powers the dashboard's /api/v1/cost summary +
	// /api/v1/cost/events list. nil = the endpoints return empty
	// zeros (boot mode without DB).
	CostAggregate *cost.Aggregate
	// Budgets is the per-tenant monthly budget store. nil disables
	// the /api/v1/admin/budgets endpoints (which return 503).
	Budgets *cost.Budgets
	// RBAC gates operator/admin actions. nil uses the built-in Casbin
	// policy for suite_operators roles.
	RBAC *rbac.Enforcer
	// Sandbox is the Phase 9.1 sandbox runs service. nil = the
	// /api/v1/sandbox/* endpoints either 503 (mutating) or return
	// tolerant empty responses (reads).
	Sandbox *sandbox.Service
	// BillingSettings stores operator-panel Stripe keys. nil = the
	// admin billing-settings endpoints report settings_writable=false.
	BillingSettings *billing.SettingsStore
	// Billing is the Phase 10.4 Stripe billing service. nil = the
	// /api/v1/billing/* read endpoints return empty / synthesised
	// rows; portal-link mutations return 503 BILLING_NOT_CONFIGURED.
	Billing *billing.Service
	// Notifications is the Phase 10.1 outbox-style notifications
	// service. nil = the /api/v1/notifications/* endpoints either 503
	// (POST) or return tolerant empty responses (GET).
	Notifications *notifications.Service
	// Webhooks is the Phase 10.2 + 10.3 webhook outbox/inbox facade.
	// nil = the /api/v1/webhooks/* mutating endpoints return 503
	// WEBHOOKS_NOT_CONFIGURED; the list endpoint serves an empty
	// page (so the dashboard renders the empty state).
	Webhooks *webhooks.Service
	// Skills is the Phase 11.3 skills install/list/attach store. nil =
	// the /api/v1/skills/* mutating endpoints return 503
	// SKILLS_NOT_CONFIGURED; GET returns an empty list. main.go
	// constructs the store when a DB is available.
	Skills *skills.Store
	// SkillsInstaller turns source strings into Skill records ready to
	// hand to Skills.Install. main.go wires the default installer; tests
	// can pass a stub. When nil the install handler falls back to
	// skills.NewInstaller() so the route stays usable.
	SkillsInstaller *skills.Installer
	// Harnesses powers /api/v1/harnesses/* (Phase 11.4). main.go
	// constructs the service at boot and warms the cache by calling
	// ListAll once before the HTTP server starts accepting traffic.
	// nil = endpoints degrade (list -> empty array; get -> 404;
	// probe -> 503 HARNESSES_NOT_CONFIGURED).
	Harnesses *harnesses.Service
	// MCP is the Phase 11.1 Model Context Protocol pool — long-lived
	// connections to external MCP servers (stdio or sse) keyed by
	// server name. nil = mutating endpoints return 503
	// MCP_NOT_CONFIGURED; reads return an empty list / catalogue.
	// MCPStore is the persistence layer the mutating endpoints write
	// through. main.go calls Pool.Reconcile() at boot and starts the
	// 5-minute refresh goroutine.
	MCP      *mcp.Pool
	MCPStore *mcp.Store
	// Crons is the Phase 12.2 cron schedules store. nil pool ⇒
	// mutating endpoints return 503 CRONS_NOT_CONFIGURED; reads serve
	// an empty page.
	Crons *crons.Store
	// ToolAdapters is the built-in agent tool adapter service. nil =
	// list returns empty; mutations/calls return 503.
	ToolAdapters *tooladapters.Service
	// AdapterRegistry inventories the runtime's configured adapter slots.
	// nil means /api/v1/admin/adapters returns an empty slot list.
	AdapterRegistry *adapterregistry.Registry
	ProbeRegistry   *probe.Registry
	FeatureConfig   config.FeatureConfig
	FeatureWarnings []config.ValidationError
	// ToolsRegistry is the strict-interface native tool surface. nil =
	// /api/v1/tools/native list returns empty; enable/invoke return 503.
	ToolsRegistry *tools.Registry
	// OAuthManager/OAuthFactory power OAuth-on-behalf-of-user. nil
	// leaves the provider list available when Factory exists, but
	// connect/token/disconnect return 503.
	OAuthManager *oauth.Manager
	OAuthFactory *oauth.Factory
	// Shipwright stores task + patch metadata for the autonomous
	// coding-agent factory. nil means /api/v1/shipwright/* returns 503.
	Shipwright ShipwrightStore
	// Approvals stores tenant-scoped human decision gates. nil means
	// /api/v1/approvals/* returns 503.
	Approvals ApprovalsStore
	// LogRing is the in-memory log buffer the /api/v1/logs endpoint
	// reads from. Constructed in main.go and shared with the logger.
	LogRing *logger.Ring
	// LogsStore is the active logs adapter. When nil, New falls back to the
	// compatibility ring path and the admin endpoint serves an empty page.
	LogsStore logs.Store
	// TracesStore is the active traces adapter. When nil, trace search serves
	// an empty page and trace detail returns traces_no_backend.
	TracesStore traces.Store
	// MetricsStore is the active metrics adapter. When nil, admin metrics
	// queries return metrics_no_backend.
	MetricsStore metrics.Store
	// ErrorsStore is the active errors adapter. When nil, admin errors queries
	// return errors_no_backend.
	ErrorsStore obserrors.Store
	// Version is the runtime version string surfaced via
	// /api/v1/metrics/summary. Defaults to "dev".
	Version string
	// Idempotency backs the inbound idempotency middleware (PRD R1). When
	// nil, New() constructs a PgStore from DB when a pool is present;
	// tests inject a fake to exercise the middleware without a database.
	Idempotency idempotency.Store
}

// New constructs a Server with the given config + logger + dependencies.
func New(cfg config.Config, log *slog.Logger, deps Deps) *Server {
	mux := http.NewServeMux()
	builder := deps.OpenAPIBuilder
	if builder == nil {
		builder = openapi.NewBuilder("AF Stack", "0.1.0")
		builder.SetDescription("Public gateway, multi-tenant admin, storage, secrets, jobs.")
		builder.AddTag("agents", "Agent invocation + discovery")
		builder.AddTag("dashboard", "Dashboard read-only endpoints")
		builder.AddTag("storage", "Object storage")
		builder.AddTag("secrets", "Per-tenant secrets vault")
		builder.AddTag("jobs", "Background jobs queue")
		builder.AddTag("admin", "Multi-tenancy admin")
		builder.AddTag("llm", "OpenAI-compatible LLM gateway")
		builder.AddTag("realtime", "Postgres LISTEN/NOTIFY WebSocket bridge")
		builder.AddTag("search", "Tenant-scoped app-data search")
		builder.AddTag("activity", "Tenant-scoped user/product activity log")
		builder.AddTag("approvals", "Human approval gates for workflows")
		builder.AddTag("config", "Runtime configuration and feature flags")
		builder.AddTag("tools", "Built-in agent tool adapters")
		builder.AddTag("oauth", "OAuth on behalf of user")
		builder.AddTag("memory", "Suite memory (pgvector-backed key/value)")
		builder.AddTag("db", "Database studio (introspection + SQL runner)")
		builder.AddTag("system", "Health, readiness, metrics")
	}
	s := &Server{
		cfg:             cfg,
		log:             log,
		mux:             mux,
		health:          &Health{StartedAt: time.Now()},
		drain:           NewDrain(cfg.Server.ShutdownTimeout),
		db:              deps.DB,
		af:              deps.AF,
		tel:             deps.Telemetry,
		hooks:           deps.Hooks,
		storage:         deps.Storage,
		storagePrefix:   deps.StoragePrefix,
		secrets:         deps.Secrets,
		jobs:            deps.Jobs,
		tenancy:         deps.Tenancy,
		openapi:         builder,
		llmCache:        deps.LLMCache,
		llmGateway:      deps.LLMGateway,
		guardrails:      deps.Guardrails,
		memory:          deps.Memory,
		search:          deps.Search,
		activity:        deps.Activity,
		featureFlags:    deps.FeatureFlags,
		dbStudio:        deps.DBStudio,
		costAgg:         deps.CostAggregate,
		budgets:         deps.Budgets,
		rbac:            deps.RBAC,
		sandbox:         deps.Sandbox,
		billing:         deps.Billing,
		billingSettings: deps.BillingSettings,
		notifications:   deps.Notifications,
		webhooks:        deps.Webhooks,
		skills:          deps.Skills,
		skillsInstaller: deps.SkillsInstaller,
		harnesses:       deps.Harnesses,
		mcp:             deps.MCP,
		mcpStore:        deps.MCPStore,
		crons:           deps.Crons,
		toolAdapters:    deps.ToolAdapters,
		adapterRegistry: deps.AdapterRegistry,
		probeRegistry:   deps.ProbeRegistry,
		featureConfig:   deps.FeatureConfig,
		featureWarnings: deps.FeatureWarnings,
		toolsRegistry:   deps.ToolsRegistry,
		oauthManager:    deps.OAuthManager,
		oauthFactory:    deps.OAuthFactory,
		shipwright:      deps.Shipwright,
		approvals:       deps.Approvals,
		metricsRing:     newMetricsRing(MetricsRingSize),
		logRing:         deps.LogRing,
		logsStore:       deps.LogsStore,
		tracesStore:     deps.TracesStore,
		metricsStore:    deps.MetricsStore,
		errorsStore:     deps.ErrorsStore,
		version:         deps.Version,
	}
	if s.billing != nil {
		// Close the billing loop: plan applied (webhook or stub
		// checkout) -> the plan's LLM budget becomes the tenant's
		// enforced monthly cap.
		s.billing.SetOnPlanApplied(s.applyPlanHook)
	}
	if s.rbac == nil {
		s.rbac = rbac.NewDefault()
	}
	s.operatorResolver = s.resolveOperatorPrincipal
	if deps.DB != nil {
		s.audit = audit.New(deps.DB.Pool, log)
	} else {
		s.audit = audit.New(nil, log)
	}
	// Inbound idempotency (PRD R1). Prefer an injected store (tests); else
	// build a PgStore over the DB pool. nil ⇒ middleware not installed.
	if deps.Idempotency != nil {
		s.idem = deps.Idempotency
	} else if deps.DB != nil {
		if st, err := idempotency.New(deps.DB.Pool); err == nil {
			s.idem = st
		}
	}
	// OAuth provider credentials resolve vault-first (operator-entered
	// integration slots) with an env fallback owned by the factory. Wiring
	// the resolver here — rather than at factory construction in main — is
	// what lets a credential saved via /admin/integrations take effect on
	// the next OAuth request without a runtime restart. The resolver reads
	// s.integrationStore() lazily, so it also honours the test store hook.
	if s.oauthFactory != nil {
		s.oauthFactory.SetCredentialResolver(s.newOAuthCredentialResolver())
	}
	s.registerRoutes()

	// Wrap mux with OTel tracing first, then structured logging on the outside
	// so logs include final status code. CORS wraps last so OPTIONS preflights
	// short-circuit before hitting the routes.
	handler := http.Handler(mux)
	// Inbound idempotency (PRD R1) wraps the mux so it runs INSIDE the
	// tenant resolver (needs the resolved tenant) but around every handler.
	// nil store ⇒ not installed, zero overhead. See idempotency_mw.go.
	if s.idem != nil {
		handler = s.idempotencyMiddleware(handler)
	}
	// Per-tenant request throttling is enforced upstream by LiteLLM via
	// the virtual-key rpm_limit / tpm_limit issued by tenancy.IssueAPIKey
	// (see internal/llmgateway/litellm_admin.go). When LiteLLM returns
	// 429 the LLM handler proxies Retry-After + X-RateLimit-* headers
	// back to the client unchanged.
	//
	// Tenant resolver wraps the mux so the resolved tenant id is
	// available to downstream handlers. Public paths (/health,
	// /openapi.json, dashboard reads) bypass internally — see
	// isPublicPath.
	handler = s.tenantResolver(handler)
	// withRecover sits just outside the resolver + mux so it catches
	// panics from any handler (and the resolver), turning them into a
	// logged error + 500 envelope. It runs inside withLogging so the
	// recovered 500 is still access-logged and metered.
	handler = withRecover(log, handler)
	if deps.Telemetry != nil {
		handler = observability.TraceMiddleware(cfg.Observability.ServiceName)(handler)
	}
	handler = withLogging(log, s.metricsRing, handler)
	handler = withCORS(handler)
	// withDrain is the outermost middleware so it runs before CORS,
	// logging, tracing — every other layer. This guarantees a draining
	// server short-circuits requests with minimal work, and that the
	// active-request counter is incremented before any per-request
	// allocation happens.
	handler = withDrain(s.drain, handler)

	s.srv = &http.Server{
		Addr:              cfg.Server.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return s
}

// registerRoutes wires the default endpoints. Modules will add more in Phase 1.5+.
func (s *Server) registerRoutes() {
	// Catch-all so an unmatched route returns the canonical JSON error
	// envelope instead of Go's default plain-text "404 page not found".
	// The "/" pattern is the least specific in the mux, so every
	// registered route still wins; only genuinely-unknown paths land here.
	s.mux.HandleFunc("/", s.handleNotFound)

	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.openapi.Register("GET", "/health", openapi.RouteMeta{
		Summary: "Liveness probe", Tags: []string{"system"},
		OkDescription: "Runtime is up; per-dependency status in body",
	})
	s.mux.HandleFunc("GET /ready", s.handleReady)
	s.openapi.Register("GET", "/ready", openapi.RouteMeta{
		Summary: "Readiness probe", Tags: []string{"system"},
		OkDescription: "Runtime + required deps are healthy",
	})
	s.mux.HandleFunc("GET /openapi.json", s.handleOpenAPI)
	s.openapi.Register("GET", "/openapi.json", openapi.RouteMeta{
		Summary: "OpenAPI 3.1 spec for this runtime", Tags: []string{"system"},
	})
	// Alias under the versioned prefix. AGENTS.md, the CLI, and several
	// docs reference GET /api/v1/openapi.json; serve it there too so every
	// documented path resolves. Kept public in the tenant resolver.
	s.mux.HandleFunc("GET /api/v1/openapi.json", s.handleOpenAPI)

	s.mux.HandleFunc("GET /api/v1/realtime", s.handleRealtime)
	s.openapi.Register("GET", "/api/v1/realtime", openapi.RouteMeta{
		Summary: "Subscribe to Postgres-backed realtime events", Tags: []string{"realtime"},
		Parameters: []openapi.Parameter{
			{Name: "table", In: "query", Required: true,
				Description: "Table identifier emitted in NOTIFY payloads, e.g. public.messages",
				Schema:      map[string]any{"type": "string"}},
			{Name: "filter", In: "query", Required: false,
				Description: "Optional JSON object matched against payload.record",
				Schema:      map[string]any{"type": "string"}},
		},
	})

	// #15 Realtime run subscriptions — WebSocket stream of live
	// AgentField run events with server-side tenant/user/agent/run
	// filters. Bridges AF's SSE stream(s); falls back to polling when
	// AF's UI module isn't enabled. See internal/server/runs.go.
	s.mux.HandleFunc("GET /api/v1/realtime/runs", s.handleRunsSubscribe)
	s.openapi.Register("GET", "/api/v1/realtime/runs", openapi.RouteMeta{
		Summary: "Subscribe to live AgentField run events", Tags: []string{"realtime"},
		Parameters: []openapi.Parameter{
			{Name: "tenant_id", In: "query", Required: false,
				Description: "Auto-bound to caller's tenant when multi-tenancy is on",
				Schema:      map[string]any{"type": "string"}},
			{Name: "user_id", In: "query", Required: false,
				Schema: map[string]any{"type": "string"}},
			{Name: "agent", In: "query", Required: false,
				Description: "Filter to one agent_node_id",
				Schema:      map[string]any{"type": "string"}},
			{Name: "run_id", In: "query", Required: false,
				Schema: map[string]any{"type": "string"}},
			{Name: "execution_id", In: "query", Required: false,
				Description: "Pin subscription to one execution (uses per-exec SSE)",
				Schema:      map[string]any{"type": "string"}},
		},
	})

	s.mux.HandleFunc("GET /api/v1/agents", s.handleListAgents)
	s.openapi.Register("GET", "/api/v1/agents", openapi.RouteMeta{
		Summary: "Discover registered AgentField agents", Tags: []string{"agents"},
	})

	// Agent invocation — the central forwarding endpoints. Every LLM call
	// in the suite eventually routes through these.
	s.mux.HandleFunc("POST /api/v1/agents/{call}", s.handleAgentCall)
	s.openapi.Register("POST", "/api/v1/agents/{call}", openapi.RouteMeta{
		Summary: "Synchronously invoke an agent", Tags: []string{"agents"},
		Parameters: []openapi.Parameter{
			{Name: "call", In: "path", Required: true,
				Description: "ns.func identifier (e.g. sample.echo)",
				Schema:      map[string]any{"type": "string"}},
		},
	})
	s.mux.HandleFunc("POST /api/v1/agents/async/{call}", s.handleAgentCallAsync)
	s.openapi.Register("POST", "/api/v1/agents/async/{call}", openapi.RouteMeta{
		Summary: "Asynchronously invoke an agent", Tags: []string{"agents"},
		Parameters: []openapi.Parameter{
			{Name: "call", In: "path", Required: true,
				Schema: map[string]any{"type": "string"}},
		},
	})
	s.mux.HandleFunc("GET /api/v1/executions/{id}", s.handleGetExecution)
	s.openapi.Register("GET", "/api/v1/executions/{id}", openapi.RouteMeta{
		Summary: "Get execution status (proxied to AgentField)", Tags: []string{"agents"},
		Parameters: []openapi.Parameter{
			{Name: "id", In: "path", Required: true,
				Schema: map[string]any{"type": "string"}},
		},
	})
	s.mux.HandleFunc("DELETE /api/v1/executions/{id}", s.handleCancelExecution)
	s.openapi.Register("DELETE", "/api/v1/executions/{id}", openapi.RouteMeta{
		Summary: "Cancel an in-flight execution", Tags: []string{"agents"},
		Parameters: []openapi.Parameter{
			{Name: "id", In: "path", Required: true,
				Schema: map[string]any{"type": "string"}},
		},
	})

	// Dashboard read-only endpoints (see internal/server/dashboard.go).
	// S1b: the bulk runs list bypasses the tenant resolver (publicPrefixes) and
	// honours an attacker-controlled ?tenant= filter — gate it to operators.
	s.mux.HandleFunc("GET /api/v1/runs", s.operatorGuard(rbac.ResourceAdminRuns, s.handleListRuns))
	s.openapi.Register("GET", "/api/v1/runs", openapi.RouteMeta{
		Summary: "List recent agent runs", Tags: []string{"dashboard"},
	})
	// S1b: cross-tenant cost aggregate — operator-only, like /api/v1/runs.
	s.mux.HandleFunc("GET /api/v1/reasoners/analytics", s.operatorGuard(rbac.ResourceAdminReasoners, s.handleReasonersAnalytics))
	s.openapi.Register("GET", "/api/v1/reasoners/analytics", openapi.RouteMeta{
		Summary: "Reasoner cost, latency, and error analytics", Tags: []string{"dashboard"},
	})
	s.mux.HandleFunc("GET /api/v1/tools/usage", s.handleToolsUsage)
	s.openapi.Register("GET", "/api/v1/tools/usage", openapi.RouteMeta{
		Summary: "Native and MCP tool usage analytics", Tags: []string{"dashboard"},
	})
	s.mux.HandleFunc("GET /api/v1/oauth/refresh-history", s.handleOAuthRefreshHistory)
	s.openapi.Register("GET", "/api/v1/oauth/refresh-history", openapi.RouteMeta{
		Summary: "OAuth refresh attempt history", Tags: []string{"oauth"},
	})
	// /api/v1/runs is a public prefix and the sibling list route is already
	// operator-gated; the per-run event stream was missed, leaving it
	// unauthenticated. Gate it too so run events aren't streamable by an
	// unauthenticated caller who can guess a run id.
	s.mux.HandleFunc("GET /api/v1/runs/{id}/events", s.operatorGuard(rbac.ResourceAdminRuns, s.handleRunEvents))
	s.openapi.Register("GET", "/api/v1/runs/{id}/events", openapi.RouteMeta{
		Summary: "Subscribe to AgentField run events", Tags: []string{"agents"},
		Parameters: []openapi.Parameter{
			{Name: "id", In: "path", Required: true,
				Description: "AgentField run/workflow id",
				Schema:      map[string]any{"type": "string"}},
		},
	})
	// #25 AgentField data in dashboard: per-run summary + control actions.
	// Each verb proxies to AgentField's agent-api surface. See
	// internal/server/run_agentfield.go for handler details.
	s.mux.HandleFunc("GET /api/v1/runs/{id}/agentfield", s.handleRunAgentField)
	s.mux.HandleFunc("POST /api/v1/runs/{id}/cancel", s.handleRunCancel)
	s.mux.HandleFunc("POST /api/v1/runs/{id}/pause", s.handleRunPause)
	s.mux.HandleFunc("POST /api/v1/runs/{id}/resume", s.handleRunResume)
	s.mux.HandleFunc("POST /api/v1/runs/{id}/request-approval", s.handleRunRequestApproval)
	s.registerRunAgentFieldOpenAPI()
	s.mux.HandleFunc("GET /api/v1/home/overview", s.handleHomeOverview)
	s.openapi.Register("GET", "/api/v1/home/overview", openapi.RouteMeta{
		Summary: "Dashboard home overview", Tags: []string{"dashboard"},
	})
	s.mux.HandleFunc("GET /api/v1/cost", s.handleCostSummary)
	s.openapi.Register("GET", "/api/v1/cost", openapi.RouteMeta{
		Summary: "Cost summary by period", Tags: []string{"dashboard"},
	})
	s.mux.HandleFunc("GET /api/v1/modules", s.handleModulesState)
	s.openapi.Register("GET", "/api/v1/modules", openapi.RouteMeta{
		Summary: "Module enablement state", Tags: []string{"dashboard"},
	})
	// S1b: /api/v1/queues is on publicPrefixes — cross-tenant job summary,
	// operator-only.
	s.mux.HandleFunc("GET /api/v1/queues/summary", s.operatorGuard(rbac.ResourceAdminQueues, s.handleQueueSummary))
	s.openapi.Register("GET", "/api/v1/queues/summary", openapi.RouteMeta{
		Summary: "Background jobs queue summary", Tags: []string{"dashboard"},
	})

	// Storage (Phase 5). Endpoints return 503 when no adapter is wired.
	s.registerStorageRoutes()
	s.registerStorageOpenAPI()

	// Secrets vault (Phase 5). Endpoints return 503 when no vault is wired.
	s.registerSecretsRoutes()
	s.registerSecretsOpenAPI()

	// Jobs queue (Phase 5). Endpoints return tolerant empty responses when
	// no manager is wired (no DB present at boot time).
	s.mux.HandleFunc("POST /api/v1/jobs", s.handleEnqueueJob)
	s.openapi.Register("POST", "/api/v1/jobs", openapi.RouteMeta{
		Summary: "Enqueue a job", Tags: []string{"jobs"},
	})
	s.mux.HandleFunc("GET /api/v1/jobs", s.handleListJobs)
	s.openapi.Register("GET", "/api/v1/jobs", openapi.RouteMeta{
		Summary: "List jobs", Tags: []string{"jobs"},
	})
	s.mux.HandleFunc("GET /api/v1/jobs/definitions", s.handleJobDefinitions)
	s.openapi.Register("GET", "/api/v1/jobs/definitions", openapi.RouteMeta{
		Summary: "List job kind definitions + stats", Tags: []string{"jobs"},
	})
	s.mux.HandleFunc("GET /api/v1/jobs/{id}", s.handleGetJob)
	s.openapi.Register("GET", "/api/v1/jobs/{id}", openapi.RouteMeta{
		Summary: "Get a single job", Tags: []string{"jobs"},
	})
	s.mux.HandleFunc("POST /api/v1/jobs/{id}/retry", s.handleRetryJob)
	s.openapi.Register("POST", "/api/v1/jobs/{id}/retry", openapi.RouteMeta{
		Summary: "Mark a job retryable", Tags: []string{"jobs"},
	})

	// LLM gateway (Phase 7.1). Endpoints return 503 when no
	// llmgateway.Gateway is wired (main.go constructs one from the
	// runtime's provider env keys).
	s.registerLLMRoutes()
	s.registerLLMOpenAPI()

	// Suite memory (Phase 8.2). Endpoints return 503 when no
	// memory.Store is wired (no DB at boot).
	s.registerMemoryRoutes()
	s.registerMemoryOpenAPI()

	// App-data search (Phase 2 completeness). Endpoints return 503 when
	// no search.Store is wired (no DB at boot). This is separate from
	// AgentField stateful primitives and suite memory.
	s.registerSearchRoutes()
	s.registerSearchOpenAPI()

	// User activity log (Phase 2 completeness). Product/customer events
	// live here; admin mutations stay in suite_audit_log.
	s.registerActivityRoutes()
	s.registerActivityOpenAPI()

	// Runtime feature flags (Phase 2 completeness). GET can serve the
	// built-in defaults even without a DB; mutations need persistence.
	s.registerConfigRoutes()
	s.registerConfigOpenAPI()

	// DB studio (Phase 8.1). Endpoints return 503 when no
	// dbstudio.Studio is wired (no DB at boot).
	s.registerDBStudioRoutes()
	s.registerDBStudioOpenAPI()

	// Multi-tenancy admin (Phase 6). Endpoints return 503 when the
	// multi-tenancy module is disabled or when no tenancy.Manager is
	// wired (the latter is Phase 6.1's responsibility).
	s.registerAdminRoutes()
	s.registerAdminOpenAPI()
	s.registerAdminAdapterRoutes()
	s.registerAdminAdapterOpenAPI()
	s.registerAdminIntegrationRoutes()
	s.registerAdminIntegrationOpenAPI()
	s.registerAdminServicesRoutes()
	s.registerAdminServicesOpenAPI()
	s.registerAdminEventsRoutes()
	s.registerAdminEventsOpenAPI()
	s.registerAdminSessionsRoutes()
	s.registerAdminSessionsOpenAPI()
	s.registerAdminAnchorsRoutes()
	s.registerAdminAnchorsOpenAPI()
	s.registerAdminDemoRoutes()
	s.registerAdminDemoOpenAPI()
	s.registerAdminFeaturesRoutes()
	s.registerAdminFeaturesOpenAPI()
	s.registerDBHealthRoutes()
	s.registerDBHealthOpenAPI()
	s.registerLLMProviderHealthRoutes()
	s.registerLLMProviderHealthOpenAPI()
	s.registerAdminBrandRoutes()
	s.registerAdminBrandOpenAPI()

	// GDPR/data-rights endpoints for AF Stack-held app/backend records.
	// AgentField-owned runs/spans/traces/sessions/memory stay in
	// AgentField and are only referenced in exports when AF Stack stores
	// an execution id.
	s.registerGDPRRoutes()

	// Cost ledger + budgets (Phase 7). Cost-event endpoint degrades to
	// empty list when no Aggregate is wired; budget endpoints are gated
	// by the multi-tenancy module flag and return 503 otherwise.
	s.registerCostRoutes()
	s.registerCostOpenAPI()

	// Sandboxes (Phase 9.1). Endpoints return 503 when no adapter is
	// wired; reads degrade to empty pages.
	s.registerSandboxRoutes()
	s.registerSandboxOpenAPI()

	// Notifications outbox (Phase 10.1). POST returns 503 when no
	// adapter is wired; reads degrade to empty pages.
	s.registerNotificationsRoutes()
	s.registerNotificationsOpenAPI()

	// Webhooks (Phase 10.2 + 10.3). Mutating endpoints (send, retry,
	// endpoint CRUD) return 503 when no store is wired; the list
	// endpoint degrades to an empty page.
	s.registerWebhooksRoutes()
	s.registerWebhookSubscriptionRoutes()
	s.registerWebhooksOpenAPI()

	// Billing (Phase 10.4). Read endpoints serve empty / synthesised
	// rows when no service is wired; the portal-link mutation returns
	// 503 BILLING_NOT_CONFIGURED. Also registers the Stripe webhook
	// receiver at /webhooks/in/stripe — see billing.go file-level
	// comment for the Phase 10.2 coordination note.
	s.registerBillingRoutes()
	s.registerBillingPlanRoutes()
	s.registerBillingOpenAPI()

	// Auth identity introspection: GET /api/v1/auth/whoami returns the
	// tenant/user/key the current credential resolves to. Auth lifecycle
	// (sign-up / sign-in) stays with the app-layer (better-auth); this is
	// the read-only "who am I" primitive that belongs in the SDK.
	s.registerAuthRoutes()
	s.registerAuthOpenAPI()

	// Skills (Phase 11.3). Mutating endpoints (install / uninstall /
	// attach) return 503 when no store is wired; the list endpoint
	// degrades to an empty page so the dashboard renders.
	s.registerSkillsRoutes()
	s.registerSkillsOpenAPI()

	// Harnesses (Phase 11.4). Probe-only — main.go constructs the
	// service and warms the cache at boot. nil service ⇒ list returns
	// an empty array, get returns 404, probe returns 503.
	s.registerHarnessesRoutes()
	s.registerHarnessesOpenAPI()

	// MCP (Phase 11.1). The Pool owns long-lived JSON-RPC connections
	// to external MCP servers and proxies tool calls. nil pool =
	// mutating endpoints return 503 MCP_NOT_CONFIGURED; GETs return
	// tolerant empty pages.
	s.registerMCPRoutes()
	s.registerMCPOpenAPI()

	// Plugins (Phase 12.3). Endpoint returns an empty PluginList — the
	// dashboard owns plugin discovery at its own build time and merges
	// the local manifest client-side. See plugins.go for rationale.
	s.registerPluginsRoutes()
	s.registerPluginsOpenAPI()

	// Crons (Phase 12.2). Mutating endpoints return 503 when no store
	// is wired; reads degrade to empty pages.
	s.registerCronsRoutes()
	s.registerCronsOpenAPI()

	// Built-in tool adapters (Phase 2 AI completeness). This owns
	// tenant-scoped enablement + stateless calls; AgentField owns traces.
	s.registerToolAdapterRoutes()
	s.registerToolAdapterOpenAPI()

	// Native tool adapters (#16 — strict-interface adapter set in
	// internal/tools/). Lives alongside the legacy /api/v1/tools/adapters
	// surface; new agents call into the `native:<tool>` MCP namespace.
	s.registerNativeToolRoutes()
	s.registerNativeToolOpenAPI()

	// OAuth-on-behalf-of-user (Phase 2 AI completeness). Stores
	// third-party user grants in the secrets vault for backend agents.
	s.registerOAuthRoutes()
	s.registerOAuthOpenAPI()

	// Shipwright (Phase 3 Tier 1). AF Stack owns task/patch metadata;
	// AgentField owns the coding-agent execution state.
	s.registerShipwrightRoutes()
	s.registerShipwrightOpenAPI()

	// Approvals (Phase 3 Tier 1). General-purpose human decision gates
	// for AF Stack workflows; distinct from AgentField run approvals.
	s.registerApprovalRoutes()
	s.registerApprovalOpenAPI()

	// Metrics summary (Phase 12.2). Always wired — runtime + ring data
	// is available even without a DB.
	s.registerMetricsRoutes()
	s.registerMetricsOpenAPI()
	s.registerAdminMetricsRoutes()
	s.registerAdminMetricsOpenAPI()

	// Errors adapter slot. The default logfilter adapter groups error logs with
	// volatile status overrides; GlitchTip/remote activate by config.
	s.registerAdminErrorsRoutes()
	s.registerAdminErrorsOpenAPI()

	// Logs (Phase 12.2). When no ring is wired the endpoint serves an
	// empty array; otherwise returns the most-recent buffered lines.
	s.registerLogsRoutes()
	s.registerLogsOpenAPI()

	// Traces adapter slot. The default empty adapter serves zero-state search
	// and reports no backend for trace detail; Tempo/remote activate by config.
	s.registerTraceRoutes()
	s.registerTraceOpenAPI()

	if s.tel != nil {
		// Serve /metrics on a dedicated internal listener when MetricsAddr is
		// set to a distinct port; otherwise fall back to the main mux so
		// single-port local-dev setups keep working. The dedicated listener
		// keeps per-tenant metric labels off the public API port.
		if metricsOnSeparatePort(s.cfg.Server) {
			metricsMux := http.NewServeMux()
			metricsMux.Handle("GET /metrics", s.tel.MetricsHandler())
			// A liveness endpoint on the metrics port so an orchestrator can
			// probe the internal listener without touching the public API.
			metricsMux.HandleFunc("GET /health", s.handleHealth)
			s.metricsSrv = &http.Server{
				Addr:              s.cfg.Server.MetricsAddr,
				Handler:           metricsMux,
				ReadHeaderTimeout: 10 * time.Second,
			}
		} else {
			s.mux.Handle("GET /metrics", s.tel.MetricsHandler())
			s.openapi.Register("GET", "/metrics", openapi.RouteMeta{
				Summary: "Prometheus scrape endpoint", Tags: []string{"system"},
			})
		}
	}
}

// Start runs the HTTP server. Blocks until ctx is cancelled, then
// orchestrates graceful shutdown:
//
//  1. drain.Start() — /ready flips to 503 {"status":"draining"} and
//     new requests get rejected with 503 + Connection: close.
//  2. Wait up to cfg.Server.ShutdownTimeout for in-flight requests to
//     complete.
//  3. http.Server.Shutdown() — close listener, drain idle conns.
//
// Background workers + DB pool + storage adapter shutdown is the
// caller's responsibility (see main.go). This method only owns the
// HTTP listener lifecycle so tests can use it directly without
// wiring every dependency.
//
// Phase 14.3 note: the SIGTERM handler in main.go calls drain.Start()
// + shutdown of background workers BEFORE cancelling ctx. By the
// time we reach the ctx.Done() branch here drain is already in
// flight and the active counter may already be zero — that's fine,
// we just call http.Server.Shutdown() which is idempotent.
// metricsOnSeparatePort reports whether the Prometheus scrape endpoint should
// bind its own listener. True when a metrics address is configured and differs
// from the main HTTP address; false collapses metrics onto the main mux for
// single-port local dev.
func metricsOnSeparatePort(cfg config.ServerConfig) bool {
	return cfg.MetricsAddr != "" && cfg.MetricsAddr != cfg.HTTPAddr
}

func (s *Server) Start(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.log.Info("http server listening", "addr", s.cfg.Server.HTTPAddr)
		err := s.srv.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		s.log.Info("shutdown requested, draining")
		// If main.go hasn't already called drain.Start(), do it now
		// so /ready flips and new requests are shed. Start() is
		// idempotent (sync.Once) so a duplicate call from main.go's
		// SIGTERM handler is harmless.
		drainCtx, drainCancel := context.WithTimeout(context.Background(), s.cfg.Server.ShutdownTimeout)
		drainErr := s.drain.Start(drainCtx)
		drainCancel()
		if drainErr != nil {
			s.log.Warn("drain timed out with in-flight requests",
				"active_requests", s.drain.Active(),
				"error", drainErr)
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.Server.ShutdownTimeout)
		defer cancel()
		if err := s.srv.Shutdown(shutdownCtx); err != nil {
			s.log.Error("http shutdown failed", "error", err)
			return err
		}
		s.log.Info("http shutdown complete")
		return nil
	case err := <-errCh:
		return err
	}
}

// handleHealth is the Kubernetes-style liveness probe. It returns 200
// as long as the process is responsive — it does NOT check downstream
// dependencies (DB, AF, etc.) because a transient DB blip should not
// cause the orchestrator to kill the pod. That's the job of /ready.
//
// Cheap by design: no DB calls, no AF calls, no allocation beyond the
// response body. Kubernetes default liveness period is 10s; this
// handler should respond in microseconds.
//
// Response shape (stable contract — do not change without versioning):
//
//	{
//	  "status":     "alive",
//	  "uptime_s":   42,
//	  "started_at": "2026-06-07T12:00:00Z"
//	}
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "alive",
		"uptime_s":   int(time.Since(s.health.StartedAt).Seconds()),
		"started_at": s.health.StartedAt.UTC().Format(time.RFC3339),
	})
}

// handleReady is the Kubernetes-style readiness probe. It returns 200
// ONLY when:
//
//  1. The process has finished startup (MarkReady() has been called by
//     main.go after migrations + worker startup).
//  2. The DB is reachable (when configured).
//  3. The process is NOT in drain mode.
//
// Otherwise it returns 503 with a payload that tells operators (and
// load-balancer probes) WHY the pod isn't ready:
//
//	{"status": "booting" | "draining" | "db_unavailable", "since_s": N}
//
// The Retry-After header is set when draining so well-behaved clients
// know how long to wait before retrying. Booting / db_unavailable use
// a conservative 5s.
//
// 503 responses use the standard error envelope under the hood (so
// SDK clients keep their existing error.code branching) AND surface
// the same fields at the top level so naive readiness-probe parsers
// don't need to dig into error.details. Belt-and-braces.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	// Draining wins over every other state — once we've started
	// shedding load, the LB needs to see 503 immediately regardless
	// of DB state.
	if s.drain != nil && s.drain.IsDraining() {
		started := s.drain.DrainStart()
		since := time.Since(started)
		remaining := s.drain.Timeout() - since
		if remaining < time.Second {
			remaining = time.Second
		}
		w.Header().Set("Retry-After", strconv.Itoa(int(remaining.Seconds())))
		writeReadyError(w, "draining", "server is draining; retry on another instance", map[string]any{
			"since_s":         int(since.Seconds()),
			"timeout_s":       int(s.drain.Timeout().Seconds()),
			"active_requests": s.drain.Active(),
		})
		return
	}

	// Still booting — main.go hasn't called MarkReady() yet (migrations
	// or worker startup in progress). Kubernetes will keep probing
	// every readinessProbe.periodSeconds; 5s is a reasonable hint.
	if !s.ready.Load() {
		since := time.Since(s.health.StartedAt)
		w.Header().Set("Retry-After", "5")
		writeReadyError(w, "booting", "server has not finished startup", map[string]any{
			"since_s": int(since.Seconds()),
		})
		return
	}

	// DB check — uses a short timeout via db.Health (2s internal).
	// We DON'T check AF here because /ready is about whether THIS
	// pod can serve traffic; AF unreachability is degraded service,
	// not unready service. Surface AF state via /health checks
	// payload instead (callers can decide based on use-case).
	if s.db != nil {
		if err := s.db.Health(r.Context()); err != nil {
			w.Header().Set("Retry-After", "5")
			writeReadyError(w, "db_unavailable", "database unreachable", map[string]any{
				"db_error": err.Error(),
			})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ready",
		"uptime_s": int(time.Since(s.health.StartedAt).Seconds()),
	})
}

// MarkReady flips the readiness flag. Called by main.go AFTER all
// migrations have applied and all background workers have been
// spawned. Until this is called /ready returns 503 {"status":"booting"}.
//
// Idempotent — calling twice is harmless. Safe to call from any
// goroutine (atomic store).
func (s *Server) MarkReady() {
	if s == nil {
		return
	}
	s.ready.Store(true)
}

// Drain returns the drain controller. main.go uses this to trigger
// graceful shutdown from its SIGTERM handler. Returns nil only when
// the Server was constructed with a nil cfg (never happens in
// production; tests that don't need drain semantics can leave it
// alone).
func (s *Server) Drain() *Drain {
	if s == nil {
		return nil
	}
	return s.drain
}

// HTTPServer exposes the underlying *http.Server so main.go can call
// Shutdown(ctx) directly after the drain step. We avoid wrapping
// Shutdown in a Server method because main.go needs to interleave it
// between drain.Start() and the worker-shutdown sequence; cleaner to
// hand out the *http.Server.
func (s *Server) HTTPServer() *http.Server {
	if s == nil {
		return nil
	}
	return s.srv
}

// MetricsServer returns the dedicated internal listener that serves the
// Prometheus /metrics endpoint, or nil when metrics share the main API port
// (MetricsAddr empty or equal to HTTPAddr). main.go owns the lifecycle: it
// starts this alongside the API listener and shuts it down during drain — the
// same reason HTTPServer() is exposed.
func (s *Server) MetricsServer() *http.Server {
	if s == nil {
		return nil
	}
	return s.metricsSrv
}

// handleOpenAPI serves the live OpenAPI 3.1 spec generated by the
// in-process Builder. Every Register() call at boot accumulates into
// this; the response is a deterministic snapshot (path map is sorted by
// the JSON encoder).
//
// We always emit "application/json" — many OpenAPI consumers (Swagger
// UI, Redoc) accept JSON for 3.1 even though the spec also allows YAML.
func (s *Server) handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.openapi.Build())
}

// handleAgentCall forwards a synchronous agent invocation to AgentField.
//
// URL path: POST /api/v1/agents/{call} where {call} is "node_id.func_name".
// Request body: JSON; forwarded to AF unchanged.
// Response: AF's response, status code preserved.
//
// This is the core forwarding endpoint. Phase 2 keeps it minimal; later
// phases add auth, multi-tenancy, rate limiting, audit logging, hooks.
func (s *Server) handleAgentCall(w http.ResponseWriter, r *http.Request) {
	s.forwardAgentCall(w, r, "/api/v1/execute/")
}

// handleAgentCallAsync forwards an async agent invocation.
func (s *Server) handleAgentCallAsync(w http.ResponseWriter, r *http.Request) {
	s.forwardAgentCall(w, r, "/api/v1/execute/async/")
}

func (s *Server) forwardAgentCall(w http.ResponseWriter, r *http.Request, afPrefix string) {
	start := time.Now()
	if s.af == nil {
		writeJSON(w, http.StatusServiceUnavailable, errEnvelope("AGENTFIELD_NOT_CONFIGURED", "agentfield client not configured"))
		return
	}
	call := r.PathValue("call")
	if call == "" {
		writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", "agent call name required"))
		return
	}
	if !validAgentName(call) {
		writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", "invalid agent name: must be ns.func"))
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errEnvelope("BAD_REQUEST", "could not read request body"))
		return
	}

	endpoint := afPrefix + call
	afResp, err := s.af.Execute(r.Context(), endpoint, body)
	if err != nil {
		s.log.Error("agent forward failed", "call", call, "error", err)
		s.logGatewayRequest(r, endpoint, http.StatusBadGateway, len(body), 0, "", start)
		writeJSON(w, http.StatusBadGateway, errEnvelope("AGENTFIELD_UNREACHABLE", err.Error()))
		return
	}

	// Upstream agent errors (non-2xx) are mapped into the canonical error
	// envelope so SDK/dashboard consumers branch on error.code instead of
	// the agent's raw, per-framework error shape. Success + redirects pass
	// through untouched.
	if afResp.StatusCode >= 400 {
		if afResp.StatusCode >= 500 {
			// A 5xx from the agent means the run itself failed (the reasoner
			// raised, timed out, or the harness crashed). Emit at Error
			// level so it surfaces on the Errors dashboard, which derives
			// its groups from error-level log lines — otherwise a failed run
			// is visible only in the Runs view.
			s.log.Error("agent run failed",
				"call", call,
				"status", afResp.StatusCode,
				"execution_id", afResp.ExecutionID,
			)
		}
		s.writeAgentUpstreamError(w, afResp)
		s.logGatewayRequest(r, endpoint, afResp.StatusCode, len(body), len(afResp.Body), afResp.ExecutionID, start)
		return
	}

	if afResp.ContentType != "" {
		w.Header().Set("Content-Type", afResp.ContentType)
	}
	if afResp.ExecutionID != "" {
		w.Header().Set("X-Execution-ID", afResp.ExecutionID)
	}
	w.WriteHeader(afResp.StatusCode)
	_, _ = w.Write(afResp.Body)

	s.logGatewayRequest(r, endpoint, afResp.StatusCode, len(body), len(afResp.Body),
		afResp.ExecutionID, start)
}

// handleNotFound is the mux catch-all: any path with no registered route
// gets the canonical JSON error envelope rather than Go's default
// plain-text 404.
func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "NOT_FOUND",
		"no route for "+r.Method+" "+r.URL.Path, nil)
}

// writeAgentUpstreamError renders a non-2xx agent response as the
// canonical error envelope. If the agent already returned a canonical
// envelope ({error:{code,...}}), it is forwarded unchanged so we never
// double-wrap; otherwise the upstream payload is preserved under
// details.upstream and the status is mapped to a stable code.
func (s *Server) writeAgentUpstreamError(w http.ResponseWriter, afResp agentfield.ExecuteResponse) {
	if hasCanonicalErrorEnvelope(afResp.Body) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(afResp.StatusCode)
		_, _ = w.Write(afResp.Body)
		return
	}
	code, message := agentUpstreamErrorCode(afResp.StatusCode)
	writeError(w, afResp.StatusCode, code, message, upstreamErrorDetails(afResp.Body))
}

// agentUpstreamErrorCode maps an upstream HTTP status to a stable,
// machine-readable code for the error envelope.
func agentUpstreamErrorCode(status int) (code, message string) {
	switch {
	case status == http.StatusNotFound:
		return "AGENT_NOT_FOUND", "agent or reasoner not found"
	case status == http.StatusTooManyRequests:
		return "AGENT_RATE_LIMITED", "agent rate limited the call"
	case status >= 500:
		return "AGENT_ERROR", "agent execution failed"
	default:
		return "AGENT_CALL_REJECTED", "agent rejected the call"
	}
}

// hasCanonicalErrorEnvelope reports whether body is already an AF Stack
// error envelope ({"error":{"code":"...", ...}}), so we can forward it
// unchanged rather than nesting it.
func hasCanonicalErrorEnvelope(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	var probe struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return false
	}
	return probe.Error != nil && probe.Error.Code != ""
}

// upstreamErrorDetails preserves the agent's original error payload under
// a details object: parsed JSON when the body is JSON, otherwise a
// bounded raw string. Returns nil for an empty body (details omitted).
func upstreamErrorDetails(body []byte) any {
	if len(body) == 0 {
		return nil
	}
	var parsed any
	if err := json.Unmarshal(body, &parsed); err == nil {
		return map[string]any{"upstream": parsed}
	}
	const maxRaw = 2048
	s := string(body)
	if len(s) > maxRaw {
		s = s[:maxRaw]
	}
	return map[string]any{"upstream_body": s}
}

// logGatewayRequest writes a row into suite_gateway_requests so the
// dashboard's /api/v1/runs and home overview endpoints have data.
//
// Best-effort: a write failure is logged but never blocks the response.
//
// tenant_id + api_key_id come from the request's context (populated by
// the Phase 6.1 tenant resolver). Empty strings → NULL columns, which
// is the correct shape for unauthenticated paths like /health.
func (s *Server) logGatewayRequest(
	r *http.Request,
	endpoint string,
	status int,
	requestBytes int,
	responseBytes int,
	executionID string,
	startedAt time.Time,
) {
	s.logGatewayRequestLabeled(r, endpoint, "", status, requestBytes, responseBytes, executionID, startedAt)
}

// logGatewayRequestLabeled is logGatewayRequest with an explicit caller
// label for endpoints whose path doesn't identify the caller (the LLM
// gateway: every call shares "/api/v1/llm/chat/completions", so the
// X-AF-Reasoner-derived label is what the Runs view displays). Empty
// label → NULL column; execute-endpoint rows keep deriving their agent
// from the endpoint path.
func (s *Server) logGatewayRequestLabeled(
	r *http.Request,
	endpoint string,
	agentLabel string,
	status int,
	requestBytes int,
	responseBytes int,
	executionID string,
	startedAt time.Time,
) {
	// backai_runs_total intentionally uses this hook because /api/v1/runs
	// is also sourced from suite_gateway_requests filtered to agent execute
	// endpoints. This keeps the metric and dashboard on one terminal source.
	if strings.HasPrefix(endpoint, "/api/v1/execute/") {
		metricStatus := "failed"
		if status >= 200 && status < 400 {
			metricStatus = "succeeded"
		}
		appmetrics.ObserveRun(agentFromEndpoint(endpoint), metricStatus)
	}

	if s.db == nil || s.db.Pool == nil {
		return
	}
	durationMS := int(time.Since(startedAt).Milliseconds())

	// Use a fresh, short-lived context so a slow write doesn't tie up the
	// response goroutine.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var execIDPtr *string
	if executionID != "" {
		execIDPtr = &executionID
	}
	tenantID := tenantctx.TenantID(r.Context())
	if tenantID == "" {
		// Untenanted runtime calls — the operator console, or customer-app
		// agent calls proxied before tenant resolution — record under the
		// platform's zero-UUID tenant. suite_gateway_requests.tenant_id is
		// NOT NULL (added by the RLS migration) with this value as its
		// DEFAULT; passing an explicit NULL violated the constraint and
		// silently dropped every such run, so /api/v1/runs stayed empty.
		// Mirrors the audit logger's behaviour (internal/audit/audit.go).
		tenantID = defaultTenantUUID
	}
	apiKeyID := tenantctx.APIKeyID(r.Context())
	var keyPtr *string
	if apiKeyID != "" {
		keyPtr = &apiKeyID
	}
	// Bind the fresh write context to the row's tenant so the pool's
	// PrepareConn hook sets app.tenant_id and the RLS WITH CHECK on
	// suite_gateway_requests passes — the background context above carries no
	// tenant of its own.
	ctx = tenantctx.WithTenant(ctx, tenantID, apiKeyID)
	var labelPtr *string
	if agentLabel != "" {
		labelPtr = &agentLabel
	}
	_, err := s.db.Pool.Exec(ctx, `
        insert into suite_gateway_requests
            (tenant_id, api_key_id, endpoint, method, status_code, duration_ms,
             request_bytes, response_bytes, af_execution_id, user_agent, agent_label)
        values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
    `,
		tenantID,
		keyPtr,
		endpoint,
		r.Method,
		status,
		durationMS,
		requestBytes,
		responseBytes,
		execIDPtr,
		r.Header.Get("User-Agent"),
		labelPtr,
	)
	if err != nil {
		s.log.Warn("failed to log gateway request", "endpoint", endpoint, "error", err)
	}
}

// handleGetExecution proxies execution status from AF.
func (s *Server) handleGetExecution(w http.ResponseWriter, r *http.Request) {
	if s.af == nil {
		writeJSON(w, http.StatusServiceUnavailable, errEnvelope("AGENTFIELD_NOT_CONFIGURED", "agentfield client not configured"))
		return
	}
	id := r.PathValue("id")
	resp, err := s.af.GetExecution(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errEnvelope("AGENTFIELD_UNREACHABLE", err.Error()))
		return
	}
	if resp.ContentType != "" {
		w.Header().Set("Content-Type", resp.ContentType)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(resp.Body)
}

// handleCancelExecution proxies a cancel to AF.
func (s *Server) handleCancelExecution(w http.ResponseWriter, r *http.Request) {
	if s.af == nil {
		writeJSON(w, http.StatusServiceUnavailable, errEnvelope("AGENTFIELD_NOT_CONFIGURED", "agentfield client not configured"))
		return
	}
	id := r.PathValue("id")
	resp, err := s.af.CancelExecution(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errEnvelope("AGENTFIELD_UNREACHABLE", err.Error()))
		return
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(resp.Body)
}

// validAgentName checks `ns.func` shape. Disallow paths that try to traverse
// outside AF's execute namespace.
func validAgentName(name string) bool {
	if name == "" || len(name) > 256 {
		return false
	}
	for _, ch := range name {
		switch {
		case ch >= 'a' && ch <= 'z':
		case ch >= 'A' && ch <= 'Z':
		case ch >= '0' && ch <= '9':
		case ch == '_' || ch == '-' || ch == '.':
		default:
			return false
		}
	}
	return true
}

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	if s.af == nil {
		writeError(w, http.StatusServiceUnavailable,
			"AGENTFIELD_NOT_CONFIGURED", "agentfield client not configured", nil)
		return
	}
	agents, err := s.af.Discover(r.Context())
	if err != nil {
		s.log.Error("agentfield discover failed", "error", err)
		writeError(w, http.StatusBadGateway,
			"AGENTFIELD_UNREACHABLE", "agentfield unreachable: "+err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": agents})
}

// writeReadyError emits a 503 response that is friendly to BOTH
// Kubernetes-style readiness probes AND the SDK error-envelope
// contract:
//
//	{
//	  "status":  "draining" | "booting" | "db_unavailable",
//	  "since_s": N,                  // top-level so probe parsers don't dig
//	  ...detailFields...             // any extra context the caller passed
//	  "error": {                     // Phase 6 error envelope for SDKs
//	    "code":    "NOT_READY",
//	    "message": "<human reason>",
//	    "details": { "reason": status, ...detailFields }
//	  }
//	}
//
// This is the only handler that mixes the two shapes — every other
// 503 in the runtime uses writeError() directly. /ready is special
// because Kubernetes probes don't speak our envelope and operators
// reading kubectl describe pod want the reason at the top.
func writeReadyError(w http.ResponseWriter, reason, message string, details map[string]any) {
	body := map[string]any{
		"status": reason,
	}
	for k, v := range details {
		body[k] = v
	}
	envelopeDetails := map[string]any{"reason": reason}
	for k, v := range details {
		envelopeDetails[k] = v
	}
	body["error"] = map[string]any{
		"code":    "NOT_READY",
		"message": message,
		"details": envelopeDetails,
	}
	writeJSON(w, http.StatusServiceUnavailable, body)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// withCORS allows the dashboard (and other in-stack browsers) to call the
// runtime's REST API from the operator's machine.
//
// Security: credentialed cross-origin requests are restricted to an
// explicit allowlist. Without this gate, any website the operator
// visits could fire authenticated requests against the runtime using
// their better-auth session cookie — classic CSRF. We allowlist:
//
//   - http://localhost:<DASHBOARD_PORT> and http://127.0.0.1:<DASHBOARD_PORT>
//     for the bundled dev experience (DASHBOARD_PORT defaults to 3000)
//   - any comma-separated origin in AF_STACK_CORS_ORIGINS
//
// For non-allowlisted origins we still echo the Origin header so simple
// (non-credentialed) GETs work — but we do NOT set
// Access-Control-Allow-Credentials, so the browser drops cookies and
// the request is effectively anonymous.
func withCORS(next http.Handler) http.Handler {
	allow := loadCORSAllowlist()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			if _, ok := allow[origin]; ok {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			w.Header().Set("Access-Control-Allow-Methods",
				"GET,POST,PUT,DELETE,OPTIONS,PATCH")
			w.Header().Set("Access-Control-Allow-Headers",
				"Content-Type,Authorization,X-Request-ID,traceparent,X-AF-Stack-Tenant-ID")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// loadCORSAllowlist builds the set of origins that may make
// credentialed cross-origin requests. Always includes the bundled dev
// dashboard on localhost + 127.0.0.1 (default port 3000, overridable
// via DASHBOARD_PORT). Additional origins come from a comma-separated
// AF_STACK_CORS_ORIGINS env var.
func loadCORSAllowlist() map[string]struct{} {
	out := map[string]struct{}{}
	port := strings.TrimSpace(os.Getenv("DASHBOARD_PORT"))
	if port == "" {
		port = "3000"
	}
	for _, host := range []string{"localhost", "127.0.0.1"} {
		out["http://"+host+":"+port] = struct{}{}
		out["https://"+host+":"+port] = struct{}{}
	}
	raw := strings.TrimSpace(os.Getenv("AF_STACK_CORS_ORIGINS"))
	if raw == "" {
		return out
	}
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out[p] = struct{}{}
		}
	}
	return out
}

// withLogging is a tiny structured-log middleware. OTel tracing comes in 1.7.
//
// Also feeds the metrics ring buffer so /api/v1/metrics/summary has a
// real p95 + per-route counters to serve. The ring is owned by the
// Server; nil-safety lives inside metricsRing.observe.
func withLogging(log *slog.Logger, ring *metricsRing, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// Correlation id: prefer the W3C traceparent trace-id the caller
		// sent (so every call a client makes under one traceparent shares
		// a trace_id on the Logs page), else a fresh id. The ring logger's
		// populateLineField routes a "trace_id" attr into the log line's
		// trace column, which the dashboard Logs view surfaces + filters.
		traceID := requestTraceID(r)
		// Expose the correlation id on every response (success and error)
		// and stash it on the writer so writeError can stamp it into the
		// envelope's error.request_id — the id AGENTS.md promises on every
		// failure. Set before the handler runs so it lands ahead of the
		// first WriteHeader.
		w.Header().Set("X-Request-ID", traceID)
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK, requestID: traceID}
		next.ServeHTTP(sw, r)
		dur := time.Since(start)
		log.Info("http.request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration_ms", dur.Milliseconds(),
			"trace_id", traceID,
		)
		ring.observe(r.URL.Path, dur.Milliseconds(), sw.status)
	})
}

// requestTraceID returns a correlation id for the request. It extracts the
// 32-hex trace-id from a W3C `traceparent` header
// (00-<trace>-<span>-<flags>) when present + well-formed, so a client that
// stamps one traceparent across a burst of calls sees them grouped on the
// Logs page. Falls back to a random 16-hex id.
func requestTraceID(r *http.Request) string {
	if tp := strings.TrimSpace(r.Header.Get("traceparent")); tp != "" {
		parts := strings.Split(tp, "-")
		if len(parts) >= 2 && len(parts[1]) == 32 && isHex(parts[1]) {
			return parts[1]
		}
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return "unknown"
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

type statusWriter struct {
	http.ResponseWriter
	status int
	// requestID is the per-request correlation id (also emitted as the
	// X-Request-ID response header). writeError reads it to populate
	// error.request_id in the canonical envelope.
	requestID string
	// wrote records whether the response has been committed, so the
	// panic-recovery middleware knows not to write a second (superfluous)
	// header after a handler already started responding.
	wrote bool
	// captureBuf, when non-nil, tees written response bytes so the
	// idempotency middleware can persist the response for replay. Writes
	// still pass through to the client immediately (streaming is
	// unaffected). captureLimit caps the tee; on overflow captureOver is
	// set, the buffer is discarded, and teeing stops — the response is
	// then treated as too large to store. See idempotency_mw.go.
	captureBuf   *bytes.Buffer
	captureLimit int
	captureOver  bool
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.wrote = true
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	s.wrote = true
	if s.captureBuf != nil && !s.captureOver {
		if s.captureBuf.Len()+len(b) > s.captureLimit {
			// Response exceeds the replay cap — stop teeing and drop what
			// we buffered so oversized responses don't pin memory.
			s.captureOver = true
			s.captureBuf.Reset()
		} else {
			s.captureBuf.Write(b)
		}
	}
	return s.ResponseWriter.Write(b)
}

// beginCapture arms the response tee with the given byte cap. Called by
// the idempotency middleware only after it has claimed the key.
func (s *statusWriter) beginCapture(limit int) {
	s.captureBuf = new(bytes.Buffer)
	s.captureLimit = limit
	s.captureOver = false
}

// captured returns the teed response bytes and whether the response
// overflowed the cap (in which case the bytes are empty and the response
// must not be stored).
func (s *statusWriter) captured() (body []byte, overflowed bool) {
	if s.captureBuf == nil {
		return nil, s.captureOver
	}
	return s.captureBuf.Bytes(), s.captureOver
}

// withRecover converts an unrecovered handler panic into a logged error
// plus a clean 500 envelope. Without it, net/http's built-in per-request
// recovery writes the panic to the process-default stderr logger,
// bypassing slog and the log ring — so the Errors dashboard (which
// derives groups from error-level ring lines) never sees it. The
// error-level line carries a "stack" field so logfilter fingerprints
// panics by call site rather than by message.
func withRecover(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			log.Error("handler panic recovered",
				"method", r.Method,
				"path", r.URL.Path,
				"panic", fmt.Sprintf("%v", rec),
				"stack", string(debug.Stack()),
			)
			// Only send the envelope if the handler hadn't already
			// committed a response before panicking.
			if sw, ok := w.(*statusWriter); !ok || !sw.wrote {
				writeError(w, http.StatusInternalServerError, "INTERNAL",
					"internal server error", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Flush propagates to the underlying writer so SSE handlers
// (LLM streaming, future agent progress channels) still work behind
// this middleware. Standard Go middleware pattern; without it,
// the gateway's `w.(http.Flusher)` cast fails after the logging
// middleware wraps the writer.
func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack propagates WebSocket upgrades through the logging middleware.
func (s *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := s.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return h.Hijack()
}

// Unwrap lets the stdlib's ResponseController find the underlying
// hijacker / pusher / flusher implementations on Go 1.20+.
func (s *statusWriter) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}

// SPDX-License-Identifier: Apache-2.0

// Package server wires HTTP handlers and lifecycle for the suite runtime.
package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/agentfield"
	"github.com/Agent-Field/backai/services/runtime/internal/billing"
	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/cost"
	"github.com/Agent-Field/backai/services/runtime/internal/crons"
	"github.com/Agent-Field/backai/services/runtime/internal/db"
	"github.com/Agent-Field/backai/services/runtime/internal/dbstudio"
	"github.com/Agent-Field/backai/services/runtime/internal/harnesses"
	"github.com/Agent-Field/backai/services/runtime/internal/hooks"
	"github.com/Agent-Field/backai/services/runtime/internal/jobs"
	"github.com/Agent-Field/backai/services/runtime/internal/llmcache"
	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway"
	"github.com/Agent-Field/backai/services/runtime/internal/logger"
	"github.com/Agent-Field/backai/services/runtime/internal/mcp"
	"github.com/Agent-Field/backai/services/runtime/internal/memory"
	"github.com/Agent-Field/backai/services/runtime/internal/notifications"
	"github.com/Agent-Field/backai/services/runtime/internal/observability"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/ratelimit"
	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
	"github.com/Agent-Field/backai/services/runtime/internal/audit"
	"github.com/Agent-Field/backai/services/runtime/internal/secrets"
	"github.com/Agent-Field/backai/services/runtime/internal/skills"
	"github.com/Agent-Field/backai/services/runtime/internal/storage"
	"github.com/Agent-Field/backai/services/runtime/internal/tenancy"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
	"github.com/Agent-Field/backai/services/runtime/internal/webhooks"
)

// Server holds the HTTP server and shared dependencies.
type Server struct {
	cfg           config.Config
	log           *slog.Logger
	mux           *http.ServeMux
	srv           *http.Server
	health        *Health
	// drain is the Phase 14.3 graceful-shutdown controller. Always
	// constructed in New(); main.go calls drain.Start() from its
	// SIGTERM handler. The drain middleware (outermost in the chain)
	// shares it so probes can report draining state and new requests
	// are rejected once drain begins.
	drain *Drain
	// ready is set to true by MarkReady() once main.go has finished
	// startup (migrations applied, workers spawned). Until then
	// /ready returns 503 {"status":"booting"}.
	ready atomic.Bool
	db            *db.DB
	af            *agentfield.Client
	tel           *observability.Telemetry
	hooks         *hooks.Engine
	storage       storage.Storage
	storagePrefix string // tenant key-prefix when multi-tenancy is on; "" otherwise
	secrets       *secrets.Vault
	jobs          *jobs.Manager
	tenancy       *tenancy.Manager
	limiter       ratelimit.Limiter
	openapi       *openapi.Builder
	llmCache      *llmcache.Cache
	llmGateway    *llmgateway.Gateway
	// memory is the Phase 8.2 suite memory store. nil when no DB is
	// wired; the /api/v1/memory/* endpoints return 503 in that case.
	memory *memory.Store
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
	// sandbox powers /api/v1/sandbox/*. nil = endpoints either return
	// 503 SANDBOX_NOT_CONFIGURED (mutating) or tolerant empty
	// responses (reads).
	sandbox *sandbox.Service
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
	// metricsRing collects HTTP request durations + per-route counters
	// for the /api/v1/metrics/summary handler. Always constructed in
	// New().
	metricsRing *metricsRing
	// logRing is the in-memory log buffer the /api/v1/logs endpoint
	// reads from. nil ⇒ the endpoint serves an empty list. main.go
	// constructs one at boot and tees the structured logger into it.
	logRing *logger.Ring
	// version is the runtime version label surfaced in
	// /api/v1/metrics/summary. Defaults to "dev" when empty.
	version string
	// audit writes append-only rows into suite_audit_log for every
	// admin mutation. Always non-nil; falls back to no-op when no DB
	// pool is wired.
	audit *audit.Writer
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
	// RateLimiter throttles per-tenant traffic at the public gateway.
	// nil means no throttling (every request passes through). The
	// constructor in main.go always wires an InMemory limiter; tests
	// pass nil to opt out.
	RateLimiter ratelimit.Limiter
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
	// Memory is the Phase 8.2 suite memory store (pgvector-backed
	// key/value with optional embeddings). nil means /api/v1/memory/*
	// endpoints return 503 MEMORY_NOT_CONFIGURED.
	Memory *memory.Store
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
	// Sandbox is the Phase 9.1 sandbox runs service. nil = the
	// /api/v1/sandbox/* endpoints either 503 (mutating) or return
	// tolerant empty responses (reads).
	Sandbox *sandbox.Service
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
	// LogRing is the in-memory log buffer the /api/v1/logs endpoint
	// reads from. Constructed in main.go and shared with the logger.
	LogRing *logger.Ring
	// Version is the runtime version string surfaced via
	// /api/v1/metrics/summary. Defaults to "dev".
	Version string
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
		builder.AddTag("memory", "Suite memory (pgvector-backed key/value)")
		builder.AddTag("db", "Database studio (introspection + SQL runner)")
		builder.AddTag("system", "Health, readiness, metrics")
	}
	s := &Server{
		cfg:           cfg,
		log:           log,
		mux:           mux,
		health:        &Health{StartedAt: time.Now()},
		drain:         NewDrain(cfg.Server.ShutdownTimeout),
		db:            deps.DB,
		af:            deps.AF,
		tel:           deps.Telemetry,
		hooks:         deps.Hooks,
		storage:       deps.Storage,
		storagePrefix: deps.StoragePrefix,
		secrets:       deps.Secrets,
		jobs:          deps.Jobs,
		tenancy:       deps.Tenancy,
		limiter:       deps.RateLimiter,
		openapi:       builder,
		llmCache:      deps.LLMCache,
		llmGateway:    deps.LLMGateway,
		memory:        deps.Memory,
		dbStudio:      deps.DBStudio,
		costAgg:       deps.CostAggregate,
		budgets:       deps.Budgets,
		sandbox:       deps.Sandbox,
		billing:       deps.Billing,
		notifications:   deps.Notifications,
		webhooks:        deps.Webhooks,
		skills:          deps.Skills,
		skillsInstaller: deps.SkillsInstaller,
		harnesses:       deps.Harnesses,
		mcp:             deps.MCP,
		mcpStore:        deps.MCPStore,
		crons:           deps.Crons,
		metricsRing:     newMetricsRing(MetricsRingSize),
		logRing:         deps.LogRing,
		version:         deps.Version,
	}
	if deps.DB != nil {
		s.audit = audit.New(deps.DB.Pool, log)
	} else {
		s.audit = audit.New(nil, log)
	}
	s.registerRoutes()

	// Wrap mux with OTel tracing first, then structured logging on the outside
	// so logs include final status code. CORS wraps last so OPTIONS preflights
	// short-circuit before hitting the routes.
	handler := http.Handler(mux)
	if s.limiter != nil {
		// Rate-limit middleware sits BETWEEN tracing and the mux so the
		// 429 response is recorded as a span but doesn't consume an
		// agent-execute slot.
		handler = withRateLimit(s.limiter, log)(handler)
	}
	// Tenant resolver wraps both rate-limit and the mux so the resolved
	// tenant id is available to both the limiter (per-tenant buckets)
	// and downstream handlers. Public paths (/health, /openapi.json,
	// dashboard reads) bypass internally — see isPublicPath.
	handler = s.tenantResolver(handler)
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
	// Phase 4: no admin auth gating yet — Phase 6 wires that.
	s.mux.HandleFunc("GET /api/v1/runs", s.handleListRuns)
	s.openapi.Register("GET", "/api/v1/runs", openapi.RouteMeta{
		Summary: "List recent agent runs", Tags: []string{"dashboard"},
	})
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
	s.mux.HandleFunc("GET /api/v1/queues/summary", s.handleQueueSummary)
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

	// DB studio (Phase 8.1). Endpoints return 503 when no
	// dbstudio.Studio is wired (no DB at boot).
	s.registerDBStudioRoutes()
	s.registerDBStudioOpenAPI()

	// Multi-tenancy admin (Phase 6). Endpoints return 503 when the
	// multi-tenancy module is disabled or when no tenancy.Manager is
	// wired (the latter is Phase 6.1's responsibility).
	s.registerAdminRoutes()
	s.registerAdminOpenAPI()

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
	s.registerWebhooksOpenAPI()

	// Billing (Phase 10.4). Read endpoints serve empty / synthesised
	// rows when no service is wired; the portal-link mutation returns
	// 503 BILLING_NOT_CONFIGURED. Also registers the Stripe webhook
	// receiver at /webhooks/in/stripe — see billing.go file-level
	// comment for the Phase 10.2 coordination note.
	s.registerBillingRoutes()
	s.registerBillingOpenAPI()

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

	// Metrics summary (Phase 12.2). Always wired — runtime + ring data
	// is available even without a DB.
	s.registerMetricsRoutes()
	s.registerMetricsOpenAPI()

	// Logs (Phase 12.2). When no ring is wired the endpoint serves an
	// empty array; otherwise returns the most-recent buffered lines.
	s.registerLogsRoutes()
	s.registerLogsOpenAPI()

	if s.tel != nil {
		s.mux.Handle("GET /metrics", s.tel.MetricsHandler())
		s.openapi.Register("GET", "/metrics", openapi.RouteMeta{
			Summary: "Prometheus scrape endpoint", Tags: []string{"system"},
		})
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
	apiKeyID := tenantctx.APIKeyID(r.Context())
	var tenantPtr, keyPtr *string
	if tenantID != "" {
		tenantPtr = &tenantID
	}
	if apiKeyID != "" {
		keyPtr = &apiKeyID
	}
	_, err := s.db.Pool.Exec(ctx, `
        insert into suite_gateway_requests
            (tenant_id, api_key_id, endpoint, method, status_code, duration_ms,
             request_bytes, response_bytes, af_execution_id, user_agent)
        values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
    `,
		tenantPtr,
		keyPtr,
		endpoint,
		r.Method,
		status,
		durationMS,
		requestBytes,
		responseBytes,
		execIDPtr,
		r.Header.Get("User-Agent"),
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

// rateLimitedRoutes is the list of URL-prefix matchers the rate-limit
// middleware applies to. Anything not matching here passes through
// untouched (e.g. /health, /openapi.json, dashboard reads).
//
// Decision: match on path PREFIX, not exact route. ServeMux's pattern
// table is keyed on the templated path so a fully precise lookup would
// require re-parsing; prefixes are good enough for throttling and keep
// the middleware oblivious to the route table.
var rateLimitedRoutes = []string{
	"/api/v1/agents/",        // every agent invocation (sync + async)
	"/api/v1/llm/",           // future LLM gateway routes
	"/api/v1/storage/upload", // multipart uploads only
}

// routeKey returns the bucket label for a request. The limiter key is
// (tenantID, route) — using a coarse label here means every agent call
// shares one bucket per tenant, which is what we want for v1. When LLM
// metering arrives, switch to per-endpoint keys.
//
// An empty return means "not throttled".
func routeKey(path string) string {
	switch {
	case strings.HasPrefix(path, "/api/v1/agents/"):
		return "agents"
	case strings.HasPrefix(path, "/api/v1/llm/"):
		return "llm"
	case strings.HasPrefix(path, "/api/v1/storage/upload"):
		return "storage.upload"
	default:
		_ = rateLimitedRoutes // keep var alive for readers who grep
		return ""
	}
}

// withRateLimit is the per-tenant throttle. Placed AFTER the tenant
// resolver (Phase 6.1) so tenantctx.TenantID(ctx) returns a real id;
// when the resolver isn't wired yet, Allow() falls back to a shared
// bucket keyed on the empty tenant id (still throttles abusive
// unauthenticated callers).
//
// A 429 response:
//   - sets Retry-After (seconds, rounded up; minimum 1)
//   - emits the standard error envelope with code RATE_LIMITED + a
//     details.retry_after_seconds field for clients that prefer JSON
//   - never logs the request body — only path / method / tenant id.
func withRateLimit(lim ratelimit.Limiter, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route := routeKey(r.URL.Path)
			if route == "" {
				next.ServeHTTP(w, r)
				return
			}
			tenantID := tenantctx.TenantID(r.Context())
			allowed, retryAfter, err := lim.Allow(r.Context(), tenantID, route)
			if err != nil {
				// Fail-open: a Limiter that returns an err (Redis blip)
				// shouldn't take down the gateway. Log + admit.
				log.Warn("rate-limit lookup failed; admitting request",
					"tenant", tenantID, "route", route, "error", err)
				next.ServeHTTP(w, r)
				return
			}
			if allowed {
				next.ServeHTTP(w, r)
				return
			}
			// Round retryAfter up to the nearest second; floor at 1 so
			// clients never busy-loop on a 0-second hint.
			secs := int(retryAfter / time.Second)
			if retryAfter%time.Second != 0 {
				secs++
			}
			if secs < 1 {
				secs = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			writeError(w, http.StatusTooManyRequests,
				"RATE_LIMITED",
				"request exceeded per-tenant rate limit",
				map[string]any{"retry_after_seconds": secs},
			)
		})
	}
}

// withLogging is a tiny structured-log middleware. OTel tracing comes in 1.7.
//
// Also feeds the metrics ring buffer so /api/v1/metrics/summary has a
// real p95 + per-route counters to serve. The ring is owned by the
// Server; nil-safety lives inside metricsRing.observe.
func withLogging(log *slog.Logger, ring *metricsRing, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		dur := time.Since(start)
		log.Info("http.request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration_ms", dur.Milliseconds(),
		)
		ring.observe(r.URL.Path, dur.Milliseconds(), sw.status)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
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

// Unwrap lets the stdlib's ResponseController find the underlying
// hijacker / pusher / flusher implementations on Go 1.20+.
func (s *statusWriter) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}
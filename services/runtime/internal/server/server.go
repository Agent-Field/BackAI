// Package server wires HTTP handlers and lifecycle for the suite runtime.
package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/agentfield"
	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/cost"
	"github.com/Agent-Field/backai/services/runtime/internal/db"
	"github.com/Agent-Field/backai/services/runtime/internal/hooks"
	"github.com/Agent-Field/backai/services/runtime/internal/jobs"
	"github.com/Agent-Field/backai/services/runtime/internal/llmcache"
	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway"
	"github.com/Agent-Field/backai/services/runtime/internal/observability"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/ratelimit"
	"github.com/Agent-Field/backai/services/runtime/internal/secrets"
	"github.com/Agent-Field/backai/services/runtime/internal/storage"
	"github.com/Agent-Field/backai/services/runtime/internal/tenancy"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// Server holds the HTTP server and shared dependencies.
type Server struct {
	cfg           config.Config
	log           *slog.Logger
	mux           *http.ServeMux
	srv           *http.Server
	health        *Health
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
	// costAgg powers /api/v1/cost + /api/v1/cost/events. nil when no
	// DB is wired; handlers return empty/zero responses.
	costAgg *cost.Aggregate
	// budgets powers /api/v1/admin/budgets (Get/Set/List) and is the
	// authority for the cost summary's BudgetUSD field. nil disables
	// budget endpoints (they return 503).
	budgets *cost.Budgets
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
	// CostAggregate powers the dashboard's /api/v1/cost summary +
	// /api/v1/cost/events list. nil = the endpoints return empty
	// zeros (boot mode without DB).
	CostAggregate *cost.Aggregate
	// Budgets is the per-tenant monthly budget store. nil disables
	// the /api/v1/admin/budgets endpoints (which return 503).
	Budgets *cost.Budgets
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
		builder.AddTag("system", "Health, readiness, metrics")
	}
	s := &Server{
		cfg:           cfg,
		log:           log,
		mux:           mux,
		health:        &Health{StartedAt: time.Now()},
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
		costAgg:       deps.CostAggregate,
		budgets:       deps.Budgets,
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
	handler = withLogging(log, handler)
	handler = withCORS(handler)

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

	if s.tel != nil {
		s.mux.Handle("GET /metrics", s.tel.MetricsHandler())
		s.openapi.Register("GET", "/metrics", openapi.RouteMeta{
			Summary: "Prometheus scrape endpoint", Tags: []string{"system"},
		})
	}
}

// Start runs the HTTP server. Blocks until ctx is cancelled, then drains
// in-flight requests up to the configured shutdown timeout.
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.Server.ShutdownTimeout)
		defer cancel()
		if err := s.srv.Shutdown(shutdownCtx); err != nil {
			s.log.Error("shutdown failed", "error", err)
			return err
		}
		s.log.Info("shutdown complete")
		return nil
	case err := <-errCh:
		return err
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	checks := map[string]any{
		"runtime": map[string]any{"status": "ok"},
	}
	overall := "ok"

	if s.db != nil {
		if err := s.db.Health(r.Context()); err != nil {
			checks["database"] = map[string]any{"status": "down", "error": err.Error()}
			overall = "degraded"
		} else {
			checks["database"] = map[string]any{"status": "ok"}
		}
	} else {
		checks["database"] = map[string]any{"status": "not_configured"}
	}

	if s.af != nil {
		if _, err := s.af.Health(r.Context()); err != nil {
			checks["agentfield"] = map[string]any{
				"status": "down",
				"url":    s.af.BaseURL(),
				"error":  err.Error(),
			}
			overall = "degraded"
		} else {
			checks["agentfield"] = map[string]any{
				"status": "ok",
				"url":    s.af.BaseURL(),
			}
		}
	} else {
		checks["agentfield"] = map[string]any{"status": "not_configured"}
	}

	resp := map[string]any{
		"status":     overall,
		"started_at": s.health.StartedAt.UTC().Format(time.RFC3339),
		"uptime_s":   int(time.Since(s.health.StartedAt).Seconds()),
		"checks":     checks,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	// Ready = DB connected AND AF healthy (when both configured).
	//
	// 503 responses use the standard error envelope (Phase 6 contract)
	// so every non-2xx body from the runtime is machine-parsable by the
	// same {error: {code, message, details?}} shape the dashboard +
	// SDKs already branch on.
	if s.db != nil {
		if err := s.db.Health(r.Context()); err != nil {
			writeError(w, http.StatusServiceUnavailable,
				"NOT_READY", "database unhealthy",
				map[string]any{"component": "database"})
			return
		}
	}
	if s.af != nil {
		if _, err := s.af.Health(r.Context()); err != nil {
			writeError(w, http.StatusServiceUnavailable,
				"NOT_READY", "agentfield unhealthy",
				map[string]any{"component": "agentfield"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// withCORS allows the dashboard (and other in-stack browsers) to call the
// runtime's REST API from the operator's machine. Phase 6 narrows the
// allow-list per tenant; for now any origin is accepted since auth is
// per-cookie at the dashboard layer.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
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
func withLogging(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		log.Info("http.request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
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

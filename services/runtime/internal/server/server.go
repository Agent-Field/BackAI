// Package server wires HTTP handlers and lifecycle for the suite runtime.
package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/agentfield"
	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/db"
	"github.com/Agent-Field/backai/services/runtime/internal/observability"
)

// Server holds the HTTP server and shared dependencies.
type Server struct {
	cfg    config.Config
	log    *slog.Logger
	mux    *http.ServeMux
	srv    *http.Server
	health *Health
	db     *db.DB
	af     *agentfield.Client
	tel    *observability.Telemetry
}

// Health aggregates dependency health for the /health endpoint.
type Health struct {
	StartedAt time.Time
}

// Deps groups the runtime dependencies the server uses. nil entries are
// gracefully tolerated (the health check reports them as not-configured).
type Deps struct {
	DB        *db.DB
	AF        *agentfield.Client
	Telemetry *observability.Telemetry
}

// New constructs a Server with the given config + logger + dependencies.
func New(cfg config.Config, log *slog.Logger, deps Deps) *Server {
	mux := http.NewServeMux()
	s := &Server{
		cfg:    cfg,
		log:    log,
		mux:    mux,
		health: &Health{StartedAt: time.Now()},
		db:     deps.DB,
		af:     deps.AF,
		tel:    deps.Telemetry,
	}
	s.registerRoutes()

	// Wrap mux with OTel tracing first, then structured logging on the outside
	// so logs include final status code.
	handler := http.Handler(mux)
	if deps.Telemetry != nil {
		handler = observability.TraceMiddleware(cfg.Observability.ServiceName)(handler)
	}
	handler = withLogging(log, handler)

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
	s.mux.HandleFunc("GET /ready", s.handleReady)
	s.mux.HandleFunc("GET /openapi.json", s.handleOpenAPI)
	s.mux.HandleFunc("GET /api/v1/agents", s.handleListAgents)

	// Agent invocation — the central forwarding endpoints. Every LLM call
	// in the suite eventually routes through these.
	s.mux.HandleFunc("POST /api/v1/agents/{call}", s.handleAgentCall)
	s.mux.HandleFunc("POST /api/v1/agents/async/{call}", s.handleAgentCallAsync)
	s.mux.HandleFunc("GET /api/v1/executions/{id}", s.handleGetExecution)
	s.mux.HandleFunc("DELETE /api/v1/executions/{id}", s.handleCancelExecution)

	// Dashboard read-only endpoints (see internal/server/dashboard.go).
	// Phase 4: no admin auth gating yet — Phase 6 wires that.
	s.mux.HandleFunc("GET /api/v1/runs", s.handleListRuns)
	s.mux.HandleFunc("GET /api/v1/home/overview", s.handleHomeOverview)
	s.mux.HandleFunc("GET /api/v1/cost", s.handleCostSummary)
	s.mux.HandleFunc("GET /api/v1/modules", s.handleModulesState)
	s.mux.HandleFunc("GET /api/v1/queues/summary", s.handleQueueSummary)

	if s.tel != nil {
		s.mux.Handle("GET /metrics", s.tel.MetricsHandler())
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
	if s.db != nil {
		if err := s.db.Health(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "not_ready",
				"reason": "database unhealthy",
			})
			return
		}
	}
	if s.af != nil {
		if _, err := s.af.Health(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "not_ready",
				"reason": "agentfield unhealthy",
			})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	stub := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "AF Stack",
			"version": "0.0.1",
		},
		"paths": map[string]any{
			"/health": map[string]any{
				"get": map[string]any{"summary": "Service health"},
			},
			"/ready": map[string]any{
				"get": map[string]any{"summary": "Service readiness"},
			},
			"/api/v1/agents": map[string]any{
				"get": map[string]any{"summary": "Discover registered AgentField agents"},
			},
		},
	}
	writeJSON(w, http.StatusOK, stub)
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
	_, err := s.db.Pool.Exec(ctx, `
        insert into suite_gateway_requests
            (endpoint, method, status_code, duration_ms,
             request_bytes, response_bytes, af_execution_id, user_agent)
        values ($1, $2, $3, $4, $5, $6, $7, $8)
    `,
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

// errEnvelope is the standard AF Stack error envelope.
func errEnvelope(code, message string) map[string]any {
	return map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
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
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "agentfield not configured",
		})
		return
	}
	agents, err := s.af.Discover(r.Context())
	if err != nil {
		s.log.Error("agentfield discover failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "agentfield unreachable",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": agents})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
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

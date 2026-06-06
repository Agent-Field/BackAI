// Package server wires HTTP handlers and lifecycle for the suite runtime.
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/agentfield"
	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/db"
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
}

// Health aggregates dependency health for the /health endpoint.
type Health struct {
	StartedAt time.Time
}

// Deps groups the runtime dependencies the server uses. nil entries are
// gracefully tolerated (the health check reports them as not-configured).
type Deps struct {
	DB *db.DB
	AF *agentfield.Client
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
	}
	s.registerRoutes()
	s.srv = &http.Server{
		Addr:              cfg.Server.HTTPAddr,
		Handler:           withLogging(log, mux),
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

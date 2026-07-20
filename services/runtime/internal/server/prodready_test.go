// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/prodcheck"
)

func prodReadyServer(cfg config.Config) *Server {
	return &Server{cfg: cfg, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// When the production contract is not armed, /ready posture is a no-op and must
// never touch the DB (s.db is nil here — a DB access would panic).
func TestProductionReadyNoOpWhenNotArmed(t *testing.T) {
	cfg := config.Default() // saas, but Env empty -> not armed
	s := prodReadyServer(cfg)
	code, ok := s.productionReady(context.Background())
	if !ok || code != "" {
		t.Errorf("not-armed posture = (%q,%v), want (\"\",true)", code, ok)
	}

	// Personal mode + production env is also exempt.
	cfg2 := config.Default()
	cfg2.Mode = config.ModePersonal
	cfg2.Env = config.EnvProduction
	s2 := prodReadyServer(cfg2)
	if code, ok := s2.productionReady(context.Background()); !ok || code != "" {
		t.Errorf("personal+production posture = (%q,%v), want (\"\",true)", code, ok)
	}
}

// When armed but no DB is wired, the posture cannot be verified and the pod is
// not ready, surfacing the catalog-unavailable code.
func TestProductionReadyArmedWithoutDB(t *testing.T) {
	cfg := config.Default()
	cfg.Env = config.EnvProduction // saas + production -> armed
	s := prodReadyServer(cfg)
	code, ok := s.productionReady(context.Background())
	if ok {
		t.Fatal("expected not-ready when armed without a DB")
	}
	if code != prodcheck.CodeCatalogUnavailable {
		t.Errorf("code = %q, want %q", code, prodcheck.CodeCatalogUnavailable)
	}
}

// The posture result is cached: a second call within the TTL returns the cached
// code without re-evaluating (verified by mutating the cache under the lock).
func TestProductionReadyCachesResult(t *testing.T) {
	cfg := config.Default()
	cfg.Env = config.EnvProduction
	s := prodReadyServer(cfg)

	// Prime the cache with a fabricated clean result.
	s.prodReady.mu.Lock()
	s.prodReady.valid = true
	s.prodReady.at = time.Now()
	s.prodReady.code = ""
	s.prodReady.mu.Unlock()

	code, ok := s.productionReady(context.Background())
	if !ok || code != "" {
		t.Errorf("cached clean posture = (%q,%v), want (\"\",true)", code, ok)
	}

	// Expire the cache; now the real (DB-less) evaluation runs and fails.
	s.prodReady.mu.Lock()
	s.prodReady.at = time.Now().Add(-2 * prodReadyTTL)
	s.prodReady.mu.Unlock()
	if code, ok := s.productionReady(context.Background()); ok || code != prodcheck.CodeCatalogUnavailable {
		t.Errorf("post-expiry posture = (%q,%v), want (%q,false)", code, ok, prodcheck.CodeCatalogUnavailable)
	}
}

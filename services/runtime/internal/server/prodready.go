// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/prodcheck"
	"github.com/Agent-Field/backai/services/runtime/internal/secrets"
)

// prodReadyTTL bounds how often /ready re-evaluates the production posture
// (which queries pg_catalog). Readiness probes fire every few seconds; the
// posture only drifts on an operator DDL change, so a short cache keeps the hot
// path cheap while still catching drift within the window.
const prodReadyTTL = 30 * time.Second

// prodReadyCache memoises the last production-posture evaluation.
type prodReadyCache struct {
	mu    sync.Mutex
	code  string // "" means posture OK
	at    time.Time
	valid bool
}

// productionReady reports whether the runtime satisfies the R7 production
// operating contract. It returns (code, ok):
//
//   - ok=true, code="" when the contract is not armed (not saas+production) or
//     when every posture check passes.
//   - ok=false, code=<PRODCHECK_*> when a posture check fails; the code is the
//     first failing check's stable code.
//
// The evaluation is cached for prodReadyTTL. A transient catalog error is
// treated as posture-unknown → not ready (the boot preflight already proved the
// posture once, so a mid-flight failure to re-verify is a signal, not noise).
func (s *Server) productionReady(ctx context.Context) (string, bool) {
	if !s.cfg.ProductionHardening() {
		return "", true
	}

	s.prodReady.mu.Lock()
	if s.prodReady.valid && time.Since(s.prodReady.at) < prodReadyTTL {
		code := s.prodReady.code
		s.prodReady.mu.Unlock()
		return code, code == ""
	}
	s.prodReady.mu.Unlock()

	code := s.evaluateProductionPosture(ctx)

	s.prodReady.mu.Lock()
	s.prodReady.code = code
	s.prodReady.at = time.Now()
	s.prodReady.valid = true
	s.prodReady.mu.Unlock()

	return code, code == ""
}

// evaluateProductionPosture runs the full prodcheck evaluation against the live
// catalog + current config. Returns the first failing code, or "" when clean.
// A missing DB or catalog error yields the catalog-unavailable code.
func (s *Server) evaluateProductionPosture(ctx context.Context) string {
	if s.db == nil || s.db.Pool == nil {
		return prodcheck.CodeCatalogUnavailable
	}
	gatherCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	kmsConfigured := secrets.KMSConfigured()
	inputs, err := prodcheck.Gather(gatherCtx, s.db.Pool, prodcheck.ConfigInputs{
		CORSOrigins:          corsOriginsFromEnv(),
		KMSConfigured:        kmsConfigured,
		KMSDevMode:           !kmsConfigured,
		StorageConfigured:    s.storage != nil,
		MultiTenancyEnabled:  s.multiTenancyEnabled(),
		SandboxNetworkPolicy: s.cfg.SandboxNetworkPolicy(),
	})
	if err != nil {
		return prodcheck.CodeCatalogUnavailable
	}
	return prodcheck.Evaluate(inputs).FirstFailureCode()
}

// corsOriginsFromEnv parses AF_STACK_CORS_ORIGINS the same way loadCORSAllowlist
// does — the operator-supplied credentialed origins the posture check inspects.
func corsOriginsFromEnv() []string {
	raw := strings.TrimSpace(os.Getenv("AF_STACK_CORS_ORIGINS"))
	if raw == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

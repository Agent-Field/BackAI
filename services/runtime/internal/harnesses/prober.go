// SPDX-License-Identifier: Apache-2.0

// prober.go — aggregates harness availability from AgentField agents.
//
// Before the OSS-3 refactor this file probed the runtime's own
// container with exec.LookPath. That was wrong: the runtime is
// distroless, so the probe always returned "missing" and the dashboard
// looked broken. Harnesses are CLI tools (claude-code, codex, gemini,
// opencode) that the AF agents invoke via ``app.harness(...)`` — they
// belong in the agent container, not the runtime.
//
// The new model:
//
//  1. Each AF agent declares its harness availability at registration
//     via the ``__capabilities__`` reasoner (see
//     apps/backend/agents/sample/main.py).
//  2. The runtime calls ``Capabilities()`` on the AgentField client,
//     which fans out to every registered agent's __capabilities__
//     reasoner and collects the per-agent answers.
//  3. ``Service.aggregate()`` folds the per-agent answers into a single
//     row per provider — the same Harness shape the dashboard already
//     consumes, so the wire contract is unchanged.
//
// Aggregation rule (per provider, across N agents):
//   - "ready"      iff at least one agent reports ready
//   - "needs_auth" iff no agent reports ready, and at least one has the
//                  binary present but is missing env
//   - "errored"    iff no agent reports ready/needs_auth, and at least
//                  one agent reports the binary present but a version
//                  probe failed (agent-side)
//   - "missing"    iff no agent has the binary at all
//
// binary_path becomes the node_id of the agent the row came from
// (formatted as "agent:<node_id>"). The dashboard renders this so the
// operator can tell *which* agent has the tool ready — useful when a
// stack has multiple AF agents with different harness configurations.
package harnesses

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/Agent-Field/backai/services/runtime/internal/agentfield"
)

// CapabilitiesProvider abstracts the AF client method used to fetch
// per-agent capabilities. Kept as an interface so unit tests can stub
// without spinning up an httptest server. *agentfield.Client satisfies
// this via its Capabilities method.
type CapabilitiesProvider interface {
	Capabilities(ctx context.Context) ([]agentfield.AgentCapabilities, error)
}

// Service is the public surface the REST handlers + CLI consume. Safe
// for concurrent use; the mutex guards the cache map.
type Service struct {
	log   *slog.Logger
	src   CapabilitiesProvider
	mu    sync.RWMutex
	cache map[Provider]Harness
}

// NewService constructs a Service backed by an AF capabilities source.
// When src is nil the service degrades gracefully — every provider
// returns Missing — so the dashboard still renders during boot or when
// AF is unreachable. Call ListAll(ctx) once at startup to warm the
// cache.
func NewService(src CapabilitiesProvider, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		log:   log,
		src:   src,
		cache: make(map[Provider]Harness),
	}
}

// ListAll returns one Harness row per supported provider. Aggregates
// freshly from the AF capabilities source, replaces the cache, and
// returns the populated slice. Always returns len(AllProviders)
// entries — a missing harness is just a row with Status=Missing.
func (s *Service) ListAll(ctx context.Context) []Harness {
	caps := s.fetchCapabilities(ctx)
	out := make([]Harness, 0, len(AllProviders))
	for _, p := range AllProviders {
		h := s.aggregate(p, caps)
		s.mu.Lock()
		s.cache[p] = h
		s.mu.Unlock()
		out = append(out, h)
	}
	return out
}

// Get returns the cached probe for a single provider, fresh-aggregating
// if the cache is empty. Returns ErrUnknownProvider when p isn't one of
// the four supported harnesses.
func (s *Service) Get(ctx context.Context, p Provider) (Harness, error) {
	if !p.IsValid() {
		return Harness{}, fmt.Errorf("%w: %q", ErrUnknownProvider, string(p))
	}
	s.mu.RLock()
	h, ok := s.cache[p]
	s.mu.RUnlock()
	if ok {
		return h, nil
	}
	// First-touch: aggregate fresh and cache.
	caps := s.fetchCapabilities(ctx)
	row := s.aggregate(p, caps)
	s.mu.Lock()
	s.cache[p] = row
	s.mu.Unlock()
	return row, nil
}

// Probe forces a fresh aggregation across all agents and updates the
// per-provider cache. The dashboard's "Probe" button hits this;
// everything else reads from the cache.
func (s *Service) Probe(ctx context.Context, p Provider) (Harness, error) {
	if !p.IsValid() {
		return Harness{}, fmt.Errorf("%w: %q", ErrUnknownProvider, string(p))
	}
	caps := s.fetchCapabilities(ctx)
	row := s.aggregate(p, caps)
	s.mu.Lock()
	s.cache[p] = row
	s.mu.Unlock()
	return row, nil
}

// fetchCapabilities pulls the per-agent answers from AF. Errors are
// logged + treated as "no agents declared anything" — the dashboard
// still renders an honest Missing row for every provider rather than
// blanking out on a transient AF outage.
func (s *Service) fetchCapabilities(ctx context.Context) []agentfield.AgentCapabilities {
	if s.src == nil {
		return nil
	}
	caps, err := s.src.Capabilities(ctx)
	if err != nil {
		s.log.Warn("harnesses: capabilities fetch failed",
			"error", err)
		return nil
	}
	return caps
}

// aggregate folds the per-agent capability rows for one provider into a
// single Harness suitable for the dashboard. The aggregation rule is
// described at the top of the file. RequiredEnv is taken from the
// static registry (probes.Registry()) so we still have something to
// display even when zero agents reported the provider.
func (s *Service) aggregate(p Provider, caps []agentfield.AgentCapabilities) Harness {
	h := Harness{
		Provider:    p,
		IsInstalled: false,
		Status:      StatusMissing,
		RequiredEnv: []string{},
	}
	if pr, ok := providerProbe(string(p)); ok {
		if len(pr.RequiredEnv) > 0 {
			h.RequiredEnv = append([]string{}, pr.RequiredEnv...)
		}
		if pr.InstallDocURL != "" {
			doc := pr.InstallDocURL
			h.InstallDocURL = &doc
		}
	}

	// Collect candidates by status across all agents.
	var (
		readyAgent     string
		readyVersion   string
		needsAuthAgent string
		needsAuthErr   string
		erroredAgent   string
		erroredMsg     string
		installedAgent string
		installedVer   string
	)
	for _, ac := range caps {
		for _, ah := range ac.Harnesses {
			if ah.Provider != string(p) {
				continue
			}
			if ah.IsInstalled && installedAgent == "" {
				installedAgent = ac.NodeID
				if ah.Version != nil {
					installedVer = *ah.Version
				}
			}
			switch ah.Status {
			case "ready":
				if readyAgent == "" {
					readyAgent = ac.NodeID
					if ah.Version != nil {
						readyVersion = *ah.Version
					}
				}
			case "needs_auth":
				if needsAuthAgent == "" {
					needsAuthAgent = ac.NodeID
					if ah.LastError != nil {
						needsAuthErr = *ah.LastError
					}
				}
			case "errored":
				if erroredAgent == "" {
					erroredAgent = ac.NodeID
					if ah.LastError != nil {
						erroredMsg = *ah.LastError
					}
				}
			}
		}
	}

	switch {
	case readyAgent != "":
		h.IsInstalled = true
		h.Status = StatusReady
		bp := "agent:" + readyAgent
		h.BinaryPath = &bp
		if readyVersion != "" {
			v := readyVersion
			h.Version = &v
		}
	case needsAuthAgent != "":
		h.IsInstalled = true
		h.Status = StatusNeedsAuth
		bp := "agent:" + needsAuthAgent
		h.BinaryPath = &bp
		msg := needsAuthErr
		if msg == "" {
			msg = "required env not set: " + strings.Join(h.RequiredEnv, ", ")
		}
		h.LastError = &msg
		if installedVer != "" {
			v := installedVer
			h.Version = &v
		}
	case erroredAgent != "":
		h.IsInstalled = true
		h.Status = StatusErrored
		bp := "agent:" + erroredAgent
		h.BinaryPath = &bp
		if erroredMsg != "" {
			msg := erroredMsg
			h.LastError = &msg
		}
	case installedAgent != "":
		// Binary present but no recognised status — surface it as
		// errored with a generic message rather than lying with Ready.
		h.IsInstalled = true
		h.Status = StatusErrored
		bp := "agent:" + installedAgent
		h.BinaryPath = &bp
		msg := "agent reported binary present but no recognised status"
		h.LastError = &msg
	default:
		// No agent declared this provider at all. Add a helpful hint
		// for the operator pointing at the agent Dockerfile.
		if len(caps) > 0 {
			msg := fmt.Sprintf(
				"no registered agent declares %s — install in apps/backend/agents/<name>/Dockerfile",
				string(p),
			)
			h.LastError = &msg
		}
	}
	return h
}

// providerProbe returns the static probe definition for the given
// provider id, used for required_env + install_doc_url metadata. Lives
// here rather than as a direct import so the test suite can swap it
// out (the harnesses package owns the wire shape; probes is just
// reference data).
var providerProbe = func(id string) (struct {
	RequiredEnv   []string
	InstallDocURL string
}, bool) {
	type out = struct {
		RequiredEnv   []string
		InstallDocURL string
	}
	defaults := map[string]out{
		"claude-code": {RequiredEnv: []string{"ANTHROPIC_API_KEY"},
			InstallDocURL: "https://docs.claude.com/claude-code"},
		"codex":    {RequiredEnv: []string{"OPENAI_API_KEY"}, InstallDocURL: "https://github.com/openai/codex"},
		"gemini":   {RequiredEnv: []string{"GEMINI_API_KEY"}, InstallDocURL: "https://github.com/google-gemini/gemini-cli"},
		"opencode": {RequiredEnv: []string{}, InstallDocURL: "https://opencode.ai"},
	}
	v, ok := defaults[id]
	return v, ok
}

// ─── Errors ──────────────────────────────────────────────────────────────

// errFetchFailed is reserved for future use when callers want to
// distinguish "AF unreachable" from "AF returned no agents". Today the
// service masks transient failures behind a Missing row so the
// dashboard never blanks.
var errFetchFailed = errors.New("harnesses: capabilities fetch failed")

var _ = errFetchFailed // suppress unused-var until callers materialise

// SPDX-License-Identifier: Apache-2.0

// Tests cover the aggregator behaviour of Service across multiple
// agents. Each test stubs a CapabilitiesProvider with a hand-crafted
// set of per-agent answers and asserts the fold rule. The provider
// constants (claude-code, codex, etc.) and Status enum values match
// what apps/backend/agents/sample/main.py declares — drift those in
// lockstep with this file.
package harnesses

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/agentfield"
)

// stubProvider returns the given capabilities (or error) when called.
type stubProvider struct {
	caps []agentfield.AgentCapabilities
	err  error
}

func (s *stubProvider) Capabilities(_ context.Context) ([]agentfield.AgentCapabilities, error) {
	return s.caps, s.err
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ptr returns a pointer to s. Used for the *string fields in
// AgentCapabilities.
func ptr(s string) *string { return &s }

func TestProvider_IsValid(t *testing.T) {
	cases := []struct {
		in   Provider
		want bool
	}{
		{Claudecode, true},
		{Codex, true},
		{Gemini, true},
		{Opencode, true},
		{Provider("bogus"), false},
		{Provider(""), false},
	}
	for _, c := range cases {
		if got := c.in.IsValid(); got != c.want {
			t.Errorf("Provider(%q).IsValid() = %v, want %v", c.in, got, c.want)
		}
	}
}

// When no AF source is wired (e.g. AF unreachable at boot) every
// provider should render as Missing. The wire contract requires that
// ListAll always returns one row per supported provider so the
// dashboard's grid never blanks out.
func TestService_ListAll_NilSourceMissingByDefault(t *testing.T) {
	svc := NewService(nil, quietLogger())
	list := svc.ListAll(context.Background())
	if got, want := len(list), len(AllProviders); got != want {
		t.Fatalf("ListAll returned %d entries, want %d", got, want)
	}
	for _, h := range list {
		if h.Status != StatusMissing {
			t.Errorf("provider %s: status = %s, want %s",
				h.Provider, h.Status, StatusMissing)
		}
		if h.IsInstalled {
			t.Errorf("provider %s: is_installed = true, want false", h.Provider)
		}
		if h.RequiredEnv == nil {
			t.Errorf("provider %s: required_env nil; should be empty slice",
				h.Provider)
		}
	}
}

// One agent declares claude-code ready. Aggregator should return
// Ready, binary_path = "agent:sample", and the agent-reported version.
func TestService_Aggregate_ReadyFromOneAgent(t *testing.T) {
	src := &stubProvider{caps: []agentfield.AgentCapabilities{{
		NodeID: "sample",
		Harnesses: []agentfield.AgentHarness{{
			Provider:    "claude-code",
			IsInstalled: true,
			BinaryPath:  ptr("/usr/local/bin/claude"),
			Version:     ptr("claude 1.2.3"),
			Status:      "ready",
			RequiredEnv: []string{"ANTHROPIC_API_KEY"},
		}},
	}}}
	svc := NewService(src, quietLogger())
	h, err := svc.Probe(context.Background(), Claudecode)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if h.Status != StatusReady {
		t.Errorf("status = %s, want %s", h.Status, StatusReady)
	}
	if h.BinaryPath == nil || *h.BinaryPath != "agent:sample" {
		t.Errorf("binary_path = %v, want agent:sample", h.BinaryPath)
	}
	if h.Version == nil || *h.Version != "claude 1.2.3" {
		t.Errorf("version = %v, want claude 1.2.3", h.Version)
	}
	if !h.IsInstalled {
		t.Errorf("is_installed = false, want true")
	}
}

// One agent has the binary but no env key; aggregator returns
// NeedsAuth and surfaces the missing env in last_error.
func TestService_Aggregate_NeedsAuthOnly(t *testing.T) {
	src := &stubProvider{caps: []agentfield.AgentCapabilities{{
		NodeID: "sample",
		Harnesses: []agentfield.AgentHarness{{
			Provider:    "codex",
			IsInstalled: true,
			Status:      "needs_auth",
			LastError:   ptr("required env not set: OPENAI_API_KEY"),
			RequiredEnv: []string{"OPENAI_API_KEY"},
		}},
	}}}
	svc := NewService(src, quietLogger())
	h, err := svc.Probe(context.Background(), Codex)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if h.Status != StatusNeedsAuth {
		t.Errorf("status = %s, want %s", h.Status, StatusNeedsAuth)
	}
	if h.LastError == nil ||
		*h.LastError != "required env not set: OPENAI_API_KEY" {
		t.Errorf("last_error = %v, want surfaced from agent", h.LastError)
	}
}

// Two agents: one needs_auth, one ready. Ready wins (the runtime should
// route harness calls to the agent that can actually authenticate).
func TestService_Aggregate_ReadyWinsOverNeedsAuth(t *testing.T) {
	src := &stubProvider{caps: []agentfield.AgentCapabilities{
		{
			NodeID: "agent-a",
			Harnesses: []agentfield.AgentHarness{{
				Provider:    "claude-code",
				IsInstalled: true,
				Status:      "needs_auth",
				LastError:   ptr("required env not set: ANTHROPIC_API_KEY"),
				RequiredEnv: []string{"ANTHROPIC_API_KEY"},
			}},
		},
		{
			NodeID: "agent-b",
			Harnesses: []agentfield.AgentHarness{{
				Provider:    "claude-code",
				IsInstalled: true,
				Status:      "ready",
				Version:     ptr("claude 1.5.0"),
				RequiredEnv: []string{"ANTHROPIC_API_KEY"},
			}},
		},
	}}
	svc := NewService(src, quietLogger())
	h, err := svc.Probe(context.Background(), Claudecode)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if h.Status != StatusReady {
		t.Errorf("status = %s, want %s", h.Status, StatusReady)
	}
	if h.BinaryPath == nil || *h.BinaryPath != "agent:agent-b" {
		t.Errorf("binary_path = %v, want agent:agent-b", h.BinaryPath)
	}
}

// No agent declares the provider at all -> Missing with a helpful hint
// when at least one agent reported back.
func TestService_Aggregate_MissingWhenNoAgentDeclares(t *testing.T) {
	src := &stubProvider{caps: []agentfield.AgentCapabilities{{
		NodeID: "sample",
		Harnesses: []agentfield.AgentHarness{{
			Provider: "claude-code",
			Status:   "missing",
		}},
	}}}
	svc := NewService(src, quietLogger())
	h, err := svc.Probe(context.Background(), Gemini)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if h.Status != StatusMissing {
		t.Errorf("status = %s, want %s", h.Status, StatusMissing)
	}
	if h.LastError == nil {
		t.Errorf("last_error nil; expected install hint")
	}
}

// Get caches the first aggregation and returns it on subsequent calls.
// Probe forces a fresh aggregation that overwrites the cache.
func TestService_Get_UsesCacheAndProbeOverrides(t *testing.T) {
	src := &stubProvider{caps: []agentfield.AgentCapabilities{{
		NodeID: "sample",
		Harnesses: []agentfield.AgentHarness{{
			Provider: "opencode", Status: "ready", IsInstalled: true,
		}},
	}}}
	svc := NewService(src, quietLogger())
	first, err := svc.Get(context.Background(), Opencode)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if first.Status != StatusReady {
		t.Fatalf("first status = %s, want Ready", first.Status)
	}
	// Mutate the source to report missing — cache should still serve Ready.
	src.caps = []agentfield.AgentCapabilities{{
		NodeID: "sample",
		Harnesses: []agentfield.AgentHarness{{
			Provider: "opencode", Status: "missing",
		}},
	}}
	second, err := svc.Get(context.Background(), Opencode)
	if err != nil {
		t.Fatalf("Get cached: %v", err)
	}
	if second.Status != StatusReady {
		t.Errorf("cached status = %s, want Ready (cache should have served)",
			second.Status)
	}
	// Forced Probe re-aggregates -> now Missing.
	third, err := svc.Probe(context.Background(), Opencode)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if third.Status != StatusMissing {
		t.Errorf("forced probe status = %s, want Missing", third.Status)
	}
}

// AF unreachable -> Capabilities returns an error. The service must
// degrade gracefully: every provider renders Missing, no error
// bubbles up to the dashboard.
func TestService_Aggregate_AFErrorDegradesToMissing(t *testing.T) {
	src := &stubProvider{err: errors.New("agentfield unreachable")}
	svc := NewService(src, quietLogger())
	list := svc.ListAll(context.Background())
	if got, want := len(list), len(AllProviders); got != want {
		t.Fatalf("ListAll returned %d, want %d", got, want)
	}
	for _, h := range list {
		if h.Status != StatusMissing {
			t.Errorf("provider %s: status = %s, want Missing on AF error",
				h.Provider, h.Status)
		}
	}
}

// Unknown provider id is rejected by Get/Probe.
func TestService_UnknownProvider(t *testing.T) {
	svc := NewService(nil, quietLogger())
	if _, err := svc.Get(context.Background(), Provider("bogus")); err == nil {
		t.Errorf("Get(bogus) = nil error, want ErrUnknownProvider")
	}
	if _, err := svc.Probe(context.Background(), Provider("bogus")); err == nil {
		t.Errorf("Probe(bogus) = nil error, want ErrUnknownProvider")
	}
}

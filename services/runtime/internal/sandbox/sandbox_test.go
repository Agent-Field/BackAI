// SPDX-License-Identifier: Apache-2.0

// Unit tests for the sandbox package.
//
// The validation tests cover the RunSpec.Validate normalisation +
// bound-check path. The Service tests exercise the orchestration
// without a real adapter — a fakeAdapter satisfies sandbox.Sandbox and
// records what it received.
//
// Integration tests against a live Docker daemon live behind the
// `integration` build tag (see sandbox_integration_test.go).

package sandbox_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
)

// ─── RunSpec.Validate ─────────────────────────────────────────────────────

func TestValidate_AppliesDefaults(t *testing.T) {
	spec := sandbox.RunSpec{Image: "alpine", Command: []string{"echo", "hi"}}
	if err := spec.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if spec.TimeoutS != sandbox.DefaultTimeoutS {
		t.Errorf("TimeoutS default = %d, want %d", spec.TimeoutS, sandbox.DefaultTimeoutS)
	}
	if spec.CPU != sandbox.DefaultCPU {
		t.Errorf("CPU default = %d, want %d", spec.CPU, sandbox.DefaultCPU)
	}
	if spec.MemoryGB != sandbox.DefaultMemoryGB {
		t.Errorf("MemoryGB default = %d, want %d", spec.MemoryGB, sandbox.DefaultMemoryGB)
	}
	// Secure by default: an unspecified network is "isolated" (no egress),
	// not "open" — "open" can reach host-published services (incl. the
	// RLS-bypassing suite Postgres) and must be an explicit opt-in.
	if spec.Network != sandbox.NetworkIsolated {
		t.Errorf("Network default = %q, want %q", spec.Network, sandbox.NetworkIsolated)
	}
}

func TestValidate_OpenNetworkIsOptIn(t *testing.T) {
	spec := sandbox.RunSpec{Image: "alpine", Command: []string{"echo", "hi"}, Network: sandbox.NetworkOpen}
	if err := spec.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if spec.Network != sandbox.NetworkOpen {
		t.Errorf("explicit Network=open must be preserved, got %q", spec.Network)
	}
}

func TestValidate_RejectsBadImage(t *testing.T) {
	cases := []struct {
		name  string
		image string
	}{
		{"empty", ""},
		{"whitespace", "alpine latest"},
		{"shell metachar pipe", "alpine|nc"},
		{"shell metachar bang", "$alpine"},
		{"too long", strings.Repeat("a", sandbox.MaxImageLen+1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec := sandbox.RunSpec{Image: c.image, Command: []string{"ls"}}
			err := spec.Validate()
			if !errors.Is(err, sandbox.ErrInvalidSpec) {
				t.Errorf("expected ErrInvalidSpec, got %v", err)
			}
		})
	}
}

func TestValidate_RejectsCommandTooLong(t *testing.T) {
	huge := strings.Repeat("x", sandbox.MaxCommandLen+1)
	spec := sandbox.RunSpec{Image: "alpine", Command: []string{huge}}
	err := spec.Validate()
	if !errors.Is(err, sandbox.ErrInvalidSpec) {
		t.Errorf("expected ErrInvalidSpec, got %v", err)
	}
}

func TestValidate_RejectsEmptyCommand(t *testing.T) {
	spec := sandbox.RunSpec{Image: "alpine", Command: nil}
	err := spec.Validate()
	if !errors.Is(err, sandbox.ErrInvalidSpec) {
		t.Errorf("expected ErrInvalidSpec, got %v", err)
	}
}

func TestValidate_RejectsBadBounds(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*sandbox.RunSpec)
	}{
		{"timeout 0 ok (defaulted)", func(s *sandbox.RunSpec) { s.TimeoutS = 0 }},
		{"timeout negative", func(s *sandbox.RunSpec) { s.TimeoutS = -5 }},
		{"timeout too large", func(s *sandbox.RunSpec) { s.TimeoutS = sandbox.MaxTimeoutS + 1 }},
		{"cpu too large", func(s *sandbox.RunSpec) { s.CPU = sandbox.MaxCPU + 1 }},
		{"cpu negative", func(s *sandbox.RunSpec) { s.CPU = -1 }},
		{"memory too large", func(s *sandbox.RunSpec) { s.MemoryGB = sandbox.MaxMemoryGB + 1 }},
		{"memory negative", func(s *sandbox.RunSpec) { s.MemoryGB = -1 }},
		{"bad network", func(s *sandbox.RunSpec) { s.Network = "swarm" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec := sandbox.RunSpec{Image: "alpine", Command: []string{"ls"}}
			c.mut(&spec)
			err := spec.Validate()
			if c.name == "timeout 0 ok (defaulted)" {
				if err != nil {
					t.Errorf("expected nil (default applied), got %v", err)
				}
				return
			}
			if !errors.Is(err, sandbox.ErrInvalidSpec) {
				t.Errorf("expected ErrInvalidSpec, got %v", err)
			}
		})
	}
}

func TestValidate_AcceptsImplementedNetworkModes(t *testing.T) {
	for _, mode := range []sandbox.NetworkMode{
		sandbox.NetworkOpen, sandbox.NetworkIsolated,
	} {
		spec := sandbox.RunSpec{Image: "alpine", Command: []string{"ls"}, Network: mode}
		if err := spec.Validate(); err != nil {
			t.Errorf("network %q rejected: %v", mode, err)
		}
	}
}

// TestValidate_RejectsRestrictedNetwork locks the S4 contract: until per-host
// egress filtering exists, "restricted" is rejected rather than silently
// granting a full bridge.
func TestValidate_RejectsRestrictedNetwork(t *testing.T) {
	spec := sandbox.RunSpec{Image: "alpine", Command: []string{"ls"}, Network: sandbox.NetworkRestricted}
	if err := spec.Validate(); err == nil {
		t.Fatal("network=restricted accepted; want rejection until egress filtering is implemented")
	}
}

// ─── Status ──────────────────────────────────────────────────────────────

func TestStatusIsTerminal(t *testing.T) {
	terminal := []sandbox.Status{
		sandbox.StatusDone, sandbox.StatusFailed,
		sandbox.StatusTimeout, sandbox.StatusKilled,
	}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("status %q should be terminal", s)
		}
	}
	nonTerminal := []sandbox.Status{
		sandbox.StatusQueued, sandbox.StatusRunning,
	}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Errorf("status %q should not be terminal", s)
		}
	}
}

// ─── Service (with fakeAdapter) ──────────────────────────────────────────

// fakeAdapter is a tiny sandbox.Sandbox stand-in that returns a
// pre-built RunResult and records what it was called with.
type fakeAdapter struct {
	caps   sandbox.Capabilities
	result *sandbox.RunResult
	err    error
	calls  int
	lastID string
}

func (f *fakeAdapter) Run(ctx context.Context, spec sandbox.RunSpec) (*sandbox.RunResult, error) {
	f.calls++
	f.lastID = spec.ID
	return f.result, f.err
}
func (f *fakeAdapter) Stream(ctx context.Context, spec sandbox.RunSpec) (<-chan sandbox.LogLine, <-chan *sandbox.RunResult, error) {
	lines := make(chan sandbox.LogLine)
	results := make(chan *sandbox.RunResult, 1)
	close(lines)
	results <- f.result
	close(results)
	return lines, results, f.err
}
func (f *fakeAdapter) Stop(ctx context.Context, runID string) error { return nil }
func (f *fakeAdapter) Capabilities() sandbox.Capabilities           { return f.caps }

func TestService_RunNoDB(t *testing.T) {
	// No DB wired -> service synthesises an in-memory row and the
	// adapter result is applied.
	adapter := &fakeAdapter{
		caps: sandbox.Capabilities{Adapter: "fake", MaxTimeoutS: 100},
		result: &sandbox.RunResult{
			Status:    sandbox.StatusDone,
			ExitCode:  0,
			DurationS: 1,
			StartedAt: time.Now().UTC(),
			EndedAt:   time.Now().UTC(),
		},
	}
	svc := sandbox.NewService(adapter, nil, nil, nil)
	spec := sandbox.RunSpec{Image: "alpine", Command: []string{"echo", "hi"}}
	run, err := svc.Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if run == nil {
		t.Fatal("expected run, got nil")
	}
	if run.Status != sandbox.StatusDone {
		t.Errorf("status = %q, want done", run.Status)
	}
	if run.ExitCode == nil || *run.ExitCode != 0 {
		t.Errorf("exit_code = %v, want 0", run.ExitCode)
	}
	if adapter.calls != 1 {
		t.Errorf("adapter calls = %d, want 1", adapter.calls)
	}
}

func TestService_RunValidationError(t *testing.T) {
	adapter := &fakeAdapter{caps: sandbox.Capabilities{Adapter: "fake"}}
	svc := sandbox.NewService(adapter, nil, nil, nil)
	_, err := svc.Run(context.Background(), sandbox.RunSpec{Image: "", Command: nil})
	if !errors.Is(err, sandbox.ErrInvalidSpec) {
		t.Errorf("expected ErrInvalidSpec, got %v", err)
	}
	if adapter.calls != 0 {
		t.Errorf("adapter should not have been called; calls=%d", adapter.calls)
	}
}

func TestService_NotConfigured(t *testing.T) {
	svc := sandbox.NewService(nil, nil, nil, nil)
	if svc.AdapterAvailable() {
		t.Error("expected AdapterAvailable=false when adapter nil")
	}
	_, err := svc.Run(context.Background(), sandbox.RunSpec{Image: "alpine", Command: []string{"ls"}})
	if !errors.Is(err, sandbox.ErrNotConfigured) {
		t.Errorf("expected ErrNotConfigured, got %v", err)
	}
}

func TestService_PoolStatsNoDB(t *testing.T) {
	adapter := &fakeAdapter{caps: sandbox.Capabilities{Adapter: "fake", MaxTimeoutS: 100, ColdStartMS: 500}}
	svc := sandbox.NewService(adapter, nil, nil, nil)
	stats, err := svc.PoolStats(context.Background())
	if err != nil {
		t.Fatalf("PoolStats: %v", err)
	}
	if stats.Adapter != "fake" {
		t.Errorf("adapter label = %q, want fake", stats.Adapter)
	}
	if stats.Capabilities.MaxTimeoutS != 100 {
		t.Errorf("capabilities.MaxTimeoutS = %d, want 100", stats.Capabilities.MaxTimeoutS)
	}
}

func TestService_RunAdapterFailureWithRow(t *testing.T) {
	// Adapter returns a populated result + error -> we still get the
	// row back so the dashboard can render the failure.
	adapter := &fakeAdapter{
		caps: sandbox.Capabilities{Adapter: "fake"},
		result: &sandbox.RunResult{
			Status:   sandbox.StatusFailed,
			ExitCode: 1,
			Error:    errors.New("boom"),
		},
		err: nil, // result-carried error path
	}
	svc := sandbox.NewService(adapter, nil, nil, nil)
	run, err := svc.Run(context.Background(), sandbox.RunSpec{Image: "alpine", Command: []string{"sh", "-c", "exit 1"}})
	if err != nil {
		t.Fatalf("expected no error (result carries failure), got %v", err)
	}
	if run.Status != sandbox.StatusFailed {
		t.Errorf("status = %q, want failed", run.Status)
	}
	if run.ExitCode == nil || *run.ExitCode != 1 {
		t.Errorf("exit_code = %v, want 1", run.ExitCode)
	}
}

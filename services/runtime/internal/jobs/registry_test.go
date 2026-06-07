// SPDX-License-Identifier: Apache-2.0

// registry_test.go covers the pure-Go pieces of the jobs package — the
// in-memory Registry, dispatchArgs JSON shape, and the stats helper. No
// Postgres required, so these run as part of `go test ./...`.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

func TestRegistryRegisterGoAndGet(t *testing.T) {
	r := NewRegistry(nil)
	called := false
	err := r.RegisterGo(Definition{
		Name:        "x",
		Language:    LanguageGo,
		Description: "test",
		MaxAttempts: 5,
	}, func(ctx context.Context, args []byte) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	d := r.Get("x")
	if d == nil {
		t.Fatal("expected Get to return a definition")
	}
	if !d.HasLiveHandler() {
		t.Error("expected live handler")
	}
	if d.Language != LanguageGo {
		t.Errorf("language = %q, want go", d.Language)
	}
	if d.MaxAttempts != 5 {
		t.Errorf("max_attempts = %d, want 5", d.MaxAttempts)
	}
	if err := d.goHandler(context.Background(), nil); err != nil {
		t.Errorf("handler returned error: %v", err)
	}
	if !called {
		t.Error("handler not actually invoked")
	}
}

func TestRegistryRegisterGoDuplicate(t *testing.T) {
	r := NewRegistry(nil)
	if err := r.RegisterGo(Definition{Name: "dup"}, func(context.Context, []byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterGo(Definition{Name: "dup"}, func(context.Context, []byte) error { return nil }); err == nil {
		t.Error("expected duplicate-registration error")
	}
}

func TestRegistryRegisterGoEmptyName(t *testing.T) {
	r := NewRegistry(nil)
	if err := r.RegisterGo(Definition{Name: ""}, func(context.Context, []byte) error { return nil }); err == nil {
		t.Error("expected empty-name error")
	}
}

func TestRegistryRegisterGoNilHandler(t *testing.T) {
	r := NewRegistry(nil)
	if err := r.RegisterGo(Definition{Name: "x"}, nil); err == nil {
		t.Error("expected nil-handler error")
	}
}

func TestRegistryRegisterRemote(t *testing.T) {
	r := NewRegistry(nil)
	if err := r.RegisterRemote(Definition{Name: "py", Language: LanguagePython}); err != nil {
		t.Fatalf("register python: %v", err)
	}
	d := r.Get("py")
	if d == nil {
		t.Fatal("expected definition")
	}
	if d.HasLiveHandler() {
		t.Error("remote definition should not have a live handler")
	}
}

func TestRegistryRegisterRemoteRejectsGoLanguage(t *testing.T) {
	r := NewRegistry(nil)
	if err := r.RegisterRemote(Definition{Name: "x", Language: LanguageGo}); err == nil {
		t.Error("expected error registering Go as remote")
	}
}

func TestRegistryAllSnapshotsOrderIndependent(t *testing.T) {
	r := NewRegistry(nil)
	for _, name := range []string{"a", "b", "c"} {
		if err := r.RegisterRemote(Definition{Name: name, Language: LanguagePython}); err != nil {
			t.Fatal(err)
		}
	}
	got := r.All()
	if len(got) != 3 {
		t.Errorf("expected 3, got %d", len(got))
	}
}

// TestDispatchArgsKindZeroValue verifies the umbrella sentinel comes
// through for a zero value, so River's AddWorker keys the worker
// correctly.
func TestDispatchArgsKindZeroValue(t *testing.T) {
	var d dispatchArgs
	if d.Kind() != umbrellaKind {
		t.Errorf("zero-value Kind() = %q, want %q", d.Kind(), umbrellaKind)
	}
}

// TestDispatchArgsKindReturnsConcrete confirms the real kind comes
// through for a populated value (insert time).
func TestDispatchArgsKindReturnsConcrete(t *testing.T) {
	d := dispatchArgs{kindName: "real-kind"}
	if d.Kind() != "real-kind" {
		t.Errorf("Kind() = %q, want real-kind", d.Kind())
	}
}

// TestDispatchArgsRoundTripsJSON ensures the persisted args don't
// accidentally include the unexported kindName field (which would leak
// into the dashboard payload).
func TestDispatchArgsRoundTripsJSON(t *testing.T) {
	in := dispatchArgs{
		Payload:  json.RawMessage(`{"x":1}`),
		TenantID: "tenant-a",
		kindName: "real-kind",
	}
	enc, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	// Should not contain kindName.
	if got := string(enc); got != `{"payload":{"x":1},"tenant_id":"tenant-a"}` {
		t.Errorf("marshalled = %s", got)
	}
	var out dispatchArgs
	if err := json.Unmarshal(enc, &out); err != nil {
		t.Fatal(err)
	}
	if string(out.Payload) != `{"x":1}` {
		t.Errorf("payload roundtrip lost data: %s", out.Payload)
	}
	if out.TenantID != "tenant-a" {
		t.Errorf("tenant_id roundtrip lost data: %s", out.TenantID)
	}
}

// TestKindAliasesReadsPackageLevel verifies that setAliases / KindAliases
// share the same package-level slice (which is what makes the
// register-then-AddWorker pattern work).
func TestKindAliasesReadsPackageLevel(t *testing.T) {
	t.Cleanup(func() { setAliases(nil) })
	setAliases([]string{"a", "b", "c"})
	var d dispatchArgs
	got := d.KindAliases()
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Errorf("KindAliases() = %v, want [a b c]", got)
	}
}

// TestStatsFromJobs verifies the bucketing math directly.
func TestStatsFromJobs(t *testing.T) {
	rows := []*rivertype.JobRow{
		{State: rivertype.JobStateCompleted},
		{State: rivertype.JobStateCompleted},
		{State: rivertype.JobStateDiscarded},
		{State: rivertype.JobStateCancelled},
		{State: rivertype.JobStateRunning},
		{State: rivertype.JobStateAvailable}, // not counted in any bucket
	}
	s := statsFromJobs(rows)
	if s.Succeeded != 2 {
		t.Errorf("succeeded = %d, want 2", s.Succeeded)
	}
	if s.Failed != 2 {
		t.Errorf("failed = %d, want 2", s.Failed)
	}
	if s.Running != 1 {
		t.Errorf("running = %d, want 1", s.Running)
	}
}

// fakeStatsContext is a tiny stub that implements statsContext for unit
// testing RecentStatsFor without spinning up River.
type fakeStatsContext struct {
	rows []*rivertype.JobRow
	err  error
}

func (f *fakeStatsContext) JobList(ctx context.Context, params *river.JobListParams) (*river.JobListResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &river.JobListResult{Jobs: f.rows}, nil
}

func TestRecentStatsForPassesThrough(t *testing.T) {
	r := NewRegistry(nil)
	stub := &fakeStatsContext{rows: []*rivertype.JobRow{
		{State: rivertype.JobStateCompleted, CreatedAt: nowRecent()},
		{State: rivertype.JobStateRunning, CreatedAt: nowRecent()},
	}}
	stats, err := r.RecentStatsFor(context.Background(), stub, "anything")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Succeeded != 1 {
		t.Errorf("succeeded = %d, want 1", stats.Succeeded)
	}
	if stats.Running != 1 {
		t.Errorf("running = %d, want 1", stats.Running)
	}
}

func TestRecentStatsForPropagatesError(t *testing.T) {
	r := NewRegistry(nil)
	stub := &fakeStatsContext{err: errors.New("db down")}
	_, err := r.RecentStatsFor(context.Background(), stub, "anything")
	if err == nil {
		t.Error("expected error to propagate")
	}
}

// nowRecent returns a time within the recent window so it passes the
// 24h filter in RecentStatsFor.
func nowRecent() (t time.Time) {
	return time.Now().Add(-1 * time.Hour)
}
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"
)

// stubModule is a Module that records calls.
type stubModule struct {
	id        string
	deps      []string
	initErr   error
	stopErr   error
	inited    *[]string
	shutdowns *[]string
}

func (s *stubModule) Manifest() Manifest {
	return Manifest{ID: s.id, Name: s.id, Version: "0.0.1", Dependencies: s.deps}
}
func (s *stubModule) Init(_ context.Context, _ Deps) error {
	if s.initErr != nil {
		return s.initErr
	}
	if s.inited != nil {
		*s.inited = append(*s.inited, s.id)
	}
	return nil
}
func (s *stubModule) Shutdown(_ context.Context) error {
	if s.shutdowns != nil {
		*s.shutdowns = append(*s.shutdowns, s.id)
	}
	return s.stopErr
}

func TestRegisterRejectsDuplicate(t *testing.T) {
	r := NewRegistry(slog.Default())
	if err := r.Register(&stubModule{id: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(&stubModule{id: "a"}); err == nil {
		t.Error("expected duplicate ID to be rejected")
	}
}

func TestResolveTopologicalOrder(t *testing.T) {
	r := NewRegistry(slog.Default())
	_ = r.Register(&stubModule{id: "c", deps: []string{"b"}})
	_ = r.Register(&stubModule{id: "a"})
	_ = r.Register(&stubModule{id: "b", deps: []string{"a"}})

	if err := r.Resolve(); err != nil {
		t.Fatal(err)
	}
	order := r.IDs()
	// 'a' must come before 'b' which must come before 'c'
	aIdx := slices.Index(order, "a")
	bIdx := slices.Index(order, "b")
	cIdx := slices.Index(order, "c")
	if !(aIdx < bIdx && bIdx < cIdx) {
		t.Errorf("expected a < b < c, got %v", order)
	}
}

func TestResolveDetectsCycle(t *testing.T) {
	r := NewRegistry(slog.Default())
	_ = r.Register(&stubModule{id: "a", deps: []string{"b"}})
	_ = r.Register(&stubModule{id: "b", deps: []string{"a"}})

	if err := r.Resolve(); err == nil {
		t.Error("expected cycle detection")
	}
}

func TestResolveDetectsMissingDep(t *testing.T) {
	r := NewRegistry(slog.Default())
	_ = r.Register(&stubModule{id: "a", deps: []string{"missing"}})

	if err := r.Resolve(); err == nil {
		t.Error("expected missing-dep error")
	}
}

func TestStartInitsInOrderAndStopReverses(t *testing.T) {
	inits := []string{}
	shuts := []string{}
	r := NewRegistry(slog.Default())
	_ = r.Register(&stubModule{id: "a", inited: &inits, shutdowns: &shuts})
	_ = r.Register(&stubModule{id: "b", deps: []string{"a"}, inited: &inits, shutdowns: &shuts})

	if err := r.Start(context.Background(), Deps{Logger: slog.Default()}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(inits, []string{"a", "b"}) {
		t.Errorf("expected init order [a b], got %v", inits)
	}
	r.Stop(context.Background())
	if !slices.Equal(shuts, []string{"b", "a"}) {
		t.Errorf("expected shutdown order [b a], got %v", shuts)
	}
}

func TestStartRollsBackOnFailure(t *testing.T) {
	inits := []string{}
	shuts := []string{}
	r := NewRegistry(slog.Default())
	_ = r.Register(&stubModule{id: "a", inited: &inits, shutdowns: &shuts})
	_ = r.Register(&stubModule{id: "b", deps: []string{"a"}, initErr: errors.New("boom"), inited: &inits, shutdowns: &shuts})

	err := r.Start(context.Background(), Deps{Logger: slog.Default()})
	if err == nil {
		t.Fatal("expected error")
	}
	// 'a' was initialised; on failure it must be shut down.
	if !slices.Equal(shuts, []string{"a"}) {
		t.Errorf("expected rollback to shut down [a], got %v", shuts)
	}
}
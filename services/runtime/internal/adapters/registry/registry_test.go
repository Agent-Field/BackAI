// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistry_RegisterAndList(t *testing.T) {
	r := New()
	r.Register(Slot{
		ID:           "sandbox",
		Tier:         Tier1,
		Kind:         KindBuiltin,
		Name:         "docker",
		Version:      "26.0",
		Capabilities: json.RawMessage(`{"supports_gpu":false}`),
		SwapMethod:   "env_var",
		SwapEnv:      "AF_STACK_SANDBOX_ADAPTER",
	})
	r.Register(Slot{
		ID:         "storage",
		Tier:       Tier1,
		Kind:       KindBuiltin,
		Name:       "minio",
		SwapMethod: "env_var",
		SwapEnv:    "AF_STACK_S3_ADAPTER",
	})

	resp := r.List(context.Background())
	if len(resp.Slots) != 2 {
		t.Fatalf("expected 2 slots, got %d", len(resp.Slots))
	}
	// Sorted alphabetically.
	if resp.Slots[0].Slot != "sandbox" || resp.Slots[1].Slot != "storage" {
		t.Errorf("not sorted: %+v", resp.Slots)
	}
	if resp.Slots[0].Active.Status != StatusHealthy {
		t.Errorf("expected always-healthy default, got %v", resp.Slots[0].Active.Status)
	}
}

func TestRegistry_ProbeRefreshAfterTTL(t *testing.T) {
	var calls atomic.Int32
	r := New()
	r.StatusTTL = 50 * time.Millisecond
	r.Register(Slot{
		ID:         "x",
		Tier:       Tier1,
		Kind:       KindRemote,
		Name:       "remote",
		SwapMethod: "env_var",
		Probe: func(_ context.Context) (Status, error) {
			calls.Add(1)
			return StatusHealthy, nil
		},
	})

	_ = r.List(context.Background())
	_ = r.List(context.Background())
	if got := calls.Load(); got != 1 {
		t.Errorf("expected 1 probe within TTL, got %d", got)
	}
	time.Sleep(60 * time.Millisecond)
	_ = r.List(context.Background())
	if got := calls.Load(); got != 2 {
		t.Errorf("expected 2 probes after TTL, got %d", got)
	}
}

func TestRegistry_ProbeFailureRecorded(t *testing.T) {
	r := New()
	r.Register(Slot{
		ID:         "x",
		Tier:       Tier1,
		Kind:       KindRemote,
		Name:       "remote",
		SwapMethod: "env_var",
		Probe: func(_ context.Context) (Status, error) {
			return StatusUnknown, errors.New("boom")
		},
	})
	resp := r.List(context.Background())
	if resp.Slots[0].Active.Status != StatusUnhealthy {
		t.Errorf("expected unhealthy on probe error, got %v", resp.Slots[0].Active.Status)
	}
	if resp.Slots[0].Active.LastError != "boom" {
		t.Errorf("expected error message recorded, got %q", resp.Slots[0].Active.LastError)
	}
}

func TestRegistry_ProbeTimeoutBounded(t *testing.T) {
	r := New()
	r.ProbeTimeout = 30 * time.Millisecond
	r.Register(Slot{
		ID:         "x",
		Tier:       Tier1,
		Kind:       KindRemote,
		Name:       "slow",
		SwapMethod: "env_var",
		Probe: func(ctx context.Context) (Status, error) {
			<-ctx.Done()
			return StatusUnknown, ctx.Err()
		},
	})
	start := time.Now()
	resp := r.List(context.Background())
	dur := time.Since(start)
	if dur > 200*time.Millisecond {
		t.Errorf("probe should have been timed out fast; took %v", dur)
	}
	if resp.Slots[0].Active.Status != StatusUnhealthy {
		t.Errorf("expected unhealthy on probe timeout, got %v", resp.Slots[0].Active.Status)
	}
}

func TestRegistry_InvalidateForcesReprobe(t *testing.T) {
	var calls atomic.Int32
	r := New()
	r.Register(Slot{
		ID:         "x",
		Tier:       Tier1,
		Kind:       KindRemote,
		Name:       "remote",
		SwapMethod: "env_var",
		Probe: func(_ context.Context) (Status, error) {
			calls.Add(1)
			return StatusHealthy, nil
		},
	})
	_ = r.List(context.Background())
	r.InvalidateStatus("x")
	_ = r.List(context.Background())
	if calls.Load() != 2 {
		t.Errorf("expected 2 probes after invalidate, got %d", calls.Load())
	}
}

func TestRegistry_HandlerJSONResponse(t *testing.T) {
	r := New()
	r.Register(Slot{
		ID:         "sandbox",
		Tier:       Tier1,
		Kind:       KindBuiltin,
		Name:       "docker",
		SwapMethod: "env_var",
		SwapEnv:    "AF_STACK_SANDBOX_ADAPTER",
		Capabilities: json.RawMessage(`{"supports_gpu":false,"max_timeout_s":3600}`),
	})

	srv := httptest.NewServer(Handler(r))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/admin/adapters")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d", resp.StatusCode)
	}

	var out AdaptersResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Slots) != 1 || out.Slots[0].Slot != "sandbox" {
		t.Errorf("unexpected response: %+v", out)
	}
	if out.Slots[0].Active.Name != "docker" {
		t.Errorf("active name wrong: %+v", out.Slots[0].Active)
	}
}

func TestRegistry_NilHandler503(t *testing.T) {
	srv := httptest.NewServer(Handler(nil))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", resp.StatusCode)
	}
}

func TestRegistry_RegisterOverwrites(t *testing.T) {
	r := New()
	r.Register(Slot{ID: "x", Tier: Tier1, Kind: KindBuiltin, Name: "old"})
	r.Register(Slot{ID: "x", Tier: Tier1, Kind: KindBuiltin, Name: "new"})
	resp := r.List(context.Background())
	if len(resp.Slots) != 1 || resp.Slots[0].Active.Name != "new" {
		t.Errorf("expected single slot named 'new', got %+v", resp.Slots)
	}
}

func TestRegistry_RegisterIDRequiredPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic on missing ID")
		}
	}()
	r := New()
	r.Register(Slot{Tier: Tier1, Kind: KindBuiltin})
}

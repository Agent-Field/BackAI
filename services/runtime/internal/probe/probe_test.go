// SPDX-License-Identifier: Apache-2.0

package probe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	adapterregistry "github.com/Agent-Field/backai/services/runtime/internal/adapters/registry"
)

type fakeProbe struct {
	id       string
	slot     string
	interval time.Duration
	value    any
	calls    atomic.Int32
}

func (f *fakeProbe) ID() string              { return f.id }
func (f *fakeProbe) Slot() string            { return f.slot }
func (f *fakeProbe) Schedule() time.Duration { return f.interval }
func (f *fakeProbe) Run(context.Context) (Result, error) {
	f.calls.Add(1)
	return Result{
		ProbeID:    f.id,
		Capability: "llm_gateway.virtual_keys_active",
		Value:      f.value,
		Severity:   SeverityOK,
		Detail:     "ok",
		LastRun:    time.Now().UTC(),
	}, nil
}

func TestRegistryRunAllSnapshot(t *testing.T) {
	reg := NewRegistry(nil)
	p := &fakeProbe{id: "p1", slot: "llm-chat", value: true}
	reg.Register(p)
	reg.RunAll(context.Background())
	snap := reg.Snapshot()
	if got := snap["p1"].Value; got != true {
		t.Fatalf("snapshot value = %v", got)
	}
	if p.calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", p.calls.Load())
	}
}

func TestRegistryScheduledProbeReruns(t *testing.T) {
	reg := NewRegistry(nil)
	p := &fakeProbe{id: "p1", slot: "llm-chat", interval: 20 * time.Millisecond, value: true}
	reg.Register(p)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reg.StartScheduled(ctx)
	deadline := time.After(250 * time.Millisecond)
	for p.calls.Load() < 2 {
		select {
		case <-deadline:
			t.Fatalf("scheduled calls = %d, want >=2", p.calls.Load())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestRegistryUpdatesAdapterCapabilities(t *testing.T) {
	adapters := adapterregistry.New()
	adapters.Register(adapterregistry.Slot{
		ID:           "llm-chat",
		Tier:         adapterregistry.Tier1,
		Kind:         adapterregistry.KindBuiltin,
		Name:         "litellm",
		Capabilities: json.RawMessage(`{"supports_chat":true}`),
	})
	reg := NewRegistry(nil).WithAdapterRegistry(adapters)
	reg.StoreResult(Result{
		ProbeID:    LiteLLMVirtualKeysProbeID,
		Capability: "llm_gateway.virtual_keys_active",
		Value:      true,
		Severity:   SeverityOK,
		LastRun:    time.Now().UTC(),
	})
	resp := adapters.List(context.Background())
	var caps map[string]any
	if err := json.Unmarshal(resp.Slots[0].Active.Capabilities, &caps); err != nil {
		t.Fatal(err)
	}
	if caps["virtual_keys_active"] != true {
		t.Fatalf("capabilities = %+v", caps)
	}
	if caps["key_management_mode"] != "virtual_keys" {
		t.Fatalf("capabilities = %+v", caps)
	}
}

func TestLiteLLMVirtualKeysProbeStateless(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "DB not connected", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	p := NewLiteLLMVirtualKeysProbe(srv.URL, "sk-test", 0)
	res, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Value != false || res.Severity != SeverityUnavailable {
		t.Fatalf("res = %+v", res)
	}
}

func TestLiteLLMVirtualKeysProbeVirtualKeys(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()
	p := NewLiteLLMVirtualKeysProbe(srv.URL, "sk-test", 0)
	res, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Value != true || res.Severity != SeverityOK {
		t.Fatalf("res = %+v", res)
	}
}

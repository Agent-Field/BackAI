// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"testing"
)

// fakeTool is a minimal Tool used to exercise the Registry without
// touching real adapters.
type fakeTool struct {
	name       string
	desc       string
	adapter    string
	configured bool
	verbs      []Verb
	lastVerb   string
	lastArgs   map[string]any
	returnErr  error
	returnVal  any
}

func (t *fakeTool) Name() string        { return t.name }
func (t *fakeTool) Description() string { return t.desc }
func (t *fakeTool) AdapterID() string   { return t.adapter }
func (t *fakeTool) Configured() bool    { return t.configured }
func (t *fakeTool) Verbs() []Verb       { return t.verbs }
func (t *fakeTool) Invoke(_ context.Context, verb string, args map[string]any) (any, error) {
	t.lastVerb = verb
	t.lastArgs = args
	if t.returnErr != nil {
		return nil, t.returnErr
	}
	return t.returnVal, nil
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := NewRegistry(nil, nil)
	tool := &fakeTool{name: "browser", desc: "x", adapter: "browser-use", configured: true}
	r.Register(tool)

	got, err := r.Tool(ToolBrowser)
	if err != nil {
		t.Fatalf("Tool() err = %v", err)
	}
	if got.Name() != "browser" {
		t.Errorf("Tool() name = %q, want %q", got.Name(), "browser")
	}
}

func TestRegistry_ToolNotFound(t *testing.T) {
	r := NewRegistry(nil, nil)
	if _, err := r.Tool(ToolBrowser); !errors.Is(err, ErrToolNotFound) {
		t.Errorf("Tool() err = %v, want ErrToolNotFound", err)
	}
}

func TestRegistry_AllOrdered(t *testing.T) {
	r := NewRegistry(nil, nil)
	r.Register(&fakeTool{name: "search", configured: true})
	r.Register(&fakeTool{name: "browser", configured: true})
	r.Register(&fakeTool{name: "fs", configured: true})

	got := r.All()
	if len(got) != 3 {
		t.Fatalf("All() len = %d, want 3", len(got))
	}
	want := []string{"browser", "fs", "search"}
	for i, n := range want {
		if got[i].Name() != n {
			t.Errorf("All()[%d] = %q, want %q", i, got[i].Name(), n)
		}
	}
}

func TestRegistry_InvokeRoutes(t *testing.T) {
	r := NewRegistry(nil, nil)
	t1 := &fakeTool{
		name:       "browser",
		configured: true,
		returnVal:  map[string]any{"ok": true},
	}
	r.Register(t1)

	// Empty tenantID skips the per-tenant gate.
	got, err := r.Invoke(context.Background(), "", ToolBrowser, "navigate", map[string]any{"url": "x"})
	if err != nil {
		t.Fatalf("Invoke() err = %v", err)
	}
	if m, ok := got.(map[string]any); !ok || m["ok"] != true {
		t.Errorf("Invoke() got = %v", got)
	}
	if t1.lastVerb != "navigate" {
		t.Errorf("lastVerb = %q, want navigate", t1.lastVerb)
	}
}

func TestRegistry_InvokeUnconfigured(t *testing.T) {
	r := NewRegistry(nil, nil)
	r.Register(&fakeTool{name: "browser", configured: false})

	_, err := r.Invoke(context.Background(), "", ToolBrowser, "navigate", nil)
	if !errors.Is(err, ErrAdapterNotConfigured) {
		t.Errorf("Invoke() err = %v, want ErrAdapterNotConfigured", err)
	}
}

func TestRegistry_ListStatusNoDB(t *testing.T) {
	r := NewRegistry(nil, nil)
	r.Register(&fakeTool{
		name: "browser", desc: "x", adapter: "browser-use", configured: true,
		verbs: []Verb{{Name: "navigate"}},
	})

	got, err := r.ListStatus(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("ListStatus() err = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListStatus() len = %d, want 1", len(got))
	}
	if got[0].AdapterID != "browser-use" {
		t.Errorf("AdapterID = %q, want browser-use", got[0].AdapterID)
	}
	if got[0].Enabled {
		t.Errorf("Enabled = true, want false (no row → default disabled)")
	}
	if !got[0].Configured {
		t.Errorf("Configured = false, want true")
	}
	if len(got[0].Verbs) != 1 {
		t.Errorf("Verbs len = %d, want 1", len(got[0].Verbs))
	}
}

func TestIsValidToolName(t *testing.T) {
	for _, n := range []string{"browser", "search", "fs", "exec", "http", "sql"} {
		if !IsValidToolName(n) {
			t.Errorf("IsValidToolName(%q) = false, want true", n)
		}
	}
	for _, n := range []string{"", "BROWSER", "invalid", "browser-use"} {
		if IsValidToolName(n) {
			t.Errorf("IsValidToolName(%q) = true, want false", n)
		}
	}
}

func TestIsNativeServer(t *testing.T) {
	if !IsNativeServer("native:browser") {
		t.Errorf("IsNativeServer(native:browser) = false, want true")
	}
	if IsNativeServer("ext:something") {
		t.Errorf("IsNativeServer(ext:something) = true, want false")
	}
}

func TestRegistry_ListNativeMCPTools(t *testing.T) {
	r := NewRegistry(nil, nil)
	r.Register(&fakeTool{
		name: "browser", desc: "x", adapter: "browser-use", configured: true,
		verbs: []Verb{
			{Name: "navigate", Description: "Load a URL", InputSchema: map[string]any{"type": "object"}},
			{Name: "screenshot"},
		},
	})
	r.Register(&fakeTool{name: "search", configured: false}) // skipped (unconfigured)

	got, err := r.ListNativeMCPTools(context.Background(), "")
	if err != nil {
		t.Fatalf("ListNativeMCPTools err = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (navigate + screenshot)", len(got))
	}
	for _, row := range got {
		if row.Server != "native:browser" {
			t.Errorf("Server = %q, want native:browser", row.Server)
		}
	}
}

func TestRegistry_CallNativeRoutes(t *testing.T) {
	r := NewRegistry(nil, nil)
	tool := &fakeTool{
		name: "browser", configured: true,
		returnVal: map[string]any{"navigated": "https://example.com"},
	}
	r.Register(tool)

	got, err := r.CallNative(context.Background(), "", "native:browser", "navigate", map[string]any{"url": "https://example.com"})
	if err != nil {
		t.Fatalf("CallNative err = %v", err)
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("CallNative result not a map: %T", got)
	}
	if m["navigated"] != "https://example.com" {
		t.Errorf("CallNative result = %v", got)
	}
}

func TestRegistry_CallNativeNonNative(t *testing.T) {
	r := NewRegistry(nil, nil)
	_, err := r.CallNative(context.Background(), "", "ext:foo", "do", nil)
	if !errors.Is(err, ErrToolNotFound) {
		t.Errorf("CallNative non-native err = %v, want ErrToolNotFound", err)
	}
}

func TestCheckConfigured(t *testing.T) {
	if err := CheckConfigured(ToolBrowser, "browser-use", true); err != nil {
		t.Errorf("CheckConfigured configured: err = %v", err)
	}
	err := CheckConfigured(ToolBrowser, "steel", false)
	if err == nil {
		t.Fatal("CheckConfigured unconfigured: err = nil, want non-nil")
	}
}

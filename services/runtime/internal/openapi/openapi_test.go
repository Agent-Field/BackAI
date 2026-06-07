// SPDX-License-Identifier: Apache-2.0

package openapi

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBuilderEmitsValidThreeOneSpec asserts that the Build output is
// JSON-encodable and has the OpenAPI 3.1 top-level keys the dashboard's
// consumer relies on (openapi, info, paths).
func TestBuilderEmitsValidThreeOneSpec(t *testing.T) {
	b := NewBuilder("AF Stack", "0.1.0")
	b.SetDescription("test spec")
	b.AddServer("http://localhost:8080", "local")
	b.AddTag("system", "diagnostics")

	b.Register("GET", "/health", RouteMeta{Summary: "Liveness"})

	spec := b.Build()
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("Build marshal: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, key := range []string{"openapi", "info", "paths"} {
		if _, ok := parsed[key]; !ok {
			t.Errorf("spec missing required top-level key %q", key)
		}
	}
	if parsed["openapi"] != "3.1.0" {
		t.Errorf("openapi = %v, want 3.1.0", parsed["openapi"])
	}
	info, _ := parsed["info"].(map[string]any)
	if info["title"] != "AF Stack" {
		t.Errorf("info.title = %v, want AF Stack", info["title"])
	}
	if info["version"] != "0.1.0" {
		t.Errorf("info.version = %v, want 0.1.0", info["version"])
	}
}

// TestBuilderAccumulatesAtLeast10Paths registers a realistic surface
// area and verifies the spec exposes at least 10 distinct URL templates
// — the floor the dashboard's API explorer assumes for v1.
func TestBuilderAccumulatesAtLeast10Paths(t *testing.T) {
	b := NewBuilder("test", "0")
	routes := []struct{ method, path string }{
		{"GET", "/health"},
		{"GET", "/ready"},
		{"GET", "/openapi.json"},
		{"GET", "/api/v1/agents"},
		{"POST", "/api/v1/agents/{call}"},
		{"POST", "/api/v1/agents/async/{call}"},
		{"GET", "/api/v1/executions/{id}"},
		{"DELETE", "/api/v1/executions/{id}"},
		{"GET", "/api/v1/runs"},
		{"GET", "/api/v1/home/overview"},
		{"GET", "/api/v1/cost"},
		{"GET", "/api/v1/modules"},
	}
	for _, r := range routes {
		b.Register(r.method, r.path, RouteMeta{Summary: "test"})
	}
	if got := b.PathCount(); got < 10 {
		t.Errorf("PathCount = %d, want >= 10", got)
	}
	spec := b.Build()
	if len(spec.Paths) < 10 {
		t.Errorf("len(spec.Paths) = %d, want >= 10", len(spec.Paths))
	}
}

// TestRegisterNormalisesMethod confirms uppercase + lowercase HTTP
// verbs both end up as the lowercase form OpenAPI expects under each
// path entry.
func TestRegisterNormalisesMethod(t *testing.T) {
	b := NewBuilder("test", "0")
	b.Register("GET", "/x", RouteMeta{Summary: "a"})
	b.Register("post", "/x", RouteMeta{Summary: "b"})

	raw, _ := json.Marshal(b.Build())
	if !strings.Contains(string(raw), `"get"`) {
		t.Errorf("expected lowercase get key, body=%s", raw)
	}
	if !strings.Contains(string(raw), `"post"`) {
		t.Errorf("expected lowercase post key, body=%s", raw)
	}
}

// TestEmptyMethodOrPathIsNoop guards the "register inside a loop with
// guarded inputs" use case where we'd rather skip than blow up on a
// zero entry.
func TestEmptyMethodOrPathIsNoop(t *testing.T) {
	b := NewBuilder("test", "0")
	b.Register("", "/x", RouteMeta{})
	b.Register("GET", "", RouteMeta{})
	if got := b.PathCount(); got != 0 {
		t.Errorf("PathCount = %d, want 0 after empty registers", got)
	}
}

// TestErrorEnvelopeIsSeeded confirms the standard error envelope
// component schema is present after construction so handlers can $ref
// it without an extra AddSchema call.
func TestErrorEnvelopeIsSeeded(t *testing.T) {
	b := NewBuilder("test", "0")
	spec := b.Build()
	if _, ok := spec.Components.Schemas["ErrorEnvelope"]; !ok {
		t.Errorf("ErrorEnvelope schema not seeded")
	}
}

// TestDefaultBuilderResets confirms ResetDefault wipes accumulated
// state so tests in package-level init mode aren't dependent on each
// other's order.
func TestDefaultBuilderResets(t *testing.T) {
	Register("GET", "/before", RouteMeta{})
	if c := Default().PathCount(); c == 0 {
		t.Fatalf("expected at least one path after Register")
	}
	ResetDefault()
	if c := Default().PathCount(); c != 0 {
		t.Errorf("PathCount after Reset = %d, want 0", c)
	}
}

// TestConcurrentRegisterAndBuild verifies the RWMutex inside Builder
// keeps a Build() snapshot consistent under concurrent Register calls.
// Run with -race to catch regressions.
func TestConcurrentRegisterAndBuild(t *testing.T) {
	b := NewBuilder("test", "0")
	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			b.Register("GET", "/p"+string(rune('a'+(i%26))), RouteMeta{})
		}
		close(done)
	}()
	for i := 0; i < 50; i++ {
		_ = b.Build()
	}
	<-done
}

// TestStandardErrorResponsesIncluded verifies every Register call
// also seeds the standard error responses so the spec stays consistent
// across handlers.
func TestStandardErrorResponsesIncluded(t *testing.T) {
	b := NewBuilder("test", "0")
	b.Register("GET", "/x", RouteMeta{Summary: "test"})
	spec := b.Build()
	path, ok := spec.Paths["/x"]
	if !ok {
		t.Fatalf("path /x not registered")
	}
	op, ok := path.Operations["get"]
	if !ok {
		t.Fatalf("get op missing")
	}
	for _, code := range []string{"400", "401", "403", "404", "429", "500", "502", "503"} {
		if _, ok := op.Responses[code]; !ok {
			t.Errorf("response %s missing for /x get", code)
		}
	}
}
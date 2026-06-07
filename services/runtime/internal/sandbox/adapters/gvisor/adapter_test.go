// gvisor adapter unit tests.
//
// We don't talk to a real Docker daemon — the point is to verify the
// gvisor wrapper sets the right knobs on the underlying Docker adapter
// (Runtime="runsc" + AdapterName="gvisor"). Once Phase 9.1 lands the real
// Docker code path, an integration test will exercise the actual
// container-create call.
package gvisor_test

import (
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
	dockeradapter "github.com/Agent-Field/backai/services/runtime/internal/sandbox/adapters/docker"
	"github.com/Agent-Field/backai/services/runtime/internal/sandbox/adapters/gvisor"
)

func TestNewSetsRunscRuntime(t *testing.T) {
	s, err := gvisor.New(gvisor.Config{Host: "unix:///var/run/docker.sock"})
	if err != nil {
		t.Fatalf("gvisor.New: %v", err)
	}

	// The wrapper returns a *docker.Adapter under the sandbox.Sandbox
	// interface — type-assert to inspect the resolved Config.
	docker, ok := s.(*dockeradapter.Adapter)
	if !ok {
		t.Fatalf("gvisor.New returned %T, want *docker.Adapter", s)
	}
	cfg := docker.Config()
	if cfg.Runtime != "runsc" {
		t.Errorf("Runtime = %q, want %q", cfg.Runtime, "runsc")
	}
	if cfg.AdapterName != "gvisor" {
		t.Errorf("AdapterName = %q, want %q", cfg.AdapterName, "gvisor")
	}
	if cfg.Host != "unix:///var/run/docker.sock" {
		t.Errorf("Host = %q, did not propagate", cfg.Host)
	}
}

func TestCapabilitiesLabelGvisor(t *testing.T) {
	s, err := gvisor.New(gvisor.Config{})
	if err != nil {
		t.Fatalf("gvisor.New: %v", err)
	}
	caps := s.Capabilities()
	if caps.Adapter != "gvisor" {
		t.Errorf("Capabilities.Adapter = %q, want %q", caps.Adapter, "gvisor")
	}
	// gvisor inherits docker's surface so these should be true.
	if !caps.SupportsNetwork || !caps.SupportsMounts {
		t.Errorf("expected network+mounts support, got %+v", caps)
	}
}

// Compile-time check: the gvisor adapter (via the docker adapter it returns)
// satisfies sandbox.Sandbox. This guards against drift if Phase 9.1 changes
// the interface.
func TestImplementsSandboxInterface(t *testing.T) {
	s, err := gvisor.New(gvisor.Config{})
	if err != nil {
		t.Fatalf("gvisor.New: %v", err)
	}
	var _ sandbox.Sandbox = s
}

package firecracker_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
	"github.com/Agent-Field/backai/services/runtime/internal/sandbox/adapters/firecracker"
)

func TestNewSucceeds(t *testing.T) {
	s, err := firecracker.New(firecracker.Config{})
	if err != nil {
		t.Fatalf("firecracker.New: %v", err)
	}
	if s == nil {
		t.Fatal("firecracker.New returned nil adapter")
	}
}

func TestRunReturnsRequiresFlintlock(t *testing.T) {
	s, err := firecracker.New(firecracker.Config{})
	if err != nil {
		t.Fatalf("firecracker.New: %v", err)
	}
	_, err = s.Run(context.Background(), sandbox.RunSpec{Image: "python:3.12-slim"})
	if !errors.Is(err, firecracker.ErrFirecrackerRequiresFlintlock) {
		t.Fatalf("Run err = %v, want ErrFirecrackerRequiresFlintlock", err)
	}
	if !errors.Is(err, sandbox.ErrAdapterUnavailable) {
		t.Errorf("Run err = %v, expected to wrap sandbox.ErrAdapterUnavailable", err)
	}
	if !strings.Contains(err.Error(), "Flintlock") {
		t.Errorf("Run err message %q should mention Flintlock", err.Error())
	}
	if !strings.Contains(err.Error(), "docs/sandbox-adapters.md") {
		t.Errorf("Run err message %q should point at docs/sandbox-adapters.md", err.Error())
	}
}

func TestStreamReturnsRequiresFlintlock(t *testing.T) {
	s, _ := firecracker.New(firecracker.Config{})
	_, _, err := s.Stream(context.Background(), sandbox.RunSpec{})
	if !errors.Is(err, firecracker.ErrFirecrackerRequiresFlintlock) {
		t.Errorf("Stream err = %v, want ErrFirecrackerRequiresFlintlock", err)
	}
}

func TestStopReturnsRequiresFlintlock(t *testing.T) {
	s, _ := firecracker.New(firecracker.Config{})
	err := s.Stop(context.Background(), "some-job-id")
	if !errors.Is(err, firecracker.ErrFirecrackerRequiresFlintlock) {
		t.Errorf("Stop err = %v, want ErrFirecrackerRequiresFlintlock", err)
	}
}

func TestCapabilities(t *testing.T) {
	s, _ := firecracker.New(firecracker.Config{})
	caps := s.Capabilities()
	if caps.Adapter != "firecracker" {
		t.Errorf("Adapter = %q, want firecracker", caps.Adapter)
	}
	if caps.MaxTimeoutS != 86400 {
		t.Errorf("MaxTimeoutS = %d, want 86400", caps.MaxTimeoutS)
	}
	if !caps.SupportsGPU || !caps.SupportsNetwork || !caps.SupportsMounts {
		t.Errorf("expected full feature support, got %+v", caps)
	}
	if caps.ColdStartMS != 5000 {
		t.Errorf("ColdStartMS = %d, want 5000", caps.ColdStartMS)
	}
}

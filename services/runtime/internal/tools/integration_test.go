// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
	execad "github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/exec"
	fsad "github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/fs"
	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/httpfetch"
	sqlad "github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/sql"
)

// fakeSandboxRunner is a stub that records the spec and returns a
// canned SandboxRun.
type fakeSandboxRunner struct {
	lastSpec sandbox.RunSpec
}

func (f *fakeSandboxRunner) Run(_ context.Context, spec sandbox.RunSpec) (*sandbox.SandboxRun, error) {
	f.lastSpec = spec
	id := "run-1"
	return &sandbox.SandboxRun{ID: id, Image: spec.Image, Status: sandbox.StatusDone}, nil
}

// stubBrowserAdapter returns a Configured() == false adapter for the
// integration test (we don't need a working browser sidecar).
type stubBrowserAdapter struct{}

func (s *stubBrowserAdapter) ID() string       { return "test-stub" }
func (s *stubBrowserAdapter) Configured() bool { return false }
func (s *stubBrowserAdapter) Navigate(_ context.Context, _, _ string) (browserResult, error) {
	return browserResult{}, ErrAdapterNotConfigured
}

// browserResult is a local alias to avoid the heavier import chain in
// this integration test file.
type browserResult = struct{}

func TestIntegration_AllToolsRegistered(t *testing.T) {
	runner := &fakeSandboxRunner{}
	r := NewRegistry(nil, nil)

	// FS / exec / http / sql — work with stubs/nil pool.
	r.Register(NewFSTool(fsad.New(runner)))
	r.Register(NewExecTool(execad.New(runner)))
	r.Register(NewHTTPTool(httpfetch.New(0)))
	r.Register(NewSQLTool(sqlad.New(nil))) // pool nil → unconfigured but registered

	got := r.All()
	if len(got) != 4 {
		t.Fatalf("All() len = %d, want 4 (fs, exec, http, sql)", len(got))
	}
	names := map[string]bool{}
	for _, tool := range got {
		names[tool.Name()] = true
	}
	for _, want := range []string{"fs", "exec", "http", "sql"} {
		if !names[want] {
			t.Errorf("missing tool %q in registry", want)
		}
	}
}

func TestIntegration_FSReadInvokesSandbox(t *testing.T) {
	runner := &fakeSandboxRunner{}
	r := NewRegistry(nil, nil)
	r.Register(NewFSTool(fsad.New(runner)))

	args := map[string]any{
		"path":  "hello.txt",
		"files": map[string]any{"hello.txt": "world"},
	}
	_, err := r.Invoke(context.Background(), "", ToolFS, "read", args)
	if err != nil {
		t.Fatalf("Invoke fs.read err = %v", err)
	}
	if runner.lastSpec.Image != "alpine:3.20" {
		t.Errorf("sandbox spec image = %q, want alpine:3.20", runner.lastSpec.Image)
	}
	if runner.lastSpec.Network != sandbox.NetworkIsolated {
		t.Errorf("sandbox spec network = %q, want isolated", runner.lastSpec.Network)
	}
}

func TestIntegration_ExecShellInvokesSandbox(t *testing.T) {
	runner := &fakeSandboxRunner{}
	r := NewRegistry(nil, nil)
	r.Register(NewExecTool(execad.New(runner)))

	args := map[string]any{
		"command": "echo hi",
	}
	_, err := r.Invoke(context.Background(), "", ToolExec, "shell", args)
	if err != nil {
		t.Fatalf("Invoke exec.shell err = %v", err)
	}
	if runner.lastSpec.Image != "alpine:3.20" {
		t.Errorf("sandbox spec image = %q, want alpine:3.20", runner.lastSpec.Image)
	}
	if len(runner.lastSpec.Command) < 3 || runner.lastSpec.Command[2] != "echo hi" {
		t.Errorf("sandbox spec command = %v, want [sh -lc 'echo hi']", runner.lastSpec.Command)
	}
}

func TestIntegration_HTTPGetInvalidURL(t *testing.T) {
	r := NewRegistry(nil, nil)
	r.Register(NewHTTPTool(httpfetch.New(0)))

	_, err := r.Invoke(context.Background(), "", ToolHTTP, "get", map[string]any{"url": ""})
	if err == nil {
		t.Errorf("Invoke http.get empty url: err = nil, want error")
	}
}

func TestIntegration_SQLUnconfigured(t *testing.T) {
	r := NewRegistry(nil, nil)
	r.Register(NewSQLTool(sqlad.New(nil)))

	_, err := r.Invoke(context.Background(), "", ToolSQL, "query", map[string]any{"sql": "select 1"})
	if !errors.Is(err, ErrAdapterNotConfigured) {
		t.Errorf("Invoke sql.query nil pool: err = %v, want ErrAdapterNotConfigured", err)
	}
}

func TestIntegration_VerbNotFound(t *testing.T) {
	runner := &fakeSandboxRunner{}
	r := NewRegistry(nil, nil)
	r.Register(NewExecTool(execad.New(runner)))

	_, err := r.Invoke(context.Background(), "", ToolExec, "nonexistent-verb", nil)
	if !errors.Is(err, ErrVerbNotFound) {
		t.Errorf("Invoke unknown verb: err = %v, want ErrVerbNotFound", err)
	}
}

func TestIntegration_CallNativeRouting(t *testing.T) {
	runner := &fakeSandboxRunner{}
	r := NewRegistry(nil, nil)
	r.Register(NewExecTool(execad.New(runner)))

	_, err := r.CallNative(context.Background(), "", "native:exec", "shell", map[string]any{"command": "true"})
	if err != nil {
		t.Fatalf("CallNative native:exec err = %v", err)
	}
	if runner.lastSpec.Command[2] != "true" {
		t.Errorf("expected command 'true', got %v", runner.lastSpec.Command)
	}
}

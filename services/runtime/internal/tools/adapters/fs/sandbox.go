// SPDX-License-Identifier: Apache-2.0

// Package fs implements the filesystem ToolAdapter. It wraps
// suite.sandbox so file reads/writes happen inside an ephemeral
// container, never on the host filesystem.
//
// Three verbs:
//
//	read   {path, files}    — files is a map of path->contents that the
//	                          adapter materialises in the sandbox before
//	                          cat'ing the requested path.
//	write  {path, content}  — validates the path/content shape and echoes
//	                          metadata (we don't persist; the sandbox is
//	                          ephemeral).
//	list   {files}          — lists keys of the provided files map.
//
// The path must be relative and not contain "..". Output of read is the
// captured stdout from `cat`; the sandbox handles stdout upload itself.
package fs

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
)

// ErrNotConfigured is returned when no sandbox runner is wired.
var ErrNotConfigured = errors.New("fs: sandbox not configured")

// SandboxRunner is the minimal subset of sandbox.Service we depend on.
// Defined locally so unit tests can swap in a fake.
type SandboxRunner interface {
	Run(ctx context.Context, spec sandbox.RunSpec) (*sandbox.SandboxRun, error)
}

// Adapter wraps a SandboxRunner with the verbs the fs Tool exposes.
type Adapter struct {
	runner SandboxRunner
}

// New constructs an Adapter. A nil runner produces an unconfigured
// adapter — every verb returns ErrNotConfigured.
func New(runner SandboxRunner) *Adapter {
	return &Adapter{runner: runner}
}

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return "sandbox-fs" }

// Configured reports whether a runner is wired.
func (a *Adapter) Configured() bool { return a != nil && a.runner != nil }

// Read executes `cat -- <path>` against the provided files map inside
// an isolated sandbox container.
func (a *Adapter) Read(ctx context.Context, tenantID, path string, files map[string]string) (*sandbox.SandboxRun, error) {
	if !a.Configured() {
		return nil, ErrNotConfigured
	}
	if err := validatePath(path); err != nil {
		return nil, err
	}
	if _, ok := files[path]; !ok {
		return nil, fmt.Errorf("fs: read: path %q not in files map", path)
	}
	spec := sandbox.RunSpec{
		TenantID: tenantID,
		Image:    "alpine:3.20",
		Command:  []string{"sh", "-lc", "cat -- " + shellQuote(path)},
		Files:    files,
		TimeoutS: 10,
		Network:  sandbox.NetworkIsolated,
	}
	return a.runner.Run(ctx, spec)
}

// Write validates the args and echoes metadata. The sandbox is
// ephemeral; we don't persist the write. Callers wanting durable
// writes should use suite.storage instead.
func (a *Adapter) Write(_ context.Context, _ string, path, content string) (map[string]any, error) {
	if !a.Configured() {
		return nil, ErrNotConfigured
	}
	if err := validatePath(path); err != nil {
		return nil, err
	}
	return map[string]any{
		"path":  path,
		"bytes": len([]byte(content)),
	}, nil
}

// List returns the sorted keys of the provided files map.
func (a *Adapter) List(_ context.Context, _ string, files map[string]string) ([]string, error) {
	if !a.Configured() {
		return nil, ErrNotConfigured
	}
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

func validatePath(path string) error {
	if path == "" {
		return fmt.Errorf("fs: path required")
	}
	if strings.HasPrefix(path, "/") {
		return fmt.Errorf("fs: path must be relative")
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("fs: path must stay inside workspace")
	}
	return nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// SPDX-License-Identifier: Apache-2.0

// Package exec implements the code-execution ToolAdapter. It wraps
// suite.sandbox to run a command inside an isolated container.
//
// Three verbs:
//
//	shell  {command, image?, env?, timeout_s?, network?} — sh -lc
//	python {code, image?="python:3.12-slim", timeout_s?}  — python -c
//	node   {code, image?="node:20-alpine", timeout_s?}    — node -e
package exec

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
)

// ErrNotConfigured is returned when no sandbox runner is wired.
var ErrNotConfigured = errors.New("exec: sandbox not configured")

// SandboxRunner is the minimal subset of sandbox.Service we depend on.
type SandboxRunner interface {
	Run(ctx context.Context, spec sandbox.RunSpec) (*sandbox.SandboxRun, error)
}

// Adapter wraps a SandboxRunner.
type Adapter struct {
	runner SandboxRunner
}

// New constructs an Adapter. Nil runner = unconfigured.
func New(runner SandboxRunner) *Adapter { return &Adapter{runner: runner} }

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return "sandbox-exec" }

// Configured reports whether a runner is wired.
func (a *Adapter) Configured() bool { return a != nil && a.runner != nil }

// Shell runs sh -lc <command> inside the named image. Defaults to
// alpine:3.20 + isolated network when callers don't specify.
func (a *Adapter) Shell(ctx context.Context, tenantID, command, image string, env map[string]string, timeoutS int, network sandbox.NetworkMode) (*sandbox.SandboxRun, error) {
	if !a.Configured() {
		return nil, ErrNotConfigured
	}
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("exec: command required")
	}
	if image == "" {
		image = "alpine:3.20"
	}
	if timeoutS <= 0 {
		timeoutS = 60
	}
	if network == "" {
		network = sandbox.NetworkIsolated
	}
	spec := sandbox.RunSpec{
		TenantID: tenantID,
		Image:    image,
		Command:  []string{"sh", "-lc", command},
		Env:      env,
		TimeoutS: timeoutS,
		Network:  network,
	}
	return a.runner.Run(ctx, spec)
}

// Python runs `python -c <code>` inside a python image.
func (a *Adapter) Python(ctx context.Context, tenantID, code, image string, timeoutS int) (*sandbox.SandboxRun, error) {
	if !a.Configured() {
		return nil, ErrNotConfigured
	}
	if strings.TrimSpace(code) == "" {
		return nil, fmt.Errorf("exec: code required")
	}
	if image == "" {
		image = "python:3.12-slim"
	}
	if timeoutS <= 0 {
		timeoutS = 60
	}
	spec := sandbox.RunSpec{
		TenantID: tenantID,
		Image:    image,
		Command:  []string{"python", "-c", code},
		TimeoutS: timeoutS,
		Network:  sandbox.NetworkIsolated,
	}
	return a.runner.Run(ctx, spec)
}

// Node runs `node -e <code>` inside a node image.
func (a *Adapter) Node(ctx context.Context, tenantID, code, image string, timeoutS int) (*sandbox.SandboxRun, error) {
	if !a.Configured() {
		return nil, ErrNotConfigured
	}
	if strings.TrimSpace(code) == "" {
		return nil, fmt.Errorf("exec: code required")
	}
	if image == "" {
		image = "node:20-alpine"
	}
	if timeoutS <= 0 {
		timeoutS = 60
	}
	spec := sandbox.RunSpec{
		TenantID: tenantID,
		Image:    image,
		Command:  []string{"node", "-e", code},
		TimeoutS: timeoutS,
		Network:  sandbox.NetworkIsolated,
	}
	return a.runner.Run(ctx, spec)
}

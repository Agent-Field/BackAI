// SPDX-License-Identifier: Apache-2.0

//go:build integration

// Integration tests for the Docker sandbox adapter. Requires a
// reachable Docker daemon at the default socket. Run with:
//
//	go test -tags integration ./services/runtime/internal/sandbox/...
//
// The tests pull the official alpine image; first invocation may take
// a few seconds on a cold cache.

package sandbox_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
	dockersandbox "github.com/Agent-Field/backai/services/runtime/internal/sandbox/adapters/docker"
)

// alpineImage is the smallest image guaranteed to be available on
// Docker Hub. Using the digest pin would be more reproducible but
// requires periodic rotation; the latest tag is fine for CI.
const alpineImage = "alpine:3"

// newDockerAdapterT constructs a docker adapter or skips the test if
// the daemon is unreachable.
func newDockerAdapterT(t *testing.T) *dockersandbox.Adapter {
	t.Helper()
	a, err := dockersandbox.New(dockersandbox.Config{})
	if err != nil {
		t.Skipf("docker.New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.Ping(ctx); err != nil {
		t.Skipf("docker daemon not reachable: %v", err)
	}
	return a
}

func TestDocker_EchoSucceeds(t *testing.T) {
	a := newDockerAdapterT(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	spec := sandbox.RunSpec{
		ID:       "test-echo",
		Image:    alpineImage,
		Command:  []string{"echo", "hello"},
		TimeoutS: 30,
		CPU:      1,
		MemoryGB: 1,
		Network:  sandbox.NetworkRestricted,
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	res, err := a.Run(ctx, spec)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != sandbox.StatusDone {
		t.Errorf("status = %q, want done", res.Status)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit_code = %d, want 0", res.ExitCode)
	}
	if res.DurationS < 0 {
		t.Errorf("duration_s should be >= 0, got %d", res.DurationS)
	}
}

func TestDocker_NonZeroExitFails(t *testing.T) {
	a := newDockerAdapterT(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	spec := sandbox.RunSpec{
		ID:       "test-fail",
		Image:    alpineImage,
		Command:  []string{"sh", "-c", "exit 1"},
		TimeoutS: 30,
		CPU:      1,
		MemoryGB: 1,
		Network:  sandbox.NetworkIsolated,
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	res, err := a.Run(ctx, spec)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != sandbox.StatusFailed {
		t.Errorf("status = %q, want failed", res.Status)
	}
	if res.ExitCode != 1 {
		t.Errorf("exit_code = %d, want 1", res.ExitCode)
	}
}

func TestDocker_StopRunningContainer(t *testing.T) {
	a := newDockerAdapterT(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	spec := sandbox.RunSpec{
		ID:       "test-stop",
		Image:    alpineImage,
		Command:  []string{"sleep", "30"},
		TimeoutS: 60,
		CPU:      1,
		MemoryGB: 1,
		Network:  sandbox.NetworkIsolated,
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	// Launch in a goroutine so we can call Stop while it's running.
	resCh := make(chan *sandbox.RunResult, 1)
	errCh := make(chan error, 1)
	go func() {
		r, e := a.Run(ctx, spec)
		resCh <- r
		errCh <- e
	}()
	// Give the container a moment to start.
	time.Sleep(2 * time.Second)
	if err := a.Stop(ctx, spec.ID); err != nil && !errors.Is(err, sandbox.ErrNotFound) {
		t.Logf("Stop returned: %v", err)
	}
	res := <-resCh
	err := <-errCh
	if err != nil {
		t.Fatalf("Run after Stop: %v", err)
	}
	// Stop -> non-zero exit; container terminates via SIGTERM/SIGKILL.
	if res.Status == sandbox.StatusDone {
		t.Errorf("expected non-done status after Stop, got done")
	}
}
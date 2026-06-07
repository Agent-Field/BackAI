// Package firecracker is a v2 scaffold for running sandbox workloads
// inside Firecracker micro-VMs.
//
// PHASE 9.2 STUB — NOT PRODUCTION-READY
//
// Firecracker on its own is a per-VM hypervisor; it does NOT orchestrate
// VM lifecycle, image conversion, networking, or pool warm-up across a
// fleet. The Phase 9.2 design intentionally defers that work to a
// dedicated orchestrator (Flintlock — https://github.com/liquidmetal-dev/flintlock,
// or an equivalent) rather than bake it into the runtime. This adapter
// therefore returns a sentinel error from every operation until the
// orchestrator integration lands; the package exists so:
//
//  1. The main.go selection switch can list "firecracker" as a known
//     adapter and fail loudly with a useful message.
//  2. Capabilities() reports realistic micro-VM numbers so the dashboard
//     can render the adapter card even when it's not wired.
//  3. Phase 9.1's interface contract is exercised (compile-time check).
//
// HOST REQUIREMENTS (future)
//
//   - Bare-metal or KVM-capable VM host (Linux x86_64 / aarch64).
//   - Firecracker binary >= v1.x in PATH.
//   - Flintlock (or comparable) reachable for VM orchestration.
//   - Kernel + rootfs images published to the configured registry.
//
// See docs/sandbox-adapters.md for the v2 deployment plan.
package firecracker

import (
	"context"
	"fmt"

	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
)

// ErrFirecrackerRequiresFlintlock is the sentinel returned by Run / Stream /
// Stop until the orchestrator integration ships. Wraps
// sandbox.ErrAdapterUnavailable so callers can branch on errors.Is.
var ErrFirecrackerRequiresFlintlock = fmt.Errorf(
	"%w: firecracker sandbox requires Flintlock orchestration; see docs/sandbox-adapters.md",
	sandbox.ErrAdapterUnavailable,
)

// Config holds adapter settings.
//
// All fields are currently unused — the adapter returns the sentinel
// regardless. They're declared so the public surface doesn't churn when
// the orchestrator integration lands.
type Config struct {
	// FlintlockEndpoint is the gRPC address of the Flintlock orchestrator
	// (e.g. "flintlock:9090"). Empty in the stub.
	FlintlockEndpoint string

	// KernelImage / RootFSImage are the OCI references for the VM kernel
	// and root filesystem. Empty in the stub.
	KernelImage  string
	RootFSImage  string
}

// Adapter implements sandbox.Sandbox by returning a not-implemented sentinel
// from every operation. Capabilities() reports the production numbers we
// plan to support once Flintlock is wired up.
type Adapter struct {
	cfg Config
}

// compile-time interface check.
var _ sandbox.Sandbox = (*Adapter)(nil)

// New constructs the adapter. The stub never fails — if the host is
// missing Firecracker or Flintlock, the failure surfaces on the first
// Run call (clearer error site for callers).
func New(cfg Config) (*Adapter, error) {
	return &Adapter{cfg: cfg}, nil
}

// Run is a Phase 9.2 stub — returns ErrFirecrackerRequiresFlintlock.
func (a *Adapter) Run(ctx context.Context, spec sandbox.RunSpec) (*sandbox.RunResult, error) {
	return nil, ErrFirecrackerRequiresFlintlock
}

// Stream is a Phase 9.2 stub — returns ErrFirecrackerRequiresFlintlock.
func (a *Adapter) Stream(ctx context.Context, spec sandbox.RunSpec) (<-chan sandbox.LogLine, <-chan *sandbox.RunResult, error) {
	return nil, nil, ErrFirecrackerRequiresFlintlock
}

// Stop is a Phase 9.2 stub — returns ErrFirecrackerRequiresFlintlock.
func (a *Adapter) Stop(ctx context.Context, runID string) error {
	return ErrFirecrackerRequiresFlintlock
}

// Capabilities reports the production numbers we plan to support.
//
// max_timeout_s = 24h (we allow long-running jobs in micro-VMs); GPU
// support tracks whether the host has SR-IOV / passthrough configured
// (we report true here so the dashboard knows the adapter CAN do it —
// per-host realities are checked at scheduling time).
func (a *Adapter) Capabilities() sandbox.Capabilities {
	return sandbox.Capabilities{
		Adapter:         "firecracker",
		MaxTimeoutS:     sandbox.MaxTimeoutS,
		SupportsGPU:     true,
		SupportsNetwork: true,
		SupportsMounts:  true,
		ColdStartMS:     5000, // typical Firecracker boot + rootfs mount
	}
}

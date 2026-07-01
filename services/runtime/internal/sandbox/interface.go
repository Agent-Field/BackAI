// SPDX-License-Identifier: Apache-2.0

// Package sandbox is AF Stack's sandboxed code-execution layer.
//
// A Sandbox spins up a short-lived isolated environment (Docker
// container in v1; future adapters: gVisor, Firecracker, e2b) to run an
// arbitrary image + command, capture stdout/stderr, meter
// cpu/memory/network, and persist the artefacts to object storage. The
// Service struct in service.go orchestrates: validate -> insert ledger
// row at status=queued -> hand off to the adapter -> write the terminal
// row -> emit a cost event.
//
// Contract notes
//
//	The wire schemas live in apps/dashboard/src/lib/api.ts (SandboxRunSchema,
//	SandboxRunListSchema, SandboxRunInputSchema, SandboxPoolStatsSchema,
//	SandboxCapabilitiesSchema). Field names, enum values, and nullable
//	semantics here MUST mirror those zod schemas — the dashboard's
//	safeParse will reject anything drifting.
//
//	Terminal status values: 'done' (exit_code==0), 'failed' (exit_code!=0
//	or container error), 'timeout' (deadline hit), 'killed' (operator
//	stop). Non-terminal: 'queued' (row inserted, container not started),
//	'running' (container alive).
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Status is the lifecycle phase of a sandbox run.
type Status string

const (
	StatusQueued  Status = "queued"
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
	StatusTimeout Status = "timeout"
	StatusKilled  Status = "killed"
)

// IsTerminal reports whether the status represents a finished run.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusDone, StatusFailed, StatusTimeout, StatusKilled:
		return true
	default:
		return false
	}
}

// NetworkMode controls outbound network access for a run.
//
//   - "open"       — full egress (the default)
//   - "restricted" — per-host egress allowlist. NOT YET IMPLEMENTED: Validate
//     rejects it rather than silently granting full egress.
//   - "isolated"   — no network at all
type NetworkMode string

const (
	NetworkOpen       NetworkMode = "open"
	NetworkRestricted NetworkMode = "restricted"
	NetworkIsolated   NetworkMode = "isolated"
)

// ErrAdapterUnavailable is returned by adapters that are present as a
// scaffold but not wired to a real backend yet (e.g. firecracker without
// a Flintlock orchestrator). The REST layer translates this to a 503.
var ErrAdapterUnavailable = errors.New("sandbox: adapter unavailable")

// RunSpec is the input to a single sandbox run. Mirrors
// SandboxRunInputSchema with defaults applied.
//
// Files is a map of in-container path -> file contents (text). The
// adapter writes each path under a working directory before starting
// the container. Binary files should be base64-encoded by the caller
// (the wire schema is string-valued).
type RunSpec struct {
	// ID is the suite_sandbox_runs row id. The Service fills this in
	// before calling the adapter so the adapter can use it to name
	// the container / storage prefix / log stream subscription key.
	ID string

	// TenantID is empty when the caller is unauthenticated; the adapter
	// does not consume it but it ends up in the cost event.
	TenantID    string
	WorkspaceID string

	Image   string            // e.g. "python:3.12-slim"
	Command []string          // argv
	Files   map[string]string // path -> contents, written before exec
	Env     map[string]string

	TimeoutS    int
	CPU         int
	MemoryGB    int
	Network     NetworkMode
	AllowEgress []string
}

// RunResult is the terminal outcome of a sandbox run.
//
// StdoutURL and StderrURL point to the persisted log objects (object
// storage). ArtifactsURL is reserved for Phase 9 follow-ups that
// preserve produced files; v1 leaves it empty.
//
// Error carries any infrastructure failure (couldn't pull image,
// docker daemon went away). Distinguished from a non-zero exit code:
// a process that exits 1 is StatusFailed with Error=nil; a daemon
// failure is StatusFailed with Error set.
type RunResult struct {
	Status          Status
	ExitCode        int
	DurationS       int
	CPUSeconds      float64
	MemoryPeakMB    int
	NetworkBytesIn  int64
	NetworkBytesOut int64
	StdoutURL       string
	StderrURL       string
	ArtifactsURL    string
	StartedAt       time.Time
	EndedAt         time.Time

	Error error
}

// LogLine is one streamed line of stdout / stderr emitted while the
// container runs. Stream is "stdout" or "stderr".
type LogLine struct {
	TS     time.Time
	Stream string
	Text   string
}

// Capabilities reports adapter-level limits + features. Mirrors
// SandboxCapabilitiesSchema in apps/dashboard/src/lib/api.ts.
type Capabilities struct {
	Adapter         string `json:"adapter"`
	MaxTimeoutS     int    `json:"max_timeout_s"`
	SupportsGPU     bool   `json:"supports_gpu"`
	SupportsNetwork bool   `json:"supports_network"`
	SupportsMounts  bool   `json:"supports_mounts"`
	ColdStartMS     int    `json:"cold_start_ms"`
}

// Sandbox is the adapter contract.
//
// All methods take a context and respect cancellation. Implementations
// must be safe for concurrent use from multiple goroutines.
type Sandbox interface {
	// Run executes the spec to completion and returns the terminal
	// RunResult. Errors are reserved for "couldn't even attempt";
	// a container that exited non-zero is reported via
	// RunResult.Status (= StatusFailed) + ExitCode != 0, error = nil.
	Run(ctx context.Context, spec RunSpec) (*RunResult, error)

	// Stream executes the spec and concurrently streams stdout / stderr
	// log lines on the returned channel. The lines channel is closed
	// when the run terminates; the final RunResult is delivered on the
	// result channel (one value, then closed).
	Stream(ctx context.Context, spec RunSpec) (<-chan LogLine, <-chan *RunResult, error)

	// Stop signals a running container to terminate. id is RunSpec.ID,
	// NOT the adapter-internal container id.
	Stop(ctx context.Context, runID string) error

	// Capabilities reports the static caps of this adapter.
	Capabilities() Capabilities
}

// SandboxRun is the persisted row shape returned to clients. Mirrors
// SandboxRunSchema exactly. Nullable wire fields use pointer types so
// json.Marshal emits null rather than the zero value.
type SandboxRun struct {
	ID              string   `json:"id"`
	TenantID        *string  `json:"tenant_id"`
	WorkspaceID     *string  `json:"workspace_id"`
	Adapter         string   `json:"adapter"`
	Image           string   `json:"image"`
	Command         []string `json:"command"`
	Status          Status   `json:"status"`
	ExitCode        *int     `json:"exit_code"`
	DurationS       *int     `json:"duration_s"`
	CPUSeconds      *float64 `json:"cpu_seconds"`
	MemoryPeakMB    *int     `json:"memory_peak_mb"`
	NetworkBytesIn  *int64   `json:"network_bytes_in"`
	NetworkBytesOut *int64   `json:"network_bytes_out"`
	CostUSD         *float64 `json:"cost_usd"`
	StdoutURL       *string  `json:"stdout_url"`
	StderrURL       *string  `json:"stderr_url"`
	ArtifactsURL    *string  `json:"artifacts_url"`
	StartedAt       *string  `json:"started_at"`
	EndedAt         *string  `json:"ended_at"`
	CreatedAt       string   `json:"created_at"`
}

// ListFilters mirrors the query params on GET /api/v1/sandbox/runs.
type ListFilters struct {
	TenantID string
	Status   string
	Limit    int
	Offset   int
}

// ListResult is one page of suite_sandbox_runs.
type ListResult struct {
	Runs    []SandboxRun
	Total   int
	HasMore bool
}

// PoolStats mirrors SandboxPoolStatsSchema.
type PoolStats struct {
	Adapter         string       `json:"adapter"`
	Warm            int          `json:"warm"`
	Active          int          `json:"active"`
	Queued          int          `json:"queued"`
	TotalRunsToday  int          `json:"total_runs_today"`
	CPUSecondsToday float64      `json:"cpu_seconds_today"`
	CostUSDToday    float64      `json:"cost_usd_today"`
	Capabilities    Capabilities `json:"capabilities"`
}

// ─── Defaults + validation ────────────────────────────────────────────────

// Defaults from SandboxRunInputSchema. Matched by name so a contract
// drift is grep-able.
const (
	DefaultTimeoutS = 300
	DefaultCPU      = 2
	DefaultMemoryGB = 4

	MaxTimeoutS   = 86400
	MaxCPU        = 32
	MaxMemoryGB   = 64
	MaxCommandLen = 4096
	MaxImageLen   = 512
)

// Sentinel errors. The REST layer maps each to a code + status.
var (
	ErrInvalidSpec   = errors.New("sandbox: invalid spec")
	ErrNotConfigured = errors.New("sandbox: not configured")
	ErrNotFound      = errors.New("sandbox: run not found")
	ErrAdapter       = errors.New("sandbox: adapter error")
)

// Validate normalises the spec in-place (applies defaults, lowercases
// network) and returns an error if any field is out of bounds. Called
// by Service.Run before touching the DB or the adapter.
func (s *RunSpec) Validate() error {
	if s == nil {
		return fmt.Errorf("%w: nil spec", ErrInvalidSpec)
	}
	s.Image = strings.TrimSpace(s.Image)
	if s.Image == "" {
		return fmt.Errorf("%w: image is required", ErrInvalidSpec)
	}
	if len(s.Image) > MaxImageLen {
		return fmt.Errorf("%w: image name exceeds %d chars", ErrInvalidSpec, MaxImageLen)
	}
	// Reject obviously-bad image refs: whitespace, control chars, shell
	// metacharacters. The docker client validates further (and rejects
	// invalid manifests), but this catches user-pasted nonsense before
	// we even touch the daemon.
	for _, r := range s.Image {
		if r <= 0x20 || r == 0x7f {
			return fmt.Errorf("%w: image contains whitespace or control char", ErrInvalidSpec)
		}
		switch r {
		case '`', '$', '\\', '"', '\'', ';', '|', '&', '<', '>', '(', ')':
			return fmt.Errorf("%w: image contains shell metacharacter %q", ErrInvalidSpec, r)
		}
	}
	if len(s.Command) == 0 {
		return fmt.Errorf("%w: command is required", ErrInvalidSpec)
	}
	totalCmdLen := 0
	for _, p := range s.Command {
		totalCmdLen += len(p)
	}
	if totalCmdLen > MaxCommandLen {
		return fmt.Errorf("%w: command exceeds %d bytes", ErrInvalidSpec, MaxCommandLen)
	}

	if s.TimeoutS == 0 {
		s.TimeoutS = DefaultTimeoutS
	}
	if s.TimeoutS < 1 || s.TimeoutS > MaxTimeoutS {
		return fmt.Errorf("%w: timeout_s must be in [1, %d]", ErrInvalidSpec, MaxTimeoutS)
	}
	if s.CPU == 0 {
		s.CPU = DefaultCPU
	}
	if s.CPU < 1 || s.CPU > MaxCPU {
		return fmt.Errorf("%w: cpu must be in [1, %d]", ErrInvalidSpec, MaxCPU)
	}
	if s.MemoryGB == 0 {
		s.MemoryGB = DefaultMemoryGB
	}
	if s.MemoryGB < 1 || s.MemoryGB > MaxMemoryGB {
		return fmt.Errorf("%w: memory_gb must be in [1, %d]", ErrInvalidSpec, MaxMemoryGB)
	}
	switch s.Network {
	case "":
		// Default to open. Per-host egress filtering ("restricted") is not
		// implemented yet, and we will not silently grant full egress under
		// that name (which is what the old default did). Callers that want no
		// egress must ask for "isolated" explicitly.
		s.Network = NetworkOpen
	case NetworkOpen, NetworkIsolated:
		// ok
	case NetworkRestricted:
		return fmt.Errorf("%w: network=restricted is not yet supported (per-host egress filtering is unimplemented); use network=isolated for no egress or network=open for full egress", ErrInvalidSpec)
	default:
		return fmt.Errorf("%w: network must be open|isolated (restricted is not yet supported)", ErrInvalidSpec)
	}
	return nil
}

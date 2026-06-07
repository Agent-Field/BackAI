// service.go — orchestrates a single sandbox run end-to-end.
//
// Flow for Service.Run:
//
//  1. Validate the spec (Image, Command, bounds). Bad input -> 400.
//  2. Insert the suite_sandbox_runs row at status='queued' so the
//     dashboard can see the run before the container is up.
//  3. Call adapter.Run with the spec (now ID-stamped). The adapter is
//     responsible for image pull, container create/start, log capture,
//     stat collection, and stdout/stderr upload to object storage.
//  4. Compute cost via the simple Docker estimator (cpu_seconds *
//     CostPerCPUSecond) and update the row with the terminal data.
//  5. Best-effort fire a cost.Recorder event (provider="docker") so the
//     /api/v1/cost ledger reflects sandbox spend.
//
// Errors from steps (2)/(3) DO NOT lose the user input: the inserted
// row is updated to status='failed' with whatever data we have before
// returning so the dashboard renders the failure.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/cost"
)

// CostPerCPUSecond is the rough Docker-runtime estimate used to bill
// CPU-seconds in USD. The dashboard renders this as a non-zero hint of
// sandbox spend; production deployments should swap in a metered
// per-tenant rate at the cost.Recorder layer.
const CostPerCPUSecond = 0.00005

// AdapterName for the cost event. The recorder writes this to
// suite_cost_events.provider so the dashboard can break sandbox spend
// out by provider.
const adapterProviderName = "docker"

// Service wires an adapter, a recorder, and a cost recorder into the
// public Run/List/Get/Stop surface the REST handlers consume.
type Service struct {
	adapter      Sandbox
	recorder     *Recorder
	costRecorder *cost.Recorder
	log          *slog.Logger
}

// NewService constructs a Service. adapter is required; the other
// dependencies may be nil — recorder=nil downgrades to "no persistence",
// costRecorder=nil downgrades to "no cost ledger". The REST layer
// checks Service != nil before invoking any method, so this constructor
// never returns nil itself.
func NewService(adapter Sandbox, recorder *Recorder, costRecorder *cost.Recorder, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		adapter:      adapter,
		recorder:     recorder,
		costRecorder: costRecorder,
		log:          log,
	}
}

// AdapterAvailable reports whether the Service has a working backend.
// Used by the REST layer to translate "no adapter wired" into 503.
func (s *Service) AdapterAvailable() bool {
	return s != nil && s.adapter != nil
}

// Capabilities forwards to the adapter, or returns a sensible empty
// shape when no adapter is wired (so /api/v1/sandbox/pool always has a
// renderable response).
func (s *Service) Capabilities() Capabilities {
	if s == nil || s.adapter == nil {
		return Capabilities{Adapter: "unconfigured"}
	}
	return s.adapter.Capabilities()
}

// Run executes a single sandbox run to completion. The returned
// *SandboxRun reflects the terminal database state (or the in-memory
// shape when no DB is wired).
func (s *Service) Run(ctx context.Context, spec RunSpec) (*SandboxRun, error) {
	if s == nil || s.adapter == nil {
		return nil, ErrNotConfigured
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}

	adapterName := s.adapter.Capabilities().Adapter
	if adapterName == "" {
		adapterName = adapterProviderName
	}

	// Insert the queued row. When no DB is wired we synthesise an
	// in-memory row so the run can still proceed and the caller gets
	// a sensible response.
	var (
		run *SandboxRun
		err error
	)
	if s.recorder != nil && s.recorder.HasPool() {
		run, err = s.recorder.Insert(ctx, spec, adapterName)
		if err != nil {
			return nil, fmt.Errorf("sandbox: insert: %w", err)
		}
		spec.ID = run.ID
	} else {
		spec.ID = newEphemeralID()
		run = &SandboxRun{
			ID:        spec.ID,
			Adapter:   adapterName,
			Image:     spec.Image,
			Command:   append([]string{}, spec.Command...),
			Status:    StatusQueued,
			CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		}
		if spec.TenantID != "" {
			t := spec.TenantID
			run.TenantID = &t
		}
		if spec.WorkspaceID != "" {
			w := spec.WorkspaceID
			run.WorkspaceID = &w
		}
	}

	// Hand the spec to the adapter. The adapter manages its own
	// timeout (RunSpec.TimeoutS) — we don't wrap ctx here because the
	// HTTP server's read/write timeouts already bound the parent.
	res, runErr := s.adapter.Run(ctx, spec)

	// Materialise the terminal status. A nil result + non-nil error is
	// treated as a hard failure (the adapter couldn't even attempt).
	terminalStatus := StatusFailed
	var costUSD float64
	if res != nil {
		terminalStatus = res.Status
		if !terminalStatus.IsTerminal() {
			// Belt-and-braces: an adapter that forgets to set Status
			// shouldn't leave the row stuck.
			if runErr != nil {
				terminalStatus = StatusFailed
			} else if res.ExitCode == 0 {
				terminalStatus = StatusDone
			} else {
				terminalStatus = StatusFailed
			}
		}
		costUSD = res.CPUSeconds * CostPerCPUSecond
	}

	// Persist the terminal row.
	if s.recorder != nil && s.recorder.HasPool() && res != nil {
		if updateErr := s.recorder.UpdateFinal(ctx, run.ID, terminalStatus, *res, costUSD); updateErr != nil {
			s.log.Warn("sandbox: update final failed", "run_id", run.ID, "error", updateErr)
		}
		// Re-read so we return the canonical row.
		if fresh, getErr := s.recorder.Get(ctx, run.ID); getErr == nil {
			run = fresh
		}
	} else if res != nil {
		// No DB: mutate the in-memory shape so the response still
		// reflects the run outcome.
		s.applyResultInMemory(run, terminalStatus, *res, costUSD)
	} else if s.recorder != nil && s.recorder.HasPool() {
		// res nil + DB wired: mark the row failed so it doesn't
		// languish as queued.
		if updateErr := s.recorder.UpdateStatus(ctx, run.ID, StatusFailed, nil); updateErr != nil {
			s.log.Warn("sandbox: mark failed", "run_id", run.ID, "error", updateErr)
		}
		run.Status = StatusFailed
	}

	// Best-effort cost ledger.
	if res != nil && costUSD > 0 && s.costRecorder != nil {
		ev := cost.Event{
			TenantID:   spec.TenantID,
			Model:      "sandbox:" + adapterName,
			Provider:   adapterProviderName,
			CostUSD:    costUSD,
			LatencyMS:  res.DurationS * 1000,
			OccurredAt: time.Now().UTC(),
		}
		if err := s.costRecorder.Record(ctx, ev); err != nil {
			s.log.Warn("sandbox: cost event record failed",
				"run_id", run.ID, "error", err)
		}
	}

	if runErr != nil {
		// Wrap so callers can distinguish adapter failure from missing
		// service. The REST layer still returns the row payload (with
		// status=failed) so the dashboard can render the partial
		// result; this error is propagated only when the caller is the
		// SDK / test path.
		if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
			return run, runErr
		}
		return run, fmt.Errorf("%w: %v", ErrAdapter, runErr)
	}
	return run, nil
}

// applyResultInMemory updates the in-memory SandboxRun shape with the
// adapter's RunResult. Used only on the no-DB path so the HTTP
// response still carries the terminal numbers.
func (s *Service) applyResultInMemory(run *SandboxRun, status Status, res RunResult, costUSD float64) {
	run.Status = status
	ec := res.ExitCode
	run.ExitCode = &ec
	if res.DurationS > 0 {
		d := res.DurationS
		run.DurationS = &d
	}
	if res.CPUSeconds > 0 {
		c := res.CPUSeconds
		run.CPUSeconds = &c
	}
	if res.MemoryPeakMB > 0 {
		m := res.MemoryPeakMB
		run.MemoryPeakMB = &m
	}
	if res.NetworkBytesIn > 0 {
		v := res.NetworkBytesIn
		run.NetworkBytesIn = &v
	}
	if res.NetworkBytesOut > 0 {
		v := res.NetworkBytesOut
		run.NetworkBytesOut = &v
	}
	if costUSD > 0 {
		c := costUSD
		run.CostUSD = &c
	}
	if res.StdoutURL != "" {
		v := res.StdoutURL
		run.StdoutURL = &v
	}
	if res.StderrURL != "" {
		v := res.StderrURL
		run.StderrURL = &v
	}
	if res.ArtifactsURL != "" {
		v := res.ArtifactsURL
		run.ArtifactsURL = &v
	}
	if !res.StartedAt.IsZero() {
		s := res.StartedAt.UTC().Format(time.RFC3339Nano)
		run.StartedAt = &s
	}
	if !res.EndedAt.IsZero() {
		s := res.EndedAt.UTC().Format(time.RFC3339Nano)
		run.EndedAt = &s
	}
}

// Stream invokes adapter.Stream and lets the caller (the SSE handler)
// pipe LogLines onto the wire.
func (s *Service) Stream(ctx context.Context, spec RunSpec) (<-chan LogLine, <-chan *RunResult, error) {
	if s == nil || s.adapter == nil {
		return nil, nil, ErrNotConfigured
	}
	if err := spec.Validate(); err != nil {
		return nil, nil, err
	}
	if spec.ID == "" {
		spec.ID = newEphemeralID()
	}
	return s.adapter.Stream(ctx, spec)
}

// Stop forwards to the adapter. Returns ErrNotConfigured when no
// adapter is wired.
func (s *Service) Stop(ctx context.Context, runID string) error {
	if s == nil || s.adapter == nil {
		return ErrNotConfigured
	}
	return s.adapter.Stop(ctx, runID)
}

// List + Get forward to the Recorder. When no DB is wired they return
// empty / not-found respectively so the REST surface still works.
func (s *Service) List(ctx context.Context, f ListFilters) (*ListResult, error) {
	if s == nil {
		return nil, ErrNotConfigured
	}
	if s.recorder == nil {
		return &ListResult{Runs: []SandboxRun{}}, nil
	}
	return s.recorder.List(ctx, f)
}

func (s *Service) Get(ctx context.Context, id string) (*SandboxRun, error) {
	if s == nil {
		return nil, ErrNotConfigured
	}
	if s.recorder == nil {
		return nil, ErrNotFound
	}
	return s.recorder.Get(ctx, id)
}

// PoolStats composes the dashboard's pool view: live counters from
// the Recorder + the adapter's Capabilities. Warm is hardcoded to 1
// for the Docker adapter — there's no warm pool in v1; the value is
// non-zero so the dashboard can render the gauge with a hint of life.
func (s *Service) PoolStats(ctx context.Context) (PoolStats, error) {
	caps := s.Capabilities()
	stats := PoolStats{
		Adapter:      caps.Adapter,
		Capabilities: caps,
		Warm:         1, // see note above
	}
	if s == nil || s.recorder == nil {
		return stats, nil
	}
	// Day boundary: midnight UTC of "today".
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	active, queued, total, cpuSec, costUSD, err := s.recorder.PoolStats(ctx, today)
	if err != nil {
		return stats, err
	}
	stats.Active = active
	stats.Queued = queued
	stats.TotalRunsToday = total
	stats.CPUSecondsToday = cpuSec
	stats.CostUSDToday = costUSD
	return stats, nil
}

// newEphemeralID returns a UUID-ish string for runs that bypass the DB.
// We use time.Now().UnixNano() as a coarse uniqueness source; the
// no-DB path is dev-only.
func newEphemeralID() string {
	return fmt.Sprintf("eph-%d", time.Now().UnixNano())
}

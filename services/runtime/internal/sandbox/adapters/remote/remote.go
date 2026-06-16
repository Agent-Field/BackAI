// SPDX-License-Identifier: Apache-2.0

// Package remote implements sandbox.Sandbox by talking the HTTP
// protocol defined in docs/adapters/protocols/sandbox-v1.md to a
// sidecar adapter. Selected via AF_STACK_SANDBOX_ADAPTER=remote.
//
// One Adapter per AF_STACK_SANDBOX_ADAPTER_URL. Goroutine-safe; the
// underlying remote.Client owns the connection pool.
package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/adapters/remote"
	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
)

// Adapter is a sandbox.Sandbox backed by a remote sidecar speaking the
// sandbox-v1 protocol.
type Adapter struct {
	client *remote.Client
	caps   sandbox.Capabilities
	name   string // active adapter name from /v1/capabilities

	// CachedAt is the time we last refreshed capabilities. Exposed
	// for tests and the operator-facing /admin/adapters status.
	CachedAt time.Time
}

// Compile-time interface check.
var _ sandbox.Sandbox = (*Adapter)(nil)

// Config is the env-driven configuration. Use NewFromEnv for the
// canonical wire-up.
type Config struct {
	BaseURL    string
	Token      string
	Timeout    time.Duration
	MaxRetries int
}

// New returns an Adapter for the given config. Performs a synchronous
// GET /v1/capabilities to verify the sidecar speaks the protocol; if
// that call fails, returns the error so the runtime can refuse to start
// (rather than discovering the problem on the first user request).
func New(ctx context.Context, cfg Config) (*Adapter, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("sandbox/remote: BaseURL required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 2
	}
	c, err := remote.NewClient(remote.Config{
		BaseURL:    cfg.BaseURL,
		Token:      cfg.Token,
		Timeout:    cfg.Timeout,
		MaxRetries: cfg.MaxRetries,
	})
	if err != nil {
		return nil, fmt.Errorf("sandbox/remote: client: %w", err)
	}

	a := &Adapter{client: c}
	if err := a.refreshCapabilities(ctx); err != nil {
		return nil, fmt.Errorf("sandbox/remote: capabilities probe: %w", err)
	}
	return a, nil
}

// refreshCapabilities pulls /v1/capabilities and decodes the slot-
// specific shape into our typed sandbox.Capabilities.
func (a *Adapter) refreshCapabilities(ctx context.Context) error {
	resp, err := a.client.Capabilities(ctx)
	if err != nil {
		return err
	}
	if resp.Slot != "sandbox" {
		return fmt.Errorf("sandbox/remote: adapter reports slot=%q; expected sandbox", resp.Slot)
	}
	a.name = resp.Name
	var caps sandboxCaps
	if len(resp.Capabilities) > 0 {
		if err := json.Unmarshal(resp.Capabilities, &caps); err != nil {
			return fmt.Errorf("sandbox/remote: decode capabilities: %w", err)
		}
	}
	a.caps = sandbox.Capabilities{
		Adapter:         resp.Name,
		MaxTimeoutS:     caps.MaxTimeoutS,
		SupportsGPU:     caps.SupportsGPU,
		SupportsNetwork: caps.SupportsNetwork,
		SupportsMounts:  caps.SupportsMounts,
		ColdStartMS:     caps.ColdStartMS,
	}
	a.CachedAt = time.Now()
	return nil
}

// Name returns the active adapter's name (from /v1/capabilities). Used
// by the registry to populate /api/v1/admin/adapters.
func (a *Adapter) Name() string {
	if a == nil {
		return ""
	}
	return a.name
}

// Capabilities returns the cached typed capabilities. The underlying
// HTTP fetch happens at New() and on demand via RefreshCapabilities.
func (a *Adapter) Capabilities() sandbox.Capabilities {
	if a == nil {
		return sandbox.Capabilities{}
	}
	return a.caps
}

// RawCapabilities returns the raw JSON payload as received from
// /v1/capabilities — useful for the adapter registry to forward to the
// dashboard without re-encoding.
func (a *Adapter) RawCapabilities(ctx context.Context) (json.RawMessage, error) {
	resp, err := a.client.Capabilities(ctx)
	if err != nil {
		return nil, err
	}
	return resp.Capabilities, nil
}

// Probe satisfies the registry.Probe contract by calling /healthz.
func (a *Adapter) Probe(ctx context.Context) (string, error) {
	h, err := a.client.Health(ctx)
	if err != nil {
		return "unhealthy", err
	}
	return h.Status, nil
}

// Run implements sandbox.Sandbox by posting the spec to /v1/runs.
func (a *Adapter) Run(ctx context.Context, spec sandbox.RunSpec) (*sandbox.RunResult, error) {
	body := specToWire(spec)
	resp, err := a.client.Do(ctx, remote.Request{
		Method:         http.MethodPost,
		Path:           "/v1/runs",
		Body:           body,
		TenantID:       spec.TenantID,
		IdempotencyKey: spec.ID, // run id is the natural idempotency key
	})
	if err != nil {
		return nil, err
	}
	var wire runResultWire
	if err := resp.DecodeJSON(&wire); err != nil {
		return nil, fmt.Errorf("sandbox/remote: decode result: %w", err)
	}
	return wireToResult(wire), nil
}

// Stream implements sandbox.Sandbox via /v1/runs/stream. The returned
// channels mirror the in-tree adapters: the line channel emits each
// LogLine as it arrives, and a single *RunResult is delivered on the
// result channel. Both channels close when the stream ends.
//
// We deliberately keep the channels unbuffered — back-pressure flows
// upstream naturally. If the consumer is slow, the adapter slows down.
func (a *Adapter) Stream(ctx context.Context, spec sandbox.RunSpec) (<-chan sandbox.LogLine, <-chan *sandbox.RunResult, error) {
	body := specToWire(spec)
	resp, err := a.client.Do(ctx, remote.Request{
		Method:         http.MethodPost,
		Path:           "/v1/runs/stream",
		Body:           body,
		TenantID:       spec.TenantID,
		IdempotencyKey: spec.ID,
		Stream:         true,
	})
	if err != nil {
		return nil, nil, err
	}

	lines := make(chan sandbox.LogLine)
	results := make(chan *sandbox.RunResult, 1)

	go func() {
		defer close(lines)
		defer close(results)
		defer resp.Body.Close()

		for ev := range resp.Events(ctx) {
			if len(ev.Data) == 0 {
				continue
			}
			// Two event shapes: log line or terminated.
			var probe struct {
				Event string `json:"event"`
			}
			_ = json.Unmarshal(ev.Data, &probe)
			if probe.Event == "terminated" {
				var wire runResultWire
				if err := json.Unmarshal(ev.Data, &wire); err == nil {
					results <- wireToResult(wire)
				}
				return
			}
			var line logLineWire
			if err := json.Unmarshal(ev.Data, &line); err == nil && line.Text != "" {
				ts, _ := time.Parse(time.RFC3339Nano, line.TS)
				select {
				case <-ctx.Done():
					return
				case lines <- sandbox.LogLine{TS: ts, Stream: line.Stream, Text: line.Text}:
				}
			}
		}
		// Stream ended without a terminated event. Deliver a
		// best-effort failed result so callers don't block forever.
		results <- &sandbox.RunResult{
			Status:  sandbox.StatusFailed,
			Error:   errors.New("sandbox/remote: stream ended without terminated event"),
			EndedAt: time.Now(),
		}
	}()

	return lines, results, nil
}

// Stop implements sandbox.Sandbox by DELETE /v1/runs/{id}.
func (a *Adapter) Stop(ctx context.Context, runID string) error {
	if runID == "" {
		return errors.New("sandbox/remote: runID required")
	}
	resp, err := a.client.Do(ctx, remote.Request{
		Method: http.MethodDelete,
		Path:   "/v1/runs/" + url.PathEscape(runID),
	})
	if err != nil {
		// 404 on cancel is fine — caller doesn't need to know.
		if remote.IsCode(err, "run_not_found") || remote.IsCode(err, "not_found") {
			return nil
		}
		return err
	}
	_ = resp.DecodeJSON(&struct{}{})
	return nil
}

// Pool fetches GET /v1/pool stats. Not on the Sandbox interface — the
// dashboard reaches it through the runtime's existing /api/v1/sandbox/pool
// handler, which calls this method when the active adapter is remote.
func (a *Adapter) Pool(ctx context.Context) (*sandbox.PoolStats, error) {
	resp, err := a.client.Do(ctx, remote.Request{Method: http.MethodGet, Path: "/v1/pool"})
	if err != nil {
		return nil, err
	}
	var wire poolWire
	if err := resp.DecodeJSON(&wire); err != nil {
		return nil, fmt.Errorf("sandbox/remote: decode pool: %w", err)
	}
	return &sandbox.PoolStats{
		Adapter:         wire.Adapter,
		Warm:            wire.Warm,
		Active:          wire.Active,
		Queued:          wire.Queued,
		TotalRunsToday:  wire.TotalRunsToday,
		CPUSecondsToday: wire.CPUSecondsToday,
		CostUSDToday:    wire.CostUSDToday,
		Capabilities:    a.caps,
	}, nil
}

// --- wire shapes ---------------------------------------------------------

type sandboxCaps struct {
	MaxTimeoutS         int      `json:"max_timeout_s"`
	SupportsGPU         bool     `json:"supports_gpu"`
	SupportsNetwork     bool     `json:"supports_network"`
	SupportsMounts      bool     `json:"supports_mounts"`
	SupportsStreaming   bool     `json:"supports_streaming"`
	ColdStartMS         int      `json:"cold_start_ms"`
	ImagePullRequired   bool     `json:"image_pull_required"`
	MaxCPU              int      `json:"max_cpu"`
	MaxMemoryGB         int      `json:"max_memory_gb"`
	NetworkModes        []string `json:"network_modes"`
	AllowEgressSupported bool    `json:"allow_egress_supported"`
	ArtifactsUpload     bool     `json:"artifacts_upload"`
}

// runSpecWire mirrors sandbox.RunSpec in the wire shape from the protocol.
type runSpecWire struct {
	ID          string            `json:"id"`
	TenantID    string            `json:"tenant_id,omitempty"`
	WorkspaceID string            `json:"workspace_id,omitempty"`
	Image       string            `json:"image"`
	Command     []string          `json:"command"`
	Files       map[string]string `json:"files,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	TimeoutS    int               `json:"timeout_s"`
	CPU         int               `json:"cpu"`
	MemoryGB    int               `json:"memory_gb"`
	Network     string            `json:"network"`
	AllowEgress []string          `json:"allow_egress,omitempty"`
}

func specToWire(s sandbox.RunSpec) runSpecWire {
	return runSpecWire{
		ID:          s.ID,
		TenantID:    s.TenantID,
		WorkspaceID: s.WorkspaceID,
		Image:       s.Image,
		Command:     s.Command,
		Files:       s.Files,
		Env:         s.Env,
		TimeoutS:    s.TimeoutS,
		CPU:         s.CPU,
		MemoryGB:    s.MemoryGB,
		Network:     string(s.Network),
		AllowEgress: s.AllowEgress,
	}
}

type runResultWire struct {
	Status          string  `json:"status"`
	ExitCode        int     `json:"exit_code"`
	DurationS       int     `json:"duration_s"`
	CPUSeconds      float64 `json:"cpu_seconds"`
	MemoryPeakMB    int     `json:"memory_peak_mb"`
	NetworkBytesIn  int64   `json:"network_bytes_in"`
	NetworkBytesOut int64   `json:"network_bytes_out"`
	StdoutURL       string  `json:"stdout_url"`
	StderrURL       string  `json:"stderr_url"`
	ArtifactsURL    string  `json:"artifacts_url"`
	StartedAt       string  `json:"started_at"`
	EndedAt         string  `json:"ended_at"`
}

func wireToResult(w runResultWire) *sandbox.RunResult {
	out := &sandbox.RunResult{
		Status:          sandbox.Status(w.Status),
		ExitCode:        w.ExitCode,
		DurationS:       w.DurationS,
		CPUSeconds:      w.CPUSeconds,
		MemoryPeakMB:    w.MemoryPeakMB,
		NetworkBytesIn:  w.NetworkBytesIn,
		NetworkBytesOut: w.NetworkBytesOut,
		StdoutURL:       w.StdoutURL,
		StderrURL:       w.StderrURL,
		ArtifactsURL:    w.ArtifactsURL,
	}
	if t, err := time.Parse(time.RFC3339Nano, w.StartedAt); err == nil {
		out.StartedAt = t
	}
	if t, err := time.Parse(time.RFC3339Nano, w.EndedAt); err == nil {
		out.EndedAt = t
	}
	return out
}

type logLineWire struct {
	TS     string `json:"ts"`
	Stream string `json:"stream"`
	Text   string `json:"text"`
}

type poolWire struct {
	Adapter         string  `json:"adapter"`
	Warm            int     `json:"warm"`
	Active          int     `json:"active"`
	Queued          int     `json:"queued"`
	TotalRunsToday  int     `json:"total_runs_today"`
	CPUSecondsToday float64 `json:"cpu_seconds_today"`
	CostUSDToday    float64 `json:"cost_usd_today"`
}

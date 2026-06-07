// Package e2b implements the sandbox.Sandbox interface against e2b.dev's
// hosted sandbox service (https://e2b.dev).
//
// e2b runs Firecracker-based micro-VMs you don't have to operate
// yourself. Each Run() here translates to three HTTP calls against the
// e2b control plane: create sandbox -> optionally write input files ->
// exec command -> close sandbox. Logs come back inline on the exec
// response; the adapter surfaces them via the storage upload path that
// the Service handles (StdoutURL / StderrURL are filled by the caller,
// not the adapter itself — see service.go).
//
// PICK THIS ADAPTER WHEN
//
//   - You want sandbox isolation without operating Firecracker/Flintlock
//     yourself.
//   - You're OK with per-run cost (e2b prices per sandbox-second).
//   - Your max workload runtime fits inside e2b's 1-hour limit.
//
// CONFIGURATION
//
// Set in config.yaml as `sandbox: { adapter: e2b, e2b_api_key: ... }` or
// via env: AF_STACK_SANDBOX_ADAPTER=e2b + E2B_API_KEY=...
//
// PHASE 9.2 SCOPE
//
// This adapter ships the synchronous Run() path. Stream() (line-by-line
// over an SSE/WebSocket subscription) is stubbed; the real implementation
// will land alongside log-streaming end-to-end in Phase 9.3. Stop()
// best-effort kills the sandbox; cancellation mid-run is honoured via
// the context.
package e2b

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
)

// DefaultBaseURL is the production e2b control-plane endpoint.
const DefaultBaseURL = "https://api.e2b.dev"

// defaultTemplate is the e2b template image used when RunSpec.Image is
// empty. e2b uses opaque template IDs; "base" is their general-purpose
// Ubuntu image with common languages preinstalled.
const defaultTemplate = "base"

// httpTimeout caps the per-request HTTP timeout. The overall Run timeout
// comes from RunSpec.TimeoutS and is enforced via ctx.
const httpTimeout = 60 * time.Second

// ErrAPIKeyMissing is returned by New when no API key is configured.
var ErrAPIKeyMissing = errors.New("sandbox/e2b: api key required (set E2B_API_KEY)")

// Config holds adapter settings.
type Config struct {
	// APIKey is the e2b.dev API key. Required.
	APIKey string

	// BaseURL is the e2b control-plane endpoint. Empty -> DefaultBaseURL.
	BaseURL string

	// HTTPClient lets tests inject a mocked transport. nil -> a client
	// with httpTimeout.
	HTTPClient *http.Client
}

// Adapter implements sandbox.Sandbox via e2b's HTTP API.
type Adapter struct {
	apiKey  string
	baseURL string
	client  *http.Client

	// lastSandboxID + lastStdout + lastStderr cache the most recent
	// run's inline output. See cacheLogs / fetchCachedLogs.
	lastSandboxID string
	lastStdout    string
	lastStderr    string
}

// compile-time interface check.
var _ sandbox.Sandbox = (*Adapter)(nil)

// New constructs the adapter. Returns ErrAPIKeyMissing when APIKey is empty
// so callers fail fast at startup rather than on the first Run.
func New(cfg Config) (*Adapter, error) {
	if cfg.APIKey == "" {
		return nil, ErrAPIKeyMissing
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	// Trim trailing slash so URL composition is unambiguous.
	baseURL = strings.TrimRight(baseURL, "/")

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: httpTimeout}
	}
	return &Adapter{
		apiKey:  cfg.APIKey,
		baseURL: baseURL,
		client:  client,
	}, nil
}

// ───────────── wire types (request/response bodies) ─────────────

// createSandboxRequest is the body of POST /sandboxes.
type createSandboxRequest struct {
	Template  string            `json:"template"`
	TimeoutMS int               `json:"timeout_ms"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	EnvVars   map[string]string `json:"env_vars,omitempty"`
}

// createSandboxResponse is the response from POST /sandboxes.
type createSandboxResponse struct {
	SandboxID string `json:"sandbox_id"`
	// ClientID etc. omitted — we don't use them for one-shot Run.
}

// writeFilesRequest is the body of PUT /sandboxes/{id}/files.
type writeFilesRequest struct {
	Files []fileEntry `json:"files"`
}

// fileEntry is one path -> contents (base64-encoded) tuple.
type fileEntry struct {
	Path    string `json:"path"`
	Content string `json:"content_b64"`
}

// execRequest is the body of POST /sandboxes/{id}/exec.
type execRequest struct {
	Command   []string          `json:"command"`
	Env       map[string]string `json:"env,omitempty"`
	CWD       string            `json:"cwd,omitempty"`
	TimeoutMS int               `json:"timeout_ms"`
}

// execResponse is the response from POST /sandboxes/{id}/exec.
//
// e2b returns the full stdout/stderr inline on this synchronous endpoint.
// We pass these back to the Service through StdoutInline / StderrInline
// (non-public fields on the adapter side — Service uploads them and fills
// the URL fields on the final RunResult).
type execResponse struct {
	ExitCode   int     `json:"exit_code"`
	Stdout     string  `json:"stdout"`
	Stderr     string  `json:"stderr"`
	DurationMS int     `json:"duration_ms"`
	CPUSeconds float64 `json:"cpu_seconds"`
	MemPeakMB  int     `json:"memory_peak_mb"`
}

// ───────────── Sandbox interface implementation ─────────────

// Run creates an e2b sandbox, writes input files, executes the command,
// and closes the sandbox. The whole flow is bounded by ctx.
//
// Note on output: the e2b API returns stdout/stderr inline as strings.
// The sandbox.RunResult shape (Phase 9.1) only carries URL fields for
// stdout/stderr — the assumption is that the adapter persists log bytes
// to object storage and returns URLs. The Phase 9.2 scaffold leaves the
// URLs empty; the Service layer (or a follow-up patch) is responsible
// for routing the inline strings to storage. The inline strings are
// preserved on the adapter receiver in lastStdout/lastStderr so the
// Service can fetch them post-Run if needed.
func (a *Adapter) Run(ctx context.Context, spec sandbox.RunSpec) (*sandbox.RunResult, error) {
	startedAt := time.Now().UTC()

	timeoutMS := spec.TimeoutS * 1000
	if timeoutMS <= 0 {
		timeoutMS = 300 * 1000 // 5min default mirrors the dashboard default
	}

	// Step 1: create sandbox.
	template := spec.Image
	if template == "" {
		template = defaultTemplate
	}
	createBody := createSandboxRequest{
		Template:  template,
		TimeoutMS: timeoutMS,
		EnvVars:   spec.Env,
	}
	if spec.WorkspaceID != "" || spec.TenantID != "" || spec.ID != "" {
		createBody.Metadata = map[string]string{}
		if spec.ID != "" {
			createBody.Metadata["run_id"] = spec.ID
		}
		if spec.WorkspaceID != "" {
			createBody.Metadata["workspace_id"] = spec.WorkspaceID
		}
		if spec.TenantID != "" {
			createBody.Metadata["tenant_id"] = spec.TenantID
		}
	}
	var created createSandboxResponse
	if err := a.do(ctx, http.MethodPost, "/sandboxes", createBody, &created); err != nil {
		return nil, fmt.Errorf("sandbox/e2b: create: %w", err)
	}
	if created.SandboxID == "" {
		return nil, fmt.Errorf("sandbox/e2b: create returned empty sandbox_id")
	}

	// Always best-effort close the sandbox on exit, even on error paths.
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), httpTimeout)
		defer cancel()
		_ = a.do(closeCtx, http.MethodDelete, "/sandboxes/"+created.SandboxID, nil, nil)
	}()

	// Step 2: write input files (if any).
	if len(spec.Files) > 0 {
		files := make([]fileEntry, 0, len(spec.Files))
		for path, body := range spec.Files {
			files = append(files, fileEntry{
				Path:    path,
				Content: base64.StdEncoding.EncodeToString([]byte(body)),
			})
		}
		if err := a.do(ctx, http.MethodPut, "/sandboxes/"+created.SandboxID+"/files",
			writeFilesRequest{Files: files}, nil); err != nil {
			return nil, fmt.Errorf("sandbox/e2b: write files: %w", err)
		}
	}

	// Step 3: exec.
	execBody := execRequest{
		Command:   spec.Command,
		Env:       spec.Env,
		TimeoutMS: timeoutMS,
	}
	var execResp execResponse
	if err := a.do(ctx, http.MethodPost, "/sandboxes/"+created.SandboxID+"/exec",
		execBody, &execResp); err != nil {
		return nil, fmt.Errorf("sandbox/e2b: exec: %w", err)
	}

	endedAt := time.Now().UTC()

	status := sandbox.StatusDone
	if execResp.ExitCode != 0 {
		status = sandbox.StatusFailed
	}

	// Cache the inline logs so the Service can persist them to object
	// storage and fill StdoutURL/StderrURL.
	a.cacheLogs(created.SandboxID, execResp.Stdout, execResp.Stderr)

	return &sandbox.RunResult{
		Status:       status,
		ExitCode:     execResp.ExitCode,
		DurationS:    execResp.DurationMS / 1000,
		CPUSeconds:   execResp.CPUSeconds,
		MemoryPeakMB: execResp.MemPeakMB,
		StartedAt:    startedAt,
		EndedAt:      endedAt,
	}, nil
}

// Stream is a Phase 9.3 stub.
//
// The real implementation will subscribe to e2b's process-output stream
// (SSE) and forward lines into the returned channel. For now, we fall
// back to a buffered translation: Run() to completion, then drain the
// cached stdout/stderr into the channel as two final lines and emit the
// terminal RunResult on the result channel. That's enough to keep the
// streaming API contract honest in tests without faking it.
func (a *Adapter) Stream(ctx context.Context, spec sandbox.RunSpec) (<-chan sandbox.LogLine, <-chan *sandbox.RunResult, error) {
	result, err := a.Run(ctx, spec)
	if err != nil {
		return nil, nil, err
	}

	stdout, stderr := a.fetchCachedLogs()

	lines := make(chan sandbox.LogLine, 2)
	now := time.Now().UTC()
	if stdout != "" {
		lines <- sandbox.LogLine{Stream: "stdout", Text: stdout, TS: now}
	}
	if stderr != "" {
		lines <- sandbox.LogLine{Stream: "stderr", Text: stderr, TS: now}
	}
	close(lines)

	resCh := make(chan *sandbox.RunResult, 1)
	resCh <- result
	close(resCh)

	return lines, resCh, nil
}

// Stop best-effort closes the sandbox by ID. runID is the e2b sandbox_id
// (or, when called via Service.Stop, the suite_sandbox_runs row id — the
// Phase 9.1 service maps between the two; until that wiring lands, this
// adapter takes the e2b sandbox_id directly).
func (a *Adapter) Stop(ctx context.Context, runID string) error {
	if runID == "" {
		return errors.New("sandbox/e2b: empty run id")
	}
	if err := a.do(ctx, http.MethodDelete, "/sandboxes/"+runID, nil, nil); err != nil {
		return fmt.Errorf("sandbox/e2b: stop %s: %w", runID, err)
	}
	return nil
}

// Capabilities reports e2b's published limits.
func (a *Adapter) Capabilities() sandbox.Capabilities {
	return sandbox.Capabilities{
		Adapter:         "e2b",
		MaxTimeoutS:     3600, // 1h — e2b's per-sandbox limit
		SupportsGPU:     false,
		SupportsNetwork: true,
		SupportsMounts:  true,
		ColdStartMS:     1000, // typical e2b warm-pool boot
	}
}

// ───────────── HTTP plumbing ─────────────

// do executes an authenticated JSON request. body may be nil; out may be
// nil to ignore the response body. Returns an error for any non-2xx
// response with the upstream message embedded.
func (a *Adapter) do(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Read up to 4 KiB of the error body for the message.
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("e2b api %s %s: status %d: %s",
			method, path, resp.StatusCode, strings.TrimSpace(string(errBody)))
	}

	if out == nil {
		// Drain the body so the connection can be reused.
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// ───────────── inline-log cache ─────────────
//
// e2b returns stdout/stderr inline on the synchronous /exec response, but
// sandbox.RunResult only exposes URL fields (the Service layer is
// expected to persist log bytes to object storage and fill the URLs).
// We hold the last run's inline logs on the adapter so the Service (or
// the Stream fallback above) can pull them out without re-querying e2b.
//
// This is intentionally NOT thread-safe across concurrent Run calls per
// adapter instance — the Phase 9.1 Service serialises adapter calls per
// run; if/when we relax that, this cache must become per-runID keyed.

// cacheLogs records the last run's inline logs for retrieval.
func (a *Adapter) cacheLogs(sandboxID, stdout, stderr string) {
	a.lastSandboxID = sandboxID
	a.lastStdout = stdout
	a.lastStderr = stderr
}

// fetchCachedLogs returns the last run's inline stdout/stderr.
func (a *Adapter) fetchCachedLogs() (stdout, stderr string) {
	return a.lastStdout, a.lastStderr
}

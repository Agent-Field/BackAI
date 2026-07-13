// SPDX-License-Identifier: Apache-2.0

// Package e2b implements the sandbox.Sandbox interface against e2b.dev's
// hosted sandbox service (https://e2b.dev).
//
// e2b runs Firecracker-based micro-VMs you don't operate yourself. E2B
// has TWO planes and this adapter drives both:
//
//   - Control plane (REST, api.e2b.app, X-API-Key): create + kill a
//     sandbox.
//   - Data plane (envd, per-sandbox, X-Access-Token): write input files
//     (plain multipart HTTP) and run the command (a server-streaming
//     Connect RPC — process.Process/Start). There is NO unary
//     "run to completion" call; the exit code and output arrive as
//     framed stream events, which the adapter accumulates.
//
// PICK THIS ADAPTER WHEN
//
//   - You want micro-VM isolation without operating Firecracker yourself.
//   - You're OK with per-run cost (e2b prices per sandbox-second).
//   - Your workload fits inside e2b's 1-hour per-sandbox limit.
//
// CONFIGURATION (opt-in)
//
// Set AF_STACK_SANDBOX_ADAPTER=e2b and provide an API key via
// E2B_API_KEY or the dashboard Integrations → Sandbox slot. Without a
// key the runtime leaves the sandbox unconfigured (503) rather than
// pretending e2b works — the adapter is never active without a key.
package e2b

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
)

const (
	// DefaultAPIBaseURL is the e2b control plane. (Not e2b.dev — the
	// production API domain is e2b.app.)
	DefaultAPIBaseURL = "https://api.e2b.app"

	// DefaultEnvdBaseURL is the per-sandbox data-plane host. The concrete
	// sandbox is selected by the E2b-Sandbox-Id / E2b-Sandbox-Port
	// routing headers rather than the hostname.
	DefaultEnvdBaseURL = "https://sandbox.e2b.app"

	// envdPort is the fixed port envd listens on inside every sandbox;
	// sent as the E2b-Sandbox-Port routing header.
	envdPort = "49983"

	// defaultTemplate is the e2b template used when RunSpec.Image is
	// empty. "base" is e2b's general-purpose image.
	defaultTemplate = "base"

	// defaultUser is the sandbox user envd runs commands / writes files
	// as when none is specified.
	defaultUser = "user"

	// defaultTimeoutS bounds a run when the spec doesn't (mirrors the
	// dashboard default).
	defaultTimeoutS = 300

	// httpTimeout caps control-plane (create/kill/files) requests. The
	// exec stream is NOT bound by this — it runs for up to the run
	// timeout and is cancelled via context.
	httpTimeout = 60 * time.Second

	// connectEndOfStream is the Connect envelope flag bit marking the
	// terminal frame of a streaming response.
	connectEndOfStream = 0b10
)

// ErrAPIKeyMissing is returned by New when no API key is configured.
var ErrAPIKeyMissing = errors.New("sandbox/e2b: api key required (set E2B_API_KEY)")

// Config holds adapter settings.
type Config struct {
	// APIKey is the e2b.dev API key (starts with "e2b_"). Required.
	APIKey string

	// BaseURL overrides the control-plane endpoint. Empty -> DefaultAPIBaseURL.
	BaseURL string

	// EnvdBaseURL overrides the data-plane (envd) endpoint. Empty ->
	// DefaultEnvdBaseURL. Primarily a test seam.
	EnvdBaseURL string

	// HTTPClient lets tests inject a mocked transport. nil -> real
	// clients (a 60s control-plane client + an unbounded stream client).
	HTTPClient *http.Client
}

// Adapter implements sandbox.Sandbox via e2b's two-plane API.
type Adapter struct {
	apiKey   string
	apiBase  string
	envdBase string

	// client bounds control-plane + file requests (httpTimeout).
	client *http.Client
	// streamClient has no timeout — the exec stream is bounded by the
	// per-run context deadline instead.
	streamClient *http.Client

	// mu guards the last-run log cache used by the Stream fallback.
	mu         sync.Mutex
	lastStdout string
	lastStderr string
}

// compile-time interface check.
var _ sandbox.Sandbox = (*Adapter)(nil)

// New constructs the adapter. Returns ErrAPIKeyMissing when APIKey is
// empty so callers fail fast at startup rather than on the first Run.
func New(cfg Config) (*Adapter, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, ErrAPIKeyMissing
	}
	apiBase := strings.TrimRight(cfg.BaseURL, "/")
	if apiBase == "" {
		apiBase = DefaultAPIBaseURL
	}
	envdBase := strings.TrimRight(cfg.EnvdBaseURL, "/")
	if envdBase == "" {
		envdBase = DefaultEnvdBaseURL
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: httpTimeout}
	}
	streamClient := cfg.HTTPClient
	if streamClient == nil {
		streamClient = &http.Client{} // no timeout; ctx bounds the stream
	}

	return &Adapter{
		apiKey:       cfg.APIKey,
		apiBase:      apiBase,
		envdBase:     envdBase,
		client:       client,
		streamClient: streamClient,
	}, nil
}

// ───────────── control-plane wire types ─────────────

type createSandboxRequest struct {
	TemplateID string            `json:"templateID"`
	Timeout    int               `json:"timeout"` // SECONDS
	Metadata   map[string]string `json:"metadata,omitempty"`
	EnvVars    map[string]string `json:"envVars,omitempty"`
}

type createSandboxResponse struct {
	SandboxID       string `json:"sandboxID"`
	EnvdAccessToken string `json:"envdAccessToken"`
	EnvdVersion     string `json:"envdVersion"`
}

// ───────────── Sandbox interface ─────────────

// Run creates a sandbox, seeds any input files, executes the command to
// completion over the envd process stream, and kills the sandbox.
func (a *Adapter) Run(ctx context.Context, spec sandbox.RunSpec) (*sandbox.RunResult, error) {
	if len(spec.Command) == 0 {
		return nil, errors.New("sandbox/e2b: command is required")
	}
	startedAt := time.Now().UTC()

	timeoutS := spec.TimeoutS
	if timeoutS <= 0 {
		timeoutS = defaultTimeoutS
	}
	// The adapter owns the run timeout (the Service intentionally doesn't
	// wrap ctx). Bound every plane call by it.
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutS)*time.Second)
	defer cancel()

	template := spec.Image
	if template == "" {
		template = defaultTemplate
	}

	// Step 1: create sandbox.
	created, err := a.createSandbox(runCtx, template, timeoutS, spec.Env)
	if err != nil {
		return nil, fmt.Errorf("sandbox/e2b: create: %w", err)
	}
	// Always best-effort kill on exit, even on error paths, on a fresh
	// context so a cancelled runCtx doesn't skip cleanup.
	defer func() {
		killCtx, killCancel := context.WithTimeout(context.Background(), httpTimeout)
		defer killCancel()
		_ = a.killSandbox(killCtx, created.SandboxID)
	}()

	// Step 2: write input files (if any).
	for path, body := range spec.Files {
		if err := a.writeFile(runCtx, created, path, []byte(body)); err != nil {
			return nil, fmt.Errorf("sandbox/e2b: write file %q: %w", path, err)
		}
	}

	// Step 3: exec via the envd process stream.
	stdout, stderr, exitCode, err := a.execStream(runCtx, created, spec)
	if err != nil {
		return nil, fmt.Errorf("sandbox/e2b: exec: %w", err)
	}
	endedAt := time.Now().UTC()

	status := sandbox.StatusDone
	if exitCode != 0 {
		status = sandbox.StatusFailed
	}
	a.cacheLogs(stdout, stderr)

	return &sandbox.RunResult{
		Status:    status,
		ExitCode:  exitCode,
		DurationS: int(endedAt.Sub(startedAt).Seconds()),
		StartedAt: startedAt,
		EndedAt:   endedAt,
	}, nil
}

// createSandbox POSTs the control plane and returns the sandbox id +
// envd access token.
func (a *Adapter) createSandbox(ctx context.Context, template string, timeoutS int, env map[string]string) (createSandboxResponse, error) {
	body := createSandboxRequest{
		TemplateID: template,
		Timeout:    timeoutS,
		EnvVars:    env,
	}
	var out createSandboxResponse
	if err := a.doControl(ctx, http.MethodPost, "/sandboxes", body, &out); err != nil {
		return createSandboxResponse{}, err
	}
	if out.SandboxID == "" {
		return createSandboxResponse{}, errors.New("create returned empty sandboxID")
	}
	return out, nil
}

// killSandbox best-effort deletes the sandbox by id.
func (a *Adapter) killSandbox(ctx context.Context, sandboxID string) error {
	if sandboxID == "" {
		return nil
	}
	return a.doControl(ctx, http.MethodDelete, "/sandboxes/"+sandboxID, nil, nil)
}

// writeFile uploads one file to envd via the plain multipart /files API.
func (a *Adapter) writeFile(ctx context.Context, s createSandboxResponse, path string, content []byte) error {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", path)
	if err != nil {
		return err
	}
	if _, err := part.Write(content); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}

	url := a.envdBase + "/files?path=" + urlQueryEscape(path) + "&username=" + defaultUser
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	a.setEnvdHeaders(req, s)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("envd files status %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
	}
	return nil
}

// ───────────── envd process stream (Connect) ─────────────

// startRequest is the process.Process/Start request message.
type startRequest struct {
	Process processConfig `json:"process"`
}

type processConfig struct {
	Cmd  string            `json:"cmd"`
	Args []string          `json:"args,omitempty"`
	Envs map[string]string `json:"envs,omitempty"`
	Cwd  string            `json:"cwd,omitempty"`
}

// startResponse is one data frame of the Start stream. The event is a
// protobuf oneof rendered as an object with exactly one key set.
type startResponse struct {
	Event struct {
		Data *struct {
			Stdout string `json:"stdout"`
			Stderr string `json:"stderr"`
		} `json:"data"`
		End *struct {
			// protojson emits lowerCamelCase; accept snake_case too.
			ExitCode      *int `json:"exitCode"`
			ExitCodeSnake *int `json:"exit_code"`
		} `json:"end"`
	} `json:"event"`
}

// connectEndStream is the terminal (0x02) frame payload.
type connectEndStream struct {
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// execStream runs the command over the server-streaming Connect RPC and
// accumulates stdout/stderr + the exit code. spec.Command is argv:
// Command[0] is the program, the rest are args (same model as docker).
func (a *Adapter) execStream(ctx context.Context, s createSandboxResponse, spec sandbox.RunSpec) (stdout, stderr string, exitCode int, err error) {
	reqMsg := startRequest{Process: processConfig{
		Cmd:  spec.Command[0],
		Args: spec.Command[1:],
		Envs: spec.Env,
	}}
	payload, err := json.Marshal(reqMsg)
	if err != nil {
		return "", "", 0, fmt.Errorf("marshal start: %w", err)
	}

	// One request frame: [flags=0x00][big-endian uint32 len][payload].
	// The Connect envelope length is a uint32; a >4 GiB start message is
	// impossible in practice but guard the conversion for gosec.
	if len(payload) > math.MaxUint32 {
		return "", "", 0, fmt.Errorf("start message too large: %d bytes", len(payload))
	}
	frame := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(payload))) //nolint:gosec // length bounded by the MaxUint32 check above
	copy(frame[5:], payload)

	url := a.envdBase + "/process.Process/Start"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(frame))
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/connect+json")
	req.Header.Set("Connect-Protocol-Version", "1")
	a.setEnvdHeaders(req, s)

	resp, err := a.streamClient.Do(req)
	if err != nil {
		return "", "", 0, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", "", 0, fmt.Errorf("envd start status %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
	}

	var (
		outBuf, errBuf bytes.Buffer
		gotExit        bool
	)
	hdr := make([]byte, 5)
	for {
		if _, rerr := io.ReadFull(resp.Body, hdr); rerr != nil {
			if rerr == io.EOF {
				break
			}
			return "", "", 0, fmt.Errorf("read frame header: %w", rerr)
		}
		flags := hdr[0]
		n := binary.BigEndian.Uint32(hdr[1:5])
		msg := make([]byte, n)
		if _, rerr := io.ReadFull(resp.Body, msg); rerr != nil {
			return "", "", 0, fmt.Errorf("read frame payload: %w", rerr)
		}

		if flags&connectEndOfStream != 0 {
			// Terminal frame: {} on success, {"error":...} on RPC failure.
			// The PROCESS exit code arrives in a data `end` event, not here.
			var end connectEndStream
			_ = json.Unmarshal(msg, &end)
			if end.Error != nil {
				return "", "", 0, fmt.Errorf("envd stream error %s: %s", end.Error.Code, end.Error.Message)
			}
			break
		}

		var ev startResponse
		if uerr := json.Unmarshal(msg, &ev); uerr != nil {
			return "", "", 0, fmt.Errorf("decode event: %w", uerr)
		}
		if d := ev.Event.Data; d != nil {
			if d.Stdout != "" {
				if b, derr := base64.StdEncoding.DecodeString(d.Stdout); derr == nil {
					outBuf.Write(b)
				}
			}
			if d.Stderr != "" {
				if b, derr := base64.StdEncoding.DecodeString(d.Stderr); derr == nil {
					errBuf.Write(b)
				}
			}
		}
		if e := ev.Event.End; e != nil {
			switch {
			case e.ExitCode != nil:
				exitCode = *e.ExitCode
			case e.ExitCodeSnake != nil:
				exitCode = *e.ExitCodeSnake
			}
			gotExit = true
		}
	}

	if !gotExit {
		return "", "", 0, errors.New("process stream ended without an exit event")
	}
	return outBuf.String(), errBuf.String(), exitCode, nil
}

// setEnvdHeaders adds the routing + auth headers every envd call needs.
func (a *Adapter) setEnvdHeaders(req *http.Request, s createSandboxResponse) {
	req.Header.Set("E2b-Sandbox-Id", s.SandboxID)
	req.Header.Set("E2b-Sandbox-Port", envdPort)
	if s.EnvdAccessToken != "" {
		req.Header.Set("X-Access-Token", s.EnvdAccessToken)
	}
}

// ───────────── Stream / Stop / Capabilities ─────────────

// Stream falls back to a buffered translation: Run to completion, then
// drain the cached stdout/stderr as two final lines and emit the
// terminal result. Native line streaming is a follow-up.
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

// Stop best-effort kills the sandbox by its e2b sandbox id.
func (a *Adapter) Stop(ctx context.Context, runID string) error {
	if runID == "" {
		return errors.New("sandbox/e2b: empty run id")
	}
	if err := a.killSandbox(ctx, runID); err != nil {
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
		ColdStartMS:     1000,
	}
}

// ───────────── control-plane HTTP + log cache ─────────────

// doControl executes an authenticated control-plane JSON request. body
// may be nil; out may be nil to ignore the response body.
func (a *Adapter) doControl(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.apiBase+path, reqBody)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("X-API-Key", a.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("e2b api %s %s: status %d: %s",
			method, path, resp.StatusCode, strings.TrimSpace(string(errBody)))
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (a *Adapter) cacheLogs(stdout, stderr string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastStdout = stdout
	a.lastStderr = stderr
}

func (a *Adapter) fetchCachedLogs() (stdout, stderr string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastStdout, a.lastStderr
}

// urlQueryEscape percent-escapes a query value without pulling in a
// url.Values just for one param.
func urlQueryEscape(s string) string {
	var b strings.Builder
	for _, r := range []byte(s) {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == '~', r == '/':
			b.WriteByte(r)
		default:
			fmt.Fprintf(&b, "%%%02X", r)
		}
	}
	return b.String()
}

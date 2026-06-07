// SPDX-License-Identifier: Apache-2.0

// e2b adapter unit tests.
//
// We stand up an httptest.Server to mock the e2b control plane, point the
// adapter at it, and assert (a) the request shape and (b) that the
// response is parsed into a sandbox.RunResult correctly.
package e2b_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
	"github.com/Agent-Field/backai/services/runtime/internal/sandbox/adapters/e2b"
)

// recorded captures one HTTP call the adapter made against the mock.
type recorded struct {
	Method string
	Path   string
	Auth   string
	Body   map[string]any
}

// newMockServer returns an httptest server that:
//   - matches the three e2b endpoints (create, write files, exec, delete)
//   - records each call
//   - returns canned responses suitable for happy-path Run().
func newMockServer(t *testing.T, calls *[]recorded) *httptest.Server {
	t.Helper()
	var execCount atomic.Int32

	mux := http.NewServeMux()

	mux.HandleFunc("/sandboxes", func(w http.ResponseWriter, r *http.Request) {
		rec := readCall(t, r)
		*calls = append(*calls, rec)
		if r.Method != http.MethodPost {
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sandbox_id": "sbx_test_123",
		})
	})

	// /sandboxes/sbx_test_123/files (PUT) and /sandboxes/sbx_test_123/exec (POST)
	// and /sandboxes/sbx_test_123 (DELETE) all live under this prefix.
	mux.HandleFunc("/sandboxes/", func(w http.ResponseWriter, r *http.Request) {
		rec := readCall(t, r)
		*calls = append(*calls, rec)
		switch {
		case strings.HasSuffix(r.URL.Path, "/files") && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/exec") && r.Method == http.MethodPost:
			execCount.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"exit_code":      0,
				"stdout":         "hello from sandbox\n",
				"stderr":         "",
				"duration_ms":    1234,
				"cpu_seconds":    0.42,
				"memory_peak_mb": 64,
			})
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected route: "+r.Method+" "+r.URL.Path,
				http.StatusNotFound)
		}
	})

	return httptest.NewServer(mux)
}

func readCall(t *testing.T, r *http.Request) recorded {
	t.Helper()
	body, _ := io.ReadAll(r.Body)
	var parsed map[string]any
	if len(body) > 0 {
		_ = json.Unmarshal(body, &parsed)
	}
	return recorded{
		Method: r.Method,
		Path:   r.URL.Path,
		Auth:   r.Header.Get("Authorization"),
		Body:   parsed,
	}
}

func TestNewRequiresAPIKey(t *testing.T) {
	_, err := e2b.New(e2b.Config{})
	if !errors.Is(err, e2b.ErrAPIKeyMissing) {
		t.Fatalf("New without APIKey err = %v, want ErrAPIKeyMissing", err)
	}
}

func TestRunHappyPath(t *testing.T) {
	var calls []recorded
	srv := newMockServer(t, &calls)
	defer srv.Close()

	a, err := e2b.New(e2b.Config{
		APIKey:  "test-key",
		BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := a.Run(ctx, sandbox.RunSpec{
		Image:    "python:3.12-slim",
		Command:  []string{"python", "-c", "print('hi')"},
		Files:    map[string]string{"main.py": "print('hi')"},
		Env:      map[string]string{"FOO": "bar"},
		TimeoutS: 60,
		Network:  sandbox.NetworkRestricted,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// ── Assert result parsing ──────────────────────────────────────
	if result.Status != sandbox.StatusDone {
		t.Errorf("Status = %q, want done", result.Status)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if result.DurationS != 1 {
		t.Errorf("DurationS = %d, want 1 (1234ms rounded down)", result.DurationS)
	}
	if result.CPUSeconds != 0.42 {
		t.Errorf("CPUSeconds = %v, want 0.42", result.CPUSeconds)
	}
	if result.MemoryPeakMB != 64 {
		t.Errorf("MemoryPeakMB = %d, want 64", result.MemoryPeakMB)
	}
	if result.StartedAt.IsZero() || result.EndedAt.IsZero() {
		t.Errorf("StartedAt/EndedAt should be set, got %+v / %+v",
			result.StartedAt, result.EndedAt)
	}

	// ── Assert request shape ───────────────────────────────────────
	// Expected sequence: POST /sandboxes -> PUT files -> POST exec -> DELETE.
	if len(calls) != 4 {
		t.Fatalf("expected 4 HTTP calls, got %d: %+v", len(calls), calls)
	}

	// All calls authed with Bearer token.
	for i, c := range calls {
		if c.Auth != "Bearer test-key" {
			t.Errorf("call %d Auth = %q, want Bearer test-key", i, c.Auth)
		}
	}

	// Call 0: create sandbox
	create := calls[0]
	if create.Method != http.MethodPost || create.Path != "/sandboxes" {
		t.Errorf("call 0 = %s %s, want POST /sandboxes", create.Method, create.Path)
	}
	if create.Body["template"] != "python:3.12-slim" {
		t.Errorf("create.template = %v, want python:3.12-slim", create.Body["template"])
	}
	if v, ok := create.Body["timeout_ms"].(float64); !ok || int(v) != 60000 {
		t.Errorf("create.timeout_ms = %v, want 60000", create.Body["timeout_ms"])
	}

	// Call 1: write files
	files := calls[1]
	if files.Method != http.MethodPut || !strings.HasSuffix(files.Path, "/files") {
		t.Errorf("call 1 = %s %s, want PUT .../files", files.Method, files.Path)
	}
	// File contents should be base64-encoded.
	fileList, _ := files.Body["files"].([]any)
	if len(fileList) != 1 {
		t.Fatalf("expected 1 file, got %d", len(fileList))
	}
	first, _ := fileList[0].(map[string]any)
	if first["path"] != "main.py" {
		t.Errorf("file path = %v, want main.py", first["path"])
	}
	wantB64 := base64.StdEncoding.EncodeToString([]byte("print('hi')"))
	if first["content_b64"] != wantB64 {
		t.Errorf("file content_b64 = %v, want %v", first["content_b64"], wantB64)
	}

	// Call 2: exec
	exec := calls[2]
	if exec.Method != http.MethodPost || !strings.HasSuffix(exec.Path, "/exec") {
		t.Errorf("call 2 = %s %s, want POST .../exec", exec.Method, exec.Path)
	}
	cmdList, _ := exec.Body["command"].([]any)
	if len(cmdList) != 3 || cmdList[0] != "python" {
		t.Errorf("exec.command = %v, want [python -c print('hi')]", exec.Body["command"])
	}

	// Call 3: delete (cleanup)
	del := calls[3]
	if del.Method != http.MethodDelete {
		t.Errorf("call 3 = %s, want DELETE", del.Method)
	}
}

func TestRunPropagatesAPIErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	a, _ := e2b.New(e2b.Config{APIKey: "k", BaseURL: srv.URL})
	_, err := a.Run(context.Background(), sandbox.RunSpec{Command: []string{"echo"}})
	if err == nil {
		t.Fatal("Run should have returned an error on 429")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error %q should mention status 429", err.Error())
	}
}

func TestStopBestEffort(t *testing.T) {
	var seen atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/sandboxes/sbx_") {
			seen.Add(1)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, "unexpected", http.StatusBadRequest)
	}))
	defer srv.Close()

	a, _ := e2b.New(e2b.Config{APIKey: "k", BaseURL: srv.URL})
	if err := a.Stop(context.Background(), "sbx_abc"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if seen.Load() != 1 {
		t.Errorf("expected 1 DELETE call, got %d", seen.Load())
	}
}

func TestCapabilities(t *testing.T) {
	a, _ := e2b.New(e2b.Config{APIKey: "k"})
	caps := a.Capabilities()
	if caps.Adapter != "e2b" {
		t.Errorf("Adapter = %q, want e2b", caps.Adapter)
	}
	if caps.MaxTimeoutS != 3600 {
		t.Errorf("MaxTimeoutS = %d, want 3600", caps.MaxTimeoutS)
	}
	if caps.SupportsGPU {
		t.Error("e2b should not advertise GPU support")
	}
	if !caps.SupportsNetwork || !caps.SupportsMounts {
		t.Errorf("expected network+mounts support, got %+v", caps)
	}
	if caps.ColdStartMS != 1000 {
		t.Errorf("ColdStartMS = %d, want 1000", caps.ColdStartMS)
	}
}
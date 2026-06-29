// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package remote

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
)

// TestE2E_RemoteAdapter_PythonSidecar launches the reference Python
// sandbox-echo adapter in examples/adapters/sandbox-echo-py/, then
// drives it through the Go remote shim to validate the full HTTP
// protocol end-to-end. Skips if the venv isn't pre-built (run
// `python3.12 -m venv .venv && .venv/bin/pip install -r requirements.txt`
// once in examples/adapters/sandbox-echo-py/).
//
// Build tag: e2e. Run with `go test -tags=e2e ./services/runtime/internal/sandbox/adapters/remote/`.
func TestE2E_RemoteAdapter_PythonSidecar(t *testing.T) {
	repoRoot := findRepoRoot(t)
	venvUvicorn := filepath.Join(repoRoot, "examples/adapters/sandbox-echo-py/.venv/bin/uvicorn")
	if _, err := os.Stat(venvUvicorn); err != nil {
		t.Skipf("python venv missing at %s — see file header", venvUvicorn)
	}

	port := freePort(t)
	cmd := exec.Command(venvUvicorn, "main:app", "--host", "127.0.0.1", "--port", fmt.Sprint(port), "--log-level", "warning")
	cmd.Dir = filepath.Join(repoRoot, "examples/adapters/sandbox-echo-py")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		t.Fatalf("launch python adapter: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	if err := waitHealthy(baseURL, 5*time.Second); err != nil {
		t.Fatalf("adapter never became healthy: %v", err)
	}

	a, err := New(context.Background(), Config{BaseURL: baseURL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	caps := a.Capabilities()
	if caps.Adapter != "echo" {
		t.Errorf("capability name: got %q want echo", caps.Adapter)
	}

	// Sync Run end-to-end.
	res, err := a.Run(context.Background(), sandbox.RunSpec{
		ID:       "e2e-run-1",
		Image:    "python:3.12-slim",
		Command:  []string{"echo", "hello", "world"},
		TimeoutS: 30,
		CPU:      2,
		MemoryGB: 4,
		Network:  sandbox.NetworkIsolated,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != sandbox.StatusDone {
		t.Errorf("Run status: %v", res.Status)
	}
	if res.ExitCode != 0 {
		t.Errorf("Run exit code: %d", res.ExitCode)
	}

	// Idempotency.
	res2, err := a.Run(context.Background(), sandbox.RunSpec{
		ID:       "e2e-run-1",
		Image:    "python:3.12-slim",
		Command:  []string{"echo", "hello", "world"},
		TimeoutS: 30,
		CPU:      2,
		MemoryGB: 4,
		Network:  sandbox.NetworkIsolated,
	})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if res2.Status != sandbox.StatusDone {
		t.Errorf("idempotent Run status: %v", res2.Status)
	}

	// Stream end-to-end.
	lines, results, err := a.Stream(context.Background(), sandbox.RunSpec{
		ID:       "e2e-stream-1",
		Image:    "python:3.12-slim",
		Command:  []string{"echo", "stream test"},
		TimeoutS: 30,
		CPU:      2,
		MemoryGB: 4,
		Network:  sandbox.NetworkIsolated,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	gotLines := 0
	for ln := range lines {
		if ln.Text == "" {
			t.Errorf("empty log line text")
		}
		gotLines++
	}
	terminal := <-results
	if terminal == nil {
		t.Fatal("Stream: nil terminal result")
	}
	if terminal.Status != sandbox.StatusDone {
		t.Errorf("Stream terminal status: %v", terminal.Status)
	}
	if gotLines == 0 {
		t.Error("Stream emitted no log lines")
	}

	// Stop on a (synthetic) id.
	if err := a.Stop(context.Background(), "no-such-id"); err != nil {
		t.Logf("Stop on unknown id returned: %v (acceptable — adapter may 404 or 204)", err)
	}

	// Pool stats.
	pool, err := a.Pool(context.Background())
	if err != nil {
		t.Fatalf("Pool: %v", err)
	}
	if pool.Adapter != "echo" {
		t.Errorf("pool adapter: %q", pool.Adapter)
	}

	// Probe.
	st, err := a.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if st != "healthy" {
		t.Errorf("Probe status: %s", st)
	}
}

// freePort finds an unused localhost port.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// waitHealthy polls /healthz until it returns 200 or the deadline passes.
func waitHealthy(baseURL string, deadline time.Duration) error {
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		resp, err := http.Get(baseURL + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("adapter did not return /healthz=200 in time")
}

// findRepoRoot walks up from the test file's directory until it finds
// the top-level go.mod.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for d := dir; d != "/" && d != ""; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			// Verify it's the repo root by checking for examples/.
			if _, err := os.Stat(filepath.Join(d, "examples/adapters")); err == nil {
				return d
			}
		}
		if strings.HasSuffix(d, "/backai") {
			return d
		}
	}
	t.Fatal("could not find repo root with go.mod + examples/adapters")
	return ""
}

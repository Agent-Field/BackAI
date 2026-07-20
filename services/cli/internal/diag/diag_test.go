// SPDX-License-Identifier: Apache-2.0

package diag

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/backai/services/cli/internal/client"
	"github.com/Agent-Field/backai/services/cli/internal/output"
)

// stubLookPath makes docker+node appear present unless listed in absent.
func stubLookPath(t *testing.T, absent ...string) {
	t.Helper()
	old := lookPath
	t.Cleanup(func() { lookPath = old })
	gone := map[string]bool{}
	for _, a := range absent {
		gone[a] = true
	}
	lookPath = func(name string) (string, error) {
		if gone[name] {
			return "", os.ErrNotExist
		}
		return "/usr/bin/" + name, nil
	}
}

// unreachableClient returns a client pointed at a closed port (connection
// refused immediately, no timeout wait).
func unreachableClient(t *testing.T) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	return &client.Client{BaseURL: url, HTTP: srv.Client()}
}

// readyClient returns a client whose runtime answers /ready, /health, and
// /api/v1/agents.
func readyClient(t *testing.T) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ready", "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/v1/agents":
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"agents":[{"node_id":"supportdesk"}]}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return &client.Client{BaseURL: srv.URL, HTTP: srv.Client()}
}

// Contract: doctor degrades gracefully offline — a down runtime is a warning,
// not a crash. The command completes (nil error, exit 0) and reports the
// runtime as unreachable.
func TestDoctorDegradesOffline(t *testing.T) {
	stubLookPath(t) // docker + node present
	var stdout bytes.Buffer
	err := RunDoctor(context.Background(), unreachableClient(t), nil, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("doctor offline should not error, got: %v", err)
	}
	out := stdout.String()
	if !contains(out, "runtime-reachable") || !contains(out, "not reachable") {
		t.Fatalf("expected offline runtime report:\n%s", out)
	}
	if !contains(out, "OK") {
		t.Fatalf("doctor should be OK offline (no critical fail):\n%s", out)
	}
}

// Contract: a missing docker (a critical prerequisite) fails doctor with exit
// code 1.
func TestDoctorFailsWhenDockerMissing(t *testing.T) {
	stubLookPath(t, "docker")
	var stdout bytes.Buffer
	err := RunDoctor(context.Background(), unreachableClient(t), nil, &stdout, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected doctor to fail when docker is missing")
	}
	if code := output.ExitCode(err); code != output.ExitGeneric {
		t.Fatalf("docker-missing exit = %d, want %d", code, output.ExitGeneric)
	}
	if !contains(stdout.String(), "FAIL") {
		t.Fatalf("expected FAIL in output:\n%s", stdout.String())
	}
}

// Contract: --json emits a stable, parseable DoctorReport.
func TestDoctorJSON(t *testing.T) {
	stubLookPath(t)
	var stdout bytes.Buffer
	if err := RunDoctor(context.Background(), readyClient(t), []string{"--json"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor --json error: %v", err)
	}
	var rep DoctorReport
	if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
		t.Fatalf("--json is not a valid DoctorReport: %v\n%s", err, stdout.String())
	}
	if !rep.OK || len(rep.Checks) == 0 {
		t.Fatalf("expected OK report with checks, got %+v", rep)
	}
	// A ready runtime must surface a passing reachability check.
	found := false
	for _, c := range rep.Checks {
		if c.Name == "runtime-reachable" && c.Status == statusPass {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a passing runtime-reachable check: %+v", rep.Checks)
	}
}

// Contract: status reports state and never fails on a down runtime.
func TestStatusOffline(t *testing.T) {
	var stdout bytes.Buffer
	if err := RunStatus(context.Background(), unreachableClient(t), []string{"--json"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("status offline should not error: %v", err)
	}
	var rep StatusReport
	if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
		t.Fatalf("status --json invalid: %v\n%s", err, stdout.String())
	}
	if rep.Reachable {
		t.Fatal("offline runtime must report reachable=false")
	}
}

// Contract: `test` degrades gracefully offline — the sdk-smoke gate skips
// (does not fail), and with no failing gates the command exits 0.
func TestTestDegradesOffline(t *testing.T) {
	var stdout bytes.Buffer
	if err := RunTest(context.Background(), unreachableClient(t), []string{"--json"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("test offline should not error: %v", err)
	}
	var rep TestReport
	if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
		t.Fatalf("test --json invalid: %v\n%s", err, stdout.String())
	}
	if !rep.OK {
		t.Fatalf("offline test should pass (all gates skip): %+v", rep.Gates)
	}
	for _, g := range rep.Gates {
		if g.Name == "sdk-smoke" && g.Status != "skip" {
			t.Fatalf("sdk-smoke should skip offline, got %q", g.Status)
		}
	}
}

// Contract: `test` fails (exit 1) when a module ships a tenant-owned table
// without row level security — the multi-tenancy invariant gate.
func TestTestFailsOnUnisolatedModule(t *testing.T) {
	root := t.TempDir()
	// Minimal checkout markers so findRoot() recognises it.
	writeF(t, root, "docker-compose.yml", "services: {}\n")
	writeF(t, root, ".env", "AF_STACK_MODE=saas\n")
	writeF(t, root, "package.json", "{}")
	writeF(t, root, "apps/dashboard/.keep", "")
	// A module with a tenant table but NO RLS.
	writeF(t, root, "workload-modules/leaky/backai.module.yaml",
		"id: leaky\nname: Leaky\nversion: 0.1.0\n")
	writeF(t, root, "workload-modules/leaky/migrations/00001_init.sql",
		"-- +goose Up\ncreate table if not exists leaky_rows (\n  id uuid primary key,\n  tenant_id uuid not null\n);\n")

	restore := chdirTest(t, root)
	defer restore()

	var stdout bytes.Buffer
	err := RunTest(context.Background(), unreachableClient(t), []string{"--json"}, &stdout, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected test to fail on an unisolated tenant table")
	}
	if code := output.ExitCode(err); code != output.ExitGeneric {
		t.Fatalf("failed-gate exit = %d, want %d", code, output.ExitGeneric)
	}
	var rep TestReport
	if jsonErr := json.Unmarshal(stdout.Bytes(), &rep); jsonErr != nil {
		t.Fatalf("test --json invalid: %v", jsonErr)
	}
	if rep.OK {
		t.Fatal("report OK should be false")
	}
	sawRLSFail := false
	for _, g := range rep.Gates {
		if g.Name == "migration-rls" && g.Status == "fail" {
			sawRLSFail = true
		}
	}
	if !sawRLSFail {
		t.Fatalf("expected migration-rls gate to fail: %+v", rep.Gates)
	}
}

func contains(s, sub string) bool { return bytes.Contains([]byte(s), []byte(sub)) }

func writeF(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func chdirTest(t *testing.T, dir string) func() {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return func() { _ = os.Chdir(old) }
}

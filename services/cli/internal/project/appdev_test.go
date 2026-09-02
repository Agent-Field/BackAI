// SPDX-License-Identifier: Apache-2.0

package project

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/cli/internal/starter"
)

// scaffoldedApp writes the markers `af-stack init <name>` leaves (the
// checkout package keys on them) without the backend files, the way an app
// written by an older CLI looks.
func scaffoldedApp(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "my-ai-product")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"package.json": `{"name":"my-ai-product","scripts":{"prestart":"af-stack dev"}}`,
		"CLAUDE.md":    "# app\n",
		".env.example": "AF_STACK_URL=http://localhost:8080\nAF_STACK_API_KEY=\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// stubDocker puts a fake `docker` first on PATH that records its argv to
// a log file, answers `compose port runtime 8080` with the fake runtime's
// host port (so the preflight treats that busy port as ours), and does
// nothing for `compose up`.
func stubDocker(t *testing.T, runtimePort string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub docker script is POSIX")
	}
	bin := t.TempDir()
	log := filepath.Join(bin, "docker.log")
	script := "#!/bin/sh\necho \"$@\" >> " + log + "\n" +
		"if [ \"$1 $2 $3 $4\" = \"compose port runtime 8080\" ]; then echo \"0.0.0.0:" + runtimePort + "\"; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func fakeRuntime(t *testing.T, agentRegistered bool) (*httptest.Server, string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})
	mux.HandleFunc("/api/v1/agents", func(w http.ResponseWriter, _ *http.Request) {
		if agentRegistered {
			_, _ = w.Write([]byte(`{"agents":[{"node_id":"supportdesk"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"agents":[]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	return srv, u.Port()
}

// Contract: in an app written by `af-stack init <name>`, `af-stack dev`
// does not demand a checkout. It writes the bundled backend when missing,
// runs `docker compose up -d` in the app, waits for the runtime, points
// the app at it through .env, and prints where things are.
func TestDevInScaffoldedAppBootsBundledBackend(t *testing.T) {
	root := scaffoldedApp(t)
	defer chdir(t, root)()
	_, port := fakeRuntime(t, true)
	log := stubDocker(t, port)
	// Pin the runtime port to the fake so the readiness wait hits it.
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("AF_STACK_PORT="+port+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	appAgentTimeout = 5 * time.Second

	var out, errOut bytes.Buffer
	if err := RunDev(context.Background(), nil, &out, &errOut); err != nil {
		t.Fatalf("dev in scaffolded app: %v\nstdout=%s\nstderr=%s", err, out.String(), errOut.String())
	}

	for _, rel := range []string{starter.ComposeFile, "backend/postgres-init.sh", "backend/litellm-config.yaml"} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("dev did not write %s: %v", rel, err)
		}
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "compose up -d") {
		t.Errorf("dev did not run docker compose up -d; docker calls:\n%s", calls)
	}
	env, _ := starter.ReadEnv(root)
	if env["AF_STACK_URL"] != "http://localhost:"+port {
		t.Errorf("AF_STACK_URL in .env = %q, want the runtime's port %s", env["AF_STACK_URL"], port)
	}
	if env["AF_STACK_PORT"] != port {
		t.Errorf("AF_STACK_PORT was moved off the port our own compose project holds: %q", env["AF_STACK_PORT"])
	}
	if env["COMPOSE_PROJECT_NAME"] != "my-ai-product" {
		t.Errorf("COMPOSE_PROJECT_NAME = %q", env["COMPOSE_PROJECT_NAME"])
	}
	for _, want := range []string{"AF_STACK_URL=http://localhost:" + port, "ready", "registered", "Operator dashboard", "docker compose down"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

// Contract: a runtime that never becomes ready is reported with the last
// status and a pointer at the logs, and .env already carries the URL so
// the next attempt is consistent.
func TestDevInScaffoldedAppReportsUnreadyRuntime(t *testing.T) {
	root := scaffoldedApp(t)
	defer chdir(t, root)()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"db_unavailable"}`))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	stubDocker(t, u.Port())
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("AF_STACK_PORT="+u.Port()+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	appReadyTimeout = 50 * time.Millisecond
	defer func() { appReadyTimeout = 5 * time.Minute }()

	var out, errOut bytes.Buffer
	err := RunDev(context.Background(), nil, &out, &errOut)
	if err == nil {
		t.Fatal("expected an error when the runtime never becomes ready")
	}
	for _, want := range []string{"did not become ready", "db_unavailable", "docker compose logs runtime"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
	env, _ := starter.ReadEnv(root)
	if env["AF_STACK_URL"] != "http://localhost:"+u.Port() {
		t.Errorf("AF_STACK_URL should be written before the wait, got %q", env["AF_STACK_URL"])
	}
}

// Contract: without docker on PATH the error says what to install rather
// than failing inside compose.
func TestDevInScaffoldedAppNeedsDocker(t *testing.T) {
	root := scaffoldedApp(t)
	defer chdir(t, root)()
	t.Setenv("PATH", t.TempDir())

	var out, errOut bytes.Buffer
	err := RunDev(context.Background(), nil, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "docker is required") {
		t.Fatalf("want a docker-is-required error, got: %v", err)
	}
}

// Contract: a directory that is neither a checkout nor a scaffolded app
// still gets the checkout explanation (covered in checkout_error_test.go);
// a scaffolded app with the saas template also gets VITE_AF_STACK_URL.
func TestDevInScaffoldedAppKeepsViteURL(t *testing.T) {
	root := scaffoldedApp(t)
	defer chdir(t, root)()
	if err := os.WriteFile(filepath.Join(root, ".env.example"), []byte("VITE_AF_STACK_URL=http://localhost:8080\nPORT=34000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, port := fakeRuntime(t, false)
	stubDocker(t, port)
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("AF_STACK_PORT="+port+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	appAgentTimeout = 10 * time.Millisecond

	var out, errOut bytes.Buffer
	if err := RunDev(context.Background(), nil, &out, &errOut); err != nil {
		t.Fatalf("dev: %v\n%s", err, errOut.String())
	}
	env, _ := starter.ReadEnv(root)
	if env["VITE_AF_STACK_URL"] != "http://localhost:"+port {
		t.Errorf("VITE_AF_STACK_URL = %q", env["VITE_AF_STACK_URL"])
	}
	if !strings.Contains(out.String(), "not yet") {
		t.Errorf("an unregistered agent should be reported, not hidden:\n%s", out.String())
	}
}

package initcmd

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The node starter is what a new user runs first, so drive the real file with
// node against fake backends instead of grepping the template.

func scaffoldNodeStarter(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	defer chdir(t, parent)()
	var out, errOut bytes.Buffer
	if err := Run([]string{"starter"}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	return filepath.Join(parent, "starter")
}

func runStarter(t *testing.T, dir string, env ...string) (string, string, int) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not on PATH")
	}
	cmd := exec.Command("node", "src/index.mjs")
	cmd.Dir = dir
	// Only what the starter needs; in particular no inherited AF_STACK_URL.
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, env...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run node: %v", err)
	}
	return out.String(), errOut.String(), code
}

func backaiRuntime(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"alive","uptime_s":1}`))
	})
	mux.HandleFunc("/api/v1/agents", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"agents":[{"node_id":"supportdesk","reasoners":["echo"]}]}`))
	})
	mux.HandleFunc("POST /api/v1/agents/supportdesk.echo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body struct {
			Input struct {
				Payload map[string]any `json:"payload"`
			} `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"execution_id": "exec-1", "status": "completed",
			"result": map[string]any{"echoed": body.Input.Payload},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// Contract: against a BackAI runtime the starter lists agents and exits 0.
func TestNodeStarterTalksToRuntime(t *testing.T) {
	dir := scaffoldNodeStarter(t)
	srv := backaiRuntime(t)
	out, errOut, code := runStarter(t, dir, "AF_STACK_URL="+srv.URL)
	if code != 0 || !strings.Contains(out, "Registered agents: supportdesk") {
		t.Fatalf("code=%d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	// The demo call round-trips through the gateway and comes back echoed.
	if !strings.Contains(out, `Echo agent replied: {"message":"hello from `+srv.URL+`"}`) {
		t.Fatalf("starter did not call supportdesk.echo:\n%s", out)
	}
}

// Contract: `cp .env.example .env` then editing AF_STACK_URL must actually
// take effect — the starter reads .env itself, with no dependency.
func TestNodeStarterReadsDotEnv(t *testing.T) {
	dir := scaffoldNodeStarter(t)
	srv := backaiRuntime(t)
	write(t, dir, ".env", "# local\nAF_STACK_URL="+srv.URL+"\nAF_STACK_API_KEY=\n")
	out, errOut, code := runStarter(t, dir)
	if code != 0 || !strings.Contains(out, "Talking to BackAI at "+srv.URL) {
		t.Fatalf("code=%d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
}

// Contract: pasting the "API runtime" URL as printed (with /api/v1) works.
func TestNodeStarterToleratesApiV1Suffix(t *testing.T) {
	dir := scaffoldNodeStarter(t)
	srv := backaiRuntime(t)
	out, errOut, code := runStarter(t, dir, "AF_STACK_URL="+srv.URL+"/api/v1/")
	if code != 0 || !strings.Contains(out, "supportdesk") {
		t.Fatalf("code=%d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
}

// Contract: when the port is held by something that is not a BackAI runtime
// (an AgentField control plane, say — the reason af-stack dev moved the API
// off :8080), the starter says so and tells the user where the URL comes from.
func TestNodeStarterExplainsForeignServer(t *testing.T) {
	dir := scaffoldNodeStarter(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"healthy","checks":{}}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"endpoint_not_found"}`, http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, errOut, code := runStarter(t, dir, "AF_STACK_URL="+srv.URL)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d\n%s", code, errOut)
	}
	for _, want := range []string{"not a BackAI runtime", "af-stack dev", "AF_STACK_URL"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr missing %q:\n%s", want, errOut)
		}
	}
}

// Contract: with nothing listening at all, the starter says that and points
// at af-stack dev.
func TestNodeStarterExplainsNothingListening(t *testing.T) {
	dir := scaffoldNodeStarter(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := "http://" + l.Addr().String()
	_ = l.Close()

	_, errOut, code := runStarter(t, dir, "AF_STACK_URL="+closed)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d\n%s", code, errOut)
	}
	for _, want := range []string{"Nothing is listening at " + closed, "af-stack dev"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr missing %q:\n%s", want, errOut)
		}
	}
}

// Contract: an auth rejection is named as such, with the fix.
func TestNodeStarterExplainsAuthRejection(t *testing.T) {
	dir := scaffoldNodeStarter(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"alive"}`))
	})
	mux.HandleFunc("/api/v1/agents", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"code":"UNAUTHORIZED"}`, http.StatusUnauthorized)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, errOut, code := runStarter(t, dir, "AF_STACK_URL="+srv.URL)
	if code != 1 || !strings.Contains(errOut, "AF_STACK_API_KEY") {
		t.Fatalf("code=%d\n%s", code, errOut)
	}
}

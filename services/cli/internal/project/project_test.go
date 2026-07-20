// SPDX-License-Identifier: Apache-2.0

package project

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/cli/internal/client"
)

func TestScaffoldCommands(t *testing.T) {
	root := fakeRepo(t)
	restore := chdir(t, root)
	defer restore()

	var stdout bytes.Buffer
	if err := RunAgent([]string{"new", "Research Agent"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunAgent returned error: %v", err)
	}
	if !exists(filepath.Join(root, "apps/backend/agents/research-agent/main.py")) {
		t.Fatal("agent main.py was not created")
	}

	if err := RunModule([]string{"new", "Notes"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunModule returned error: %v", err)
	}
	// The scaffold emits the declarative manifest shape (PRD R2): a
	// backai.module.yaml with resources, plus an RLS-compliant migration.
	got := read(t, root, "workload-modules/notes/backai.module.yaml")
	for _, want := range []string{"id: notes", "resources:", "name: items", "type: string"} {
		if !strings.Contains(got, want) {
			t.Fatalf("scaffolded manifest missing %q:\n%s", want, got)
		}
	}
	mig := read(t, root, "workload-modules/notes/migrations/00001_init.sql")
	for _, want := range []string{"tenant_id", "force row level security", "create policy"} {
		if !strings.Contains(mig, want) {
			t.Fatalf("scaffolded migration missing %q:\n%s", want, mig)
		}
	}

	if err := RunPlugin([]string{"new", "Tenant Health"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunPlugin returned error: %v", err)
	}
	if got := read(t, root, "apps/dashboard/plugins/tenant-health/plugin.ts"); !strings.Contains(got, `id: "tenant-health"`) {
		t.Fatalf("unexpected plugin manifest:\n%s", got)
	}
}

func TestAdapterNewScaffoldsSidecar(t *testing.T) {
	root := t.TempDir()
	restore := chdir(t, root)
	defer restore()

	var stdout bytes.Buffer
	if err := RunAdapter(context.Background(), nil, []string{"new", "billing", "my-stripe"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunAdapter new returned error: %v", err)
	}
	for _, rel := range []string{"main.py", "requirements.txt", "README.md", "Dockerfile", ".gitignore"} {
		if !exists(filepath.Join(root, "my-stripe", rel)) {
			t.Fatalf("adapter scaffold missing %s", rel)
		}
	}
	// The skeleton must wire the chosen slot through to the contract + docs.
	main := read(t, root, "my-stripe/main.py")
	for _, want := range []string{`"slot": "billing"`, "/v1/capabilities", "/healthz", "protocols/billing-v1.md"} {
		if !strings.Contains(main, want) {
			t.Fatalf("main.py missing %q:\n%s", want, main)
		}
	}
	// Next steps must show the conformance command + the swap env var.
	out := stdout.String()
	for _, want := range []string{"backai-adapter-conformance --slot billing", "AF_STACK_BILLING_ADAPTER=remote"} {
		if !strings.Contains(out, want) {
			t.Fatalf("adapter new output missing %q:\n%s", want, out)
		}
	}
}

func TestAdapterNewDefaultsNameAndRejectsUnknownSlot(t *testing.T) {
	root := t.TempDir()
	restore := chdir(t, root)
	defer restore()

	// Unknown slot is rejected clearly.
	err := RunAdapter(context.Background(), nil, []string{"new", "rocketship"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unknown slot") {
		t.Fatalf("expected unknown-slot error, got %v", err)
	}

	// With no name, the directory defaults to <slot>-adapter.
	if err := RunAdapter(context.Background(), nil, []string{"new", "storage"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunAdapter new returned error: %v", err)
	}
	if !exists(filepath.Join(root, "storage-adapter", "main.py")) {
		t.Fatal("expected storage-adapter/main.py with defaulted name")
	}
}

// adapterRegistryJSON is a representative GET /api/v1/admin/adapters payload:
// a healthy storage slot and a sandbox slot whose active adapter is a stub
// that errors on every call (the old static table lied and called this
// "READY"). It exercises the truth contract end-to-end.
const adapterRegistryJSON = `{
  "slots": [
    {
      "slot": "storage",
      "tier": 1,
      "available_builtin": ["minio", "s3"],
      "swap_method": "env_var",
      "swap_env": "AF_STACK_S3_ADAPTER",
      "active": {"name": "minio", "status": "healthy", "kind": "builtin"}
    },
    {
      "slot": "sandbox",
      "tier": 2,
      "available_builtin": ["docker", "firecracker"],
      "swap_method": "env_var",
      "swap_env": "AF_STACK_SANDBOX_ADAPTER",
      "active": {"name": "firecracker", "status": "unhealthy", "kind": "builtin", "last_error": "firecracker adapter is a stub"}
    }
  ]
}`

func adapterTestServer(t *testing.T, status int, body string) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/adapters" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return &client.Client{BaseURL: srv.URL, HTTP: srv.Client()}
}

// Contract: `adapter list` renders one row per live slot with the active
// adapter, its real status, available builtins, swap env, and method —
// sourced from the runtime registry, never a static table. A stub adapter
// must surface as unhealthy, not as "READY".
func TestAdapterListRendersLiveRegistry(t *testing.T) {
	c := adapterTestServer(t, http.StatusOK, adapterRegistryJSON)
	var stdout bytes.Buffer
	if err := RunAdapter(context.Background(), c, []string{"list"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunAdapter list returned error: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"AREA", "ACTIVE", "STATUS", "AVAILABLE", "SWAP ENV", "METHOD",
		"storage", "minio", "healthy", "AF_STACK_S3_ADAPTER",
		"sandbox", "firecracker", "unhealthy", "AF_STACK_SANDBOX_ADAPTER",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("adapter list missing %q:\n%s", want, out)
		}
	}
	// Truth contract: the failing stub's error must be surfaced, and the
	// firecracker adapter must never be presented as ready/healthy.
	if !strings.Contains(out, "firecracker adapter is a stub") {
		t.Fatalf("adapter list hides the stub's last_error:\n%s", out)
	}
	if strings.Contains(out, "PLANNED") {
		t.Fatalf("adapter list still renders the retired static PLANNED table:\n%s", out)
	}
}

// Contract: `--json` emits the runtime registry verbatim for agents.
func TestAdapterListJSONPassthrough(t *testing.T) {
	c := adapterTestServer(t, http.StatusOK, adapterRegistryJSON)
	var stdout bytes.Buffer
	if err := RunAdapter(context.Background(), c, []string{"list", "--json"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunAdapter list --json returned error: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{`"slot"`, `"firecracker"`, `"last_error"`, `"swap_env"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("adapter list --json missing %q:\n%s", want, out)
		}
	}
}

// Contract: when the runtime/operator API is unreachable or rejects auth,
// the command must NOT fabricate a table — it returns actionable guidance.
func TestAdapterListDegradesGracefully(t *testing.T) {
	// 401 from a reachable server.
	c := adapterTestServer(t, http.StatusUnauthorized,
		`{"error":{"code":"UNAUTHORIZED","message":"operator key required"}}`)
	var stdout bytes.Buffer
	err := RunAdapter(context.Background(), c, []string{"list"}, &stdout, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected an error when the adapters endpoint rejects the request")
	}
	for _, want := range []string{"AF_STACK_URL", "AF_STACK_API_KEY", "operator key"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("degradation message missing %q: %v", want, err)
		}
	}
	if strings.Contains(stdout.String(), "firecracker") || strings.Contains(stdout.String(), "AREA") {
		t.Fatalf("adapter list fabricated a table on failure:\n%s", stdout.String())
	}

	// Connection refused (no runtime) must degrade the same way. Spin up a
	// server just to claim a port, then close it so the dial is refused
	// immediately (no timeout wait).
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	unreachableURL := closed.URL
	closed.Close()
	unreachable := &client.Client{BaseURL: unreachableURL, HTTP: closed.Client()}
	err = RunAdapter(context.Background(), unreachable, []string{"list"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "AF_STACK_URL") {
		t.Fatalf("expected connection-refused to yield guidance, got %v", err)
	}
}

func TestDeployCommandSelection(t *testing.T) {
	tests := map[string]string{
		"helm":    "helm upgrade --install af-stack ./deploy/helm/af-stack",
		"fly":     "fly deploy -c deploy/fly/fly.toml",
		"railway": "railway up",
		"render":  "render deploy",
	}
	for target, want := range tests {
		name, args, err := deployCommand(target)
		if err != nil {
			t.Fatalf("deployCommand(%q) returned error: %v", target, err)
		}
		got := strings.TrimSpace(name + " " + strings.Join(args, " "))
		if got != want {
			t.Fatalf("deployCommand(%q) = %q, want %q", target, got, want)
		}
	}
}

func TestRunDeployInvokesRunner(t *testing.T) {
	root := fakeRepo(t)
	restoreCwd := chdir(t, root)
	defer restoreCwd()
	old := runCommand
	defer func() { runCommand = old }()

	var gotDir, gotName string
	var gotArgs []string
	runCommand = func(_ context.Context, dir string, name string, args []string, _, _ io.Writer) error {
		gotDir = dir
		gotName = name
		gotArgs = append([]string(nil), args...)
		return nil
	}

	if err := RunDeploy(context.Background(), []string{"--target", "helm"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunDeploy returned error: %v", err)
	}
	if gotDir == "" || gotName != "helm" || strings.Join(gotArgs, " ") != "upgrade --install af-stack ./deploy/helm/af-stack" {
		t.Fatalf("unexpected runner call: dir=%q name=%q args=%v", gotDir, gotName, gotArgs)
	}
}

func TestRunOperatorCreateRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("AF_STACK_DATABASE_URL", "")
	err := RunOperator(
		context.Background(),
		[]string{"create", "--email", "founder@example.com"},
		&bytes.Buffer{},
		&bytes.Buffer{},
	)
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("expected database URL error, got %v", err)
	}
}

func fakeRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, "package.json", "{}")
	write(t, root, "apps/dashboard/.keep", "")
	write(t, root, "apps/customer-app/.keep", "")
	return root
}

func chdir(t *testing.T, dir string) func() {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Chdir(old); err != nil {
			t.Fatal(err)
		}
	}
}

func write(t *testing.T, root, rel, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestReadEnvValue(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".env", "AF_STACK_DASHBOARD_PORT=33200\n# comment\nAF_STACK_PORT = \"8280\"\n")

	if got := readEnvValue(root, "AF_STACK_DASHBOARD_PORT", "33000"); got != "33200" {
		t.Errorf("dashboard port = %q, want 33200", got)
	}
	// Quotes and surrounding spaces are trimmed.
	if got := readEnvValue(root, "AF_STACK_PORT", "8080"); got != "8280" {
		t.Errorf("runtime port = %q, want 8280", got)
	}
	// Missing key falls back to the default.
	if got := readEnvValue(root, "MISSING_KEY", "33000"); got != "33000" {
		t.Errorf("missing key = %q, want default 33000", got)
	}
	// Process env takes precedence over .env.
	t.Setenv("AF_STACK_DASHBOARD_PORT", "39999")
	if got := readEnvValue(root, "AF_STACK_DASHBOARD_PORT", "33000"); got != "39999" {
		t.Errorf("env override = %q, want 39999", got)
	}
}

func TestReadEnvValueNoFile(t *testing.T) {
	root := t.TempDir() // no .env
	if got := readEnvValue(root, "AF_STACK_DASHBOARD_PORT", "33000"); got != "33000" {
		t.Errorf("no .env = %q, want default 33000", got)
	}
}

// --no-preflight starts the stack with a single `docker compose up` and never
// shells out to the preflight script.
func TestRunDevNoPreflightRunsComposeOnly(t *testing.T) {
	root := fakeRepo(t)
	restoreCwd := chdir(t, root)
	defer restoreCwd()
	old := runCommand
	defer func() { runCommand = old }()

	var calls [][]string
	runCommand = func(_ context.Context, _ string, name string, args []string, _, _ io.Writer) error {
		calls = append(calls, append([]string{name}, args...))
		return nil
	}

	if err := RunDev(context.Background(), []string{"--no-preflight", "--detach", "--no-open"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunDev returned error: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 command, got %d: %v", len(calls), calls)
	}
	if got := strings.Join(calls[0], " "); got != "docker compose up -d" {
		t.Fatalf("command = %q, want 'docker compose up -d'", got)
	}
}

// With no preflight script present, RunDev logs and continues to compose
// rather than failing.
func TestRunDevMissingPreflightScriptIsNonFatal(t *testing.T) {
	root := fakeRepo(t) // fakeRepo has no scripts/preflight.mjs
	restoreCwd := chdir(t, root)
	defer restoreCwd()
	old := runCommand
	defer func() { runCommand = old }()

	var composeRan bool
	runCommand = func(_ context.Context, _ string, name string, args []string, _, _ io.Writer) error {
		if name == "docker" && len(args) > 0 && args[0] == "compose" {
			composeRan = true
		}
		return nil
	}

	if err := RunDev(context.Background(), nil, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunDev returned error: %v", err)
	}
	if !composeRan {
		t.Fatal("docker compose was not run when preflight script was absent")
	}
}

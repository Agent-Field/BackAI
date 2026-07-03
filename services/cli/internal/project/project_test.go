// SPDX-License-Identifier: Apache-2.0

package project

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if got := read(t, root, "workload-modules/notes/manifest.yaml"); !strings.Contains(got, "id: notes") {
		t.Fatalf("unexpected module manifest:\n%s", got)
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
	if err := RunAdapter([]string{"new", "billing", "my-stripe"}, &stdout, &bytes.Buffer{}); err != nil {
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
	err := RunAdapter([]string{"new", "rocketship"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unknown slot") {
		t.Fatalf("expected unknown-slot error, got %v", err)
	}

	// With no name, the directory defaults to <slot>-adapter.
	if err := RunAdapter([]string{"new", "storage"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunAdapter new returned error: %v", err)
	}
	if !exists(filepath.Join(root, "storage-adapter", "main.py")) {
		t.Fatal("expected storage-adapter/main.py with defaulted name")
	}
}

func TestAdapterListUsesEnvOverride(t *testing.T) {
	t.Setenv("AF_STACK_SANDBOX_ADAPTER", "gvisor")
	var stdout bytes.Buffer
	if err := RunAdapter([]string{"list"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunAdapter returned error: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"Storage", "minio", "Sandbox", "gvisor", "Billing"} {
		if !strings.Contains(out, want) {
			t.Fatalf("adapter list missing %q:\n%s", want, out)
		}
	}
	// Truth contract: the table must distinguish adapters the runtime actually
	// constructs from ones the docs mark as planned. "remote" is a real sandbox
	// adapter (main.go newSandbox) and must be listed; planned entries must
	// appear under a PLANNED column, never presented as ready-to-use choices.
	if !strings.Contains(out, "PLANNED") {
		t.Fatalf("adapter list missing PLANNED column:\n%s", out)
	}
	if !strings.Contains(out, "remote") {
		t.Fatalf("adapter list omits the real 'remote' sandbox adapter:\n%s", out)
	}
	readyLine := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Storage") {
			readyLine = line
		}
	}
	// r2/gcs/azure-blob are planned; they must sit in the PLANNED column, to the
	// right of the READY column — never adjacent to the ready "minio, s3" set.
	if strings.Contains(readyLine, "minio, s3, r2") {
		t.Fatalf("planned storage adapters leaked into the READY column:\n%s", readyLine)
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

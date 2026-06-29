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

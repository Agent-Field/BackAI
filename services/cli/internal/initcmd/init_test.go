// SPDX-License-Identifier: Apache-2.0

package initcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesBrandCopiesLogoAndUpdatesAgent(t *testing.T) {
	root := t.TempDir()
	write(t, root, "package.json", "{}")
	write(t, root, "apps/dashboard/.keep", "")
	write(t, root, "apps/customer-app/.keep", "")
	write(t, root, "brand.yaml", `schema_version: 1
name: af-stack
codename: af-stack
display_name: AF Stack
short_description: Old description.
palette:
  primary: "#111827"
  accent: "#16A34A"
  dark_mode: true
logos:
  light: ./brand/logo-light.svg
  dark: ./brand/logo-dark.svg
  favicon: ./brand/favicon.ico
domains:
  dashboard: admin.localhost
  customer_app: app.localhost
  api: api.localhost
surfaces:
  dashboard:
    display_name: AF Stack
    subtitle: Operator console
    description: Old description.
  customer_app:
    display_name: AF Stack
    subtitle: Customer app
    description: Customer app for AF Stack.
`)
	write(t, root, "docker-compose.yml", "services:\n  sample-agent:\n    environment:\n      NODE_ID: sample\n")
	write(t, root, "apps/backend/agents/sample/Dockerfile", "ENV NODE_ID=sample \\\n    PORT=8090\n")
	write(t, root, "apps/backend/agents/sample/main.py", `app = Agent(
    node_id=os.getenv("NODE_ID", "sample"),
)
`)
	write(t, root, "apps/backend/agents/sample/README.md", "curl /agents/sample.echo\n")
	write(t, root, "apps/backend/litellm-config.yaml", "# sample-agent points at this by default.\n")
	logo := filepath.Join(root, "source-logo.png")
	if err := os.WriteFile(logo, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	restoreCwd := chdir(t, root)
	defer restoreCwd()
	restoreGenerator := stubGenerator(t, func(gotRoot string) error {
		wantRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			t.Fatal(err)
		}
		gotRoot, err = filepath.EvalSymlinks(gotRoot)
		if err != nil {
			t.Fatal(err)
		}
		if gotRoot != wantRoot {
			t.Fatalf("generator root = %q, want %q", gotRoot, wantRoot)
		}
		write(t, root, "apps/dashboard/public/brand/logo-light.png", "png")
		write(t, root, "apps/customer-app/public/brand/logo-light.png", "png")
		return nil
	})
	defer restoreGenerator()

	var stdout bytes.Buffer
	if err := Run([]string{"--name", "DocuChat", "--color", "#0a66c2", "--logo", logo}, strings.NewReader(""), &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	brand := read(t, root, "brand.yaml")
	for _, want := range []string{
		"name: docuchat",
		"codename: docuchat",
		"display_name: DocuChat",
		"primary: '#0A66C2'",
		"light: ./brand/logo.png",
		"dark: ./brand/logo.png",
	} {
		if !strings.Contains(brand, want) {
			t.Fatalf("brand.yaml missing %q:\n%s", want, brand)
		}
	}
	if got := read(t, root, "brand/logo.png"); got != "png" {
		t.Fatalf("copied logo = %q", got)
	}
	if got := read(t, root, "docker-compose.yml"); !strings.Contains(got, "NODE_ID: docuchat") {
		t.Fatalf("compose not updated:\n%s", got)
	}
	if got := read(t, root, "apps/backend/agents/sample/main.py"); !strings.Contains(got, `os.getenv("NODE_ID", "docuchat")`) {
		t.Fatalf("agent source not updated:\n%s", got)
	}
	if !strings.Contains(stdout.String(), "default agent node_id set to docuchat") {
		t.Fatalf("unexpected stdout:\n%s", stdout.String())
	}
}

func TestRunRejectsInvalidColor(t *testing.T) {
	root := t.TempDir()
	write(t, root, "package.json", "{}")
	write(t, root, "apps/dashboard/.keep", "")
	write(t, root, "apps/customer-app/.keep", "")
	restoreCwd := chdir(t, root)
	defer restoreCwd()

	err := Run([]string{"--name", "DocuChat", "--color", "blue"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--color must be") {
		t.Fatalf("expected color error, got %v", err)
	}
}

func TestSlugify(t *testing.T) {
	tests := map[string]string{
		"DocuChat":         "docuchat",
		"Legal AI Backend": "legal-ai-backend",
		"  ---  ":          "app",
		"CRM.Core_v2":      "crm-core-v2",
	}
	for input, want := range tests {
		if got := slugify(input); got != want {
			t.Fatalf("slugify(%q) = %q, want %q", input, got, want)
		}
	}
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

func stubGenerator(t *testing.T, fn func(string) error) func() {
	t.Helper()
	old := runBrandGenerator
	runBrandGenerator = fn
	return func() { runBrandGenerator = old }
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

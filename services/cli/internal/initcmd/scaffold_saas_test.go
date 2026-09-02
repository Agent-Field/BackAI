// SPDX-License-Identifier: Apache-2.0

package initcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Agent-Field/backai/services/cli/internal/validate"
)

// saasExpectedFiles is the golden manifest of the --template saas tree.
var saasExpectedFiles = []string{
	"package.json",
	"index.html",
	"vite.config.ts",
	"vitest.config.ts",
	"tsconfig.json",
	"tsconfig.node.json",
	".env.example",
	".gitignore",
	"src/main.tsx",
	"src/App.tsx",
	"src/vite-env.d.ts",
	"src/api/client.ts",
	"src/api/client.test.ts",
	"src/pages/Login.tsx",
	"src/pages/Notes.tsx",
	"modules/notes/backai.module.yaml",
	"modules/notes/migrations/00001_init.sql",
	"modules/notes/README.md",
	"agents/notes-assistant/main.py",
	"agents/notes-assistant/requirements.txt",
	"agents/notes-assistant/Dockerfile",
	"docker-compose.reference.yml",
	"docker-compose.yml",
	"backend/postgres-init.sh",
	"backend/litellm-config.yaml",
	"capabilities.json",
	"README.md",
	"AGENTS.md",
	"CLAUDE.md",
}

// Contract: `af-stack init <name> --template saas` scaffolds a COMPLETE
// project — customer app, one domain module (notes) with a tenant-isolated
// migration, one agent, tests, env + compose references, agent docs, and a
// machine-readable capabilities.json.
func TestScaffoldSaaS_GoldenTree(t *testing.T) {
	root := t.TempDir()
	restore := chdir(t, root)
	defer restore()

	var stdout, stderr bytes.Buffer
	if err := Run([]string{"Saas Demo", "--template", "saas"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("saas scaffold error: %v\nstderr=%s", err, stderr.String())
	}

	proj := filepath.Join(root, "saas-demo")
	for _, rel := range saasExpectedFiles {
		if _, err := os.Stat(filepath.Join(proj, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected scaffold file %s: %v", rel, err)
		}
	}

	// JSON assets must parse.
	for _, rel := range []string{"package.json", "capabilities.json", "tsconfig.json", "tsconfig.node.json"} {
		var v any
		if err := json.Unmarshal([]byte(read(t, proj, rel)), &v); err != nil {
			t.Fatalf("%s is not valid JSON: %v", rel, err)
		}
	}

	// The manifest must be valid YAML with the expected id.
	var manifest map[string]any
	if err := yaml.Unmarshal([]byte(read(t, proj, "modules/notes/backai.module.yaml")), &manifest); err != nil {
		t.Fatalf("module manifest is not valid YAML: %v", err)
	}
	if manifest["id"] != "notes" {
		t.Fatalf("module id = %v, want notes", manifest["id"])
	}

	// End-to-end: the generated notes module must pass the platform's own
	// module validator — manifest shape AND the tenant-isolated migration
	// (tenant_id + FORCE RLS + policy on app.tenant_id).
	res := validate.ModuleDir(filepath.Join(proj, "modules", "notes"))
	if !res.OK {
		t.Fatalf("generated notes module fails validation: %+v", res.Findings)
	}

	// The API client must be auth-aware and consume the /api/v1 abstraction.
	client := read(t, proj, "src/api/client.ts")
	for _, want := range []string{"/api/v1", "Bearer", "getToken", "af_stack_token"} {
		if !strings.Contains(client, want) {
			t.Fatalf("api client missing %q", want)
		}
	}

	// capabilities.json must carry constraints + urls for coding agents.
	caps := read(t, proj, "capabilities.json")
	for _, want := range []string{"constraints", "api_prefix", "customer_app", "FORCE row level security"} {
		if !strings.Contains(caps, want) {
			t.Fatalf("capabilities.json missing %q", want)
		}
	}

	if !strings.Contains(stdout.String(), "af-stack test") {
		t.Fatalf("saas next-steps should mention `af-stack test`:\n%s", stdout.String())
	}
}

// Contract: --json emits the created file list as a stable, parseable
// document (for agents scripting the scaffold).
func TestScaffoldSaaS_JSON(t *testing.T) {
	root := t.TempDir()
	restore := chdir(t, root)
	defer restore()

	var stdout bytes.Buffer
	if err := Run([]string{"jsonapp", "--template", "saas", "--json"}, strings.NewReader(""), &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("saas scaffold --json error: %v", err)
	}
	var doc struct {
		Project  string   `json:"project"`
		Template string   `json:"template"`
		Files    []string `json:"files"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("--json output is not valid JSON: %v\n%s", err, stdout.String())
	}
	if doc.Project != "jsonapp" || doc.Template != "saas" {
		t.Fatalf("unexpected doc: %+v", doc)
	}
	if len(doc.Files) < len(saasExpectedFiles) {
		t.Fatalf("expected >= %d files, got %d", len(saasExpectedFiles), len(doc.Files))
	}
}

// Contract: the dependency-free API client typechecks with a bare tsc + DOM
// lib. Skips when tsc is not installed (deterministic in CI without a TS
// toolchain).
func TestScaffoldSaaS_ClientTypechecks(t *testing.T) {
	tscPath, err := exec.LookPath("tsc")
	if err != nil {
		t.Skip("tsc not on PATH; skipping scaffold typecheck (matches the offline gate)")
	}
	files := SaaSTemplateFiles("Probe", "probe")
	src := files[SaaSStandaloneClientPath]
	dir := t.TempDir()
	file := filepath.Join(dir, "client.ts")
	if err := os.WriteFile(file, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, tscPath,
		"--noEmit", "--strict", "--skipLibCheck",
		"--target", "ES2020", "--module", "ESNext",
		"--moduleResolution", "bundler", "--lib", "ES2020,DOM",
		file)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("scaffold API client failed tsc:\n%s", out)
	}
}

// TestScaffoldSaaS_ValidateCommandsUsePathForm pins that every generated
// mention of `af-stack {module,agent} validate` names a directory. The bare
// id form (`af-stack module validate notes`) exits 4 - the scaffold used to
// ship it in three places.
func TestScaffoldSaaS_ValidateCommandsUsePathForm(t *testing.T) {
	files := SaaSTemplateFiles("Notes App", "notes-app")
	for rel, contents := range files {
		for _, bare := range []string{
			"af-stack module validate notes",
			"af-stack agent validate notes-assistant",
		} {
			if strings.Contains(contents, bare) {
				t.Fatalf("%s ships the bare-id validate form %q (exits 4); use the path form", rel, bare)
			}
		}
	}
	for _, tc := range []struct{ rel, want string }{
		{"README.md", "af-stack module validate modules/notes"},
		{"README.md", "af-stack agent validate agents/notes-assistant"},
		{"modules/notes/README.md", "af-stack module validate modules/notes"},
		{"capabilities.json", "af-stack module validate modules/notes"},
		{"capabilities.json", "af-stack agent validate agents/notes-assistant"},
	} {
		if !strings.Contains(files[tc.rel], tc.want) {
			t.Fatalf("%s missing %q:\n%s", tc.rel, tc.want, files[tc.rel])
		}
	}
}

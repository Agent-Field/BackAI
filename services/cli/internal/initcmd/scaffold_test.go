// SPDX-License-Identifier: Apache-2.0

package initcmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Contract: `af-stack init <name>` creates ./<slug>/ with a complete,
// install-able starter project (no fork required).
func TestScaffold_CreatesConsumingProject(t *testing.T) {
	root := t.TempDir()
	restore := chdir(t, root)
	defer restore()

	var stdout, stderr bytes.Buffer
	if err := Run([]string{"Docu Chat"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("Run scaffold returned error: %v\nstderr=%s", err, stderr.String())
	}

	proj := filepath.Join(root, "docu-chat")
	for _, rel := range []string{"package.json", "src/index.mjs", ".env.example", ".gitignore", "README.md"} {
		if _, err := os.Stat(filepath.Join(proj, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected scaffold file %s: %v", rel, err)
		}
	}

	// package.json must be valid JSON, name = slug, and have zero runtime
	// dependencies so `npm install` always succeeds.
	raw := read(t, proj, "package.json")
	var pkg map[string]any
	if err := json.Unmarshal([]byte(raw), &pkg); err != nil {
		t.Fatalf("package.json is not valid JSON: %v\n%s", err, raw)
	}
	if pkg["name"] != "docu-chat" {
		t.Fatalf("package.json name = %v, want docu-chat", pkg["name"])
	}
	if _, hasDeps := pkg["dependencies"]; hasDeps {
		t.Fatalf("starter must have zero runtime dependencies, got: %s", raw)
	}

	// The starter consumes the backend abstraction (single base URL + /api/v1),
	// not per-service URLs.
	index := read(t, proj, "src/index.mjs")
	if !strings.Contains(index, "AF_STACK_URL") || !strings.Contains(index, "/api/v1") {
		t.Fatalf("starter does not consume the AF_STACK_URL abstraction:\n%s", index)
	}

	if !strings.Contains(stdout.String(), "Next steps") {
		t.Fatalf("expected next-steps guidance in output:\n%s", stdout.String())
	}
}

// Contract: the name can come from --name instead of a positional.
func TestScaffold_NameFlag(t *testing.T) {
	root := t.TempDir()
	restore := chdir(t, root)
	defer restore()
	if err := Run([]string{"--name", "Legal AI"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "legal-ai", "package.json")); err != nil {
		t.Fatalf("expected legal-ai project: %v", err)
	}
}

// Contract: scaffolding refuses to clobber a non-empty directory.
func TestScaffold_RefusesNonEmptyDir(t *testing.T) {
	root := t.TempDir()
	restore := chdir(t, root)
	defer restore()
	// Pre-create a non-empty target.
	write(t, root, filepath.Join("taken", "keep.txt"), "x")

	err := Run([]string{"taken"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "already exists and is not empty") {
		t.Fatalf("expected non-empty-dir refusal, got %v", err)
	}
}

// Contract: an unknown template is rejected clearly.
func TestScaffold_UnknownTemplate(t *testing.T) {
	root := t.TempDir()
	restore := chdir(t, root)
	defer restore()
	err := Run([]string{"app", "--template", "rails"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unknown template") {
		t.Fatalf("expected unknown-template error, got %v", err)
	}
}

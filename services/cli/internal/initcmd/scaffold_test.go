// SPDX-License-Identifier: Apache-2.0

package initcmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
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
	for _, rel := range []string{"package.json", "src/index.mjs", ".env.example", ".gitignore", "README.md", "CLAUDE.md"} {
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

	// CLAUDE.md orients a coding agent opened inside the scaffold — it must
	// say the project consumes a backend rather than forking the stack.
	claude := read(t, proj, "CLAUDE.md")
	if !strings.Contains(claude, "/api/v1") || !strings.Contains(claude, "CONSUMES") {
		t.Fatalf("CLAUDE.md does not orient agents to the consumer model:\n%s", claude)
	}

	if !strings.Contains(stdout.String(), "Next steps") {
		t.Fatalf("expected next-steps guidance in output:\n%s", stdout.String())
	}
}

// Contract: all-flag invocations (no positional name) stay on the legacy
// in-checkout path — `af-stack init --name X` must NOT scaffold a new
// project directory, so existing rebrand docs and acceptance flows keep
// working unchanged.
func TestScaffold_FlagOnlyInvocationStaysLegacy(t *testing.T) {
	root := t.TempDir()
	restore := chdir(t, root)
	defer restore()

	// Outside an AF Stack checkout the legacy path fails on repo-root
	// discovery — which proves dispatch did not go to the scaffold.
	err := Run([]string{"--name", "Legal AI"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("expected legacy path to error outside a checkout")
	}
	if _, statErr := os.Stat(filepath.Join(root, "legal-ai")); !os.IsNotExist(statErr) {
		t.Fatalf("flag-only invocation must not scaffold a project dir, stat err = %v", statErr)
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

// Contract: an unknown template is rejected clearly, and the error points
// power users at the in-checkout hero template.
func TestScaffold_UnknownTemplate(t *testing.T) {
	root := t.TempDir()
	restore := chdir(t, root)
	defer restore()
	err := Run([]string{"app", "--template", "rails"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unknown template") {
		t.Fatalf("expected unknown-template error, got %v", err)
	}
	if !strings.Contains(err.Error(), "coding-agent") {
		t.Fatalf("unknown-template error should mention the in-checkout coding-agent path, got %v", err)
	}
}

// TestInitHelpNamesBothModes pins the cross-references between init's two
// flag sets. `af-stack init` (flags only) re-themes a checkout; `af-stack
// init <name>` scaffolds a standalone app. Neither used to mention the
// other, and the flag form advertised --template coding-agent to readers
// standing outside a checkout, where the positional form rejects it.
func TestInitHelpNamesBothModes(t *testing.T) {
	var stderr bytes.Buffer
	if err := Run([]string{"-h"}, strings.NewReader(""), &bytes.Buffer{}, &stderr); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("Run(-h) error = %v, want flag.ErrHelp", err)
	}
	for _, want := range []string{
		"in-checkout re-theme",
		"af-stack init <name>",
		"flag-form only; requires a BackAI checkout",
		"brand/logo.<ext>",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("init --help missing %q:\n%s", want, stderr.String())
		}
	}

	stderr.Reset()
	if err := Run([]string{"acme", "-h"}, strings.NewReader(""), &bytes.Buffer{}, &stderr); err == nil {
		t.Fatal("Run(<name> -h) should not succeed")
	}
	for _, want := range []string{
		"af-stack init <name>",
		"node | saas",
		"checkout-only",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("init <name> --help missing %q:\n%s", want, stderr.String())
		}
	}
}

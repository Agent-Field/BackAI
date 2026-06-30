// SPDX-License-Identifier: Apache-2.0

package initcmd

import (
	"bytes"
	"strings"
	"testing"
)

// minimalRepo writes the smallest tree Run needs: the repo-root markers
// plus the rebrand targets the default (node) path touches. Returns the
// root dir.
func minimalRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, "package.json", "{}")
	write(t, root, "apps/dashboard/.keep", "")
	write(t, root, "apps/customer-app/.keep", "")
	write(t, root, "docker-compose.yml", "services:\n  sample-agent:\n    environment:\n      NODE_ID: sample\n")
	return root
}

// runInit runs Run from inside root with a stubbed brand generator and
// returns stdout. Fails the test on error.
func runInit(t *testing.T, root string, args ...string) string {
	t.Helper()
	restoreCwd := chdir(t, root)
	defer restoreCwd()
	restoreGen := stubGenerator(t, func(string) error { return nil })
	defer restoreGen()
	var stdout bytes.Buffer
	if err := Run(args, strings.NewReader(""), &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(%v) error: %v", args, err)
	}
	return stdout.String()
}

// T1: the default template (node) does NOT scaffold a coding agent —
// back-compat with the historical rebrand-only behaviour.
func TestInitDefaultTemplateScaffoldsNoAgent(t *testing.T) {
	root := minimalRepo(t)
	out := runInit(t, root, "--name", "DocuChat")
	if exists(root + "/apps/backend/agents/coding-agent/main.py") {
		t.Fatal("default template should not scaffold the coding agent")
	}
	if !strings.Contains(out, "template: node") {
		t.Fatalf("expected node template in output:\n%s", out)
	}
}

// T2 + T3: --template coding-agent scaffolds the four files, and the
// generated agent registers the canonical node_id and reads GH_TOKEN.
func TestInitCodingAgentTemplateScaffolds(t *testing.T) {
	root := minimalRepo(t)
	out := runInit(t, root, "--name", "AcmeCoder", "--template", "coding-agent")

	for _, rel := range []string{
		"apps/backend/agents/coding-agent/main.py",
		"apps/backend/agents/coding-agent/Dockerfile",
		"apps/backend/agents/coding-agent/requirements.txt",
		"apps/backend/agents/coding-agent/README.md",
	} {
		if !exists(root + "/" + rel) {
			t.Fatalf("expected scaffolded file %s", rel)
		}
	}

	main := read(t, root, "apps/backend/agents/coding-agent/main.py")
	for _, want := range []string{
		`node_id=os.getenv("NODE_ID", "coding-agent")`, // canonical node_id
		`os.getenv("GH_TOKEN"`,                          // credential from the secret slot
		"async def run(",                               // the reachable reasoner
		"NotImplementedError",                          // honest seam, not a fake artifact
	} {
		if !strings.Contains(main, want) {
			t.Fatalf("scaffolded main.py missing %q:\n%s", want, main)
		}
	}

	dockerfile := read(t, root, "apps/backend/agents/coding-agent/Dockerfile")
	if !strings.Contains(dockerfile, "NODE_ID=coding-agent") {
		t.Fatalf("Dockerfile missing canonical NODE_ID:\n%s", dockerfile)
	}

	if !strings.Contains(out, "scaffolded coding agent") || !strings.Contains(out, "GH_TOKEN") {
		t.Fatalf("expected scaffold summary in output:\n%s", out)
	}
}

// T4: an unknown template fails fast with a clear, enumerated error.
func TestInitRejectsUnknownTemplate(t *testing.T) {
	root := minimalRepo(t)
	restoreCwd := chdir(t, root)
	defer restoreCwd()
	restoreGen := stubGenerator(t, func(string) error { return nil })
	defer restoreGen()
	err := Run([]string{"--name", "X", "--template", "rust"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unknown --template") {
		t.Fatalf("expected unknown-template error, got %v", err)
	}
	if !strings.Contains(err.Error(), "coding-agent") {
		t.Fatalf("error should enumerate valid templates, got %v", err)
	}
}

// T5: re-running the coding-agent template leaves existing files
// untouched and reports that nothing was overwritten.
func TestInitCodingAgentIdempotent(t *testing.T) {
	root := minimalRepo(t)
	_ = runInit(t, root, "--name", "AcmeCoder", "--template", "coding-agent")
	// Mark main.py with a user edit; a second run must not clobber it.
	write(t, root, "apps/backend/agents/coding-agent/main.py", "# user edited\n")
	out := runInit(t, root, "--name", "AcmeCoder", "--template", "coding-agent")
	if got := read(t, root, "apps/backend/agents/coding-agent/main.py"); got != "# user edited\n" {
		t.Fatalf("second run clobbered user edit: %q", got)
	}
	if !strings.Contains(out, "left unchanged") {
		t.Fatalf("expected 'left unchanged' note on re-run:\n%s", out)
	}
}

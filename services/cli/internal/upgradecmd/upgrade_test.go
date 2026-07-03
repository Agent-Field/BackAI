// SPDX-License-Identifier: Apache-2.0

package upgradecmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// git runs a git command in dir, failing the test on error.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, buf.String())
	}
	return buf.String()
}

func write(t *testing.T, dir, rel, contents string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", msg)
}

// newForkPair creates an "upstream" repo with one platform file, one
// fork-surface file, and one migration — then clones it as the fork.
func newForkPair(t *testing.T) (upstream, fork string) {
	t.Helper()
	base := t.TempDir()
	upstream = filepath.Join(base, "upstream")
	fork = filepath.Join(base, "fork")
	if err := os.MkdirAll(upstream, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, upstream, "init", "-q", "-b", "main")
	git(t, upstream, "config", "user.email", "up@test")
	git(t, upstream, "config", "user.name", "Upstream")
	write(t, upstream, "services/runtime/server.go", "package main // v1\n")
	write(t, upstream, "apps/customer-app/src/page.tsx", "export default 1 // v1\n")
	write(t, upstream, migrationsDir+"/00001_init.sql", "-- +goose Up\nselect 1;\n-- +goose Down\n")
	commitAll(t, upstream, "v1")

	git(t, base, "clone", "-q", upstream, fork)
	git(t, fork, "config", "user.email", "fork@test")
	git(t, fork, "config", "user.name", "Fork")
	return upstream, fork
}

// run invokes Run with the fork as CWD, capturing output.
func run(t *testing.T, fork string, args ...string) (string, string, error) {
	t.Helper()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(fork); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	var stdout, stderr bytes.Buffer
	runErr := Run(args, strings.NewReader(""), &stdout, &stderr)
	return stdout.String(), stderr.String(), runErr
}

// Contract: fork-owned vs platform classification follows the "What You
// Edit" ownership table.
func TestClassifyPath(t *testing.T) {
	forkOwned := []string{
		"apps/customer-app/src/app/page.tsx",
		"apps/backend/agents/courtsim/main.py",
		"workload-modules/notes/routes.go",
		"apps/dashboard/plugins/metrics/tab.tsx",
		"brand.yaml",
		"docker-compose.yml",
	}
	platform := []string{
		"services/runtime/internal/server/server.go",
		"packages/sdk-py/af_stack/billing.py",
		"apps/dashboard/src/components/app-sidebar.tsx",
		"scripts/acceptance.sh",
	}
	for _, p := range forkOwned {
		if !classifyPath(p) {
			t.Errorf("%s should be fork-owned", p)
		}
	}
	for _, p := range platform {
		if classifyPath(p) {
			t.Errorf("%s should be platform", p)
		}
	}
}

// Contract: a dirty tree is refused before anything else happens.
func TestUpgrade_RefusesDirtyTree(t *testing.T) {
	_, fork := newForkPair(t)
	write(t, fork, "apps/customer-app/src/page.tsx", "dirty\n")
	_, _, err := run(t, fork, "--check", "--remote", "origin")
	if err == nil || !strings.Contains(err.Error(), "commit or stash") {
		t.Fatalf("expected dirty-tree refusal, got %v", err)
	}
}

// Contract: --check reports behind-count and incoming migrations without
// changing the fork.
func TestUpgradeCheck_ReportsMigrationsAndPosition(t *testing.T) {
	upstream, fork := newForkPair(t)

	// Fork gains a local customization; upstream gains a platform change
	// + a migration.
	write(t, fork, "apps/customer-app/src/custom.tsx", "custom\n")
	commitAll(t, fork, "fork customization")
	write(t, upstream, "services/runtime/server.go", "package main // v2\n")
	write(t, upstream, migrationsDir+"/00002_plans.sql", "-- +goose Up\nselect 2;\n-- +goose Down\n")
	commitAll(t, upstream, "v2 + migration")

	head := git(t, fork, "rev-parse", "HEAD")
	out, _, err := run(t, fork, "--check", "--remote", "origin")
	if err != nil {
		t.Fatalf("check: %v\n%s", err, out)
	}
	for _, want := range []string{
		"1 new commit(s)",
		"00002_plans.sql",
		"No merge conflicts predicted",
		"--check: no changes made",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("check output missing %q:\n%s", want, out)
		}
	}
	if got := git(t, fork, "rev-parse", "HEAD"); got != head {
		t.Error("--check must not move HEAD")
	}
}

// Contract: a conflict-free upgrade merges upstream and prints the
// rebuild-to-migrate next steps.
func TestUpgradeApply_CleanMerge(t *testing.T) {
	upstream, fork := newForkPair(t)
	write(t, fork, "apps/customer-app/src/custom.tsx", "custom\n")
	commitAll(t, fork, "fork customization")
	write(t, upstream, "services/runtime/newfile.go", "package main\n")
	write(t, upstream, migrationsDir+"/00002_plans.sql", "-- +goose Up\nselect 2;\n-- +goose Down\n")
	commitAll(t, upstream, "v2")

	out, _, err := run(t, fork, "--yes", "--skip-backup", "--remote", "origin")
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Upgraded to origin/main") {
		t.Errorf("missing success line:\n%s", out)
	}
	if !strings.Contains(out, "docker compose up -d --build") {
		t.Errorf("missing next steps:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(fork, "services/runtime/newfile.go")); err != nil {
		t.Error("upstream file missing after merge")
	}
	if _, err := os.Stat(filepath.Join(fork, "apps/customer-app/src/custom.tsx")); err != nil {
		t.Error("fork customization lost by merge")
	}
}

// Contract: conflicts stop the merge with per-file guidance, classified
// into platform (checkout --theirs suggestion) and fork-owned (manual).
func TestUpgradeApply_ConflictsClassified(t *testing.T) {
	upstream, fork := newForkPair(t)

	// Both sides edit the same lines of a platform file AND a fork file.
	write(t, fork, "services/runtime/server.go", "package main // fork patch\n")
	write(t, fork, "apps/customer-app/src/page.tsx", "export default 2 // fork\n")
	commitAll(t, fork, "fork edits")
	write(t, upstream, "services/runtime/server.go", "package main // upstream v2\n")
	write(t, upstream, "apps/customer-app/src/page.tsx", "export default 3 // upstream\n")
	commitAll(t, upstream, "upstream edits")

	out, errOut, err := run(t, fork, "--yes", "--skip-backup", "--remote", "origin")
	if err == nil {
		t.Fatalf("expected conflict error\n%s", out)
	}
	if !strings.Contains(errOut, "git checkout --theirs services/runtime/server.go") {
		t.Errorf("platform conflict guidance missing:\n%s", errOut)
	}
	if !strings.Contains(errOut, "apps/customer-app/src/page.tsx") ||
		!strings.Contains(errOut, "merge by hand") {
		t.Errorf("fork conflict guidance missing:\n%s", errOut)
	}
	// The merge is left in progress for resolution.
	if _, err := os.Stat(filepath.Join(fork, ".git", "MERGE_HEAD")); err != nil {
		t.Error("expected merge-in-progress state (MERGE_HEAD)")
	}
}

// Contract: an up-to-date fork is a no-op that says so.
func TestUpgrade_AlreadyUpToDate(t *testing.T) {
	_, fork := newForkPair(t)
	out, _, err := run(t, fork, "--check", "--remote", "origin")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(out, "Already up to date") {
		t.Errorf("missing up-to-date message:\n%s", out)
	}
}

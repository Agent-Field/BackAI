package initcmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func minimalFork(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, "package.json", "{}")
	write(t, root, "apps/dashboard/.keep", "")
	write(t, root, "apps/customer-app/.keep", "")
	write(t, root, "brand.yaml", "schema_version: 1\nname: af-stack\ncodename: af-stack\ndisplay_name: AF Stack\n")
	return root
}

func stubPnpm(t *testing.T, present bool) {
	t.Helper()
	old := pnpmOnPath
	pnpmOnPath = func() bool { return present }
	t.Cleanup(func() { pnpmOnPath = old })
}

// Contract: on a fresh clone (no node_modules yet) `init --name` must not
// run the brand generator at all — running it prints a Node stack trace as
// the first thing the user sees — and must say in one line what to run.
func TestRunSkipsBrandGeneratorWithoutNodeDeps(t *testing.T) {
	root := minimalFork(t)
	defer chdir(t, root)()
	stubPnpm(t, true)
	defer stubGenerator(t, func(string) error {
		t.Fatal("generator must not run when node_modules is missing")
		return nil
	})()

	var out, errOut bytes.Buffer
	if err := Run([]string{"--name", "Acme AI"}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("init: %v", err)
	}
	if got := errOut.String(); !strings.Contains(got, "Node deps are not installed yet") || !strings.Contains(got, "pnpm install && pnpm run generate:brand") {
		t.Errorf("stderr should carry a one-line hint, got:\n%s", got)
	}
	if got := errOut.String(); strings.Contains(got, "generate brand:") || strings.Count(got, "\n") > 1 {
		t.Errorf("stderr should be a single line, got:\n%s", got)
	}
	if !strings.Contains(out.String(), "brand CSS/modules NOT regenerated") {
		t.Errorf("stdout should say the assets were not regenerated, got:\n%s", out.String())
	}
}

// Contract: with deps present, a genuine generator failure is still
// reported — trimmed to its tail, which is where the actual error is.
func TestRunReportsTrimmedGeneratorFailure(t *testing.T) {
	root := minimalFork(t)
	write(t, root, "node_modules/.keep", "")
	defer chdir(t, root)()
	stubPnpm(t, true)
	defer stubGenerator(t, func(string) error {
		return errors.New("init: generate brand: exit status 1\n" + lastLines(strings.Repeat("noise\n", 30)+"Error: real cause\n", 8))
	})()

	var out, errOut bytes.Buffer
	if err := Run([]string{"--name", "Acme AI"}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("init: %v", err)
	}
	got := errOut.String()
	if !strings.Contains(got, "Error: real cause") {
		t.Errorf("stderr should carry the generator's actual error, got:\n%s", got)
	}
	if strings.Count(got, "noise") > 8 {
		t.Errorf("stderr should be trimmed to the tail, got %d noise lines", strings.Count(got, "noise"))
	}
}

func TestLastLinesKeepsTailOnly(t *testing.T) {
	in := "a\nb\nc\nd\ne\n"
	if got := lastLines(in, 2); got != "…\nd\ne" {
		t.Fatalf("lastLines = %q", got)
	}
	if got := lastLines(in, 10); got != "a\nb\nc\nd\ne" {
		t.Fatalf("lastLines (no trim) = %q", got)
	}
}

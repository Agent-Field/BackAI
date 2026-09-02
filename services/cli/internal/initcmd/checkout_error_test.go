package initcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/cli/internal/checkout"
)

func chdirTemp(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Fatal(err)
		}
	})
}

// Contract: the flag form of init, run outside a clone, fails with a
// message that says what a checkout is, how to clone one, and that the
// positional form scaffolds a standalone app instead.
func TestRunOutsideCheckoutExplainsHowToGetOne(t *testing.T) {
	chdirTemp(t, t.TempDir())
	var out, errOut bytes.Buffer
	err := Run([]string{"--name", "Acme AI", "--color", "#2563EB"}, strings.NewReader(""), &out, &errOut)
	if err == nil {
		t.Fatal("expected an error outside a checkout")
	}
	msg := err.Error()
	for _, want := range []string{"init: must run from inside a BackAI checkout", "git clone " + checkout.RepoURL, "af-stack init <name>"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q:\n%s", want, msg)
		}
	}
}

// Contract: run inside an app that `af-stack init <name>` scaffolded (the
// README sequence a user actually follows), the error names that app and
// says it is standalone, not a fork.
func TestRunInsideScaffoldedAppSaysSo(t *testing.T) {
	parent := t.TempDir()
	chdirTemp(t, parent)
	var out, errOut bytes.Buffer
	if err := Run([]string{"my-ai-product"}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	app := filepath.Join(parent, "my-ai-product")
	chdirTemp(t, app)

	err := Run([]string{"--name", "Acme AI"}, strings.NewReader(""), &out, &errOut)
	if err == nil {
		t.Fatal("expected an error inside a scaffolded app")
	}
	msg := err.Error()
	if !strings.Contains(msg, "is a standalone app created by `af-stack init <name>`") {
		t.Errorf("error should identify the scaffolded app:\n%s", msg)
	}
	if !strings.Contains(msg, "git clone "+checkout.RepoURL) {
		t.Errorf("error should give the clone command:\n%s", msg)
	}
}

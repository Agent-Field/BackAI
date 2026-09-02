package project

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/cli/internal/checkout"
)

// Contract: the fork scaffolds and dev, run outside a clone, fail with a
// message that says what a checkout is, how to clone one, and that
// `af-stack init <name>` scaffolds a standalone app instead.
func TestForkCommandsOutsideCheckoutExplain(t *testing.T) {
	restore := chdir(t, t.TempDir())
	defer restore()

	cases := map[string]func() error{
		"agent new": func() error {
			var out, errOut bytes.Buffer
			return RunAgent([]string{"new", "researcher"}, &out, &errOut)
		},
		"module new": func() error {
			var out, errOut bytes.Buffer
			return RunModule([]string{"new", "billing"}, &out, &errOut)
		},
		"plugin new": func() error {
			var out, errOut bytes.Buffer
			return RunPlugin([]string{"new", "billing"}, &out, &errOut)
		},
		"dev": func() error {
			var out, errOut bytes.Buffer
			return RunDev(context.Background(), []string{"--no-preflight"}, &out, &errOut)
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			err := run()
			if err == nil {
				t.Fatal("expected an error outside a checkout")
			}
			msg := err.Error()
			for _, want := range []string{"must run from inside a BackAI checkout", "git clone " + checkout.RepoURL, "af-stack init <name>"} {
				if !strings.Contains(msg, want) {
					t.Errorf("error missing %q:\n%s", want, msg)
				}
			}
		})
	}
}

// Contract: inside a clone — including a subdirectory of it — the
// scaffolds still resolve the root and behave as before.
func TestForkCommandsFromCheckoutSubdir(t *testing.T) {
	root := fakeRepo(t)
	write(t, root, "apps/backend/.keep", "")
	restore := chdir(t, root+"/apps/backend")
	defer restore()

	var out, errOut bytes.Buffer
	if err := RunAgent([]string{"new", "researcher"}, &out, &errOut); err != nil {
		t.Fatalf("agent new from a subdir: %v", err)
	}
	if got := read(t, root, "apps/backend/agents/researcher/main.py"); !strings.Contains(got, "researcher") {
		t.Fatalf("agent scaffold not written at the checkout root; got:\n%s", got)
	}
}

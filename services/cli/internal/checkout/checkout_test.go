package checkout

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func fakeCheckout(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, "package.json", "{}")
	write(t, root, "apps/dashboard/.keep", "")
	write(t, root, "apps/customer-app/.keep", "")
	return root
}

func fakeScaffoldedApp(t *testing.T, env string) string {
	t.Helper()
	app := filepath.Join(t.TempDir(), "my-ai-product")
	write(t, app, "package.json", `{"name":"my-ai-product"}`)
	write(t, app, "CLAUDE.md", "# my-ai-product — an app on the AF Stack backend")
	write(t, app, ".env.example", env)
	write(t, app, "src/index.mjs", "")
	return app
}

// Contract: inside a clone, or any subdirectory of it, the root is found
// and commands behave exactly as before.
func TestFindFromInsideCheckoutSubdir(t *testing.T) {
	root := fakeCheckout(t)
	deep := filepath.Join(root, "apps", "backend", "agents")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindFrom(deep)
	if err != nil {
		t.Fatalf("FindFrom(subdir) error: %v", err)
	}
	if got != root {
		t.Fatalf("FindFrom(subdir) = %q, want %q", got, root)
	}
}

// Contract: outside a checkout the error names the directory, says what a
// checkout is, gives the clone command, and offers the standalone scaffold.
func TestFindFromOutsideCheckoutExplains(t *testing.T) {
	dir := t.TempDir()
	_, err := FindFrom(dir)
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("want *NotFoundError, got %T: %v", err, err)
	}
	if nf.ScaffoldedApp != "" {
		t.Fatalf("empty dir should not look scaffolded, got %q", nf.ScaffoldedApp)
	}
	msg := err.Error()
	for _, want := range []string{
		dir,
		"apps/dashboard and apps/customer-app",
		"git clone " + RepoURL,
		"af-stack init <name>",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "standalone app created by") {
		t.Errorf("empty dir must not be described as a scaffolded app:\n%s", msg)
	}
}

// Contract: inside an app written by `af-stack init <name>` the error says
// this is a standalone app, not a fork — for both scaffold templates.
func TestFindFromInsideScaffoldedAppSaysSo(t *testing.T) {
	for name, env := range map[string]string{
		"node": "AF_STACK_URL=http://localhost:8080\n",
		"saas": "VITE_AF_STACK_URL=http://localhost:8080\n",
	} {
		t.Run(name, func(t *testing.T) {
			app := fakeScaffoldedApp(t, env)
			_, err := FindFrom(filepath.Join(app, "src"))
			var nf *NotFoundError
			if !errors.As(err, &nf) {
				t.Fatalf("want *NotFoundError, got %T: %v", err, err)
			}
			if nf.ScaffoldedApp != app {
				t.Fatalf("ScaffoldedApp = %q, want %q", nf.ScaffoldedApp, app)
			}
			msg := err.Error()
			if !strings.Contains(msg, app+" is a standalone app created by `af-stack init <name>`") {
				t.Errorf("error message should identify the scaffolded app:\n%s", msg)
			}
			if !strings.Contains(msg, "git clone "+RepoURL) {
				t.Errorf("error message should still give the clone command:\n%s", msg)
			}
		})
	}
}

// Contract: a checkout that happens to carry the scaffold markers too is
// still a checkout.
func TestCheckoutWinsOverScaffoldMarkers(t *testing.T) {
	root := fakeCheckout(t)
	write(t, root, "CLAUDE.md", "@AGENTS.md")
	write(t, root, ".env.example", "AF_STACK_URL=http://localhost:8080\n")
	got, err := FindFrom(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != root {
		t.Fatalf("FindFrom = %q, want %q", got, root)
	}
}

// package.json alone (any Node project) is not a scaffolded app.
func TestPlainNodeProjectIsNotScaffolded(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", "{}")
	_, err := FindFrom(dir)
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("want *NotFoundError, got %T", err)
	}
	if nf.ScaffoldedApp != "" {
		t.Fatalf("plain node project reported as scaffolded: %q", nf.ScaffoldedApp)
	}
}

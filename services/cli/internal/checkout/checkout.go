// Package checkout locates the BackAI checkout — a clone of the repository —
// that in-tree commands (`init --name`, `agent|module|plugin new`, `deploy`,
// and `dev` when there is one) operate on, and explains clearly when there
// is none.
//
// The most common way to end up outside a checkout is to scaffold a
// standalone app with `af-stack init <name>`, cd into it, and then run a
// fork command there. That directory has no apps/ tree to brand or add
// agents to; the error says so. (`dev` is the exception: the app carries a
// bundled backend, and project.RunDev runs it when NotFoundError names a
// ScaffoldedApp.)
package checkout

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RepoURL is the upstream the error message tells users to clone.
const RepoURL = "https://github.com/Agent-Field/backai"

// NotFoundError is returned when no checkout encloses the start directory.
type NotFoundError struct {
	// Dir is the directory the search started from.
	Dir string
	// ScaffoldedApp is Dir or the nearest ancestor that looks like an app
	// written by `af-stack init <name>`, or "" when there is none.
	ScaffoldedApp string
}

func (e *NotFoundError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "must run from inside a BackAI checkout — a clone of %s (a directory containing apps/dashboard and apps/customer-app); %s is not one.", RepoURL, e.Dir)
	if e.ScaffoldedApp != "" {
		fmt.Fprintf(&b, "\n  %s is a standalone app created by `af-stack init <name>`: it has a bundled backend (`af-stack dev` works there) but no fork surfaces to brand or extend.", e.ScaffoldedApp)
	}
	fmt.Fprintf(&b, "\n  To brand a fork or add agents, modules, or plugins:  git clone %s my-fork && cd my-fork", RepoURL)
	fmt.Fprintf(&b, "\n  To scaffold a standalone app instead:                af-stack init <name>   (works in any directory)")
	return b.String()
}

// Find walks up from the working directory to the nearest checkout root.
func Find() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return FindFrom(wd)
}

// FindFrom is Find starting at dir instead of the working directory.
func FindFrom(dir string) (string, error) {
	for d := dir; ; {
		if IsRoot(d) {
			return d, nil
		}
		next := filepath.Dir(d)
		if next == d {
			break
		}
		d = next
	}
	return "", &NotFoundError{Dir: dir, ScaffoldedApp: scaffoldedAppAbove(dir)}
}

// IsRoot reports whether dir is the root of a checkout: the workspace
// package.json plus both apps the platform ships.
func IsRoot(dir string) bool {
	return exists(filepath.Join(dir, "package.json")) &&
		exists(filepath.Join(dir, "apps", "dashboard")) &&
		exists(filepath.Join(dir, "apps", "customer-app"))
}

// scaffoldedAppAbove returns dir or the nearest ancestor that looks like an
// app written by `af-stack init <name>`, or "" when there is none.
func scaffoldedAppAbove(dir string) string {
	for d := dir; ; {
		if looksScaffolded(d) {
			return d
		}
		next := filepath.Dir(d)
		if next == d {
			return ""
		}
		d = next
	}
}

// looksScaffolded matches what initcmd's scaffolds write: package.json,
// CLAUDE.md, and an .env.example that points the app at AF_STACK_URL (or
// VITE_AF_STACK_URL for the saas template).
func looksScaffolded(dir string) bool {
	if !exists(filepath.Join(dir, "package.json")) || !exists(filepath.Join(dir, "CLAUDE.md")) {
		return false
	}
	// #nosec G304 -- a marker file under a directory the user is already running in.
	env, err := os.ReadFile(filepath.Join(dir, ".env.example"))
	return err == nil && strings.Contains(string(env), "AF_STACK_URL=")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

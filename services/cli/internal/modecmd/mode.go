// SPDX-License-Identifier: Apache-2.0

// Package modecmd implements `af-stack mode` — the seamless switch between
// BackAI's SaaS mode (multi-tenant, auth + billing on) and personal mode
// (single-user app, auth + billing off).
//
// It is a thin, deterministic wrapper over one env var: AF_STACK_MODE. The
// command reads/writes that var in the project's .env so the change is the
// single source of truth that the runtime and both frontends already honor.
// It never restarts anything itself — flipping the mode is a config change;
// the operator re-runs `af-stack dev` (or `docker compose up -d`) to apply it.
package modecmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	envKey       = "AF_STACK_MODE"
	modeSaaS     = "saas"
	modePersonal = "personal"
)

// modeLineRE matches an active (non-commented) AF_STACK_MODE assignment.
var modeLineRE = regexp.MustCompile(`(?m)^\s*AF_STACK_MODE\s*=.*$`)

// Run dispatches `af-stack mode [personal|saas]`.
//
//	af-stack mode            print the current mode
//	af-stack mode personal   switch to single-user personal mode (auth+billing off)
//	af-stack mode saas       switch back to multi-tenant SaaS mode
func Run(args []string, stdout, stderr io.Writer) error {
	root, err := findRoot()
	if err != nil {
		return err
	}
	envPath := filepath.Join(root, ".env")

	// No argument: report the current mode.
	if len(args) == 0 {
		current, source := currentMode(envPath)
		fmt.Fprintf(stdout, "mode: %s (%s)\n", current, source)
		return nil
	}

	want := strings.ToLower(strings.TrimSpace(args[0]))
	switch want {
	case modeSaaS, modePersonal:
	default:
		fmt.Fprintf(stderr, "unknown mode %q — expected %q or %q\n", args[0], modeSaaS, modePersonal)
		return fmt.Errorf("invalid mode: %s", args[0])
	}

	prev, _ := currentMode(envPath)
	if err := setMode(root, envPath, want); err != nil {
		return err
	}

	if prev == want {
		fmt.Fprintf(stdout, "mode already %s (.env unchanged)\n", want)
	} else {
		fmt.Fprintf(stdout, "mode: %s → %s (wrote %s=%s to .env)\n", prev, want, envKey, want)
	}
	if want == modePersonal {
		fmt.Fprintln(stdout, "  personal mode: no login, no billing — the app runs single-user under the default tenant.")
	} else {
		fmt.Fprintln(stdout, "  saas mode: multi-tenant; auth + billing governed by their module flags.")
	}
	fmt.Fprintln(stdout, "  restart to apply:  af-stack dev   (or: docker compose up -d)")
	return nil
}

// currentMode returns the effective AF_STACK_MODE and where it came from.
// A missing .env or missing key defaults to saas (matching the runtime).
func currentMode(envPath string) (mode, source string) {
	raw, err := os.ReadFile(envPath) // #nosec G304 -- project-local .env path
	if err != nil {
		return modeSaaS, "default"
	}
	if m := modeLineRE.FindString(string(raw)); m != "" {
		_, val, _ := strings.Cut(m, "=")
		val = strings.ToLower(strings.TrimSpace(val))
		if val == modeSaaS || val == modePersonal {
			return val, ".env"
		}
	}
	return modeSaaS, "default"
}

// setMode upserts AF_STACK_MODE=<mode> in .env: it replaces an existing
// active assignment, appends one if absent, or creates .env (seeded from
// .env.example when present) if there is none.
func setMode(root, envPath, mode string) error {
	line := fmt.Sprintf("%s=%s", envKey, mode)

	raw, err := os.ReadFile(envPath) // #nosec G304 -- project-local .env path
	switch {
	case err == nil:
		var out string
		if modeLineRE.Match(raw) {
			out = modeLineRE.ReplaceAllString(string(raw), line)
		} else {
			out = ensureTrailingNewline(string(raw)) +
				"\n# Deployment mode: saas | personal (set via `af-stack mode`).\n" + line + "\n"
		}
		// #nosec G306 -- .env is world-readable project config (no secrets here).
		return os.WriteFile(envPath, []byte(out), 0o644)
	case errors.Is(err, os.ErrNotExist):
		// Seed from .env.example when available so the new .env keeps the
		// full documented variable set, then upsert the mode line.
		content := ""
		if b, rerr := os.ReadFile(filepath.Join(root, ".env.example")); rerr == nil {
			content = string(b)
		}
		if modeLineRE.MatchString(content) {
			content = modeLineRE.ReplaceAllString(content, line)
		} else {
			content = ensureTrailingNewline(content) +
				"\n# Deployment mode: saas | personal (set via `af-stack mode`).\n" + line + "\n"
		}
		// #nosec G306 -- see above.
		return os.WriteFile(envPath, []byte(content), 0o644)
	default:
		return fmt.Errorf("mode: read .env: %w", err)
	}
}

func ensureTrailingNewline(s string) string {
	if s == "" || strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

// findRoot walks up from the working directory to the BackAI checkout root,
// identified by a docker-compose.yml alongside .env.example. Falls back to a
// directory that has either file so the command still works in trimmed forks.
func findRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	var fallback string
	dir := wd
	for {
		hasCompose := fileExists(filepath.Join(dir, "docker-compose.yml"))
		hasEnvish := fileExists(filepath.Join(dir, ".env")) ||
			fileExists(filepath.Join(dir, ".env.example"))
		if hasCompose && hasEnvish {
			return dir, nil
		}
		if fallback == "" && (hasCompose || hasEnvish) {
			fallback = dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
		dir = next
	}
	if fallback != "" {
		return fallback, nil
	}
	// Last resort: operate on the working directory so `af-stack mode`
	// creates a .env there rather than failing outright.
	return wd, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

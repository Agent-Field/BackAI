// SPDX-License-Identifier: Apache-2.0

package diag

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/Agent-Field/backai/services/cli/internal/client"
	"github.com/Agent-Field/backai/services/cli/internal/output"
)

// DoctorReport is the stable --json schema for `af-stack doctor`.
type DoctorReport struct {
	OK         bool    `json:"ok"`
	Checks     []Check `json:"checks"`
	Root       string  `json:"root"`
	RuntimeURL string  `json:"runtime_url"`
}

// RunDoctor executes the environment + runtime diagnostics. It exits 0 when
// no CRITICAL check failed (a down runtime is a warning, not a failure), and
// exit 1 (generic) when a critical prerequisite like docker is missing.
func RunDoctor(ctx context.Context, c *client.Client, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("af-stack doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "emit the diagnostic report as JSON")
	if err := fs.Parse(args); err != nil {
		return output.Usage("doctor: %v", err)
	}

	root := findRoot()
	checks := collectDoctorChecks(ctx, c, root)

	report := DoctorReport{
		OK:         !anyCriticalFail(checks),
		Checks:     checks,
		Root:       root,
		RuntimeURL: c.BaseURL,
	}

	err := output.Result(stdout, *asJSON, report, func(w io.Writer) error {
		fmt.Fprintln(w, "af-stack doctor")
		if root != "" {
			fmt.Fprintf(w, "  checkout: %s\n", root)
		} else {
			fmt.Fprintln(w, "  checkout: (not inside a BackAI checkout — consumer project)")
		}
		for _, ch := range checks {
			fmt.Fprintf(w, "  %-4s %-20s %s\n", glyph(ch.Status), ch.Name, ch.Detail)
		}
		if report.OK {
			fmt.Fprintln(w, "\nOK — no critical problems.")
		} else {
			fmt.Fprintln(w, "\nFAIL — fix the critical checks above before `af-stack dev`.")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !report.OK {
		return output.Fail("doctor: critical checks failed")
	}
	return nil
}

func collectDoctorChecks(ctx context.Context, c *client.Client, root string) []Check {
	var checks []Check

	// docker — the hard prerequisite for the local stack.
	if _, err := lookPath("docker"); err != nil {
		checks = append(checks, critFail("docker", "docker not found on PATH — required to run the stack"))
	} else {
		checks = append(checks, pass("docker", "docker found on PATH"))
	}

	// node — needed for the port preflight + frontends (advisory).
	if _, err := lookPath("node"); err != nil {
		checks = append(checks, warn("node", "node not found — port preflight + frontend builds need it"))
	} else {
		checks = append(checks, pass("node", "node found on PATH"))
	}

	// .env presence (advisory — the runtime has documented defaults).
	if root == "" {
		checks = append(checks, skip("env-file", "not inside a checkout; skipping .env inspection"))
	} else if fileExists(root + "/.env") {
		checks = append(checks, pass("env-file", ".env present"))
	} else {
		checks = append(checks, warn("env-file", "no .env — copy .env.example (defaults apply meanwhile)"))
	}

	// mode sanity.
	mode := strings.ToLower(readEnvValue(root, "AF_STACK_MODE", "saas"))
	if mode == "saas" || mode == "personal" {
		checks = append(checks, pass("mode", "AF_STACK_MODE="+mode))
	} else {
		checks = append(checks, critFail("mode", fmt.Sprintf("AF_STACK_MODE=%q is invalid (want saas|personal)", mode)))
	}

	// port sanity — flag a configured collision (two surfaces on one port).
	checks = append(checks, portCheck(root))

	// runtime reachability + DB/migration state (best-effort, offline-safe).
	rt, up := probeRuntime(ctx, c)
	checks = append(checks, rt)
	if up {
		checks = append(checks, migrationsCheck(ctx, c))
	} else {
		checks = append(checks, skip("db-migrations", "runtime down — cannot inspect migration state"))
	}
	return checks
}

// portCheck reports a configured port collision across the three host-facing
// surfaces. It does not bind sockets (a running stack legitimately holds
// them); it only catches misconfiguration where two surfaces share a port.
func portCheck(root string) Check {
	api := readEnvValue(root, "AF_STACK_PORT", "8080")
	dash := readEnvValue(root, "AF_STACK_DASHBOARD_PORT", "33000")
	app := readEnvValue(root, "AF_STACK_CUSTOMER_APP_PORT", "34000")
	seen := map[string]string{}
	for name, port := range map[string]string{"api": api, "dashboard": dash, "customer-app": app} {
		if other, dup := seen[port]; dup {
			return critFail("ports", fmt.Sprintf("%s and %s both bind port %s", name, other, port))
		}
		seen[port] = name
	}
	return pass("ports", fmt.Sprintf("api=%s dashboard=%s app=%s (no collision)", api, dash, app))
}

// migrationsCheck asks the runtime whether it is healthy enough that its
// migrations applied. /ready returning 2xx (checked by the caller) implies
// the DB is bound and migrated; we surface that as the migration signal
// rather than reaching into the DB (which the CLI never does directly).
func migrationsCheck(ctx context.Context, c *client.Client) Check {
	status, _, err := c.Probe(ctx, "/health")
	if err != nil || status < 200 || status >= 300 {
		return warn("db-migrations", "runtime ready but /health unclear — check runtime logs")
	}
	return pass("db-migrations", "runtime healthy — DB bound and migrations applied")
}

func glyph(s checkStatus) string {
	switch s {
	case statusPass:
		return "ok"
	case statusWarn:
		return "warn"
	case statusFail:
		return "FAIL"
	case statusSkip:
		return "skip"
	default:
		return "skip"
	}
}

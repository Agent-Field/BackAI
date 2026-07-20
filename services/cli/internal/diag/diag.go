// SPDX-License-Identifier: Apache-2.0

// Package diag implements the operational-readiness commands:
//
//	af-stack doctor   environment + runtime health checks
//	af-stack status   a compact "is it up and what's configured" snapshot
//	af-stack test     the shippable-fork gates (manifests, migrations, ...)
//
// Every command degrades gracefully offline: a runtime that is down is a
// reported check result, never a crash, so the commands are safe to run
// during first-time setup before anything is booted. Each has a --json mode
// with a stable schema so an agent can drive setup end to end.
package diag

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/cli/internal/client"
)

// lookPath is indirected so tests can simulate a machine with/without a
// given binary on PATH.
var lookPath = exec.LookPath

// checkStatus is the stable enum for a single check outcome.
type checkStatus string

const (
	statusPass checkStatus = "pass"
	statusWarn checkStatus = "warn"
	statusFail checkStatus = "fail"
	statusSkip checkStatus = "skip"
)

// Check is one diagnostic result. Critical checks gate the command's exit
// code; non-critical failures (e.g. runtime down during setup) are advisory.
type Check struct {
	Name     string      `json:"name"`
	Status   checkStatus `json:"status"`
	Detail   string      `json:"detail"`
	Critical bool        `json:"critical,omitempty"`
}

func pass(name, detail string) Check { return Check{Name: name, Status: statusPass, Detail: detail} }
func warn(name, detail string) Check { return Check{Name: name, Status: statusWarn, Detail: detail} }
func skip(name, detail string) Check { return Check{Name: name, Status: statusSkip, Detail: detail} }
func critFail(name, detail string) Check {
	return Check{Name: name, Status: statusFail, Detail: detail, Critical: true}
}

// anyCriticalFail reports whether any check failed with Critical set — the
// signal doctor/test use to choose a non-zero exit.
func anyCriticalFail(checks []Check) bool {
	for _, c := range checks {
		if c.Status == statusFail && c.Critical {
			return true
		}
	}
	return false
}

// probeRuntime does a short readiness probe and returns a check plus whether
// the runtime answered 2xx. It never returns an error — unreachable is a
// result, not a failure of the command.
func probeRuntime(ctx context.Context, c *client.Client) (Check, bool) {
	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	status, _, err := c.Probe(pctx, "/ready")
	switch {
	case err != nil:
		return warn("runtime-reachable", "runtime not reachable at "+c.BaseURL+" (start it with `af-stack dev`)"), false
	case status >= 200 && status < 300:
		return pass("runtime-reachable", "runtime ready at "+c.BaseURL), true
	case status == 503:
		return warn("runtime-reachable", "runtime is up at "+c.BaseURL+" but not ready (503) — booting or dependency down"), false
	default:
		return warn("runtime-reachable", "runtime at "+c.BaseURL+" returned status "+strconv.Itoa(status)), false
	}
}

// findRoot walks up to a BackAI checkout root (a docker-compose.yml next to
// an .env or .env.example). Returns "" when not inside a checkout — callers
// treat that as "consumer project, not a fork" rather than an error.
func findRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	dir := wd
	for {
		hasCompose := fileExists(filepath.Join(dir, "docker-compose.yml"))
		hasEnvish := fileExists(filepath.Join(dir, ".env")) || fileExists(filepath.Join(dir, ".env.example"))
		if hasCompose && hasEnvish {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			return ""
		}
		dir = next
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// readEnvValue resolves key from the process env, then <root>/.env, then def.
func readEnvValue(root, key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	if root == "" {
		return def
	}
	data, err := os.ReadFile(filepath.Join(root, ".env")) // #nosec G304 -- project-local .env
	if err != nil {
		return def
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == key {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return def
}

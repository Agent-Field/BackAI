// SPDX-License-Identifier: Apache-2.0

package diag

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Agent-Field/backai/services/cli/internal/client"
	"github.com/Agent-Field/backai/services/cli/internal/initcmd"
	"github.com/Agent-Field/backai/services/cli/internal/output"
	"github.com/Agent-Field/backai/services/cli/internal/validate"
)

// Gate is one `af-stack test` gate outcome. Status is pass|fail|skip.
type Gate struct {
	Name     string             `json:"name"`
	Status   string             `json:"status"`
	Detail   string             `json:"detail"`
	Findings []validate.Finding `json:"findings,omitempty"`
}

// TestReport is the stable --json schema for `af-stack test`.
type TestReport struct {
	OK    bool   `json:"ok"`
	Gates []Gate `json:"gates"`
}

// runTSC is indirected for testing. It typechecks a single standalone .ts
// file with a bare tsc + DOM lib (no node_modules needed).
var runTSC = func(ctx context.Context, file string) error {
	cmd := exec.CommandContext(ctx, "tsc",
		"--noEmit", "--strict", "--skipLibCheck",
		"--target", "ES2020", "--module", "ESNext",
		"--moduleResolution", "bundler", "--lib", "ES2020,DOM",
		file)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v\n%s", err, out)
	}
	return nil
}

// RunTest runs the shippable-fork gates. It exits 0 when every gate passes
// or skips, and 1 (generic) when any gate fails. It degrades gracefully
// offline: the sdk-smoke gate skips (does not fail) when the runtime is down.
func RunTest(ctx context.Context, c *client.Client, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("af-stack test", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "emit the gate report as JSON")
	if err := fs.Parse(args); err != nil {
		return output.Usage("test: %v", err)
	}

	root := findRoot()
	gates := []Gate{
		gateModuleManifests(root),
		gateMigrationRLS(root),
		gateScaffoldTypecheck(ctx),
		gateSDKSmoke(ctx, c),
	}
	ok := true
	for _, g := range gates {
		if g.Status == "fail" {
			ok = false
		}
	}
	report := TestReport{OK: ok, Gates: gates}

	err := output.Result(stdout, *asJSON, report, func(w io.Writer) error {
		fmt.Fprintln(w, "af-stack test")
		for _, g := range gates {
			fmt.Fprintf(w, "  %-4s %-20s %s\n", gateGlyph(g.Status), g.Name, g.Detail)
			for _, f := range g.Findings {
				if f.Level == "error" {
					fmt.Fprintf(w, "        - %s\n", f.Message)
				}
			}
		}
		if ok {
			fmt.Fprintln(w, "\nPASS")
		} else {
			fmt.Fprintln(w, "\nFAIL — one or more gates failed.")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !ok {
		return output.Fail("test: one or more gates failed")
	}
	return nil
}

func gateModuleManifests(root string) Gate {
	if root == "" {
		return Gate{Name: "module-manifest", Status: "skip", Detail: "not inside a checkout"}
	}
	dirs := validate.FindModuleDirs(root)
	if len(dirs) == 0 {
		return Gate{Name: "module-manifest", Status: "skip", Detail: "no modules found"}
	}
	g := Gate{Name: "module-manifest", Status: "pass"}
	count := 0
	for _, d := range dirs {
		res := validate.Manifest(d)
		count++
		if !res.OK {
			g.Status = "fail"
			g.Findings = append(g.Findings, res.Findings...)
		}
	}
	if g.Status == "pass" {
		g.Detail = fmt.Sprintf("%d module manifest(s) valid", count)
	} else {
		g.Detail = "invalid module manifest(s)"
	}
	return g
}

func gateMigrationRLS(root string) Gate {
	if root == "" {
		return Gate{Name: "migration-rls", Status: "skip", Detail: "not inside a checkout"}
	}
	dirs := validate.FindModuleDirs(root)
	g := Gate{Name: "migration-rls", Status: "pass"}
	checked := 0
	for _, d := range dirs {
		migDir := filepath.Join(d, "migrations")
		if info, err := os.Stat(migDir); err != nil || !info.IsDir() {
			continue
		}
		checked++
		res := validate.Migrations(migDir)
		if !res.OK {
			g.Status = "fail"
			g.Findings = append(g.Findings, res.Findings...)
		}
	}
	if checked == 0 {
		return Gate{Name: "migration-rls", Status: "skip", Detail: "no module migrations found"}
	}
	if g.Status == "pass" {
		g.Detail = fmt.Sprintf("%d migration set(s) tenant-isolated", checked)
	} else {
		g.Detail = "tenant-owned table(s) missing RLS"
	}
	return g
}

// gateScaffoldTypecheck typechecks the saas template's dependency-free API
// client with a bare tsc. It skips (not fails) when tsc is not installed, so
// the gate is deterministic offline / in CI without a toolchain.
func gateScaffoldTypecheck(ctx context.Context) Gate {
	if _, err := lookPath("tsc"); err != nil {
		return Gate{Name: "scaffold-typecheck", Status: "skip", Detail: "tsc not on PATH (install typescript to run this gate)"}
	}
	files := initcmd.SaaSTemplateFiles("Typecheck Probe", "typecheck-probe")
	src, ok := files[initcmd.SaaSStandaloneClientPath]
	if !ok {
		return Gate{Name: "scaffold-typecheck", Status: "fail", Detail: "template is missing " + initcmd.SaaSStandaloneClientPath}
	}
	tmp, err := os.MkdirTemp("", "af-stack-tsc-*")
	if err != nil {
		return Gate{Name: "scaffold-typecheck", Status: "fail", Detail: "mktemp: " + err.Error()}
	}
	defer os.RemoveAll(tmp)
	file := filepath.Join(tmp, "client.ts")
	if err := os.WriteFile(file, []byte(src), 0o600); err != nil {
		return Gate{Name: "scaffold-typecheck", Status: "fail", Detail: "write: " + err.Error()}
	}
	tctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := runTSC(tctx, file); err != nil {
		return Gate{Name: "scaffold-typecheck", Status: "fail", Detail: "scaffold API client failed tsc", Findings: []validate.Finding{{Level: "error", Message: err.Error()}}}
	}
	return Gate{Name: "scaffold-typecheck", Status: "pass", Detail: "scaffold API client typechecks"}
}

// gateSDKSmoke exercises the SDK surface (a GET against the agents list) when
// the runtime is reachable. Offline, it skips — a down runtime is not a fork
// defect.
func gateSDKSmoke(ctx context.Context, c *client.Client) Gate {
	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	status, _, err := c.Probe(pctx, "/ready")
	if err != nil {
		return Gate{Name: "sdk-smoke", Status: "skip", Detail: "runtime unreachable — start it with `af-stack dev`"}
	}
	if status < 200 || status >= 300 {
		return Gate{Name: "sdk-smoke", Status: "skip", Detail: fmt.Sprintf("runtime not ready (status %d)", status)}
	}
	var out struct {
		Agents []struct {
			NodeID string `json:"node_id"`
		} `json:"agents"`
	}
	if err := c.Do(pctx, "GET", "/agents", nil, &out); err != nil {
		return Gate{Name: "sdk-smoke", Status: "fail", Detail: "GET /api/v1/agents failed", Findings: []validate.Finding{{Level: "error", Message: err.Error()}}}
	}
	return Gate{Name: "sdk-smoke", Status: "pass", Detail: fmt.Sprintf("runtime answered: %d agent(s)", len(out.Agents))}
}

func gateGlyph(status string) string {
	switch status {
	case "pass":
		return "ok"
	case "fail":
		return "FAIL"
	default:
		return "skip"
	}
}

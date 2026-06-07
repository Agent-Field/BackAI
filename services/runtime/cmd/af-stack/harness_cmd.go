// SPDX-License-Identifier: Apache-2.0

// harness_cmd.go — `af-stack harness …` subcommand surface.
//
// Probe-only — `harness list` runs the local detection logic and prints
// a per-provider table; `harness install <provider>` does NOT actually
// install anything. It prints the doc URL + the env vars the operator
// needs to set, mirroring what the dashboard shows. We intentionally
// keep the installer out of the runtime binary: each harness ships its
// own (npm / pip / brew / curl) install flow and trying to wrap them is
// a recipe for drift.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/harnesses"
	"github.com/Agent-Field/backai/services/runtime/internal/harnesses/probes"
)

// runHarnessCmd is the entry point invoked from main() when argv[1] ==
// "harness". args is everything after the "harness" word.
func runHarnessCmd(args []string) {
	if len(args) == 0 {
		printHarnessUsage()
		os.Exit(2)
	}
	switch args[0] {
	case "list", "ls":
		harnessList()
	case "install":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "error: usage: af-stack harness install <provider>")
			printHarnessUsage()
			os.Exit(2)
		}
		harnessInstall(args[1])
	case "-h", "--help", "help":
		printHarnessUsage()
	default:
		fmt.Fprintf(os.Stderr, "error: unknown harness subcommand %q\n\n", args[0])
		printHarnessUsage()
		os.Exit(2)
	}
}

func printHarnessUsage() {
	out := os.Stderr
	fmt.Fprintln(out, "Usage: af-stack harness <command> [args]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Commands:")
	fmt.Fprintln(out, "  list                       Probe and print status for every supported harness")
	fmt.Fprintln(out, "  install <provider>         Print install instructions for the named harness")
	fmt.Fprintln(out, "  help                       Show this help")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Providers:")
	fmt.Fprintln(out, "  claude-code | codex | gemini | opencode")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Note: `install` does NOT run any installer — it prints the doc URL")
	fmt.Fprintln(out, "and required env vars. You install the harness yourself (npm, pip,")
	fmt.Fprintln(out, "brew, …) and then run `af-stack harness list` to verify.")
}

// quietCLILogger drops everything; the harness package only emits
// debug-level logs during normal probes so the CLI stays clean.
func quietCLILogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// harnessList probes every provider and prints a table to stdout. The
// columns mirror what the dashboard's card grid shows: provider, status,
// version, binary path, last error.
func harnessList() {
	svc := harnesses.NewService(quietCLILogger())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	list := svc.ListAll(ctx)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()
	fmt.Fprintln(w, "PROVIDER\tSTATUS\tVERSION\tBINARY\tNOTES")
	fmt.Fprintln(w, "--------\t------\t-------\t------\t-----")
	for _, h := range list {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			h.Provider,
			h.Status,
			derefOr(h.Version, "-"),
			derefOr(h.BinaryPath, "-"),
			cliNotes(h),
		)
	}
}

// cliNotes summarises any actionable hint for the operator. We prefer
// last_error when present; otherwise surface required env vars for
// needs_auth, or the install doc URL for missing harnesses.
func cliNotes(h harnesses.Harness) string {
	if h.LastError != nil && *h.LastError != "" {
		return *h.LastError
	}
	switch h.Status {
	case harnesses.StatusMissing:
		if h.InstallDocURL != nil {
			return "install: " + *h.InstallDocURL
		}
		return "not installed"
	case harnesses.StatusNeedsAuth:
		if len(h.RequiredEnv) > 0 {
			return "set env: " + strings.Join(h.RequiredEnv, ", ")
		}
	}
	return ""
}

// harnessInstall prints install instructions for a single provider.
// This intentionally does NOT shell out to any package manager — the
// operator runs the install themselves and re-probes via `harness list`.
func harnessInstall(providerArg string) {
	p := harnesses.Provider(strings.ToLower(strings.TrimSpace(providerArg)))
	if !p.IsValid() {
		fmt.Fprintf(os.Stderr,
			"error: unknown provider %q; want claude-code|codex|gemini|opencode\n",
			providerArg)
		os.Exit(2)
	}
	pr, ok := probes.For(string(p))
	if !ok {
		// Defensive — IsValid should keep this in sync.
		fmt.Fprintf(os.Stderr,
			"error: no probe registered for provider %q (internal bug)\n", p)
		os.Exit(2)
	}

	fmt.Printf("Installing %s\n", p)
	fmt.Println(strings.Repeat("=", 40+len(string(p))))
	fmt.Println()
	if pr.Description != "" {
		fmt.Printf("%s\n\n", pr.Description)
	}
	fmt.Println("This command does NOT run an installer. Follow the linked docs to")
	fmt.Println("install the harness, then re-run `af-stack harness list` to verify.")
	fmt.Println()
	if pr.InstallDocURL != "" {
		fmt.Printf("Install docs:  %s\n", pr.InstallDocURL)
	}
	if len(pr.Binary) > 0 {
		fmt.Printf("Binary names:  %s\n", strings.Join(pr.Binary, ", "))
	}
	if len(pr.RequiredEnv) > 0 {
		fmt.Printf("Required env:  %s\n", strings.Join(pr.RequiredEnv, ", "))
		fmt.Println()
		fmt.Println("Export each required variable in the environment that runs the")
		fmt.Println("af-stack runtime (docker compose .env, systemd unit, shell rc).")
	} else {
		fmt.Println("Required env:  none (credentials managed inside the CLI)")
	}
}

// derefOr returns *p or fallback when p is nil / empty.
func derefOr(p *string, fallback string) string {
	if p == nil || *p == "" {
		return fallback
	}
	return *p
}
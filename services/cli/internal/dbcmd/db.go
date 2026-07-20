// SPDX-License-Identifier: Apache-2.0

// Package dbcmd implements `af-stack db` — a thin, scriptable wrapper over
// goose for the core runtime migrations and each workload module's
// migrations:
//
//	af-stack db diff                 show migration status (applied vs pending)
//	af-stack db push                 apply pending migrations (goose up)
//	af-stack db generate <name>      scaffold a new timestamped migration
//	af-stack db reset --yes          roll every migration back (DESTRUCTIVE)
//
// The destructive `reset` requires --yes for non-interactive confirmation
// and honours --dry-run (print the goose invocation, run nothing). goose and
// DATABASE_URL are external prerequisites; a clear structured error is
// returned when either is missing rather than a cryptic exec failure.
package dbcmd

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/backai/services/cli/internal/output"
)

// coreMigrationsRel is the in-checkout path to the runtime's migrations.
const coreMigrationsRel = "services/runtime/internal/db/migrations"

// gooseRun is indirected so tests assert the constructed argv without a real
// goose binary. dir is passed as -dir; gooseArgs are the trailing command.
var gooseRun = func(ctx context.Context, dir string, gooseArgs []string, stdout, stderr io.Writer) error {
	full := append([]string{"-dir", dir}, gooseArgs...)
	cmd := exec.CommandContext(ctx, "goose", full...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// lookPath is indirected for testing.
var lookPath = exec.LookPath

// Run dispatches `af-stack db <subcommand>`.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return output.Usage("db: subcommand required: diff | push | generate | reset")
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "diff", "status":
		return runGoose(ctx, "diff", []string{"status"}, rest, stdout, stderr)
	case "push", "up":
		return runGoose(ctx, "push", []string{"up"}, rest, stdout, stderr)
	case "reset":
		return runReset(ctx, rest, stdout, stderr)
	case "generate", "create":
		return runGenerate(ctx, rest, stdout, stderr)
	default:
		return output.Usage("db: unknown subcommand %q (want diff | push | generate | reset)", sub)
	}
}

// resolveDirs returns the migration directories the command targets: the
// core runtime migrations plus, when --all or --module is set, module
// migrations. dirFlag overrides everything.
func resolveDirs(dirFlag, moduleFlag string, all bool) ([]string, error) {
	if dirFlag != "" {
		return []string{dirFlag}, nil
	}
	root, err := findRoot()
	if err != nil {
		return nil, output.Usage("db: %v (or pass --dir <migrations>)", err)
	}
	if moduleFlag != "" {
		md := filepath.Join(root, "workload-modules", moduleFlag, "migrations")
		if _, err := os.Stat(md); err != nil {
			return nil, output.NotFound("db: no migrations for module %q at %s", moduleFlag, filepath.ToSlash(md))
		}
		return []string{md}, nil
	}
	dirs := []string{filepath.Join(root, filepath.FromSlash(coreMigrationsRel))}
	if all {
		dirs = append(dirs, moduleMigrationDirs(root)...)
	}
	return dirs, nil
}

func runGoose(ctx context.Context, name string, gooseArgs, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("af-stack db "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	dirFlag := fs.String("dir", "", "migrations directory (default: core runtime migrations)")
	moduleFlag := fs.String("module", "", "target a workload module's migrations by id")
	all := fs.Bool("all", false, "include every workload module's migrations too")
	dryRun := fs.Bool("dry-run", false, "print the goose invocation without running it")
	if err := fs.Parse(args); err != nil {
		return output.Usage("db %s: %v", name, err)
	}
	dirs, err := resolveDirs(*dirFlag, *moduleFlag, *all)
	if err != nil {
		return err
	}
	// status/up need a DB; guard before shelling out for a clear error.
	dbURL, err := requireDBURL()
	if err != nil {
		return err
	}
	full := append(gooseArgs, "postgres", dbURL)
	if *dryRun {
		for _, dir := range dirs {
			fmt.Fprintf(stdout, "goose -dir %s %s\n", dir, strings.Join(gooseArgs, " "))
		}
		return nil
	}
	if err := ensureGoose(); err != nil {
		return err
	}
	for _, dir := range dirs {
		fmt.Fprintf(stdout, "== %s (%s)\n", name, filepath.ToSlash(dir))
		if err := gooseRun(ctx, dir, full, stdout, stderr); err != nil {
			return output.Remote("db %s: goose failed for %s: %v", name, filepath.ToSlash(dir), err)
		}
	}
	return nil
}

func runReset(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("af-stack db reset", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dirFlag := fs.String("dir", "", "migrations directory (default: core runtime migrations)")
	moduleFlag := fs.String("module", "", "target a workload module's migrations by id")
	all := fs.Bool("all", false, "include every workload module's migrations too")
	dryRun := fs.Bool("dry-run", false, "print the goose invocation without running it")
	yes := fs.Bool("yes", false, "confirm this DESTRUCTIVE reset (required, non-interactive)")
	if err := fs.Parse(args); err != nil {
		return output.Usage("db reset: %v", err)
	}
	dirs, err := resolveDirs(*dirFlag, *moduleFlag, *all)
	if err != nil {
		return err
	}
	dbURL, err := requireDBURL()
	if err != nil {
		return err
	}
	gooseArgs := []string{"reset"}
	if *dryRun {
		for _, dir := range dirs {
			fmt.Fprintf(stdout, "goose -dir %s reset   # DESTRUCTIVE: rolls back every migration\n", dir)
		}
		return nil
	}
	if !*yes {
		return output.Invalid("db reset is DESTRUCTIVE (rolls back every migration and drops its data). Re-run with --yes to confirm, or --dry-run to preview.")
	}
	if err := ensureGoose(); err != nil {
		return err
	}
	full := append(gooseArgs, "postgres", dbURL)
	// Reset in reverse order (modules before core) so dependent objects drop
	// first when --all pulls module dirs in after core.
	for i := len(dirs) - 1; i >= 0; i-- {
		fmt.Fprintf(stdout, "== reset (%s)\n", filepath.ToSlash(dirs[i]))
		if err := gooseRun(ctx, dirs[i], full, stdout, stderr); err != nil {
			return output.Remote("db reset: goose failed for %s: %v", filepath.ToSlash(dirs[i]), err)
		}
	}
	return nil
}

func runGenerate(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("af-stack db generate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dirFlag := fs.String("dir", "", "migrations directory (default: core runtime migrations)")
	moduleFlag := fs.String("module", "", "target a workload module's migrations by id")
	dryRun := fs.Bool("dry-run", false, "print the goose invocation without running it")
	positionals, err := output.ParseArgs(fs, args)
	if err != nil {
		return output.Usage("db generate: %v", err)
	}
	if len(positionals) < 1 {
		return output.Usage("db generate: a migration name is required (af-stack db generate <name>)")
	}
	name := strings.Join(positionals, "_")
	dirs, err := resolveDirs(*dirFlag, *moduleFlag, false)
	if err != nil {
		return err
	}
	dir := dirs[0]
	gooseArgs := []string{"create", name, "sql"}
	if *dryRun {
		fmt.Fprintf(stdout, "goose -dir %s create %s sql\n", dir, name)
		return nil
	}
	if err := ensureGoose(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "creating migration %q in %s\n", name, filepath.ToSlash(dir))
	if err := gooseRun(ctx, dir, gooseArgs, stdout, stderr); err != nil {
		return output.Remote("db generate: goose failed: %v", err)
	}
	fmt.Fprintln(stdout, "remember: guard DO $$ blocks with -- +goose StatementBegin/End,")
	fmt.Fprintln(stdout, "and give tenant-owned tables tenant_id + FORCE row level security.")
	return nil
}

func requireDBURL() (string, error) {
	url := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if url == "" {
		url = strings.TrimSpace(os.Getenv("AF_STACK_DATABASE_URL"))
	}
	if url == "" {
		return "", output.Usage("db: DATABASE_URL (or AF_STACK_DATABASE_URL) is required")
	}
	return url, nil
}

func ensureGoose() error {
	if _, err := lookPath("goose"); err != nil {
		return output.Remote("db: goose is not installed — install it (go install github.com/pressly/goose/v3/cmd/goose@latest) or run migrations via `af-stack dev`")
	}
	return nil
}

func moduleMigrationDirs(root string) []string {
	var out []string
	base := filepath.Join(root, "workload-modules")
	entries, err := os.ReadDir(base)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		md := filepath.Join(base, e.Name(), "migrations")
		if info, err := os.Stat(md); err == nil && info.IsDir() {
			out = append(out, md)
		}
	}
	return out
}

func findRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		if fileExists(filepath.Join(dir, "package.json")) &&
			fileExists(filepath.Join(dir, "apps", "dashboard")) {
			return dir, nil
		}
		next := filepath.Dir(dir)
		if next == dir {
			return "", fmt.Errorf("must run from inside a BackAI checkout")
		}
		dir = next
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

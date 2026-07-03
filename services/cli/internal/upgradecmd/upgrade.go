// SPDX-License-Identifier: Apache-2.0

// Package upgradecmd implements `af-stack upgrade`: pull the latest
// upstream AF Stack into a fork cleanly.
//
// A fork's customizations live in tracked files (the customer app, agents,
// workload modules, dashboard plugins, brand), so an upgrade is a git merge
// — but a bare `git merge` leaves the operator to figure out which
// conflicts are theirs vs. the platform's, whether new DB migrations are
// coming, and how to apply them safely. This command wraps the whole flow:
//
//	af-stack upgrade --check   # dry run: behind/ahead, incoming migrations,
//	                           # predicted conflicts split into fork-owned
//	                           # vs platform files
//	af-stack upgrade           # backup DB (when reachable + migrations
//	                           # incoming), merge upstream, report next steps
//
// Migrations never run here: the runtime applies them via goose at boot,
// so "run migrations" == rebuild/restart the runtime — which the summary
// spells out, after a pg_dump backup has been written.
package upgradecmd

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DefaultUpstreamURL is the canonical AF Stack repository.
const DefaultUpstreamURL = "https://github.com/Agent-Field/backai"

// forkOwnedPrefixes are the surfaces the README's "What You Edit" table
// assigns to the fork. Conflicts here are the operator's product code —
// only they can resolve them. Everything else is platform code, where
// taking the upstream side is almost always right.
var forkOwnedPrefixes = []string{
	"apps/customer-app/",
	"apps/backend/agents/",
	"workload-modules/",
	"apps/dashboard/plugins/",
}

// forkOwnedFiles are single files the rebrand / deploy flow customizes.
var forkOwnedFiles = []string{
	"brand.yaml",
	"docker-compose.yml",
	"docker-compose.prod.yml",
	".env.example",
	"README.md",
}

// classifyPath reports whether a repo-relative path is fork-owned.
func classifyPath(path string) bool {
	for _, p := range forkOwnedPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	for _, f := range forkOwnedFiles {
		if path == f {
			return true
		}
	}
	return false
}

// migrationsDir is where incoming schema changes are detected.
const migrationsDir = "services/runtime/internal/db/migrations"

// Run handles `af-stack upgrade`.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("af-stack upgrade", flag.ContinueOnError)
	fs.SetOutput(stderr)
	check := fs.Bool("check", false, "dry run: report what an upgrade would do, change nothing")
	remote := fs.String("remote", "upstream", "git remote name for the upstream AF Stack repo")
	url := fs.String("url", DefaultUpstreamURL, "upstream repo URL (used when the remote doesn't exist yet)")
	branch := fs.String("branch", "main", "upstream branch to upgrade to")
	yes := fs.Bool("yes", false, "skip the confirmation prompt")
	skipBackup := fs.Bool("skip-backup", false, "don't attempt a DB backup before a migration-carrying upgrade")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("upgrade: unexpected argument %q", fs.Arg(0))
	}

	root, err := gitOut("", "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("upgrade: not inside a git checkout: %w", err)
	}
	root = strings.TrimSpace(root)

	// 1. Refuse a dirty tree — an upgrade must never eat uncommitted work.
	dirty, err := gitOut(root, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(dirty) != "" {
		fmt.Fprintln(stderr, "upgrade: working tree has uncommitted changes:")
		fmt.Fprintln(stderr, indent(strings.TrimSpace(dirty)))
		return fmt.Errorf("upgrade: commit or stash your changes first (git stash), then re-run")
	}

	// 2. Ensure the upstream remote exists and fetch it.
	if _, err := gitOut(root, "remote", "get-url", *remote); err != nil {
		fmt.Fprintf(stdout, "Adding remote %q -> %s\n", *remote, *url)
		if _, err := gitOut(root, "remote", "add", *remote, *url); err != nil {
			return fmt.Errorf("upgrade: add remote: %w", err)
		}
	}
	fmt.Fprintf(stdout, "Fetching %s/%s…\n", *remote, *branch)
	if _, err := gitOut(root, "fetch", "--quiet", *remote, *branch); err != nil {
		return fmt.Errorf("upgrade: fetch %s/%s: %w", *remote, *branch, err)
	}
	target := *remote + "/" + *branch

	// 3. Position report.
	behind := gitCount(root, "rev-list", "--count", "HEAD.."+target)
	ahead := gitCount(root, "rev-list", "--count", target+"..HEAD")
	if behind == 0 {
		fmt.Fprintf(stdout, "Already up to date with %s (your fork carries %d commit(s) of its own).\n", target, ahead)
		return nil
	}
	fmt.Fprintf(stdout, "\nUpstream has %d new commit(s); your fork carries %d of its own.\n", behind, ahead)

	// 4. Incoming migrations.
	migrations := incomingMigrations(root, target)
	if len(migrations) > 0 {
		fmt.Fprintf(stdout, "\nIncoming DB migrations (%d) — applied automatically by the runtime at next boot:\n", len(migrations))
		for _, m := range migrations {
			fmt.Fprintf(stdout, "  + %s\n", m)
		}
	} else {
		fmt.Fprintln(stdout, "\nNo new DB migrations.")
	}

	// 5. Predicted conflicts, classified.
	forkConflicts, platformConflicts, err := predictConflicts(root, target)
	if err != nil {
		return err
	}
	if len(forkConflicts)+len(platformConflicts) == 0 {
		fmt.Fprintln(stdout, "No merge conflicts predicted.")
	} else {
		if len(platformConflicts) > 0 {
			fmt.Fprintf(stdout, "\nPredicted conflicts in PLATFORM files (taking upstream is almost always right):\n")
			for _, p := range platformConflicts {
				fmt.Fprintf(stdout, "  ! %s\n", p)
			}
		}
		if len(forkConflicts) > 0 {
			fmt.Fprintf(stdout, "\nPredicted conflicts in YOUR files (your product code — resolve by hand):\n")
			for _, p := range forkConflicts {
				fmt.Fprintf(stdout, "  ! %s\n", p)
			}
		}
	}

	if *check {
		fmt.Fprintln(stdout, "\n--check: no changes made. Run `af-stack upgrade` to apply.")
		return nil
	}

	// 6. Confirm.
	if !*yes {
		fmt.Fprintf(stdout, "\nMerge %s into %s? [y/N] ", target, currentBranch(root))
		line, _ := bufio.NewReader(stdin).ReadString('\n')
		if l := strings.ToLower(strings.TrimSpace(line)); l != "y" && l != "yes" {
			fmt.Fprintln(stdout, "Aborted — nothing changed.")
			return nil
		}
	}

	// 7. Backup before a migration-carrying upgrade, when a DB is in reach.
	backupPath := ""
	if len(migrations) > 0 && !*skipBackup {
		backupPath = tryBackup(root, stdout, stderr)
	}

	// 8. Merge.
	fmt.Fprintf(stdout, "\nMerging %s…\n", target)
	mergeOut, mergeErr := gitOut(root, "merge", "--no-edit", target)
	if mergeErr != nil {
		fmt.Fprintln(stdout, indent(strings.TrimSpace(mergeOut)))
		fmt.Fprintln(stderr, "\nThe merge stopped on conflicts. Resolve them, then `git add` + `git commit`:")
		if len(platformConflicts) > 0 {
			fmt.Fprintln(stderr, "\n  Platform files — take upstream unless you deliberately patched them:")
			for _, p := range platformConflicts {
				fmt.Fprintf(stderr, "    git checkout --theirs %s && git add %s\n", p, p)
			}
		}
		if len(forkConflicts) > 0 {
			fmt.Fprintln(stderr, "\n  Your files — merge by hand (upstream changed the same lines as your customization):")
			for _, p := range forkConflicts {
				fmt.Fprintf(stderr, "    %s\n", p)
			}
		}
		fmt.Fprintln(stderr, "\n  Or back out entirely: git merge --abort")
		return fmt.Errorf("upgrade: merge has conflicts (see above)")
	}

	// 9. Success summary + next steps.
	fmt.Fprintf(stdout, "\nUpgraded to %s.\n", target)
	if backupPath != "" {
		fmt.Fprintf(stdout, "DB backup: %s\n", backupPath)
	}
	fmt.Fprintln(stdout, "\nNext steps:")
	fmt.Fprintln(stdout, "  docker compose up -d --build   # rebuild; the runtime applies migrations at boot")
	if len(migrations) > 0 {
		fmt.Fprintln(stdout, "  # if boot fails on a migration: docker compose logs runtime, restore with scripts/restore.sh")
	}
	fmt.Fprintln(stdout, "  scripts/acceptance.sh          # optional: end-to-end validation")
	return nil
}

// ─── helpers ──────────────────────────────────────────────────────────────

func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	if err != nil {
		return buf.String(), fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(buf.String()))
	}
	return buf.String(), nil
}

func gitCount(root string, args ...string) int {
	out, err := gitOut(root, args...)
	if err != nil {
		return 0
	}
	n := 0
	_, _ = fmt.Sscanf(strings.TrimSpace(out), "%d", &n)
	return n
}

func currentBranch(root string) string {
	out, err := gitOut(root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "HEAD"
	}
	return strings.TrimSpace(out)
}

// incomingMigrations lists migration files added between HEAD and target.
func incomingMigrations(root, target string) []string {
	out, err := gitOut(root, "diff", "--name-only", "--diff-filter=A", "HEAD..."+target, "--", migrationsDir)
	if err != nil {
		return nil
	}
	var files []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l = strings.TrimSpace(l); l != "" && strings.HasSuffix(l, ".sql") {
			files = append(files, filepath.Base(l))
		}
	}
	sort.Strings(files)
	return files
}

// predictConflicts runs a virtual merge (git merge-tree, no working-tree
// changes) and returns the conflicted paths split fork-owned / platform.
func predictConflicts(root, target string) (fork, platform []string, err error) {
	// merge-tree exits 1 on conflicts, >1 on real errors.
	cmd := exec.Command("git", "merge-tree", "--write-tree", "--name-only", "HEAD", target)
	cmd.Dir = root
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	runErr := cmd.Run()
	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
			return nil, nil, fmt.Errorf("upgrade: merge-tree: %s", strings.TrimSpace(buf.String()))
		}
	} else {
		return nil, nil, nil // clean virtual merge
	}
	// Output: <tree-oid>\n<conflicted file>...\n\n<informational messages>
	lines := strings.Split(buf.String(), "\n")
	seen := map[string]bool{}
	for i, l := range lines {
		if i == 0 { // tree oid
			continue
		}
		l = strings.TrimSpace(l)
		if l == "" { // blank separates file list from messages
			break
		}
		if seen[l] {
			continue
		}
		seen[l] = true
		if classifyPath(l) {
			fork = append(fork, l)
		} else {
			platform = append(platform, l)
		}
	}
	sort.Strings(fork)
	sort.Strings(platform)
	return fork, platform, nil
}

// tryBackup runs scripts/backup.sh when it exists and a database URL is
// derivable. Best-effort by design: a fork without a running stack still
// upgrades; the summary just won't list a backup.
func tryBackup(root string, stdout, stderr io.Writer) string {
	script := filepath.Join(root, "scripts", "backup.sh")
	if _, err := os.Stat(script); err != nil {
		fmt.Fprintln(stdout, "No scripts/backup.sh — skipping DB backup.")
		return ""
	}
	dbURL := strings.TrimSpace(os.Getenv("AF_STACK_DATABASE_URL"))
	if dbURL == "" {
		// The compose default: postgres remapped onto the host.
		port := strings.TrimSpace(os.Getenv("POSTGRES_PORT"))
		if port == "" {
			port = "5432"
		}
		dbURL = "postgres://afstack:afstack@localhost:" + port + "/afstack?sslmode=disable"
	}
	out := filepath.Join(root, fmt.Sprintf("backup-pre-upgrade-%s.sql.gz",
		time.Now().UTC().Format("20060102-150405")))
	fmt.Fprintf(stdout, "Backing up the database before migrations (%s)…\n", filepath.Base(out))
	cmd := exec.Command("bash", script, "--url", dbURL, "--out", out)
	cmd.Dir = root
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(stderr, "Backup failed (%v) — continuing; re-run with --skip-backup to silence, or back up manually via scripts/backup.sh.\n", err)
		return ""
	}
	return out
}

func indent(s string) string {
	return "  " + strings.ReplaceAll(s, "\n", "\n  ")
}

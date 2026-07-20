// SPDX-License-Identifier: Apache-2.0

package dbcmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/cli/internal/output"
)

// captureGoose swaps gooseRun for a recorder and makes goose "present".
func captureGoose(t *testing.T) *[][]string {
	t.Helper()
	oldRun, oldLook := gooseRun, lookPath
	t.Cleanup(func() { gooseRun, lookPath = oldRun, oldLook })
	var calls [][]string
	gooseRun = func(_ context.Context, dir string, gooseArgs []string, _, _ io.Writer) error {
		calls = append(calls, append([]string{dir}, gooseArgs...))
		return nil
	}
	lookPath = func(string) (string, error) { return "/usr/bin/goose", nil }
	return &calls
}

// Contract: `db reset` is destructive and refuses to run without --yes, with
// the validation exit code.
func TestDBResetRequiresYes(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	calls := captureGoose(t)
	err := Run(context.Background(), []string{"reset", "--dir", "/tmp/m"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("reset without --yes must error")
	}
	if code := output.ExitCode(err); code != output.ExitValidation {
		t.Fatalf("reset-no-yes exit = %d, want %d", code, output.ExitValidation)
	}
	if len(*calls) != 0 {
		t.Fatalf("goose must not run without --yes, ran: %v", *calls)
	}
}

// Contract: --dry-run prints the invocation and runs nothing.
func TestDBResetDryRun(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	calls := captureGoose(t)
	var stdout bytes.Buffer
	if err := Run(context.Background(), []string{"reset", "--dir", "/tmp/m", "--dry-run"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("dry-run should succeed: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("dry-run must not run goose, ran: %v", *calls)
	}
	if !strings.Contains(stdout.String(), "reset") || !strings.Contains(stdout.String(), "DESTRUCTIVE") {
		t.Fatalf("dry-run should preview the destructive reset:\n%s", stdout.String())
	}
}

// Contract: reset --yes invokes goose reset with the resolved dir + dbstring.
func TestDBResetYesRunsGoose(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://db")
	calls := captureGoose(t)
	if err := Run(context.Background(), []string{"reset", "--dir", "/tmp/m", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("reset --yes error: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected 1 goose call, got %v", *calls)
	}
	got := strings.Join((*calls)[0], " ")
	if got != "/tmp/m reset postgres postgres://db" {
		t.Fatalf("goose argv = %q", got)
	}
}

// Contract: push maps to goose up; generate maps to goose create <name> sql.
func TestDBPushAndGenerate(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://db")
	calls := captureGoose(t)
	if err := Run(context.Background(), []string{"push", "--dir", "/m"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("push error: %v", err)
	}
	if err := Run(context.Background(), []string{"generate", "add_notes", "--dir", "/m"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("generate error: %v", err)
	}
	if len(*calls) != 2 {
		t.Fatalf("expected 2 goose calls, got %v", *calls)
	}
	if got := strings.Join((*calls)[0], " "); got != "/m up postgres postgres://db" {
		t.Fatalf("push argv = %q", got)
	}
	if got := strings.Join((*calls)[1], " "); got != "/m create add_notes sql" {
		t.Fatalf("generate argv = %q", got)
	}
}

// Contract: generate without a name is a usage error (exit 2).
func TestDBGenerateRequiresName(t *testing.T) {
	captureGoose(t)
	err := Run(context.Background(), []string{"generate", "--dir", "/m"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code := output.ExitCode(err); code != output.ExitUsage {
		t.Fatalf("generate-no-name exit = %d, want %d (err=%v)", code, output.ExitUsage, err)
	}
}

// Contract: an unknown subcommand and a missing DATABASE_URL are exit-coded.
func TestDBUsageAndDBURL(t *testing.T) {
	if code := output.ExitCode(Run(context.Background(), []string{"bogus"}, &bytes.Buffer{}, &bytes.Buffer{})); code != output.ExitUsage {
		t.Fatalf("unknown-subcommand exit = %d, want %d", code, output.ExitUsage)
	}
	// push needs a DB url; unset both.
	os.Unsetenv("DATABASE_URL")
	os.Unsetenv("AF_STACK_DATABASE_URL")
	captureGoose(t)
	err := Run(context.Background(), []string{"push", "--dir", "/m"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code := output.ExitCode(err); code != output.ExitUsage {
		t.Fatalf("no-DATABASE_URL exit = %d, want %d (err=%v)", code, output.ExitUsage, err)
	}
}

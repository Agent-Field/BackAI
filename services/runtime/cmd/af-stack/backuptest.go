// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/appmetrics"
	"github.com/Agent-Field/backai/services/runtime/internal/crons"
)

// Default daily schedule for the backup/restore verification (04:30).
const defaultBackupTestSchedule = "30 4 * * *"

// lastBackupTestSuccessUnix holds the timestamp of the most recent successful
// backup-test run (0 = never), mirrored into the metric. Exposed as a package
// var so an admin/debug surface could read it without Prometheus.
var lastBackupTestSuccessUnix atomic.Int64

// registerBackupTestCron registers the backup/restore verification as a platform
// system cron, using the existing SystemScheduler (same mechanism as
// retention.daily). It is a no-op unless BACKUP_TEST_ENABLED is truthy, so the
// heavy dump/restore never runs unless an operator opts in.
//
// The handler shells out to scripts/backup-restore-test.sh; on exit 0 it records
// the success timestamp (metric backai_backup_test_last_success_timestamp). For
// very large databases prefer running that script as an external scheduled job
// (e.g. a Kubernetes CronJob) instead — the in-process cron runs the handler
// inline on the scheduler goroutine.
func registerBackupTestCron(sched *crons.SystemScheduler, log *slog.Logger) {
	if !envBool("BACKUP_TEST_ENABLED", false) {
		return
	}
	schedule := defaultBackupTestSchedule
	if v := strings.TrimSpace(os.Getenv("BACKUP_TEST_SCHEDULE")); v != "" {
		schedule = v
	}
	if err := sched.RegisterSystem("backup.test", schedule, func(ctx context.Context) error {
		return runBackupRestoreTest(ctx, log)
	}); err != nil {
		log.Warn("backup-test cron registration failed", "error", err)
		return
	}
	log.Info("backup-test cron registered", "schedule", schedule)
}

// runBackupRestoreTest executes the verification script and, on success, records
// the timestamp. The command inherits the process env (AF_STACK_*_DATABASE_URL,
// BACKUP_TEST_SCRATCH_URL, etc.).
func runBackupRestoreTest(ctx context.Context, log *slog.Logger) error {
	script, err := resolveBackupTestScript()
	if err != nil {
		return err
	}
	timeout := 10 * time.Minute
	if v := strings.TrimSpace(os.Getenv("BACKUP_TEST_TIMEOUT_SECONDS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeout = time.Duration(n) * time.Second
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "bash", script) // #nosec G204,G702 -- allowlisted backup-restore-test.sh only
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Error("backup-test failed", "script", script, "error", err, "output", tailOutput(out, 2000))
		return err
	}
	now := time.Now().Unix()
	lastBackupTestSuccessUnix.Store(now)
	appmetrics.SetBackupTestLastSuccess(now)
	log.Info("backup-test succeeded", "script", script)
	return nil
}

// resolveBackupTestScript returns the in-checkout verification script.
// BACKUP_TEST_SCRIPT may override the path, but the resolved file must
// stay under the process working directory and be named
// backup-restore-test.sh so an env var cannot point at /tmp/evil/....
func resolveBackupTestScript() (string, error) {
	script := strings.TrimSpace(os.Getenv("BACKUP_TEST_SCRIPT"))
	if script == "" {
		script = filepath.Join("scripts", "backup-restore-test.sh")
	}
	abs, err := filepath.Abs(script)
	if err != nil {
		return "", fmt.Errorf("backup-test: resolve script: %w", err)
	}
	abs = filepath.Clean(abs)
	if filepath.Base(abs) != "backup-restore-test.sh" {
		return "", fmt.Errorf("backup-test: refused unexpected script %q", script)
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("backup-test: working directory: %w", err)
	}
	rel, err := filepath.Rel(filepath.Clean(wd), abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("backup-test: refused unexpected script %q", script)
	}
	return abs, nil
}

// tailOutput returns the last limit bytes of command output for a bounded log.
func tailOutput(b []byte, limit int) string {
	if len(b) <= limit {
		return string(b)
	}
	return "…" + string(b[len(b)-limit:])
}

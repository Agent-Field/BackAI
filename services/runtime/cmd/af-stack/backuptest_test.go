// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveBackupTestScript(t *testing.T) {
	wd := t.TempDir()
	t.Chdir(wd)
	if err := os.MkdirAll("scripts", 0o750); err != nil {
		t.Fatal(err)
	}
	allowed := filepath.Join(wd, "scripts", "backup-restore-test.sh")
	if err := os.WriteFile(allowed, []byte("#!/bin/bash\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	t.Setenv("BACKUP_TEST_SCRIPT", "")
	got, err := resolveBackupTestScript()
	if err != nil {
		t.Fatalf("default: %v", err)
	}
	if got != filepath.Clean(allowed) {
		t.Errorf("default = %q, want %q", got, allowed)
	}

	t.Setenv("BACKUP_TEST_SCRIPT", "scripts/backup-restore-test.sh")
	if _, err := resolveBackupTestScript(); err != nil {
		t.Fatalf("relative override: %v", err)
	}

	evil := filepath.Join(t.TempDir(), "backup-restore-test.sh")
	if err := os.WriteFile(evil, []byte("x"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BACKUP_TEST_SCRIPT", evil)
	if _, err := resolveBackupTestScript(); err == nil {
		t.Fatal("expected reject of script outside the working tree")
	}
}
